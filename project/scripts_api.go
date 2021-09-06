package project

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
)

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
