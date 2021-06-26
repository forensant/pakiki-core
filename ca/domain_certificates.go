package ca

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"time"
)

// CertificateForDomain either retrieves or generates a certificate for the
// provided domain, which is signed by the root CA certificate
func CertificateForDomain(domain string) (*CertificateRecord, error) {
	db, err := getDatabase()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT certificate, private_key FROM certificates WHERE domain = ?;", domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		// there's no certificate, generate one
		if domain == "CA Root" {
			return generateRootCertificate()
		} else {
			return generateCertificateForDomain(domain)
		}
	}

	var certificate, privateKey string
	err = rows.Scan(&certificate, &privateKey)
	if err != nil {
		return nil, err
	}

	record := &CertificateRecord{
		Domain:         domain,
		CertificatePEM: certificate,
	}

	plaintext, err := decryptPEMData(privateKey)
	if err != nil {
		return nil, err
	}
	record.PrivateKey = plaintext

	return record, nil
}

func generateCertificateForDomain(domain string) (*CertificateRecord, error) {
	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, err
	}

	cert := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization:  []string{"Forensant"},
			Country:       []string{"NZ"},
			Province:      []string{""},
			Locality:      []string{"Wellington"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		DNSNames:    []string{domain},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(10, 0, 0),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	certPrivKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}

	caCertPEM, err := RootCertificate()
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode([]byte(caCertPEM))
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	caKey, err := getRootPrivateKey()
	if err != nil {
		return nil, err
	}

	caPrivKeyBlock, _ := pem.Decode(caKey)
	if caPrivKeyBlock == nil || caPrivKeyBlock.Type != "EC PRIVATE KEY" {
		return nil, errors.New("private key was not in the private key block")
	}

	caPrivKey, err := x509.ParseECPrivateKey(caPrivKeyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, caCert, &certPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, err
	}

	certPEM := new(bytes.Buffer)
	pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	marshalledECKey, err := x509.MarshalECPrivateKey(certPrivKey)
	if err != nil {
		return nil, err
	}

	certPrivKeyPEM := new(bytes.Buffer)
	pem.Encode(certPrivKeyPEM, &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: marshalledECKey,
	})

	err = saveCertificateToDatabase(domain, certPEM.String(), certPrivKeyPEM.String())
	if err != nil {
		return nil, err
	}

	return &CertificateRecord{
		Domain:         domain,
		CertificatePEM: certPEM.String(),
		PrivateKey:     certPrivKeyPEM.Bytes(),
	}, nil
}

func generateSerialNumber() (*big.Int, error) {
	var serialNumberMax, e = big.NewInt(2), big.NewInt(159)
	serialNumberMax = serialNumberMax.Exp(serialNumberMax, e, nil)
	return rand.Int(rand.Reader, serialNumberMax)
}
