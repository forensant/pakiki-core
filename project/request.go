package project

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
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
	GUID                string
	Time                int64
	Protocol            string
	Verb                string
	Hash                string
	ObjectType          string `gorm:"-"`
	ResponseSize        int
	ResponseTime        int
	ResponseStatusCode  int
	ResponseContentType string `gorm:"index:,collate:nocase"`
	ScanID              string
	Notes               string
	Error               string
	DataPackets         []DataPacket `json:"-"`
	InterceptResponse   bool         `gorm:"-" json:"-"`
}

// RequestSummary represents all of the fields required by the GUI
// to render the screens where you can manipulate reqeusts
type RequestSummary struct {
	Hostname    string
	GUID        string
	Protocol    string
	RequestData string
}

// DataPacket holds further details of either the request or the response to an HTTP request
// this is done so that we can support WebSockets, HTTP/2, etc.
type DataPacket struct {
	ID        uint
	Data      []byte
	RequestID uint
	Direction string
	Modified  bool
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
	rawBytes, err := httputil.DumpRequest(httpRequest, true)
	if err != nil {
		return nil, err
	}
	return NewRequestFromHttp(httpRequest, rawBytes), nil
}

func NewRequestFromHttp(httpRequest *http.Request, rawBytes []byte) *Request {
	var body []byte
	if httpRequest.Body != nil {
		body, _ = io.ReadAll(httpRequest.Body)
		httpRequest.Body = io.NopCloser(bytes.NewBuffer(body))

		if httpRequest.Header.Get("Content-Encoding") == "gzip" && len(body) > 0 {
			reader, _ := gzip.NewReader(bytes.NewBuffer(body))
			body, _ = ioutil.ReadAll(reader)
		}
	}

	headers, _ := httputil.DumpRequest(httpRequest, false)
	responseBytes := append(headers, body...)

	r := &Request{
		URL:         httpRequest.URL.String(),
		Protocol:    httpRequest.Proto,
		Verb:        httpRequest.Method,
		DataPackets: []DataPacket{{Data: responseBytes, Direction: "Request"}},
	}

	r.update()

	return r
}

func (request *Request) GetModifiedRequest() []byte {
	req := make([]byte, 0)
	for _, dataPacket := range request.DataPackets {
		if dataPacket.Direction == "Request" && dataPacket.Modified {
			req = append(req, dataPacket.Data...)
		}
	}

	return req
}

func (request *Request) HandleResponse(resp *http.Response) {
	var body []byte
	if resp.Body != nil {
		body, _ = io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewBuffer(body))

		if resp.Header.Get("Content-Encoding") == "gzip" && len(body) > 0 {
			reader, err := gzip.NewReader(bytes.NewBuffer(body))
			if err != nil {
				errStr := "Error occurrred when gunzipping response: " + err.Error()
				fmt.Println(errStr)
				request.Error = errStr
			} else {
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

// Record sends the request to the user interface and record it in the database
func (request *Request) Record() {
	err := request.update()
	if err != nil {
		log.Println(err)
		return
	}

	ioHub.databaseWriter <- request

	request.ObjectType = "HTTP Request"
	ioHub.broadcast <- request

	if request.ScanID != "" && request.ResponseStatusCode != 0 {
		updateRequestCountForScan(request.ScanID)
	}
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
