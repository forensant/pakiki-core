package project

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScriptGroup contains a collection of scripts
type ScriptGroup struct {
	ID         uint `json:"-"`
	GUID       string
	Title      string
	Status     string
	ObjectType string `gorm:"-"`
}

func endScriptGroupIfRequired(guid string) {
	var scriptGroup ScriptGroup
	result := readableDatabase.First(&scriptGroup, "guid = ?", guid)

	if result.Error != nil {
		return
	}

	scriptGroup.ensureRunning()
}

func (scriptGroup *ScriptGroup) ensureRunning() {
	if scriptGroup.Status == "Running" && !scriptGroupRunning(scriptGroup.GUID) {
		scriptGroup.Status = "Completed"
		scriptGroup.Record()
	}
}

func (scriptGroup *ScriptGroup) Record() {
	if scriptGroup.GUID == "" {
		scriptGroup.GUID = uuid.NewString()
	}

	if scriptGroup.Status == "" {
		scriptGroup.Status = "Running"
	}

	ioHub.databaseWriter <- scriptGroup

	scriptGroup.ObjectType = "Script Group"
	ioHub.broadcast <- scriptGroup
}

func (scriptGroup *ScriptGroup) ShouldFilter(str string) bool {
	return false
}

func (scriptGroup *ScriptGroup) WriteToDatabase(db *gorm.DB) {
	db.Save(scriptGroup)
}
