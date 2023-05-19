package project

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/pipeline/proximity-core/internal/request_queue"
	"github.com/pipeline/proximity-core/internal/scripting"
)

// MakeRequestParameters contains the parameters which are parsed to the Make Request API call
type RunScriptParameters struct {
	Code        []scripting.ScriptCode
	Title       string
	Development bool
	ScriptGroup string
}

// CancelScript godoc
// @Summary Cancel the running script
// @Description cancels the provided script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "Script to cancel"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /scripts/{guid}/cancel [patch]
func CancelScriptAPI(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	err := scripting.CancelScriptInternal(guid)
	CancelScript(guid)

	if err != nil {
		http.Error(w, "Error cancelling Python script: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// check if it's an inject operation and cancel that appropriately too
	injectOp := InjectFromGUID(guid)
	if injectOp != nil {
		injectOp.TotalRequestCount = 0
		injectOp.UpdateAndRecord()
	}

	request_queue.CancelRequests(guid)

	w.Header().Set("Content-Type", "text/text")
	w.Write([]byte("Script cancelled successfully"))
}

// RunScript godoc
// @Summary Run provided script
// @Description runs the provided script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param body project.RunScriptParameters true "Run Script Parameters in JSON format"
// @Success 200 {string} string Guid
// @Failure 500 {string} string Error
// @Router /scripts/run [post]
func RunScript(w http.ResponseWriter, r *http.Request) {
	var params RunScriptParameters

	// Try to decode the request body into the struct. If there is an error,
	// respond to the client with the error message and a 400 status code.
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		http.Error(w, "Error decoding JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	mainScript := ""
	for _, scriptPart := range params.Code {
		if scriptPart.MainScript {
			mainScript = scriptPart.Code
		}
	}

	scriptRun := &ScriptRun{
		GUID:        uuid.NewString(),
		Title:       params.Title,
		Development: params.Development,
		ScriptGroup: params.ScriptGroup,
		Script:      mainScript,
	}

	scriptRun.RecordOrUpdate()

	port := getPort(r.Host)
	guid, err := scripting.StartScript(
		port,
		params.Code,
		r.Header.Get("X-API-Key"),
		scriptRun)

	if err != nil {
		http.Error(w, "Error running Python script: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/text")
	w.Write([]byte(guid))
}

// UpdateProgress godoc
// @Summary Updates running script progress
// @Description updates the progress of a currently running script
// @Tags Scripting
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "Script to update"
// @Param body body project.ScriptProgressUpdate true "Update Details"
// @Success 200
// @Failure 500 {string} string Error
// @Router /scripts/{guid}/update_progress [post]
func UpdateProgress(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	var params ScriptProgressUpdate

	// Try to decode the request body into the struct. If there is an error,
	// respond to the client with the error message and a 400 status code.
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params.ShouldUpdate = true
	params.GUID = guid

	params.Record()
}

func getPort(host string) string {
	portIdx := strings.LastIndex(host, ":")

	if portIdx == -1 {
		return ""
	}

	return host[portIdx+1:]
}
