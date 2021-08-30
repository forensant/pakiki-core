package project

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScriptRun contains the details of a script which has been run for the project
type ScriptRun struct {
	ID     uint `json:"-"`
	GUID   string
	Script string
	Title  string

	Output string
	Error  string

	ObjectType        string `gorm:"-"`
	PercentCompleted  int    `gorm:"-"`
	RequestsMadeCount int    `gorm:"-"`
	TotalRequestCount int
	DoNotRecord       bool `gorm:"-"`
}

// ScriptOutputUpdate contains the partial output of a script
type ScriptOutputUpdate struct {
	GUID       string
	ObjectType string
	Output     string
}

// ScriptProgressUpdate contains the details of script progress
type ScriptProgressUpdate struct {
	GUID       string
	Count      int
	Total      int
	ObjectType string
}

func ScriptRunFromGUID(guid string) *ScriptRun {
	var operation ScriptRun
	tx := readableDatabase.Where("guid = ?", guid).First(&operation)
	if tx.Error != nil {
		return nil
	}

	return &operation
}

// Record sends the script output update details to the user interface
func (scriptOutputUpdate *ScriptOutputUpdate) Record() {
	scriptOutputUpdate.ObjectType = "Script Output Update"
	ioHub.broadcast <- scriptOutputUpdate
}

// Record sends the script update details to the user interface
func (scriptProgressUpdate *ScriptProgressUpdate) Record() {
	scriptProgressUpdate.ObjectType = "Script Progress Update"
	ioHub.broadcast <- scriptProgressUpdate
}

// Record sends the script run to the user interface and/or records it in the database
func (scriptRun *ScriptRun) Record() {
	scriptRun.ObjectType = "Script Run"
	if scriptRun.GUID == "" {
		scriptRun.GUID = uuid.NewString()
	}

	if !scriptRun.DoNotRecord {
		ioHub.databaseWriter <- scriptRun
	}

	ioHub.broadcast <- scriptRun
}

// RecordError updates the error field and transmits notification of the error to the GUI
func (scriptRun *ScriptRun) RecordError(err string) {
	fmt.Println(err)
	scriptRun.TotalRequestCount = 0
	scriptRun.Error = err
	scriptRun.UpdateAndRecord()
}

func (scriptRun *ScriptRun) UpdateAndRecord() {
	scriptRun.updatePercentCompleted()
	scriptRun.Record()
}

func (scriptRun *ScriptRun) updatePercentCompleted() {
	var requestCount int64
	tx := readableDatabase.Model(&Request{}).Where("scan_id = ?", scriptRun.GUID).Count(&requestCount)
	if tx.Error != nil {
		return
	}

	scriptRun.RequestsMadeCount = int(requestCount)

	if requestCount >= int64(scriptRun.TotalRequestCount) {
		scriptRun.PercentCompleted = 100
	} else {
		scriptRun.PercentCompleted = int((float32(requestCount) / float32(scriptRun.TotalRequestCount)) * 100.0)
	}
}

func (scriptOutputUpdate *ScriptOutputUpdate) ShouldFilter(str string) bool {
	return false
}

func (scriptProgressUpdate *ScriptProgressUpdate) ShouldFilter(str string) bool {
	return false
}

func (scriptRun *ScriptRun) ShouldFilter(str string) bool {
	return false
}

func (scriptOutputUpdate *ScriptOutputUpdate) WriteToDatabase(db *gorm.DB) {
	// do nothing
}

func (scriptProgressUpdate *ScriptProgressUpdate) WriteToDatabase(db *gorm.DB) {
	// do nothing
}

func (scriptRun *ScriptRun) WriteToDatabase(db *gorm.DB) {
	db.Save(scriptRun)
}
