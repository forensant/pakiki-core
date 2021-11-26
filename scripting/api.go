package scripting

import (
	"encoding/json"
	"net/http"
	"strings"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
	"dev.forensant.com/pipeline/razor/proximitycore/proxy/request_queue"
)

// MakeRequestParameters contains the parameters which are parsed to the Make Request API call
type RunScriptParameters struct {
	Code        []ScriptCode
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
// @Param guid query string true "Script to cancel"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /scripts/cancel [put]
func CancelScript(w http.ResponseWriter, r *http.Request) {
	guid := r.FormValue("guid")
	err := CancelScriptInternal(guid)

	if err != nil {
		http.Error(w, "Error cancelling Python script: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// check if it's an inject operation and cancel that appropriately too
	injectOp := project.InjectFromGUID(guid)
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
// @Param default body scripting.RunScriptParameters true "Run Script Parameters in JSON format"
// @Success 200 {string} string Guid
// @Failure 500 {string} string Error
// @Router /scripts/run [post]
func RunScript(w http.ResponseWriter, r *http.Request) {
	var params RunScriptParameters

	// Try to decode the request body into the struct. If there is an error,
	// respond to the client with the error message and a 400 status code.
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	port := getPort(r.Host)
	guid, err := StartScript(port, params.Code, params.Title, params.Development, "", params.ScriptGroup, r.Header.Get("X-API-Key"), nil)

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
// @Param default body project.ScriptProgressUpdate true "Update Details"
// @Success 200
// @Failure 500 {string} string Error
// @Router /scripts/update_progress [post]
func UpdateProgress(w http.ResponseWriter, r *http.Request) {
	var params project.ScriptProgressUpdate

	// Try to decode the request body into the struct. If there is an error,
	// respond to the client with the error message and a 400 status code.
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	params.ShouldUpdate = true

	params.Record()
}

func getPort(host string) string {
	portIdx := strings.LastIndex(host, ":")

	if portIdx == -1 {
		return ""
	}

	return host[portIdx+1:]
}
