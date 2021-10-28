package project

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScriptRun contains the details of a script which has been run for the project
type ScriptRun struct {
	ID          uint `json:"-"`
	GUID        string
	Script      string
	Title       string
	Development bool
	ScriptGroup string

	TextOutput string
	HtmlOutput string
	Error      string

	ObjectType        string `gorm:"-"`
	PercentCompleted  int    `gorm:"-"`
	RequestsMadeCount int    `gorm:"-"`
	TotalRequestCount int
	DoNotRecord       bool `gorm:"-"`
	DoNotBroadcast    bool `gorm:"-" json:"-"`
	Status            string
}

// ScriptOutputUpdate contains the partial output of a script
type ScriptOutputUpdate struct {
	GUID       string
	ObjectType string
	TextOutput string
	HTMLOutput string
}

// ScriptProgressUpdate contains the details of script progress
type ScriptProgressUpdate struct {
	GUID       string
	Count      int
	Total      int
	ObjectType string
}

type runningScriptDetails struct {
	TextOutput string
	HTMLOutput string
	Count      int
	Total      int
}

var runningScripts map[string]*runningScriptDetails = make(map[string]*runningScriptDetails)

func ScriptRunFromGUID(guid string) *ScriptRun {
	var operation ScriptRun
	tx := readableDatabase.Where("guid = ?", guid).First(&operation)
	if tx.Error != nil {
		return nil
	}

	return &operation
}

func CancelScript(guid string) {
	var script ScriptRun
	result := readableDatabase.First(&script, "guid = ?", guid)

	if result.Error != nil {
		fmt.Printf("Error retrieving script to cancel from the database: %s\n", result.Error.Error())
		return
	}

	script.Error = "Cancelled"
	script.Status = "Completed"

	if _, ok := runningScripts[guid]; ok {
		script.TotalRequestCount = runningScripts[guid].Count
	} else {
		fmt.Printf("Script progress updated attempted for a script which is not running: %s\n", guid)
	}

	script.Record()
}

// Record sends the script output update details to the user interface
func (scriptOutputUpdate *ScriptOutputUpdate) Record() {
	scriptOutputUpdate.ObjectType = "Script Output Update"
	ioHub.broadcast <- scriptOutputUpdate

	guid := scriptOutputUpdate.GUID
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].TextOutput += scriptOutputUpdate.TextOutput
		runningScripts[guid].HTMLOutput += scriptOutputUpdate.HTMLOutput
	} else {
		fmt.Printf("Script output updated attempted for a script which is not running\n")
	}
}

// Record sends the script update details to the user interface
func (scriptProgressUpdate *ScriptProgressUpdate) Record() {
	scriptProgressUpdate.ObjectType = "Script Progress Update"
	ioHub.broadcast <- scriptProgressUpdate

	guid := scriptProgressUpdate.GUID
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].Count = scriptProgressUpdate.Count
		runningScripts[guid].Total = scriptProgressUpdate.Total
	} else {
		fmt.Printf("Script progress updated attempted for a script which is not running: %s\n", guid)
	}
}

// Record sends the script run to the user interface and/or records it in the database
func (scriptRun *ScriptRun) Record() {
	scriptRun.ObjectType = "Script Run"
	if scriptRun.GUID == "" {
		scriptRun.GUID = uuid.NewString()
	}

	if !scriptRun.DoNotBroadcast {
		// it's running but we don't want running broadcasts (used for HTML updates)
		if scriptRun.Status == "Running" {
			runningScripts[scriptRun.GUID] = &runningScriptDetails{}
		} else {
			delete(runningScripts, scriptRun.GUID)
		}

		ioHub.broadcast <- scriptRun
	}

	if !scriptRun.DoNotRecord {
		ioHub.databaseWriter <- scriptRun
	}
}

func (scriptRun *ScriptRun) RecordOrUpdate() {
	var script ScriptRun
	result := readableDatabase.First(&script, "guid = ?", scriptRun.GUID)

	if result.Error == nil {
		scriptRun.ID = script.ID
		scriptRun.HtmlOutput = script.HtmlOutput
	}

	if runningScript, ok := runningScripts[scriptRun.GUID]; ok {
		scriptRun.TotalRequestCount = runningScript.Total
	}

	scriptRun.Record()
}

func (scriptRun *ScriptRun) UpdateFromRunningScript() {
	if runningScript, ok := runningScripts[scriptRun.GUID]; ok {
		scriptRun.RequestsMadeCount = runningScript.Count
		scriptRun.TotalRequestCount = runningScript.Total
		scriptRun.TextOutput = runningScript.TextOutput
		scriptRun.HtmlOutput = runningScript.HTMLOutput
	} else if scriptRun.Status == "Running" {
		scriptRun.Status = "Cancelled"
	} else if scriptRun.Status == "Completed" || scriptRun.Status == "Archived" || scriptRun.Status == "Unarchived" {
		scriptRun.RequestsMadeCount = scriptRun.TotalRequestCount
	}
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
