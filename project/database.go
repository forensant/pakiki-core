package project

import (
	"gorm.io/gorm"
)

func initDatabase(db *gorm.DB) {
	db.AutoMigrate(&InjectOperation{})
	db.AutoMigrate(&Request{})
	db.AutoMigrate(&DataPacket{})
	db.AutoMigrate(&ScriptRun{})
}
