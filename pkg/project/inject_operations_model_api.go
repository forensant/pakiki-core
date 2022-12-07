package project

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetInjectOperations godoc
// @Summary Get All Inject Operations
// @Description gets a list of all injection operations
// @Tags Injection Operations
// @Produce  json
// @Security ApiKeyAuth
// @Success 200 {array} project.InjectOperation
// @Failure 500 {string} string Error
// @Router /inject_operations [get]
func GetInjectOperations(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var operations []InjectOperation
	result := db.Preload(clause.Associations).Order("inject_operations.id").Find(&operations)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	for idx := range operations {
		operations[idx].updatePercentCompleted(true)
		operations[idx].UpdateForDisplay()
	}

	response, err := json.Marshal(operations)
	if err != nil {
		http.Error(w, "Could not marshal requests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// GetInjectOperation godoc
// @Summary Get Inject Operation
// @Description gets a single inject operation
// @Tags Injection Operations
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "The GUID of the request to fetch"
// @Success 200 {object} project.InjectOperation
// @Failure 500 {string} string Error
// @Router /inject_operations/{path} [get]
func GetInjectOperation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	var operation InjectOperation
	result := readableDatabase.Preload(clause.Associations).First(&operation, "guid = ?", guid)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	operation.updatePercentCompleted(true)
	operation.UpdateForDisplay()

	response, err := json.Marshal(operation)
	if err != nil {
		http.Error(w, "Could not marshal requests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// PatchInjectOperationArchive godoc
// @Summary Archive Inject Operation
// @Description updates the the archived status of an inject operation
// @Tags Injection Operations
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "inject operation guid"
// @Param archive formData bool true "archive status to set"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /inject_operations/{guid}/archive [patch]
func PatchInjectOperationArchive(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
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

	operation := InjectFromGUID(guid)
	if operation == nil {
		http.Error(w, "Could not find injection operation", http.StatusNotFound)
		return
	}

	operation.Archived = (archived == "true")
	operation.UpdateAndRecord()

	w.Write([]byte("OK"))
}

// PatchInjectOperationArchive godoc
// @Summary Set Inject Operation Title
// @Description updates the title of an inject operation
// @Tags Injection Operations
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "inject operation guid"
// @Param title formData string true "title to set"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /inject_operations/{guid}/title [patch]
func PatchInjectOperationTitle(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
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

	operation := InjectFromGUID(guid)
	if operation == nil {
		http.Error(w, "Could not find injection operation", http.StatusNotFound)
		return
	}

	operation.Title = title
	operation.UpdateAndRecord()

	w.Write([]byte("OK"))
}
