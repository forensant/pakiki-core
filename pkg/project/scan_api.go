package project

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type requestStatusStats struct {
	ResponseStatusCode int
	Count              int
}

type RequestStatusStatistics struct {
	OneHundreds   int `json:"100"`
	TwoHundreds   int `json:"200"`
	ThreeHundreds int `json:"300"`
	FourHundreds  int `json:"400"`
	FiveHundreds  int `json:"500"`
}

// GetScanStatusStats godoc
// @Summary Get A Summary of Response Codes
// @Description gets a list of response code types and counts
// @Tags Requests
// @Produce  json
// @Param scanid path string true "Scan ID"
// @Security ApiKeyAuth
// @Success 200 {object} project.RequestStatusStatistics
// @Failure 500 {string} string Error
// @Router /scans/{scanid}/status_statistics [get]
func GetScanStatusStats(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	scanid := vars["scanid"]

	if scanid == "" {
		http.Error(w, "Scan ID not supplied", http.StatusInternalServerError)
		return
	}

	// get the scan ids and a summary of their status codes from the database
	var statusStats []requestStatusStats
	result := readableDatabase.Table("requests").Select("response_status_code, count(*) as count").Where("scan_id = ?", scanid).Group("response_status_code").Find(&statusStats)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	stats := RequestStatusStatistics{0, 0, 0, 0, 0}

	for _, status := range statusStats {
		statusCode := status.ResponseStatusCode
		if statusCode >= 100 && statusCode < 200 {
			stats.OneHundreds += status.Count
		} else if statusCode >= 200 && statusCode < 300 {
			stats.TwoHundreds += status.Count
		} else if statusCode >= 300 && statusCode < 400 {
			stats.ThreeHundreds += status.Count
		} else if statusCode >= 400 && statusCode < 500 {
			stats.FourHundreds += status.Count
		} else if statusCode >= 500 && statusCode < 600 {
			stats.FiveHundreds += status.Count
		}
	}

	response, err := json.Marshal(stats)

	if err != nil {
		http.Error(w, "Could not marshal statistics: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

type RequestObjWithHash struct {
	GUID                string
	URL                 string
	Verb                string
	ResponseStatusCode  int
	ResponseContentType string
	Hash                string
}

type SuccessfulResponsesByHash struct {
	URLs          []string
	SampleRequest RequestObjWithHash
}

// GetScanUniqueResponses godoc
// @Summary Get Unique Responses for a scan
// @Description gets a list of the unique responses, grouped by URL
// @Tags Requests
// @Produce  json
// @Param scanid path string true "Scan ID"
// @Security ApiKeyAuth
// @Success 200 {array} project.SuccessfulResponsesByHash
// @Failure 500 {string} string Error
// @Router /scans/{scanid}/unique_responses [get]
func GetScanUniqueResponses(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	scanid := vars["scanid"]

	if scanid == "" {
		http.Error(w, "Scan ID not supplied", http.StatusInternalServerError)
		return
	}

	// get the scan ids and a summary of their status codes from the database
	var requests []RequestObjWithHash
	result := readableDatabase.Table("requests").Select("guid, url, verb, response_status_code, response_content_type, hash").Where("scan_id = ? AND response_status_code >= 200 AND response_status_code < 300", scanid).Find(&requests)

	if result.Error != nil {
		http.Error(w, "Error retrieving requests from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// group the requests by hash
	requestsByHash := make(map[string]SuccessfulResponsesByHash)
	for _, request := range requests {
		obj, ok := requestsByHash[request.Hash]
		if !ok {
			requestsByHash[request.Hash] = SuccessfulResponsesByHash{
				URLs:          []string{request.URL},
				SampleRequest: request,
			}
		} else {
			obj.URLs = append(obj.URLs, request.URL)
			requestsByHash[request.Hash] = obj
		}
	}

	allResponses := make([]SuccessfulResponsesByHash, 0, len(requestsByHash))
	for _, value := range requestsByHash {
		allResponses = append(allResponses, value)
	}

	response, err := json.Marshal(allResponses)

	if err != nil {
		http.Error(w, "Could not marshal responses by hash: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}
