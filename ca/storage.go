package ca

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"io"
	"path/filepath"

	"github.com/kirsle/configdir"
	_ "github.com/mattn/go-sqlite3"
	"github.com/zalando/go-keyring"
)

const keyringService = "Proximity"
const caKeyChainKeyName = "Certificate Private Key"

var caDatabaseFilename string = "certs.db"

// CertificateRecord contains the data required to present a valid certificate to
// browsers, and encrypt intercepted traffic
type CertificateRecord struct {
	Domain         string
	CertificatePEM string
	PrivateKey     []byte
}

func decryptPEMData(encryptedData string) (plaintext []byte, err error) {
	unencodedData, err := hex.DecodeString(encryptedData)
	if err != nil {
		return nil, err
	}

	aesKey, err := getSymmetricEncryptionKey()
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := aesGCM.NonceSize()
	nonce, ciphertext := unencodedData[:nonceSize], unencodedData[nonceSize:]

	return aesGCM.Open(nil, nonce, ciphertext, nil)
}

func encryptPEMData(plaintext string) (base64CipherText string, err error) {
	aesKey, err := getSymmetricEncryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	cipherText := aesGCM.Seal(nil, nonce, []byte(plaintext), nil)

	packedData := nonce
	packedData = append(packedData, cipherText...)

	return hex.EncodeToString(packedData), nil
}

func getDatabaseFilename() (string, error) {
	configPath := configdir.LocalConfig("Forensant", "Proximity")
	err := configdir.MakePath(configPath) // Ensure it exists.
	if err != nil {
		return "", err
	}

	databaseFile := filepath.Join(configPath, caDatabaseFilename)

	return databaseFile, nil
}

func getDatabase() (*sql.DB, error) {
	databaseFile, err := getDatabaseFilename()
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", databaseFile)
	if err != nil {
		return nil, err
	}

	tx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, err
	}

	sqlStmt := `create table if not exists certificates (id integer not null primary key, domain text, certificate text, private_key text);`
	_, err = tx.Exec(sqlStmt)

	if err != nil {
		db.Close()
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func saveCertificateToDatabase(domain string, certificate string, privateKey string) error {
	encryptedKey, err := encryptPEMData(privateKey)
	if err != nil {
		return err
	}

	db, err := getDatabase()
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	insertStatement := "INSERT INTO certificates(domain, certificate, private_key) VALUES(?, ?, ?)"
	_, err = tx.Exec(insertStatement, domain, certificate, encryptedKey)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func getSymmetricEncryptionKey() ([]byte, error) {
	symmetricKey, err := keyring.Get(keyringService, caKeyChainKeyName)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(symmetricKey)
}

func storeSymmetricEncryptionKey(key []byte) error {
	hexStorageKey := hex.EncodeToString(key) //encode key in bytes to string for saving
	err := keyring.Set(keyringService, caKeyChainKeyName, hexStorageKey)
	return err
}
