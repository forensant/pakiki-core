package project

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
)

// AppendHTMLScriptParameters contains the parameters which are parsed to append HTML to the script output
type AppendHTMLScriptParameters struct {
	GUID       string
	OutputHTML string
}

// GetScript godoc
// @Summary Get A Script
// @Description gets a single script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Success 200 {string} string ScriptRun Data
// @Failure 500 {string} string Error
// @Router /project/script [get]
func GetScript(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	guid := r.FormValue("guid")

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	var script ScriptRun
	result := db.First(&script, "guid = ?", guid)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	script.UpdateFromRunningScript()

	response, err := json.Marshal(script)
	if err != nil {
		http.Error(w, "Could not marshal script: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// GetScripts godoc
// @Summary Get All Scripts
// @Description gets a list of all scripts
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Success 200 {array} project.ScriptRun
// @Failure 500 {string} string Error
// @Router /project/scripts [get]
func GetScripts(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var scripts []ScriptRun
	result := db.Order("script_runs.id").Find(&scripts)

	for idx := range scripts {
		scripts[idx].UpdateFromRunningScript()
	}

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(scripts)
	if err != nil {
		http.Error(w, "Could not marshal scripts: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// PostAppendHTMLOutputScript godoc
// @Summary Append HTML Output for a Script
// @Description appends the given HTML to the HTML output of the script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param default body project.AppendHTMLScriptParameters true "HTML Output"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /project/script/append_html_output [post]
func PostAppendHTMLOutputScript(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid HTTP method", http.StatusInternalServerError)
		return
	}

	var params AppendHTMLScriptParameters
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		http.Error(w, "Error decoding JSON:"+err.Error(), http.StatusBadRequest)
		return
	}

	if params.GUID == "" {
		http.Error(w, "the guid parameter must be present", http.StatusInternalServerError)
		return
	}

	var script ScriptRun
	tx := db.Where("guid = ?", params.GUID).First(&script)
	if tx.Error != nil {
		http.Error(w, "Could not find script: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	script.HtmlOutput += params.OutputHTML
	script.Record()

	runningUpdate := ScriptOutputUpdate{
		GUID:       params.GUID,
		HTMLOutput: params.OutputHTML,
	}
	runningUpdate.Record()

	w.Write([]byte("OK"))
}

// PutArchiveScript godoc
// @Summary Archive Script
// @Description updates the the archived status of a script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid formData string true "script guid"
// @Param archive formData bool true "archive status to set"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /project/script/archive [put]
func PutArchiveScript(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r.Method != http.MethodPut {
		http.Error(w, "Invalid HTTP method", http.StatusInternalServerError)
		return
	}

	guid := r.FormValue("guid")
	archived := r.FormValue("archive")

	if guid == "" || archived == "" || (archived != "true" && archived != "false") {
		http.Error(w, "guid and archive parameters must be present, and archive must be either \"true\" or \"false\"", http.StatusInternalServerError)
		return
	}

	var script ScriptRun
	tx := db.Where("guid = ?", guid).First(&script)
	if tx.Error != nil {
		http.Error(w, "Could not find script: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	var status = "Archived"
	if archived == "false" {
		status = "Completed"
	}

	script.Status = status
	script.RecordOrUpdate()

	w.Write([]byte("OK"))
}
