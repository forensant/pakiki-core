package project

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const RequestFilterSQL = "url LIKE ? OR id IN (SELECT request_id FROM data_packets GROUP BY request_id HAVING GROUP_CONCAT(data) LIKE ? ORDER BY direction ASC, id ASC)"
const FilterResourcesSQL = "(response_content_type NOT LIKE 'font/%' AND response_content_type NOT LIKE 'image/%' AND response_content_type NOT LIKE 'javascript/%' AND response_content_type NOT LIKE 'text/css%' AND url NOT LIKE '%.jpg%' AND url NOT LIKE '%.gif%' AND url NOT LIKE '%.png%' AND url NOT LIKE '%.woff2%' AND url NOT LIKE '%.css%' AND url NOT LIKE '%.js%')"

// RequestResponse contains the request and response in base64 format
type RequestResponse struct {
	Request          string
	Response         string
	ModifiedRequest  string
	ModifiedResponse string
}

// GetRequestResponse godoc
// @Summary Get Request and Response
// @Description gets the full request and response of a given request
// @Tags Requests
// @Produce  text/text
// @Security ApiKeyAuth
// @Success 200 {string} string Request Data
// @Failure 500 {string} string Error
// @Router /project/requestresponse [get]
func GetRequestResponse(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	guid := r.FormValue("guid")

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	var httpRequest Request
	result := db.First(&httpRequest, "guid = ?", guid)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	var dataPackets []DataPacket
	result = db.Order("direction, id").Where("request_id = ?", httpRequest.ID).Find(&dataPackets)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	var origReq []byte
	var origResp []byte
	var modReq []byte
	var modResp []byte
	for _, dataPacket := range dataPackets {
		if dataPacket.Direction == "Request" {
			if dataPacket.Modified {
				modReq = append(modReq, dataPacket.Data...)
			} else {
				origReq = append(origReq, dataPacket.Data...)
			}
		} else {
			if dataPacket.Modified {
				modResp = append(modResp, dataPacket.Data...)
			} else {
				origResp = append(origResp, dataPacket.Data...)
			}
		}
	}

	var requestResponse RequestResponse
	requestResponse.Request = base64.StdEncoding.EncodeToString(origReq)
	requestResponse.Response = base64.StdEncoding.EncodeToString(origResp)
	requestResponse.ModifiedRequest = base64.StdEncoding.EncodeToString(modReq)
	requestResponse.ModifiedResponse = base64.StdEncoding.EncodeToString(modResp)

	responseToWrite, err := json.Marshal(requestResponse)
	if err != nil {
		http.Error(w, "Error marshalling response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(responseToWrite)
}

// isInSlice takes a slice and looks for an element in it. If found it will
// return it's key, otherwise it will return -1 and a bool of false.
func isInSlice(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// GetRequests godoc
// @Summary Get All Requests
// @Description gets a list of all requests
// @Tags Requests
// @Produce  json
// @Param scanid query string false "Scan ID"
// @Param filter query string false "Only show requests which contain the filter string in the url, request, response, etc"
// @Param url_filter query string false "Only show requests which contain the given string in the URL"
// @Param sort_col query string false "Column to sort by (default time)"
// @Param sort_dir query string false "Column direction to sort by (default asc)"
// @Security ApiKeyAuth
// @Success 200 {array} project.Request
// @Failure 500 {string} string Error
// @Router /project/requests [get]
func GetRequests(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	var requests []Request
	var result *gorm.DB

	excludeResources := r.FormValue("exclude_resources")

	scanId := r.FormValue("scanid")
	var tx *gorm.DB
	if scanId == "" {
		tx = db.Where("scan_id = ''")
	} else {
		tx = db.Where("scan_id = ?", scanId)
	}

	filter := r.FormValue("filter")
	if filter != "" {
		tx = tx.Where(RequestFilterSQL, "%"+filter+"%", "%"+filter+"%")
	}

	urlFilter := r.FormValue("url_filter")
	if urlFilter != "" {
		tx = tx.Where("url LIKE ? OR url LIKE ?", "http"+urlFilter+"%", "https"+urlFilter+"%")
	}

	last := r.FormValue("last")
	if last != "" {
		lastInt, err := strconv.Atoi(last)
		if err == nil {
			tx = tx.Order("time DESC").Limit(lastInt)
		}
	}

	if excludeResources == "true" {
		tx = tx.Where(FilterResourcesSQL)
	}

	validColumns := []string{"url", "time", "protocol", "verb", "response_size", "response_time", "response_status_code", "response_content_type", "notes", "error"}
	validDirections := []string{"asc", "desc"}

	sortColumn := "time"
	sortDirection := "asc"

	requestSortCol := strings.ToLower(r.FormValue("sort_col"))
	requestSortDir := strings.ToLower(r.FormValue("sort_dir"))

	if isInSlice(validColumns, requestSortCol) {
		sortColumn = requestSortCol
	}

	if isInSlice(validDirections, requestSortDir) {
		sortDirection = requestSortDir
	}

	result = tx.Order(sortColumn + " " + sortDirection).Find(&requests)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(requests)
	if err != nil {
		http.Error(w, "Could not marshal requests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

// GetRequest godoc
// @Summary Get A Request
// @Description gets a specific request
// @Tags Requests
// @Produce  json
// @Param guid query string true "The GUID of the request to fetch"
// @Security ApiKeyAuth
// @Success 200 {object} project.RequestSummary
// @Failure 500 {string} string Error
// @Router /project/request [get]
func GetRequest(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	guid := r.FormValue("guid")

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	var httpRequest Request
	result := db.First(&httpRequest, "guid = ?", guid)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	var dataPackets []DataPacket
	result = db.Order("direction, id").Where("request_id = ? AND direction = 'Request'", httpRequest.ID).Find(&dataPackets)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	requestData := make([]byte, 0)
	for _, dataPacket := range dataPackets {
		requestData = append(requestData, dataPacket.Data...)
	}

	var requestSummary RequestSummary
	url, err := url.Parse(httpRequest.URL)
	if err != nil {
		http.Error(w, "Error parsing URL: "+err.Error(), http.StatusInternalServerError)
		return
	}

	requestSummary.GUID = httpRequest.GUID
	requestSummary.Hostname = url.Host
	requestSummary.Protocol = url.Scheme + "://"
	requestSummary.RequestData = base64.StdEncoding.EncodeToString(requestData)

	response, err := json.Marshal(requestSummary)
	if err != nil {
		http.Error(w, "Could not marshal requests: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(response)
}

func HandleRequest(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	if r.Method == "GET" {
		GetRequest(w, r, db)
	} else if r.Method == "PUT" {
		UpdateRequest(w, r, db)
	} else {
		http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
	}
}

// UpdateRequest godoc
// @Summary Update A Request
// @Description updates a specific request
// @Tags Requests
// @Produce  json
// @Param guid body string true "The GUID of the request to update"
// @Param notes body string true "The notes for the request"
// @Security ApiKeyAuth
// @Success 200 {string} string message
// @Failure 500 {string} string Error
// @Router /project/request [put]
func UpdateRequest(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	guid := r.FormValue("guid")

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	var httpRequest Request
	result := db.First(&httpRequest, "guid = ?", guid)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	httpRequest.Notes = r.FormValue("notes")
	httpRequest.Record()

	w.Write([]byte("OK"))
}
