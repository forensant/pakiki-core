package project

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"
	"gorm.io/gorm"

	_ "embed"
)

//go:embed resources/exportSingleScript.html
var exportSingleScriptTemplate string

// GetScript godoc
// @Summary Get A Script
// @Description gets a single script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "The GUID of the script to fetch"
// @Success 200 {string} string ScriptRun Data
// @Failure 500 {string} string Error
// @Router /scripts/{guid} [get]
func GetScript(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

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
// @Router /scripts [get]
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

// ExportScriptResults godoc
// @Summary HTML Export of a script result
// @Description export a script result
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "script guid"
// @Success 200 {string} string HTML Output
// @Failure 500 {string} string Error
// @Router /scripts/{guid}/export [get]
func ExportScriptResults(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	var script ScriptRun
	tx := db.Where("guid = ?", guid).First(&script)
	if tx.Error != nil {
		http.Error(w, "Could not find script: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	tmpl, err := template.New("script_export").Parse(exportSingleScriptTemplate)
	if err != nil {
		http.Error(w, "Could not generate template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var b bytes.Buffer
	if err = tmpl.Execute(&b, script); err != nil {
		http.Error(w, "Could not generate template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(b.Bytes())
}

// PatchArchiveScript godoc
// @Summary Archive Script
// @Description updates the the archived status of a script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "script guid"
// @Param archive formData bool true "archive status to set"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /scripts/{guid}/archive [patch]
func PatchArchiveScript(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	if r.Method != http.MethodPatch {
		http.Error(w, "Invalid HTTP method", http.StatusInternalServerError)
		return
	}

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

// PostAppendHTMLOutputScript godoc
// @Summary Append HTML Output for a Script
// @Description appends the given HTML to the HTML output of the script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "The GUID of the script to fetch"
// @Param html body string true "HTML Output to append"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /scripts/{guid}/append_html_output [post]
func PostAppendHTMLOutputScript(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	if r.Method != http.MethodPost {
		http.Error(w, "Invalid HTTP method", http.StatusInternalServerError)
		return
	}

	outputHTML := r.FormValue("html")

	if outputHTML == "" {
		http.Error(w, "the html parameter must be present", http.StatusInternalServerError)
		return
	}

	if guid == "" {
		http.Error(w, "the guid parameter must be present", http.StatusInternalServerError)
		return
	}

	var script ScriptRun
	tx := db.Where("guid = ?", guid).First(&script)
	if tx.Error != nil {
		http.Error(w, "Could not find script: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	script.HtmlOutput += outputHTML
	script.DoNotBroadcast = true
	script.Record()

	runningUpdate := ScriptOutputUpdate{
		GUID:       guid,
		HTMLOutput: outputHTML,
	}
	runningUpdate.Record()

	w.Write([]byte("OK"))
}
