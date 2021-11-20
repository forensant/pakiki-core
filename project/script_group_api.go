package project

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetScriptGroup godoc
// @Summary Get Script Group
// @Description gets a specific script group
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid query string true "Script group guid"
// @Success 200 project.ScriptGroup
// @Failure 500 {string} string Error
// @Router /project/script_group [get]
func getScriptGroup(w http.ResponseWriter, r *http.Request, db *gorm.DB) {

	guid := r.FormValue("guid")

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

// GetScriptGroups godoc
// @Summary Get All Script Groups
// @Description gets a list of all script groups
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Success 200 {array} project.ScriptGroup
// @Failure 500 {string} string Error
// @Router /project/script_groups [get]
func GetScriptGroups(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
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

// PostScriptGroup godoc
// @Summary Add/Update Script Group
// @Description adds or updates a script group
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param default body project.ScriptGroup true "Script Group details in JSON format"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /project/script_group [put]
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

// PutArchiveScriptGroup godoc
// @Summary Archive Script Group
// @Description updates the the archived status of a script group
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid formData string true "script group guid"
// @Param archive formData bool true "archive status to set"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /project/script_group/archive [put]
func PutArchiveScriptGroup(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
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

func HandleScriptGroup(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	switch r.Method {
	case http.MethodGet:
		getScriptGroup(w, r, db)
	case http.MethodPut:
		postScriptGroup(w, r, db)
	case http.MethodPost:
		postScriptGroup(w, r, db)
	default:
		http.Error(w, "Unsupported method", http.StatusInternalServerError)
	}
}
