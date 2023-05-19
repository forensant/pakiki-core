package project

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	_ "embed"
)

// DeleteHookError godoc
// @Summary Delete hook error
// @Description delete a hook error
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "Hook error guid"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /hooks/errors/{guid} [delete]
func DeleteHookError(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	vars := mux.Vars(r)
	guid := vars["guid"]

	// delete the hook with the given ID
	var hook HookErrorLog
	tx := writableDatabase.Where("guid = ?", guid).First(&hook)
	if tx.Error != nil {
		http.Error(w, "Error retrieving hook error from database: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	tx = writableDatabase.Delete(&hook)
	if tx.Error != nil {
		http.Error(w, "Error deleting hook error from database: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}

// GetHookErrors godoc
// @Summary Get All Hooks
// @Description gets a list of all hooks
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param type query string false "hook type"
// @Success 200 {array} project.Hook
// @Failure 500 {string} string Error
// @Router /hooks/errors [get]
func GetHookErrors(w http.ResponseWriter, r *http.Request) {
	var hookErrors []HookErrorLog
	qry := readableDatabase.Order("time")

	result := qry.Find(&hookErrors)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(hookErrors)
	if err != nil {
		http.Error(w, "Could not marshal hooks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}
