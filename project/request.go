package project

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Some fields which we had in the original, which are not pushed through, are:
// hash, flag, injectPayloads, baseRequest, showInDiscovery

// Request represents all of the fields required by the GUI to show
// a request to the user and its properties
type Request struct {
	ID                  uint   `json:"-"`
	URL                 string `gorm:"index:,collate:nocase"`
	GUID                string `gorm:"index:,collate:nocase"`
	Time                int64
	Protocol            string
	Verb                string
	Hash                string
	ObjectType          string `gorm:"-"`
	ResponseSize        int
	ResponseTime        int
	ResponseStatusCode  int
	ResponseContentType string `gorm:"index:,collate:nocase"`
	ScanID              string `gorm:"index:,collate:nocase"`
	Notes               string
	Error               string
	DataPackets         []DataPacket `json:"-"`
	Payloads            string
	InterceptResponse   bool `gorm:"-" json:"-"`

	SiteMapPathID int         `json:"-"`
	SiteMapPath   SiteMapPath `json:"-"`
}

// RequestSummary represents all of the fields required by the GUI
// to render the screens where you can manipulate reqeusts
type RequestSummary struct {
	Hostname     string
	GUID         string
	Protocol     string
	RequestData  string
	URL          string
	SiteMapPath  string
	Headers      map[string]string
	SplitRequest []InjectOperationRequestPart
}

// DataPacket holds further details of either the request or the response to an HTTP request
// this is done so that we can support WebSockets, HTTP/2, etc.
type DataPacket struct {
	ID          uint
	GUID        string
	Time        int64
	Data        []byte
	RequestID   uint
	Direction   string
	Modified    bool
	DisplayData string
}

// NewRequest creates a new request from a byte stream
func NewRequest(rawBytes []byte) (*Request, error) {
	b := bytes.NewReader(rawBytes)
	httpRequest, err := http.ReadRequest(bufio.NewReader(b))

	if err != nil {
		return nil, err
	}

	return NewRequestFromHttp(httpRequest, rawBytes), nil
}

func NewRequestFromHttpWithoutBytes(httpRequest *http.Request) (*Request, error) {
	rawBytes, err := httputil.DumpRequestOut(httpRequest, true)
	if err != nil {
		return nil, err
	}
	return NewRequestFromHttp(httpRequest, rawBytes), nil
}

func NewRequestFromHttp(httpRequest *http.Request, rawBytes []byte) *Request {
	var body []byte
	if httpRequest.Body != nil {
		requestBody := httpRequest.Body
		httpRequest.Body.Close()
		body, _ = io.ReadAll(requestBody)
		httpRequest.Body = io.NopCloser(bytes.NewBuffer(body))
		defer httpRequest.Body.Close()

		if httpRequest.Header.Get("Content-Encoding") == "gzip" && len(body) > 0 {
			reader, _ := gzip.NewReader(bytes.NewBuffer(body))
			body, _ = ioutil.ReadAll(reader)
		}
	}

	headers, _ := httputil.DumpRequestOut(httpRequest, false)
	responseBytes := append(headers, body...)

	url := httpRequest.URL.String()
	if strings.Index(url, "https://") == 0 {
		url = strings.Replace(url, ":443/", "/", 1)
	}

	r := &Request{
		URL:         url,
		Protocol:    httpRequest.Proto,
		Verb:        httpRequest.Method,
		DataPackets: []DataPacket{{Data: responseBytes, Direction: "Request"}},
	}

	r.update()

	return r
}

func CorrectLengthHeaders(request []byte) []byte {
	encoding := "\r\n"
	endOfHeaders := bytes.Index(request, []byte("\r\n\r\n"))
	if endOfHeaders == -1 {
		endOfHeaders = bytes.Index(request, []byte("\n\n"))
		encoding = "\n"
		if endOfHeaders == -1 {
			// nothing more needs to be done
			return request
		}
	}

	contentLength := len(request) - (len(encoding) * 2) - endOfHeaders
	startOfBody := endOfHeaders + (len(encoding) * 2)
	headers := request[0:endOfHeaders]

	re := regexp.MustCompile(`\s*(Transfer-Encoding|Content-Length|Content-Encoding): [A-Za-z0-9]*`)
	newHeaders := re.ReplaceAll(headers, []byte(""))
	contentLengthHeader := []byte((encoding + "Content-Length: " + strconv.Itoa(contentLength)))
	newHeaders = append(newHeaders, contentLengthHeader...)

	newRequest := append(newHeaders, []byte(encoding+encoding)...)
	newRequest = append(newRequest, request[startOfBody:]...)

	return newRequest
}

// CorrectModifiedRequestResponse removes transfer encoding headers and sets a correct content length
// it should only be called on requests/responses where we have the entire contents in one data packet
func (request *Request) CorrectModifiedRequestResponse(direction string) {
	// remove the headers which can cause problems, and replace them with correct ones
	for i, dataPacket := range request.DataPackets {
		if dataPacket.Direction == direction && dataPacket.Modified {
			request.DataPackets[i].Data = CorrectLengthHeaders(request.DataPackets[i].Data)
		}
	}
}

type urlPart struct {
	key   string
	value string
}

func findInjectPointsSplitURLEncoded(start []byte, b []byte, body bool) (parts []InjectOperationRequestPart, remaining []byte) {
	urlParts := make([]urlPart, 0)
	parts = make([]InjectOperationRequestPart, 0)
	remaining = make([]byte, 0)

	if !body {
		queryStart := bytes.Index(b, []byte("?"))
		if queryStart == -1 {
			remaining = append(remaining, start...)
			remaining = append(remaining, b...)
			return
		}
		start = append(start, b[0:queryStart+1]...)
		b = b[queryStart+1:]
	}

	queryParts := bytes.Split(b, []byte("&"))
	for _, queryPart := range queryParts {
		partStart := bytes.Index(queryPart, []byte("="))
		if partStart == -1 {
			urlParts = append(urlParts, urlPart{key: string(queryPart), value: ""})
		} else {
			urlParts = append(urlParts, urlPart{key: string(queryPart[:partStart+1]), value: string(queryPart[partStart+1:])})
		}
	}

	// now pull it back all together
	if len(urlParts) == 0 {
		remaining = append(remaining, start...)
		remaining = append(remaining, b...)
		return
	}

	keyLine := make([]byte, 0)
	for i, urlPart := range urlParts {
		if i == 0 {
			keyLine = append(keyLine, start...)

		} else {
			keyLine = append(keyLine, []byte("&")...)
		}
		keyLine = append(keyLine, []byte(urlPart.key)...)

		if keyLine[len(keyLine)-1] == '=' {
			parts = append(parts, InjectOperationRequestPart{
				RequestPart: base64.StdEncoding.EncodeToString(keyLine),
				Inject:      false,
			})

			parts = append(parts, InjectOperationRequestPart{
				RequestPart: base64.StdEncoding.EncodeToString([]byte(urlPart.value)),
				Inject:      true,
			})

			keyLine = make([]byte, 0)
		}
	}

	if len(keyLine) > 0 {
		remaining = keyLine
	}

	return
}

func findInjectPoints(b []byte) []InjectOperationRequestPart {
	parts := make([]InjectOperationRequestPart, 0)
	lines := bytes.Split(b, []byte("\r\n"))

	if len(lines) == 0 {
		return parts
	}

	firstLineParts := bytes.Split(lines[0], []byte(" "))
	if len(firstLineParts) != 3 {
		return parts
	}

	firstLineParts[0] = append(firstLineParts[0], []byte(" ")...)

	parts, remainingFirstLine := findInjectPointsSplitURLEncoded(firstLineParts[0], firstLineParts[1], false)

	remainingFirstLine = append(remainingFirstLine, []byte(" ")...)
	remainingFirstLine = append(remainingFirstLine, firstLineParts[2]...) // protocol

	remainingHeaders := remainingFirstLine
	remainingHeaders = append(remainingHeaders, []byte("\r\n")...)

	body := make([]byte, 0)
	bodyFound := false

	for i := 1; i < len(lines); i++ {
		if bodyFound {
			body = append(body, lines[i]...)
			if i < len(lines)-1 {
				remainingHeaders = append(body, []byte("\r\n")...)
			}
		} else {
			if string(lines[i]) == "" && i < len(lines)-1 && string(lines[i+1]) != "" {
				bodyFound = true
			} else {
				remainingHeaders = append(remainingHeaders, lines[i]...)
				if i < len(lines)-1 {
					remainingHeaders = append(remainingHeaders, []byte("\r\n")...)
				}
			}
		}
	}

	if bodyFound {
		remainingHeaders = append(remainingHeaders, []byte("\r\n")...)
	}

	if bodyFound && !strings.Contains(strings.ToLower(string(b)), "content-type: application/x-www-form-urlencoded") {
		remainingHeaders = append(remainingHeaders, body...)
		bodyFound = false
	}

	if !bodyFound {
		parts = append(parts, InjectOperationRequestPart{
			RequestPart: base64.StdEncoding.EncodeToString(remainingHeaders),
			Inject:      false,
		})
		return parts
	}

	bodyParts, _ := findInjectPointsSplitURLEncoded(remainingHeaders, body, true)
	parts = append(parts, bodyParts...)

	return parts
}

func GetLastResponseOfURL(url string) ([]byte, error) {
	var request Request
	// If the resource is protected by auth, get the last successful response
	result := readableDatabase.Where("url = ? AND response_status_code >= 200 AND response_status_code < 300", url).Last(&request)

	if result.Error != nil {
		return nil, result.Error
	}

	var dataPackets []DataPacket
	result = readableDatabase.Order("direction, id").Where("request_id = ?", request.ID).Find(&dataPackets)

	if result.Error != nil {
		return nil, result.Error
	}

	var origResp []byte
	for _, dataPacket := range dataPackets {
		if dataPacket.Direction == "Response" && !dataPacket.Modified {
			origResp = append(origResp, dataPacket.Data...)
		}
	}

	origResp = CorrectLengthHeaders(origResp)

	return origResp, nil
}

func (request *Request) GetRequestResponseData(direction string, modified bool) []byte {
	req := make([]byte, 0)
	for _, dataPacket := range request.DataPackets {
		if dataPacket.Direction == direction && dataPacket.Modified == modified {
			req = append(req, dataPacket.Data...)
		}
	}

	return req
}

func (request *Request) HandleResponse(resp *http.Response) {
	var body []byte
	if resp.Body != nil {
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewBuffer(body))
		defer resp.Body.Close()

		if resp.Header.Get("Content-Encoding") == "gzip" && len(body) > 0 {
			reader, err := gzip.NewReader(bytes.NewBuffer(body))
			if err != nil {
				errStr := "Error occurrred when gunzipping response: " + err.Error()
				fmt.Println(errStr)
				request.Error = errStr
			} else {
				defer reader.Close()
				body, _ = ioutil.ReadAll(reader)
			}
		}
	}

	headers, _ := httputil.DumpResponse(resp, false)

	responseBytes := append(headers, body...)

	startTime := time.Unix(request.Time, 0)

	request.ResponseStatusCode = resp.StatusCode
	request.ResponseContentType = resp.Header.Get("Content-Type")
	request.ResponseTime = int(time.Since(startTime).Milliseconds())
	request.ResponseSize = len(responseBytes)

	request.DataPackets = append(request.DataPackets, DataPacket{Data: responseBytes, Direction: "Response", Modified: false})
}

func (request *Request) isResource() bool {
	content_types := [...]string{
		"font/",
		"image/",
		"javascript/",
		"text/css",
	}

	for _, content_type := range content_types {
		if strings.HasPrefix(request.ResponseContentType, content_type) {
			return true
		}
	}

	file_types := [...]string{
		".jpg",
		".gif",
		".png",
		".svg",
		".woff2",
		".css",
		".js",
	}

	for _, file_type := range file_types {
		if strings.Contains(request.URL, file_type) {
			return true
		}
	}

	return false
}

// Record sends the request to the user interface and record it in the database
func (request *Request) Record() {
	err := request.update()
	if err != nil {
		log.Println(err)
		return
	}

	successful := request.ResponseStatusCode < 200 && request.ResponseStatusCode > 299

	if request.SiteMapPath.Path == "" &&
		request.Protocol != "Out of Band" &&
		(successful || request.ScanID == "") {

		request.SiteMapPath = getSiteMapPath(request.URL)

	}
	ioHub.databaseWriter <- request

	request.ObjectType = "HTTP Request"
	ioHub.broadcast <- request
}

func (request *Request) ShouldFilter(filter string) bool {
	if filter == "" {
		return false
	}

	excludeResources := false
	if strings.Index(filter, "exclude_resources:true") == 0 {
		filter = strings.Replace(filter, "exclude_resources:true", "", 1)
		filter = strings.TrimLeft(filter, " ")
		excludeResources = true
	}

	if excludeResources && filter == "" {
		return request.isResource()
	}

	var requests []Request
	tx := readableDatabase.Where("id = ?", request.ID)

	tx = tx.Where(
		"url LIKE ? OR id IN (SELECT request_id FROM data_packets GROUP BY request_id HAVING request_id = ? AND GROUP_CONCAT(data) LIKE ? ORDER BY direction ASC, id ASC)",
		"%"+filter+"%",
		request.ID,
		"%"+filter+"%")

	if excludeResources {
		tx = tx.Where(FilterResourcesSQL)
	}

	result := tx.Order("time").Find(&requests)
	if result.Error != nil {
		fmt.Printf("Error occurred when checking filter: %s\n", result.Error.Error())
		return false
	}

	resultFound := (len(requests) > 0)

	return !resultFound
}

func (request *Request) update() error {
	if request.GUID == "" {
		request.GUID = uuid.NewString()
	}

	if request.Time == 0 {
		request.Time = time.Now().Unix()
	}

	return nil
}

func (request *Request) WriteToDatabase(db *gorm.DB) {
	db.Save(request)
}
