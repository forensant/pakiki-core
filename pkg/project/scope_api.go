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

// GetScopeEntries godoc
// @Summary Get All Scope Entries
// @Description gets a list of all scope entries
// @Tags Requests
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {array} project.ScopeEntry
// @Failure 500 {string} string Error
// @Router /scope/entries [get]
func GetScopeEntries(w http.ResponseWriter, r *http.Request) {
	var scopeEntries []ScopeEntry
	result := readableDatabase.Order("scope_entries.sort_order").Find(&scopeEntries)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(scopeEntries)
	if err != nil {
		http.Error(w, "Could not marshal scope entries: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

type ScopeEntryImportJSON struct {
	File     string `json:"file"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Protocol string `json:"protocol"`
}

type Scope struct {
	Advanced bool                   `json:"advanced_mode"`
	Exclude  []ScopeEntryImportJSON `json:"exclude"`
	Include  []ScopeEntryImportJSON `json:"include"`
}

type ScopeTarget struct {
	Scope Scope `json:"scope"`
}

type ScopeTargetJSON struct {
	Target ScopeTarget `json:"target"`
}

// DeleteScopeEntry godoc
// @Summary Delete scope entry
// @Description delete a scope entry
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param path query string true "GUID to delete"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /scope/entry/{guid} [delete]
func DeleteScopeEntry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	vars := mux.Vars(r)
	guid := vars["guid"]

	// delete the scope entry with the given ID
	var lookupScopeEntry ScopeEntry
	tx := writableDatabase.Where("guid = ?", guid).First(&lookupScopeEntry)
	if tx.Error != nil {
		http.Error(w, "Error retrieving scope entry from database: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	tx = writableDatabase.Delete(&lookupScopeEntry)
	if tx.Error != nil {
		http.Error(w, "Error deleting scope entry from database: "+tx.Error.Error(), http.StatusInternalServerError)
		return
	}

	w.Write([]byte("OK"))
}

// ImportScope godoc
// @Summary Import a scope file
// @Description imports a scope export from a bug bounty program
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param body body project.ScopeTargetJSON true "Scope target JSON, as exported from a bug bounty program"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /scope/import [post]
func ImportScope(w http.ResponseWriter, r *http.Request) {
	var target ScopeTargetJSON
	err := json.NewDecoder(r.Body).Decode(&target)
	if err != nil {
		http.Error(w, "Error decoding JSON - ensure the scope you are importing is in the correct format.", http.StatusBadRequest)
		return
	}

	var s = target.Target.Scope
	if !s.Advanced {
		http.Error(w, "Only advanced scopes are supported", http.StatusBadRequest)
		return
	}

	count := len(scope)

	for _, entry := range s.Include {
		se := ScopeEntry{
			Name:           entry.Host,
			HostRegex:      entry.Host,
			PortRegex:      entry.Port,
			Protocol:       entry.Protocol,
			FileRegex:      entry.File,
			IncludeInScope: true,
			SortOrder:      count,
		}
		se.Record()
		count += 1
	}

	for _, entry := range s.Exclude {
		se := ScopeEntry{
			Name:           entry.Host,
			HostRegex:      entry.Host,
			PortRegex:      entry.Port,
			Protocol:       entry.Protocol,
			FileRegex:      entry.File,
			IncludeInScope: false,
			SortOrder:      count,
		}
		se.Record()
		count += 1
	}

	refreshScope()

	w.Write([]byte("OK"))
}

// PostScopeEntries godoc
// @Summary Add/Update Scope Entry
// @Description adds or updates a scope entry
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param body body project.ScopeEntry true "Script Entry details in JSON format"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /scope/entry [post]
func PostScopeEntry(w http.ResponseWriter, r *http.Request) {
	var se ScopeEntry
	err := json.NewDecoder(r.Body).Decode(&se)
	if err != nil {
		http.Error(w, "Error decoding JSON:"+err.Error(), http.StatusBadRequest)
		return
	}

	err = se.Validate()
	if err != nil {
		http.Error(w, "Error validating scope entry: "+err.Error(), http.StatusBadRequest)
		return
	}

	var lookupScopeEntry ScopeEntry
	tx := readableDatabase.Preload(clause.Associations).Where("guid = ?", se.GUID).First(&lookupScopeEntry)
	if tx.Error == nil && se.GUID != "" {
		// Update
		se.ID = lookupScopeEntry.ID
		se.SortOrder = lookupScopeEntry.SortOrder
	} else {
		// Create, so set the order to be the last order
		var scopeEntries []ScopeEntry
		result := readableDatabase.Find(&scopeEntries)
		if result.Error == nil {
			se.SortOrder = len(scopeEntries)
		}
	}

	se.Record()

	w.Write([]byte(se.GUID))
}

// OrderScopeEntries godoc
// @Summary Order Scope Entries
// @Description sets the order for the scope entries
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param body body string true "Colon separated list of GUIDs"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /scope/order [post]
func OrderScopeEntries(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading body:"+err.Error(), http.StatusBadRequest)
		return
	}

	// split the string into a slice
	orders := strings.Split(string(body), ":")
	for i, order := range orders {
		var se ScopeEntry
		tx := readableDatabase.Where("guid = ?", order).First(&se)
		if tx.Error == nil {
			se.SortOrder = i
			se.Record()
		}
	}

	refreshScope()

	w.Write([]byte("OK"))
}

// URLInScope godoc
// @Summary Checks URL Scope
// @Description checks if the given URL is in scope
// @Tags Requests
// @Produce plain
// @Security ApiKeyAuth
// @Param url query string true "URL to check"
// @Success 200 {string} string true or false
// @Failure 500 {string} string Error
// @Router /scope/url_in_scope [get]
func URLInScope(w http.ResponseWriter, r *http.Request) {
	url := r.FormValue("url")
	if url == "" {
		http.Error(w, "URL must be specified", http.StatusBadRequest)
		return
	}

	for _, entry := range scope {
		urlInScope, err := entry.URLInScope(url)
		if err != nil {
			http.Error(w, "Error checking if URL is in scope: %s"+err.Error(), http.StatusBadRequest)
			return
		}

		if !urlInScope {
			continue
		}

		if entry.IncludeInScope {
			w.Write([]byte("true"))
			return
		}

		if !entry.IncludeInScope {
			w.Write([]byte("false"))
			return
		}
	}

	if len(scope) == 0 {
		w.Write([]byte("true"))
	} else {
		w.Write([]byte("false"))
	}
}
