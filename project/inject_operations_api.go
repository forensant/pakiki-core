package project

import (
	"encoding/json"
	"net/http"

	"gorm.io/gorm"
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
	result := db.Order("id").Find(&operations)

	for idx := range operations {
		operations[idx].updatePercentCompleted()
		operations[idx].UpdateForDisplay()
	}

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(operations)
	if err != nil {
		http.Error(w, "Could not marshal requests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// PutInjectOperation godoc
// @Summary Update Inject Operation
// @Description updates the properties of an inject operation
// @Tags Injection Operations
// @Produce  json
// @Security ApiKeyAuth
// @Param default body project.InjectOperation true "Injection details in JSON format (not all fields can be set)"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /inject_operation [put]
func PutInjectOperation(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r.Method != http.MethodPut {
		http.Error(w, "Invalid HTTP method", http.StatusInternalServerError)
		return
	}

	var paramOperation InjectOperation
	err := json.NewDecoder(r.Body).Decode(&paramOperation)
	if err != nil {
		http.Error(w, "Error decoding JSON:"+err.Error(), http.StatusBadRequest)
		return
	}

	var operation InjectOperation
	tx := db.Where("guid = ?", paramOperation.GUID).First(&operation)
	if tx.Error != nil {
		http.Error(w, "Could not find injection operation: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	operation.Title = paramOperation.Title
	operation.UpdateAndRecord()

	w.Write([]byte("OK"))
}

// PutInjectOperation godoc
// @Summary Archive Inject Operation
// @Description updates the the archived status of an inject operation
// @Tags Injection Operations
// @Produce  json
// @Security ApiKeyAuth
// @Param guid formData string true "inject operation guid"
// @Param archive formData bool true "archive status to set"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /inject_operation/archive [put]
func PutArchiveInjectOperation(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
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

	var operation InjectOperation
	tx := db.Where("guid = ?", guid).First(&operation)
	if tx.Error != nil {
		http.Error(w, "Could not find injection operation: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	operation.Archived = (archived == "true")
	operation.UpdateAndRecord()

	w.Write([]byte("OK"))
}
