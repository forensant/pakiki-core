package proxy

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/zalando/go-keyring"
)

const keyringService = "Proximity"
const caKeyChainKeyName = "Certificate Private Key"
const rootCAPKKeyChainKeyName = "Root CA Private Key"
const rootCAPubCertKeyChainKeyName = "Root CA Cert"

// CertificateRecord contains the data required to present a valid certificate to
// browsers, and encrypt intercepted traffic
type CertificateRecord struct {
	CertificatePEM []byte
	PrivateKey     []byte
}

func generateSerialNumber() (*big.Int, error) {
	var serialNumberMax, e = big.NewInt(2), big.NewInt(159)
	serialNumberMax = serialNumberMax.Exp(serialNumberMax, e, nil)
	return rand.Int(rand.Reader, serialNumberMax)
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

func generateRootCertificateIfRequired() error {
	_, rootPKerr := keyring.Get(keyringService, rootCAPKKeyChainKeyName)
	_, rootCerterr := keyring.Get(keyringService, rootCAPubCertKeyChainKeyName)

	if rootPKerr == nil && rootCerterr == nil {
		return nil
	}

	caPubCert, caPrivKeyPEM, err := generateRootPEMs()
	if err != nil {
		return err
	}

	err = keyring.Set(keyringService, rootCAPKKeyChainKeyName, caPrivKeyPEM)
	if err != nil {
		return err
	}

	err = keyring.Set(keyringService, rootCAPubCertKeyChainKeyName, caPubCert)
	if err != nil {
		return err
	}

	return nil
}

func getRootPrivateKey() ([]byte, error) {
	err := generateRootCertificateIfRequired()
	if err != nil {
		return nil, err
	}

	key, err := keyring.Get(keyringService, rootCAPKKeyChainKeyName)
	return []byte(key), nil
}

func getRootCertificate() (*CertificateRecord, error) {
	err := generateRootCertificateIfRequired()
	if err != nil {
		return nil, err
	}

	pk, err := keyring.Get(keyringService, rootCAPKKeyChainKeyName)
	if err != nil {
		return nil, err
	}

	ca, err := keyring.Get(keyringService, rootCAPubCertKeyChainKeyName)
	if err != nil {
		return nil, err
	}

	return &CertificateRecord{
		PrivateKey:     []byte(pk),
		CertificatePEM: []byte(ca),
	}, nil
}
