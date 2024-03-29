package project

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	_ "embed"
)

//go:embed resources/exportScriptGroup.html
var exportScriptGroupTemplate string

type ScriptGroupExport struct {
	Title   string
	Scripts []ScriptRun
}

// GetScriptGroup godoc
// @Summary Get Script Group
// @Description gets a specific script group
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "Script group guid"
// @Success 200 {object} project.ScriptGroup
// @Failure 500 {string} string Error
// @Router /script_groups/{guid} [get]
func GetScriptGroup(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	var scriptGroup ScriptGroup
	result := db.First(&scriptGroup, "guid = ?", guid)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	scriptGroup.ensureRunning()

	response, err := json.Marshal(scriptGroup)
	if err != nil {
		http.Error(w, "Could not marshal scripts: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// getScriptGroups godoc
// @Summary Get All Script Groups
// @Description gets a list of all script groups
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Success 200 {array} project.ScriptGroup
// @Failure 500 {string} string Error
// @Router /script_groups [get]
func getScriptGroups(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var scriptGroups []ScriptGroup
	result := db.Order("script_groups.id").Find(&scriptGroups)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	for _, scriptGroup := range scriptGroups {
		scriptGroup.ensureRunning()
	}

	response, err := json.Marshal(scriptGroups)
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
// @Router /script_groups/{guid}/export [get]
func ExportScriptGroup(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	var scriptGroup ScriptGroup
	tx := db.Where("guid = ?", guid).First(&scriptGroup)
	if tx.Error != nil {
		http.Error(w, "Could not find script group: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	var scripts []ScriptRun
	tx = db.Where("script_group = ?", guid).Find(&scripts)
	if tx.Error != nil {
		http.Error(w, "Could not find scripts: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	export := ScriptGroupExport{
		Title:   scriptGroup.Title,
		Scripts: scripts,
	}

	tmpl, err := template.New("script_group_export").Parse(exportScriptGroupTemplate)
	if err != nil {
		http.Error(w, "Could not generate template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var b bytes.Buffer
	if err = tmpl.Execute(&b, export); err != nil {
		http.Error(w, "Could not generate template: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(b.Bytes())
}

// PostScriptGroup godoc
// @Summary Add/Update Script Group
// @Description adds or updates a script group
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param body body project.ScriptGroup true "Script Group details in JSON format"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /script_groups [post]
func postScriptGroup(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var paramGroup ScriptGroup
	err := json.NewDecoder(r.Body).Decode(&paramGroup)
	if err != nil {
		http.Error(w, "Error decoding JSON:"+err.Error(), http.StatusBadRequest)
		return
	}

	var group ScriptGroup
	tx := db.Preload(clause.Associations).Where("guid = ?", paramGroup.GUID).First(&group)
	if tx.Error == nil {
		// Update
		group.Title = paramGroup.Title
		group.Record()
	} else {
		// create
		paramGroup.Record()
		group = paramGroup
	}

	w.Write([]byte(group.GUID))
}

// PatchScriptGroupArchive godoc
// @Summary Archive Script Group
// @Description updates the archived status of a script group
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "script group guid"
// @Param archive formData bool true "archive status to set"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /script_groups/{guid}/archive [patch]
func PatchScriptGroupArchive(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Invalid HTTP method", http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)
	guid := vars["guid"]

	archived := r.FormValue("archive")

	if guid == "" || archived == "" || (archived != "true" && archived != "false") {
		http.Error(w, "guid and archive parameters must be present, and archive must be either \"true\" or \"false\"", http.StatusInternalServerError)
		return
	}

	var script ScriptGroup
	tx := db.Where("guid = ?", guid).First(&script)
	if tx.Error != nil {
		http.Error(w, "Could not find script group: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	var status = "Archived"
	if archived == "false" {
		status = "Completed"
	}

	script.Status = status
	script.Record()

	w.Write([]byte("OK"))
}

// PatchScriptGroupExpanded godoc
// @Summary Set Script Group Expanded Status
// @Description updates whether a script group is expanded (used for the UI)
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "script group guid"
// @Param expanded formData bool true "expanded state"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /script_groups/{guid}/expanded [patch]
func PatchScriptGroupExpanded(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Invalid HTTP method", http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)
	guid := vars["guid"]

	expandedStr := r.FormValue("expanded")

	expanded := false
	if strings.ToLower(expandedStr) == "true" {
		expanded = true
	}

	if guid == "" {
		http.Error(w, "guid must be present", http.StatusInternalServerError)
		return
	}

	var script ScriptGroup
	tx := db.Where("guid = ?", guid).First(&script)
	if tx.Error != nil {
		http.Error(w, "Could not find script group: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	script.Expanded = expanded
	script.Record()

	w.Write([]byte("OK"))
}

// PatchScriptGroupTitle godoc
// @Summary Set Script Group Title
// @Description updates the title of a script group
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "script group guid"
// @Param title formData bool true "title to set"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /script_groups/{guid}/title [patch]
func PatchScriptGroupTitle(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Invalid HTTP method", http.StatusInternalServerError)
		return
	}

	vars := mux.Vars(r)
	guid := vars["guid"]

	title := r.FormValue("title")

	if guid == "" {
		http.Error(w, "guid must be present", http.StatusInternalServerError)
		return
	}

	var script ScriptGroup
	tx := db.Where("guid = ?", guid).First(&script)
	if tx.Error != nil {
		http.Error(w, "Could not find script group: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	script.Title = title
	script.Record()

	w.Write([]byte("OK"))
}

func HandleScriptGroups(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	switch r.Method {
	case http.MethodGet:
		getScriptGroups(w, r, db)
	case http.MethodPut:
		postScriptGroup(w, r, db)
	case http.MethodPost:
		postScriptGroup(w, r, db)
	default:
		http.Error(w, "Unsupported method", http.StatusInternalServerError)
	}
}
