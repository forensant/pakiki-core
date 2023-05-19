package project

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"gorm.io/gorm/clause"

	_ "embed"
)

// DeleteHook godoc
// @Summary Delete hook
// @Description delete a hook
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "Hook guid"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /hooks/{guid} [delete]
func DeleteHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	vars := mux.Vars(r)
	guid := vars["guid"]

	// delete the hook with the given ID
	var hook Hook
	tx := writableDatabase.Where("guid = ?", guid).First(&hook)
	if tx.Error != nil {
		http.Error(w, "Error retrieving hook from database: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	tx = writableDatabase.Delete(&hook)
	if tx.Error != nil {
		http.Error(w, "Error deleting hook from database: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}

// EnableHook godoc
// @Summary Enable hook
// @Description enable or disable a given hook
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "Hook guid"
// @Param enabled query bool true "Whether the hook should be enabled or disabled"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /hooks/{guid}/enable [put]
func EnableHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	vars := mux.Vars(r)
	guid := vars["guid"]

	// find the given hook
	var hook Hook
	tx := writableDatabase.Where("guid = ?", guid).First(&hook)
	if tx.Error != nil {
		http.Error(w, "Error retrieving hook from database: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	hook.Enabled = (r.FormValue("enabled") == "true")
	hook.Record()

	w.Write([]byte("OK"))
}

// GetHooks godoc
// @Summary Get All Hooks
// @Description gets a list of all hooks
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param type query string false "hook type"
// @Success 200 {array} project.Hook
// @Failure 500 {string} string Error
// @Router /hooks [get]
func getHooks(w http.ResponseWriter, r *http.Request) {
	var hooks []Hook
	qry := readableDatabase.Order("hooks.internally_managed").Order("hooks.sort_order")

	hookType := r.FormValue("type")
	if hookType != "" {
		qry = qry.Where("hook_type = ?", hookType)
	}

	result := qry.Find(&hooks)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(hooks)
	if err != nil {
		http.Error(w, "Could not marshal hooks: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// PostHook godoc
// @Summary Add/Update Hook
// @Description adds or updates a hook
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param body body project.Hook true "Hook details in JSON format"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /hooks [post]
func postHook(w http.ResponseWriter, r *http.Request) {
	var h Hook
	err := json.NewDecoder(r.Body).Decode(&h)
	if err != nil {
		http.Error(w, "Error decoding JSON:"+err.Error(), http.StatusBadRequest)
		return
	}

	err = h.validate()
	if err != nil {
		http.Error(w, "Error validating scope entry: "+err.Error(), http.StatusBadRequest)
		return
	}

	var lookupHook Hook
	tx := readableDatabase.Preload(clause.Associations).Where("guid = ?", h.GUID).First(&lookupHook)
	if tx.Error == nil && h.GUID != "" {
		// Update
		h.ID = lookupHook.ID
		h.SortOrder = lookupHook.SortOrder
	}

	h.Record()

	w.Write([]byte(h.GUID))
}

// OrderHook godoc
// @Summary Order Hooks
// @Description sets the order for the hooks
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param body body string true "Colon separated list of GUIDs"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /hooks/order [post]
func OrderHooks(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading body:"+err.Error(), http.StatusBadRequest)
		return
	}

	// split the string into a slice
	orders := strings.Split(string(body), ":")
	for i, order := range orders {
		var h Hook
		tx := readableDatabase.Where("guid = ?", order).First(&h)
		if tx.Error == nil {
			h.SortOrder = i
			h.Record()
		}
	}

	refreshHooks()

	w.Write([]byte("OK"))
}

// SetHookLibrary godoc
// @Summary Set Hook Library
// @Description sets the library code which will be used when executing hooks
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param body body string true "Library Code in Python"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /hooks/set_library [post]
func SetHookLibrary(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading body:"+err.Error(), http.StatusBadRequest)
		return
	}

	hookLibrary.Code = string(body)

	w.Write([]byte("OK"))
}

func HandleHooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getHooks(w, r)
	case http.MethodPost:
		postHook(w, r)
	case http.MethodPut:
		postHook(w, r)
	default:
		http.Error(w, "Unsupported method", http.StatusInternalServerError)
	}
}
