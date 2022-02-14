package ca

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	_ "dev.forensant.com/pipeline/razor/proximitycore/internal/testing_init"
)

func TestMain(m *testing.M) {
	caDatabaseFilename = "certs_test.db"
	filePath, _ := getDatabaseFilename()

	os.Remove(filePath)

	os.Exit(m.Run())
}

func TestGetRootCertificate(t *testing.T) {
	rootCert, err := RootCertificate()
	if err != nil {
		t.Errorf("err = %s; want nil", err.Error())
		return
	}

	block, _ := pem.Decode([]byte(rootCert))
	certificate, err := x509.ParseCertificate(block.Bytes)

	if err != nil {
		t.Errorf("err = %s; want nil", err.Error())
		return
	}

	dnsNameCount := len(certificate.DNSNames)
	if dnsNameCount != 1 {
		t.Errorf("len(certificate.DNSNames) = %d; want 1", dnsNameCount)
		return
	}

	firstDNSName := certificate.DNSNames[0]
	if firstDNSName != "proximity.forensant.com" {
		t.Errorf("certificate.DNSNames[0] = %s; want proximity.forensant.com", firstDNSName)
		return
	}
}

func TestSignCertificate(t *testing.T) {
	pemRootCert, err := RootCertificate()
	if err != nil {
		t.Errorf("err = %s; want nil", err.Error())
		return
	}

	block, _ := pem.Decode([]byte(pemRootCert))
	rootCertificate, err := x509.ParseCertificate(block.Bytes)

	if err != nil {
		t.Errorf("err = %s; want nil", err.Error())
		return
	}

	domainCertificateRecord, err := CertificateForDomain("test.forensant.com")
	if err != nil {
		t.Errorf("err = %s; want nil", err.Error())
		return
	}

	block, _ = pem.Decode([]byte(domainCertificateRecord.CertificatePEM))
	domainCertificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Errorf("err = %s; want nil", err.Error())
		return
	}

	firstDNSName := domainCertificate.DNSNames[0]
	if firstDNSName != "test.forensant.com" {
		t.Errorf("domainCertificate.DNSNames[0] = %s; want test.forensant.com", firstDNSName)
		return
	}

	err = domainCertificate.CheckSignatureFrom(rootCertificate)
	if err != nil {
		t.Errorf("err = %s; want nil", err.Error())
		return
	}
}
