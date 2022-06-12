// ca implements a certificate authority for generating certificates to be used for interception.
// goproxy handles all of its own certificates, so much of this functionality is not used, but
// will probably be required as other functionality is added.

// TODO: Ensure the CA is regenerated within 30 years

package ca

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"time"
)

func clearExistingCADatabase() error {
	db, err := getDatabase()
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec("DELETE FROM certificates")
	if err != nil {
		return err
	}

	return tx.Commit()
}

func generate256BitEncryptionKey() ([]byte, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func generateRootPEMs() (caCertificate string, caKey string, err error) {
	serialNumber, err := generateSerialNumber()
	if err != nil {
		return "", "", err
	}

	ca := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization:  []string{"Forensant Proximity Root CA"},
			Country:       []string{"NZ"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
			CommonName:    "Forensant Proximity Root CA",
		},
		DNSNames:              []string{"proximity.forensant.com"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(30, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caPrivKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return "", "", err
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return "", "", err
	}

	caPEM := new(bytes.Buffer)
	pem.Encode(caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})

	marshalledECKey, err := x509.MarshalECPrivateKey(caPrivKey)
	if err != nil {
		return "", "", err
	}

	caPrivKeyPEM := new(bytes.Buffer)
	pem.Encode(caPrivKeyPEM, &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: marshalledECKey,
	})

	return caPEM.String(), caPrivKeyPEM.String(), nil
}

func generateRootCertificate() (*CertificateRecord, error) {
	caPubCert, caPrivKeyPEM, err := generateRootPEMs()
	if err != nil {
		return nil, err
	}

	err = clearExistingCADatabase()
	if err != nil {
		return nil, err
	}

	// in this case, we'll regenerate the encryption key for the CA
	storageKey, err := generate256BitEncryptionKey()
	if err != nil {
		return nil, err
	}

	err = storeSymmetricEncryptionKey(storageKey)
	if err != nil {
		return nil, err
	}

	err = saveCertificateToDatabase("CA Root", caPubCert, caPrivKeyPEM)
	if err != nil {
		return nil, err
	}

	record := &CertificateRecord{
		Domain:         "CA Root",
		CertificatePEM: caPubCert,
		PrivateKey:     []byte(caPrivKeyPEM),
	}

	return record, err
}

func getRootPrivateKey() ([]byte, error) {
	certificateRecord, err := CertificateForDomain("CA Root")
	if err != nil {
		return nil, err
	}
	return certificateRecord.PrivateKey, nil
}

// RootCertificate returns a PEM encoded string containing the root certificate
// used to sign all other certificates
func RootCertificate() (string, error) {
	certificateRecord, err := CertificateForDomain("CA Root")
	if err != nil {
		return "", err
	}
	return certificateRecord.CertificatePEM, nil
}
