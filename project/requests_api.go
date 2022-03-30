package project

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sergi/go-diff/diffmatchpatch"
	"gorm.io/gorm"
)

const RequestFilterSQL = "url LIKE ? OR id IN (SELECT request_id FROM data_packets GROUP BY request_id HAVING GROUP_CONCAT(data) LIKE ? ORDER BY direction ASC, id ASC)"

// Ensure that the code-based check is also updated in this scenario
const FilterResourcesSQL = "(response_content_type NOT LIKE 'font/%' AND response_content_type NOT LIKE 'image/%' AND response_content_type NOT LIKE 'javascript/%' AND response_content_type NOT LIKE 'text/css%' AND url NOT LIKE '%.jpg%' AND url NOT LIKE '%.gif%' AND url NOT LIKE '%.png%' AND url NOT LIKE '%.svg' AND url NOT LIKE '%.woff2%' AND url NOT LIKE '%.css%' AND url NOT LIKE '%.js%')"

// RequestResponse contains the request and response in base64 format
type RequestResponse struct {
	Protocol              string
	Request               string
	Response              string
	ModifiedRequest       string
	ModifiedResponse      string
	Modified              bool
	URL                   string
	MimeType              string
	DataPackets           []DataPacket
	LargeResponse         bool
	CombinedContentLength int64
}

// PartialRequestResponseData contains a slice of the request/response from a given request
type PartialRequestResponseData struct {
	From uint64
	To   uint64
	Data string
}

// RequestDifference contains an individual difference between two requests
type RequestDifference struct {
	Text    string
	Request int // 1 for request number one, 2 for request number two, 0 for both
}

// CompareRequests godoc
// @Summary Compare Two Requests
// @Description compares two requests and returns the differences
// @Tags Requests
// @Produce  text/text
// @Security ApiKeyAuth
// @Param base_guid path string true "Base Request guid"
// @Param compare_guid path string true "Request to Compare guid"
// @Success 200 {array} RequestDifference
// @Failure 500 {string} string Error
// @Router /project/requests/{base_guid}/compare/{compare_guid} [get]
func CompareRequests(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)

	var baseRequest Request
	var compareRequest Request

	result := db.First(&baseRequest, "guid = ?", vars["base_guid"])
	if result.Error != nil {
		http.Error(w, "Error retrieving base request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	result = db.First(&compareRequest, "guid = ?", vars["compare_guid"])
	if result.Error != nil {
		http.Error(w, "Error retrieving comparison request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	if baseRequest.GUID == compareRequest.GUID {
		http.Error(w, "Cannot compare a request to itself", http.StatusBadRequest)
		return
	}

	if baseRequest.Protocol != "HTTP/1.1" || compareRequest.Protocol != "HTTP/1.1" {
		http.Error(w, "Only HTTP requests can be compared.", http.StatusBadRequest)
		return
	}

	req1, large1, err := getRequestResponseString(db, baseRequest)
	if err != nil {
		http.Error(w, "Error retrieving request/response from the database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}
	req2, large2, err := getRequestResponseString(db, compareRequest)
	if err != nil {
		http.Error(w, "Error retrieving request/response from the database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	if large1 || large2 {
		http.Error(w, "One or both of the requests/responses are too large to compare.", http.StatusInternalServerError)
		return
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(req1, req2, false)

	rDiffs := make([]RequestDifference, 0)
	for _, d := range diffs {
		r := 0
		if d.Type == diffmatchpatch.DiffInsert {
			r = 2
		} else if d.Type == diffmatchpatch.DiffDelete {
			r = 1
		}

		rDiffs = append(rDiffs, RequestDifference{Text: d.Text, Request: r})
	}

	responseToWrite, err := json.Marshal(rDiffs)
	if err != nil {
		http.Error(w, "Error marshalling response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(responseToWrite)
}

// GetRequestData godoc
// @Summary Get Request/Response Data
// @Description gets part of the request/response. will attempt to return at least 5MB of data to cache
// @Tags Requests
// @Produce  text/text
// @Security ApiKeyAuth
// @Param guid path string true "Request guid"
// @Param from query int true "Offset to request from"
// @Success 200 {object} project.PartialRequestResponseData
// @Failure 500 {string} string Error
// @Router /project/requests/{guid}/data [get]
func GetRequestData(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	from, err := strconv.ParseInt(r.FormValue("from"), 10, 64)
	dataToReturn := make([]byte, 0)

	if err != nil {
		http.Error(w, "Error parsing from value: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var httpRequest Request
	result := db.First(&httpRequest, "guid = ?", guid)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	modified := false
	res := db.Where("request_id = ? AND direction = 'Request' AND modified = true", httpRequest.ID).Find(&DataPacket{})
	if res.RowsAffected > 0 {
		modified = true
	}

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	query := "request_id = ? AND ? >= start_offset AND ? < end_offset"
	if modified {
		// working under the assumption that we can't modify large responses
		query = "request_id = ? AND ? >= start_offset AND ? < end_offset AND ((direction = 'Request' AND modified = true) OR (direction = 'Response' AND modified = false))"
	}

	var dp DataPacket
	result = db.Where(query, httpRequest.ID, from, from).Limit(1).First(&dp)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	if result.RowsAffected != 1 {
		http.Error(w, "Error retrieving request from database: did not retrieve exactly one row", http.StatusInternalServerError)
		return
	}

	toWrite := (5 * 1024 * 1024) // 5MB
	dataOffset := from - dp.StartOffset
	dataToReturn = append(dataToReturn, dp.Data[dataOffset:]...)

	for len(dataToReturn) < toWrite {
		newOffset := dp.EndOffset + 1
		dp = DataPacket{}
		result = db.Where("request_id = ? AND start_offset = ?", httpRequest.ID, newOffset).Limit(1).First(&dp)

		if result.Error != nil {
			fmt.Printf("Error retrieving request from database: %s\n", result.Error.Error())
			break
		}

		if result.RowsAffected != 1 {
			fmt.Printf("Error retrieving request from database: did not retrieve exactly one row\n")
			break
		}

		dataToReturn = append(dataToReturn, dp.Data...)
	}

	if toWrite < len(dataToReturn) {
		dataToReturn = dataToReturn[0:toWrite]
	}

	response := PartialRequestResponseData{
		From: uint64(from),
		To:   uint64(from + int64(len(dataToReturn))),
		Data: base64.StdEncoding.EncodeToString(dataToReturn),
	}

	responseToWrite, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Error marshalling response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(responseToWrite)
}

// GetRequestResponse godoc
// @Summary Get Request and Response
// @Description gets the full request and response of a given request
// @Tags Requests
// @Produce  text/text
// @Security ApiKeyAuth
// @Param guid query string true "Request guid"
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

	var requestResponse RequestResponse
	requestResponse.Protocol = httpRequest.Protocol
	requestResponse.CombinedContentLength = httpRequest.RequestSize + httpRequest.ResponseSize
	requestResponse.LargeResponse = requestResponse.CombinedContentLength > int64(MaxResponsePacketSize) && httpRequest.Protocol == "HTTP/1.1"

	var dataPackets []DataPacket
	dataPacketOrder := "direction, id"
	if httpRequest.Protocol == "Websocket" {
		dataPacketOrder = "id"
	}
	qry := "request_id = ?"
	if requestResponse.LargeResponse {
		qry += " AND direction = 'Request'"
	}
	result = db.Order(dataPacketOrder).Where(qry, httpRequest.ID).Find(&dataPackets)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	if httpRequest.Protocol == "Websocket" || httpRequest.Protocol == "Out of Band" {
		requestResponse.DataPackets = dataPackets

		res := db.Where("request_id = ? AND modified = true", httpRequest.ID).Find(&DataPacket{})
		requestResponse.Modified = (res.RowsAffected > 0)

	} else {
		var origReq []byte
		var origResp []byte
		var modReq []byte
		var modResp []byte
		for _, dataPacket := range dataPackets {
			if dataPacket.Modified {
				requestResponse.Modified = true
			}

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
					if len(origResp) != 0 || len(dataPacket.Data) > (MaxResponsePacketSize) {
						requestResponse.LargeResponse = true
					}

					if len(origResp) == 0 {
						origResp = append(origResp, dataPacket.Data...)
					}
				}
			}
		}

		requestResponse.Request = base64.StdEncoding.EncodeToString(origReq)
		requestResponse.Response = base64.StdEncoding.EncodeToString(origResp)
		requestResponse.ModifiedRequest = base64.StdEncoding.EncodeToString(modReq)
		requestResponse.ModifiedResponse = base64.StdEncoding.EncodeToString(modResp)

	}

	requestResponse.URL = httpRequest.URL
	requestResponse.MimeType = httpRequest.ResponseContentType

	semicolonPos := strings.Index(requestResponse.MimeType, ";")
	if semicolonPos != -1 {
		requestResponse.MimeType = requestResponse.MimeType[:semicolonPos]
	}

	responseToWrite, err := json.Marshal(requestResponse)
	if err != nil {
		http.Error(w, "Error marshalling response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(responseToWrite)
}

func getRequestResponseString(db *gorm.DB, r Request) (string, bool, error) {
	if r.RequestSize > int64(MaxResponsePacketSize) || r.ResponseSize > int64(MaxResponsePacketSize) {
		return "", true, nil
	}

	reqData := ""
	respData := ""

	dataPackets := make([]DataPacket, 0)
	db.Where("request_id = ? AND direction = 'Request' AND modified = true", r.ID).Order("id").Find(&dataPackets)
	if len(dataPackets) == 0 {
		res := db.Where("request_id = ? AND direction = 'Request' AND modified = false", r.ID).Order("id").Find(&dataPackets)
		if res.Error != nil {
			return "", false, res.Error
		}

		for _, pkt := range dataPackets {
			reqData += string(pkt.Data)
		}
	}

	dataPackets = make([]DataPacket, 0)
	db.Where("request_id = ? AND direction = 'Response' AND modified = true", r.ID).Order("id").Find(&dataPackets)
	if len(dataPackets) == 0 {
		res := db.Where("request_id = ? AND direction = 'Response' AND modified = false", r.ID).Order("id").Find(&dataPackets)
		if res.Error != nil {
			return "", false, res.Error
		}

		for _, pkt := range dataPackets {
			respData += string(pkt.Data)
		}
	}

	return reqData + "\x0A\x0D\x0A\x0D" + respData, false, nil
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
// @Param scanid query string false "Scan ID, can be multiple separated by semi-colons"
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
		tx = db.Where("scan_id IN ?", strings.Split(scanId, ":"))
	}

	filter := r.FormValue("filter")
	if filter != "" {
		tx = tx.Where(RequestFilterSQL, "%"+filter+"%", "%"+filter+"%")
	}

	urlFilter := r.FormValue("url_filter")
	if urlFilter != "" {
		tx = tx.Where("url LIKE ? OR url LIKE ?", "http"+urlFilter+"%", "https"+urlFilter+"%")
	}

	protocol := r.FormValue("protocol")
	if protocol != "" {
		tx = tx.Where("protocol = ?", protocol)
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

	validColumns := []string{"url", "time", "protocol", "verb", "response_size", "response_time", "response_status_code", "response_content_type", "payloads", "notes", "error"}
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
// @Security ApiKeyAuth
// @Param guid query string true "The GUID of the request to fetch"
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

	var siteMapPath SiteMapPath
	db.First(&siteMapPath, "id = ?", httpRequest.SiteMapPathID)

	var dataPackets []DataPacket
	result = db.Order("direction, id").Where("request_id = ? AND direction = 'Request'", httpRequest.ID).Find(&dataPackets)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// assemble the raw request
	requestData := make([]byte, 0)
	for _, dataPacket := range dataPackets {
		requestData = append(requestData, dataPacket.Data...)
	}

	// get the headers
	b := bytes.NewReader(requestData)
	rawHttpRequest, err := http.ReadRequest(bufio.NewReader(b))

	if err != nil {
		http.Error(w, "Error reading request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var headers = make(map[string]string)
	for k, v := range rawHttpRequest.Header {
		headers[k] = v[0] // if a request has two headers which are the same, only return the first one
	}

	// compile the request summary
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
	requestSummary.URL = httpRequest.URL
	requestSummary.SiteMapPath = siteMapPath.Path
	requestSummary.Headers = headers
	requestSummary.SplitRequest = findInjectPoints(requestData)

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
	} else if r.Method == "PUT" || r.Method == "POST" {
		UpdateRequest(w, r, db)
	} else {
		http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
	}
}

// PutRequestPayloads godoc
// @Summary Set Request Payloads
// @Description sets the payloads associated with a specific request
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param guid body string true "The GUID of the request to update"
// @Param payloads body string true "A JSON Object containing the payloads in {'key':'value'} format"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /project/request/payloads [put]
func PutRequestPayloads(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	guid := r.FormValue("guid")
	payloads := r.FormValue("payloads")

	if guid == "" {
		http.Error(w, "GUID not supplied", http.StatusInternalServerError)
		return
	}

	// primarily this is a sanity check at this point, to ensure the payloads are valid JSON
	var payloadJson map[string]string
	err := json.Unmarshal([]byte(payloads), &payloadJson)
	if err != nil {
		http.Error(w, "Could not parse payloads", http.StatusInternalServerError)
		return
	}

	var httpRequest Request
	result := db.First(&httpRequest, "guid = ?", guid)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	httpRequest.Payloads = payloads
	httpRequest.Record()

	w.Write([]byte("OK"))
}

// UpdateRequest godoc
// @Summary Update A Request
// @Description updates a specific request
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param guid body string true "The GUID of the request to update"
// @Param notes body string true "The notes for the request"
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
