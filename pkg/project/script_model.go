package project

import (
	"fmt"
	"strings"
	"sync"

	"github.com/forensant/pakiki-core/internal/request_queue"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ScriptRun contains the details of a script which has been run for the project
type ScriptRun struct {
	ID          uint `json:"-"`
	GUID        string
	Script      string `json:"-"`
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
	GUID         string
	Count        int
	Total        int
	ObjectType   string
	ShouldUpdate bool `json:"-"`
}

type runningScriptDetails struct {
	TextOutput string
	HTMLOutput string
	Count      int
	Total      int
}

var runningScriptsMutex sync.Mutex
var runningScripts map[string]*runningScriptDetails = make(map[string]*runningScriptDetails)

func EscapeForPython(input string) string {
	output := strings.ReplaceAll(input, "\\", "\\\\")
	output = strings.ReplaceAll(output, "\n", "\\n")
	output = strings.ReplaceAll(output, "'", "\\'")
	output = strings.ReplaceAll(output, "\x0A", "")
	output = strings.ReplaceAll(output, "\x0D", "")
	return output
}

func ScriptIncrementTotalRequests(guid string) {
	runningScriptsMutex.Lock()
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].Total += 1
		sendScriptProgressUpdate(guid, runningScripts[guid])
	}
	runningScriptsMutex.Unlock()
}

func ScriptIncrementTotalRequestsBy(guid string, amount int) {
	runningScriptsMutex.Lock()
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].Total += amount
		sendScriptProgressUpdate(guid, runningScripts[guid])
	}
	runningScriptsMutex.Unlock()
}

func ScriptDecrementTotalRequests(guid string) {
	runningScriptsMutex.Lock()
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].Total -= 1
		sendScriptProgressUpdate(guid, runningScripts[guid])
	}
	runningScriptsMutex.Unlock()
}

func ScriptIncrementRequestCount(guid string) {
	runningScriptsMutex.Lock()
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].Count += 1
		sendScriptProgressUpdate(guid, runningScripts[guid])
	}
	runningScriptsMutex.Unlock()
}

func ScriptDecrementRequestCount(guid string) {
	runningScriptsMutex.Lock()
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].Count -= 1
		sendScriptProgressUpdate(guid, runningScripts[guid])
	}
	runningScriptsMutex.Unlock()
}

func ScriptRunFromGUID(guid string) *ScriptRun {
	var operation ScriptRun
	tx := readableDatabase.Where("guid = ?", guid).Limit(1).Find(&operation)
	if tx.Error != nil || tx.RowsAffected == 0 {
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

	runningScriptsMutex.Lock()
	if _, ok := runningScripts[guid]; ok {
		script.TotalRequestCount = runningScripts[guid].Count
	} else {
		fmt.Printf("Script cancel attempted for a script which is not running: %s\n", guid)
	}
	runningScriptsMutex.Unlock()

	script.Record()
}

func (sr *ScriptRun) GetGUID() string {
	return sr.GUID
}

// Record sends the script output update details to the user interface
func (scriptOutputUpdate *ScriptOutputUpdate) Record() {
	scriptOutputUpdate.ObjectType = "Script Output Update"
	ioHub.broadcast <- scriptOutputUpdate

	guid := scriptOutputUpdate.GUID
	runningScriptsMutex.Lock()
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].TextOutput += scriptOutputUpdate.TextOutput
		runningScripts[guid].HTMLOutput += scriptOutputUpdate.HTMLOutput
	} else {
		fmt.Printf("Script output updated attempted for a script which is not running\n")
	}
	runningScriptsMutex.Unlock()
}

// Record sends the script update details to the user interface
func (scriptProgressUpdate *ScriptProgressUpdate) Record() {
	scriptProgressUpdate.ObjectType = "Script Progress Update"
	ioHub.broadcast <- scriptProgressUpdate

	if !scriptProgressUpdate.ShouldUpdate {
		return
	}

	guid := scriptProgressUpdate.GUID
	runningScriptsMutex.Lock()
	if _, ok := runningScripts[guid]; ok {
		runningScripts[guid].Count = scriptProgressUpdate.Count
		runningScripts[guid].Total = scriptProgressUpdate.Total
	} else {
		fmt.Printf("Script progress updated attempted for a script which is not running: %s\n", guid)
	}
	runningScriptsMutex.Unlock()
}

// Record sends the script run to the user interface and/or records it in the database
func (scriptRun *ScriptRun) Record() {
	scriptRun.ObjectType = "Script Run"
	if scriptRun.GUID == "" {
		scriptRun.GUID = uuid.NewString()
	}

	if !scriptRun.DoNotBroadcast {
		ioHub.broadcast <- scriptRun
	}

	if scriptRun.ScriptGroup != "" && scriptRun.Status != "" && scriptRun.Status != "Running" {
		endScriptGroupIfRequired(scriptRun.ScriptGroup)
	}

	if !scriptRun.DoNotRecord {
		ioHub.databaseWriter <- scriptRun
	}
}

func (scriptRun *ScriptRun) RecordOrUpdate() {
	var script ScriptRun
	result := readableDatabase.Limit(1).Where("guid = ?", scriptRun.GUID).Find(&script)

	if result.Error == nil {
		scriptRun.ID = script.ID
		scriptRun.HtmlOutput = script.HtmlOutput
	}

	runningScriptsMutex.Lock()
	if runningScript, ok := runningScripts[scriptRun.GUID]; ok {
		scriptRun.TotalRequestCount = runningScript.Total
	}
	runningScriptsMutex.Unlock()

	scriptRun.Record()
}

func (scriptRun *ScriptRun) SetFullOutput(output string) {
	scriptRun.TextOutput = output
}

func (scriptRun *ScriptRun) SetOutput(output string) {
	outputUpdate := ScriptOutputUpdate{
		GUID:       scriptRun.GUID,
		TextOutput: output,
	}
	outputUpdate.Record()
}

func (scriptRun *ScriptRun) SetStatus(status string) {
	scriptRun.Status = status
	databaseScriptRun := ScriptRunFromGUID(scriptRun.GUID)
	if databaseScriptRun != nil {
		scriptRun.TotalRequestCount = databaseScriptRun.TotalRequestCount
	}
	scriptRun.UpdateRunningScripts()
	scriptRun.RecordOrUpdate()
	request_queue.CloseQueueIfEmpty(scriptRun.GUID)
}

func (scriptRun *ScriptRun) UpdateFromRunningScript() {
	runningScriptsMutex.Lock()
	if runningScript, ok := runningScripts[scriptRun.GUID]; ok {
		scriptRun.RequestsMadeCount = runningScript.Count
		scriptRun.TotalRequestCount = runningScript.Total
		scriptRun.TextOutput = runningScript.TextOutput
		scriptRun.HtmlOutput = runningScript.HTMLOutput
	} else if scriptRun.Status == "Running" {
		scriptRun.Status = "Cancelled"
	}

	if scriptRun.Status != "Running" {
		scriptRun.RequestsMadeCount = scriptRun.TotalRequestCount
	}
	runningScriptsMutex.Unlock()
}

func (scriptRun *ScriptRun) UpdateRunningScripts() {
	runningScriptsMutex.Lock()
	if _, ok := runningScripts[scriptRun.GUID]; ok {
		if scriptRun.Status != "Running" {
			scriptRun.TotalRequestCount = runningScripts[scriptRun.GUID].Total
			delete(runningScripts, scriptRun.GUID)
		}
	} else if scriptRun.Status == "Running" {
		runningScripts[scriptRun.GUID] = &runningScriptDetails{
			TextOutput: scriptRun.TextOutput,
			HTMLOutput: scriptRun.HtmlOutput,
			Count:      scriptRun.RequestsMadeCount,
			Total:      scriptRun.TotalRequestCount,
		}
	}
	runningScriptsMutex.Unlock()
}

// RecordError updates the error field and transmits notification of the error to the GUI
func (scriptRun *ScriptRun) RecordError(err string) {
	fmt.Println(err)
	databaseScriptRun := ScriptRunFromGUID(scriptRun.GUID)
	if databaseScriptRun != nil {
		scriptRun.TotalRequestCount = databaseScriptRun.TotalRequestCount
	}
	scriptRun.Error = err
	scriptRun.Status = "Error"
	scriptRun.UpdateRunningScripts()
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
	tx := db.Save(scriptRun)
	if tx.Error != nil {
		fmt.Printf("Error writing script run to database: %s\n", tx.Error)
	}
}

func scriptGroupRunning(scriptGroup string) bool {
	var scripts []ScriptRun
	tx := readableDatabase.Where("script_group = ?", scriptGroup).Find(&scripts)
	if tx.Error != nil {
		return false
	}

	found := false

	runningScriptsMutex.Lock()
	for _, script := range scripts {
		if _, ok := runningScripts[script.GUID]; ok {
			found = true
		}
	}
	runningScriptsMutex.Unlock()

	return found
}

func sendScriptProgressUpdate(guid string, u *runningScriptDetails) {
	update := &ScriptProgressUpdate{
		GUID:         guid,
		Count:        u.Count,
		Total:        u.Total,
		ShouldUpdate: false,
	}
	update.Record()
}
