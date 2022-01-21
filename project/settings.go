package project

import (
	"gorm.io/gorm"
)

// Setting represents a single setting for the project
type Setting struct {
	ID    uint `json:"-"`
	Name  string
	Value string
}

func GetSetting(name string) string {
	var setting Setting
	tx := readableDatabase.Where("name = ?", name).Limit(1).Find(&setting)
	if tx.Error != nil || tx.RowsAffected != 1 {
		return ""
	}

	return setting.Value
}

func SetSetting(name string, value string) {
	setting := &Setting{
		Name:  name,
		Value: value,
	}
	setting.Record()
}

func (setting *Setting) Record() {
	ioHub.databaseWriter <- setting
}

func (setting *Setting) WriteToDatabase(db *gorm.DB) {
	db.Save(setting)
}
