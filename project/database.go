package project

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func compressDatabase(src *gorm.DB, dstPath string) error {
	tempdbpath, err := tempFilename()
	if err != nil {
		return err
	}
	fmt.Printf("Temporary compressed path: %s\n", tempdbpath)
	defer os.Remove(tempdbpath)

	tempcompressedPath, err := tempFilename()
	if err != nil {
		fmt.Printf("Failed getting compressed path\n")
		return err
	}

	err = copyDB(src, tempdbpath)
	if err != nil {
		fmt.Printf("Failed copying the database\n")
		return err
	}

	err = compressFile(tempdbpath, tempcompressedPath)
	if err != nil {
		fmt.Printf("Failed compressing file\n")
		return err
	}

	// move once the compression has completed, to minimise the chance of data corruption
	// rather than compressing in place (which would minimise disk usage)
	os.Rename(tempcompressedPath, dstPath)

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
	zw.Name = "proximity.db"
	zw.Comment = "PROXIMITY"

	_, err = io.Copy(zw, sf)
	if err != nil {
		return err
	}

	return nil
}

func copyDB(src *gorm.DB, dst string) error {
	db, err := gorm.Open(sqlite.Open(dst))
	if err != nil {
		return err
	}
	migrateTables(db)
	udb, err := db.DB()
	if err != nil {
		return err
	}
	if err = udb.Close(); err != nil {
		return err
	}

	res := src.Exec("ATTACH DATABASE ? AS dst;", dst)
	if res.Error != nil {
		return res.Error
	}

	var tbls []string
	res = src.Raw("SELECT tbl_name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tbls)
	if res.Error != nil {
		return res.Error
	}

	for _, tbl := range tbls {
		cmd := "INSERT INTO dst." + tbl + " SELECT * FROM " + tbl
		res = src.Exec(cmd)
		if res.Error != nil {
			return res.Error
		}
	}

	res = src.Exec("DETACH DATABASE dst;")
	if res.Error != nil {
		return res.Error
	}

	return nil
}

func decompressDatabase(src string) (string, error) {
	tempdbpath, err := tempFilename()
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

func initDatabase(db *gorm.DB) {
	migrateTables(db)
	injectOperationCache = make(map[string]*InjectOperation)
	loadSitemap(db)
}

func migrateTables(db *gorm.DB) {
	db.AutoMigrate(&InjectOperationRequestPart{})
	db.AutoMigrate(&InjectOperation{})
	db.AutoMigrate(&Request{})
	db.AutoMigrate(&DataPacket{})
	db.AutoMigrate(&ScriptRun{})
	db.AutoMigrate(&ScriptGroup{})
	db.AutoMigrate(&SiteMapPath{})
	db.AutoMigrate(&Setting{})
}

func tempFilename() (string, error) {
	tempdbfile, err := ioutil.TempFile(os.TempDir(), "proximity-tmp-*.db")
	tempdbfile.Close()

	if err != nil {
		return "", err
	}

	return tempdbfile.Name(), nil
}
