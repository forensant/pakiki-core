package project

import (
	"encoding/json"
	"net/http"

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

// GetInjectOperations godoc
// @Summary Get Inject Operation
// @Description gets a single inject operation
// @Tags Injection Operations
// @Produce  json
// @Security ApiKeyAuth
// @Param guid query string true "The GUID of the request to fetch"
// @Success 200 {object} project.InjectOperation
// @Failure 500 {string} string Error
// @Router /inject_operations [get]
func GetInjectOperation(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	guid := r.FormValue("guid")

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	var operation InjectOperation
	result := db.Preload(clause.Associations).First(&operation, "guid = ?", guid)

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

func HandleInjectOperation(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r.Method == "GET" {
		GetInjectOperation(w, r, db)
	} else if r.Method == "PUT" || r.Method == "POST" {
		PutInjectOperation(w, r, db)
	} else {
		http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
	}
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

	operation := InjectFromGUID(paramOperation.GUID)
	if operation == nil {
		http.Error(w, "Could not find injection operation", http.StatusNotFound)
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

	operation := InjectFromGUID(guid)
	if operation == nil {
		http.Error(w, "Could not find injection operation", http.StatusNotFound)
		return
	}

	operation.Archived = (archived == "true")
	operation.UpdateAndRecord()

	w.Write([]byte("OK"))
}
