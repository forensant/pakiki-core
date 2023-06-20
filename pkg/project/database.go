package project

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"gorm.io/gorm"
)

func compressDatabase(src *gorm.DB, dstPath string) error {
	tempdbpath, err := tempFilename()
	if err != nil {
		return err
	}
	defer os.Remove(tempdbpath)

	tempcompressedPath, err := tempFilename()
	if err != nil {
		fmt.Printf("Failed getting compressed path\n")
		return err
	}

	fmt.Printf("Step 1 of 2: Creating a copy of the project database.\n")
	err = copyDB(src, tempdbpath)
	if err != nil {
		fmt.Printf("Failed copying the database\n")
		os.Remove(tempdbpath)
		return err
	}

	fmt.Printf("Step 2 of 2: Compressing the project.\n")
	err = compressFile(tempdbpath, tempcompressedPath)
	if err != nil {
		fmt.Printf("Failed compressing file\n")
		os.Remove(tempdbpath)
		return err
	}

	// move once the compression has completed, to minimise the chance of data corruption
	// rather than compressing in place (which would minimise disk usage)
	err = os.Rename(tempcompressedPath, dstPath)
	if err != nil {
		fmt.Printf("Failed moving file: %s, trying manual move instead\n", err.Error())
		err = moveFile(tempcompressedPath, dstPath)
		if err != nil {
			return err
		}
	}
	fmt.Printf("Project successfully copied to: %s\n", dstPath)

	return nil
}

func compressFile(src string, dst string) error {
	of, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return err
	}
	defer of.Close()

	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	zw := gzip.NewWriter(of)
	defer zw.Close()
	zw.Name = "pakiki.db"
	zw.Comment = "PAKIKI"

	_, err = io.Copy(zw, sf)
	if err != nil {
		return err
	}

	return nil
}

func copyDB(src *gorm.DB, dst string) error {
	return src.Exec("VACUUM INTO ?", dst).Error
}

func decompressDatabase(src string, path string) (string, error) {
	var tempdbpath = path
	var err error

	if tempdbpath == "" {
		tempdbpath, err = tempFilename()
	}

	if err != nil {
		return "", err
	}

	sf, err := os.Open(src)
	if os.IsNotExist(err) {
		return tempdbpath, nil
	}

	if err != nil {
		return "", err
	}

	stat, _ := sf.Stat()
	if stat.Size() == 0 {
		return tempdbpath, nil
	}

	zr, err := gzip.NewReader(sf)
	if err != nil {
		return "", err
	}

	tempdbfile, err := os.Create(tempdbpath)
	if err != nil {
		return "", err
	}

	_, err = io.Copy(tempdbfile, zr)
	if err != nil {
		return "", err
	}

	err = tempdbfile.Close()
	if err != nil {
		return "", err
	}

	return tempdbpath, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func initDatabase(db *gorm.DB) {
	migrateTables(db)
	injectOperationCache = make(map[string]*InjectOperation)
	loadSitemap(db)
}

func moveFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}

	_, err = io.Copy(destination, source)
	if err != nil {
		return err
	}

	if err = source.Close(); err != nil {
		return err
	}

	if err = destination.Close(); err != nil {
		return err
	}

	if err = os.Remove(src); err != nil {
		return err
	}

	return err
}

func migrateTables(db *gorm.DB) {
	db.AutoMigrate(&Hook{})
	db.AutoMigrate(&HookErrorLog{})
	db.AutoMigrate(&InjectOperationRequestPart{})
	db.AutoMigrate(&InjectOperation{})
	db.AutoMigrate(&Request{})
	db.AutoMigrate(&DataPacket{})
	db.AutoMigrate(&ScriptRun{})
	db.AutoMigrate(&ScriptGroup{})
	db.AutoMigrate(&ScopeEntry{})
	db.AutoMigrate(&SiteMapPath{})
	db.AutoMigrate(&Setting{})
}

func tempFilename() (string, error) {
	tempdbfile, err := ioutil.TempFile(os.TempDir(), "pakiki-tmp-*.db")
	tempdbfile.Close()

	if err != nil {
		return "", err
	}

	return tempdbfile.Name(), nil
}
