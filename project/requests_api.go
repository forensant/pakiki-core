package project

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/hex"
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

const RequestFilterSQL = "url LIKE ? OR id IN (SELECT request_id FROM data_packets WHERE request_id NOT IN (SELECT id FROM requests WHERE response_size > 10485760 OR request_size > 10485760) GROUP BY request_id HAVING GROUP_CONCAT(data) LIKE ? ORDER BY direction ASC, id ASC)"

// Ensure that the code-based check is also updated in this scenario
const FilterResourcesSQL = "(response_content_type NOT LIKE 'font/%' AND response_content_type NOT LIKE 'image/%' AND response_content_type NOT LIKE 'javascript/%' AND response_content_type NOT LIKE 'text/css%' AND url NOT LIKE '%.jpg%' AND url NOT LIKE '%.gif%' AND url NOT LIKE '%.png%' AND url NOT LIKE '%.svg' AND url NOT LIKE '%.woff2%' AND url NOT LIKE '%.css%' AND url NOT LIKE '%.js%')"

// RequestResponseContents contains the request and response in base64 format
type RequestResponseContents struct {
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

// RequestSearchResult contains the result from a search across a request/response
type RequestSearchResult struct {
	StartOffset uint64
	EndOffset   uint64
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
// @Router /requests/{base_guid}/compare/{compare_guid} [get]
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

	maxSize := int64(50 * 1024)
	req1, large1, err := getRequestResponseString(db, baseRequest, maxSize)
	if err != nil {
		http.Error(w, "Error retrieving request/response from the database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}
	req2, large2, err := getRequestResponseString(db, compareRequest, maxSize)
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

func formatResponse(resp []byte, ctype string) []byte {
	headerSep := []byte("\r\n\r\n")

	if strings.Contains(ctype, "application/json") {
		components := bytes.SplitN(resp, headerSep, 2)
		if len(components) != 2 {
			return resp
		}

		body := components[1]
		var iJson bytes.Buffer
		err := json.Indent(&iJson, body, "", "  ")

		if err != nil {
			fmt.Printf("Couldn't parse JSON for indentation: %s\n", err)
			return resp
		}

		newResp := make([]byte, 0)
		newResp = append(newResp, components[0]...)
		newResp = append(newResp, headerSep...)
		newResp = append(newResp, iJson.Bytes()...)

		return newResp
	}

	return resp
}

// GetRequestPartialData godoc
// @Summary Get Request/Response Data
// @Description gets part of the request/response. will attempt to return at least 5MB of data to cache
// @Tags Requests
// @Produce  text/text
// @Security ApiKeyAuth
// @Param guid path string true "Request guid"
// @Param from query int true "Offset to request from"
// @Success 200 {object} project.PartialRequestResponseData
// @Failure 500 {string} string Error
// @Router /requests/{guid}/partial_data [get]
func GetRequestPartialData(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
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

// GetRequestResponseContents godoc
// @Summary Get Request and Response
// @Description gets the full request and response of a given request
// @Tags Requests
// @Produce  text/text
// @Security ApiKeyAuth
// @Param guid path string true "Request GUID"
// @Success 200 {object} project.RequestResponseContents
// @Failure 500 {string} string Error
// @Router /requests/{guid}/contents [get]
func GetRequestResponseContents(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

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

	var requestResponse RequestResponseContents
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

		contentType := httpRequest.ResponseContentType

		requestResponse.Request = base64.StdEncoding.EncodeToString(origReq)
		requestResponse.Response = base64.StdEncoding.EncodeToString(formatResponse(origResp, contentType))
		requestResponse.ModifiedRequest = base64.StdEncoding.EncodeToString(modReq)
		requestResponse.ModifiedResponse = base64.StdEncoding.EncodeToString(formatResponse(modResp, contentType))

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

func getRequestResponseString(db *gorm.DB, r Request, maxSize int64) (string, bool, error) {
	if r.RequestSize > maxSize || r.ResponseSize > maxSize {
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
// @Param last query int false "Limit to the last n requests (sorted by time)"
// @Security ApiKeyAuth
// @Success 200 {array} project.Request
// @Failure 500 {string} string Error
// @Router /requests [get]
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
// @Param guid path string true "The GUID of the request to fetch"
// @Success 200 {object} project.RequestSummary
// @Failure 500 {string} string Error
// @Router /requests/{guid} [get]
func GetRequest(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

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

	dataPackets := make([]DataPacket, 0)
	db.Where("request_id = ? AND direction = 'Request' AND modified = true", httpRequest.ID).Order("id").Find(&dataPackets)
	if len(dataPackets) == 0 {
		res := db.Where("request_id = ? AND direction = 'Request' AND modified = false", httpRequest.ID).Order("id").Find(&dataPackets)
		if res.Error != nil {
			http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
			return
		}
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

// PatchRequestNotes godoc
// @Summary Update Request Notes
// @Description updates a specific request's notes
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "The GUID of the request to update"
// @Param notes body string true "The notes for the request"
// @Success 200 {string} string message
// @Failure 500 {string} string Error
// @Router /requests/{guid}/notes [patch]
func PatchRequestNotes(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

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

// PatchRequestPayloads godoc
// @Summary Set Request Payloads
// @Description sets the payloads associated with a specific request
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "The GUID of the request to update"
// @Param payloads body string true "A JSON Object containing the payloads in {'key':'value'} format"
// @Success 200 {string} string Message
// @Failure 500 {string} string Error
// @Router /requests/{guid}/payloads [patch]
func PatchRequestPayloads(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	payloads := r.FormValue("payloads")

	vars := mux.Vars(r)
	guid := vars["guid"]

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

// RequestDataSearch godoc
// @Summary Search Request/Response Data
// @Description
// @Tags Requests
// @Produce  json
// @Security ApiKeyAuth
// @Param guid path string true "Request guid"
// @Param query query string true "Base64 encoded bytes to search for"
// @Success 200 {array} project.RequestSearchResult
// @Failure 500 {string} string Error
// @Router /requests/{guid}/search [get]
func RequestDataSearch(w http.ResponseWriter, r *http.Request, db *gorm.DB) {
	vars := mux.Vars(r)
	guid := vars["guid"]

	queryStr := r.FormValue("query")

	searchQry, err := base64.StdEncoding.DecodeString(queryStr)
	if err != nil {
		http.Error(w, "Error decoding Base64: "+err.Error(), http.StatusInternalServerError)
		return
	}

	searchHex := hex.EncodeToString(searchQry)

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

	query := "request_id = ? AND hexdata LIKE ?"
	if modified {
		// working under the assumption that we can't modify large responses
		query = "request_id = ? AND hexdata LIKE ? AND ((direction = 'Request' AND modified = true) OR (direction = 'Response' AND modified = false))"
	}

	hexFilter := "%" + searchHex + "%"

	var packets []DataPacket
	result = db.Select("hex(data) as hexdata, *").Where(query, httpRequest.ID, hexFilter).Find(&packets)

	if result.Error != nil {
		http.Error(w, "Error retrieving request from database: "+result.Error.Error(), http.StatusInternalServerError)
		return
	}

	searchResults := make([]RequestSearchResult, 0)
	for _, pkt := range packets {
		offset := 0
		data := pkt.Data

		for {
			i := bytes.Index(data, searchQry)
			if i == -1 {
				break
			}

			startOffset := uint64(i) + uint64(offset) + uint64(pkt.StartOffset)

			searchResults = append(searchResults, RequestSearchResult{
				StartOffset: startOffset,
				EndOffset:   startOffset + uint64(len(searchQry)) - 1,
			})

			data = data[i+len(searchQry):]
			offset += i + len(searchQry)
		}
	}

	responseToWrite, err := json.Marshal(searchResults)
	if err != nil {
		http.Error(w, "Error marshalling response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(responseToWrite)
}
