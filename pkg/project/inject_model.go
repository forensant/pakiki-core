package project

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/forensant/pakiki-core/internal/request_queue"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InjectOperationRequestPart contains the components of a request
type InjectOperationRequestPart struct {
	ID                uint `json:"-"`
	RequestPart       string
	Inject            bool
	InjectOperationID uint `json:"-"`
}

// InjectOperation contains the parameters which are passed to the Injection API calls
type InjectOperation struct {
	ID              uint `json:"-"`
	GUID            string
	Title           string
	Request         []InjectOperationRequestPart
	Host            string
	SSL             bool
	FuzzDB          []string `gorm:"-"`
	CustomPayloads  []string `gorm:"-"`
	CustomFilenames []string `gorm:"-"`
	IterateFrom     int
	IterateTo       int
	Archived        bool `gorm:"default:false"`
	Error           string

	// Parts of the object which cannot be set by JSON
	PercentCompleted   int    `gorm:"-"`
	ObjectType         string `gorm:"-"`
	FuzzDBGorm         string `json:"-"`
	CustomFilesGorm    string `json:"-"`
	CustomPayloadsGorm string `json:"-"`
	URL                string
	InjectDescription  string `gorm:"-"`
	RequestsMadeCount  int    `gorm:"-"`
	TotalRequestCount  int
	DoNotRecord        bool `gorm:"-"`
}

var injectOperationCacheMutex sync.Mutex
var injectOperationCache map[string]*InjectOperation

func InjectFromGUID(guid string) *InjectOperation {
	injectOperationCacheMutex.Lock()
	injectOperation, cached := injectOperationCache[guid]
	injectOperationCacheMutex.Unlock()

	if cached {
		return injectOperation
	} else {
		injectOperation = &InjectOperation{}
	}

	tx := readableDatabase.Preload(clause.Associations).Where("guid = ?", guid).Limit(1).Find(injectOperation)
	if tx.Error != nil || tx.RowsAffected < 1 {
		return nil
	}

	injectOperationCache[guid] = injectOperation

	return injectOperation
}

func (injectOperation *InjectOperation) Broadcast() {
	prevValue := injectOperation.DoNotRecord
	injectOperation.updatePercentCompleted(false)
	injectOperation.DoNotRecord = true
	injectOperation.Record()
	injectOperation.DoNotRecord = prevValue
}

func (injectOp *InjectOperation) GetGUID() string {
	return injectOp.GUID
}

// Record sends the inject operation to the user interface and/or records it in the database
func (injectOperation *InjectOperation) Record() {
	injectOperation.ObjectType = "Inject Operation"
	injectOperation.FuzzDBGorm = strings.Join(injectOperation.FuzzDB[:], ";")
	injectOperation.CustomFilesGorm = strings.Join(injectOperation.CustomFilenames[:], ";")
	injectOperation.CustomPayloadsGorm = strings.Join(injectOperation.CustomPayloads[:], "\n")
	if injectOperation.GUID == "" {
		injectOperation.GUID = uuid.NewString()
	}

	if !injectOperation.DoNotRecord {
		ioHub.databaseWriter <- injectOperation
	}

	injectOperation.UpdateForDisplay()
	ioHub.broadcast <- injectOperation

	injectOperationCacheMutex.Lock()
	injectOperationCache[injectOperation.GUID] = injectOperation
	injectOperationCacheMutex.Unlock()
}

// RecordError updates the error field and transmits notification of the error to the GUI
func (injectOp *InjectOperation) RecordError(err string) {
	fmt.Println(err)
	injectOp.TotalRequestCount = 0
	injectOp.Error = err
	injectOp.UpdateAndRecord()
	request_queue.CloseQueueIfEmpty(injectOp.GUID)
}

func (injectOp *InjectOperation) SetFullOutput(string) {
	// do nothing
}

func (injectOp *InjectOperation) SetOutput(string) {
	// do nothing
}

func (injectOp *InjectOperation) SetStatus(s string) {
	if s == "Completed" || s == "Error" {
		request_queue.CloseQueueIfEmpty(injectOp.GUID)
	}
}

func TitlizeName(filename string) string {
	filename_parts := strings.Split(filename, ".")
	end_length := len(filename_parts) - 1
	if end_length < 1 {
		end_length = 1
	}
	filename = strings.Join(filename_parts[0:end_length], " ")
	filename = strings.ReplaceAll(filename, "-", " ")
	filename = strings.ReplaceAll(filename, "_", " ")
	filename = strings.Title(filename)
	return filename
}

func (injectOperation *InjectOperation) UpdateAndRecord() {
	injectOperation.updatePercentCompleted(false)
	injectOperation.UpdateForDisplay()
	injectOperation.Record()
}

func (injectOperation *InjectOperation) UpdateForDisplay() {
	if injectOperation.FuzzDBGorm == "" {
		injectOperation.FuzzDB = make([]string, 0)
	} else {
		injectOperation.FuzzDB = strings.Split(injectOperation.FuzzDBGorm, ";")
	}

	if injectOperation.CustomFilesGorm == "" {
		injectOperation.CustomFilenames = make([]string, 0)
	} else {
		injectOperation.CustomFilenames = strings.Split(injectOperation.CustomFilesGorm, ";")
	}

	payloads := make([]string, 0)
	if injectOperation.IterateFrom != 0 || injectOperation.IterateTo != 0 {
		payloads = append(payloads, "Iteration from "+strconv.Itoa(injectOperation.IterateFrom)+" to "+strconv.Itoa(injectOperation.IterateTo))
	}

	for _, filename := range injectOperation.FuzzDB {
		if filename == "" {
			continue
		}
		filename = strings.Replace(filename, "resources/fuzzdb/", "", 1)
		payloads = append(payloads, "FuzzDB: "+TitlizeName(filename))
	}

	for _, filename := range injectOperation.CustomFilenames {
		if filename == "" {
			continue
		}
		payloads = append(payloads, "Custom files: "+filename)
	}

	injectOperation.InjectDescription = strings.Join(payloads, ", ")
	injectOperation.URL = injectOperation.parseURL()
}

func (injectOp *InjectOperation) ValidateAndSanitize() error {
	if injectOp.Host == "" {
		return errors.New("please specify a host to target")
	}

	if len(injectOp.FuzzDB) == 0 && len(injectOp.CustomPayloads) == 0 && injectOp.IterateFrom == injectOp.IterateTo {
		return errors.New("please specify a payload to run")
	}

	has_fuzz_points := false
	for _, requestPart := range injectOp.Request {
		if requestPart.Inject {
			has_fuzz_points = true
		}
	}

	if !has_fuzz_points {
		return errors.New("please specify at least one injection point")
	}

	if strings.Contains(injectOp.Host, "/") {
		url, err := url.Parse(injectOp.Host)
		if err != nil || url.Host == "" {
			return errors.New("please specify a valid host")
		}

		injectOp.Host = url.Host
		if (url.Scheme == "https" && url.Port() != "443") || (url.Scheme == "http" && url.Port() != "80") {
			injectOp.Host += ":" + url.Port()
		}
	}

	return nil
}

func (injectOperation *InjectOperation) IncrementRequestCount() {
	injectOperation.RequestsMadeCount++
	injectOperation.UpdateAndRecord()
}

func (injectOperation *InjectOperation) updatePercentCompleted(queryFromDatabase bool) {
	var requestCount int64

	if queryFromDatabase {
		tx := readableDatabase.Model(&Request{}).Where("scan_id = ?", injectOperation.GUID).Count(&requestCount)
		if tx.Error != nil {
			return
		}

		injectOperation.RequestsMadeCount = int(requestCount)
	} else {
		requestCount = int64(injectOperation.RequestsMadeCount)
	}

	if requestCount >= int64(injectOperation.TotalRequestCount) {
		injectOperation.PercentCompleted = 100
	} else {
		injectOperation.PercentCompleted = int((float32(requestCount) / float32(injectOperation.TotalRequestCount)) * 100.0)
	}
}

func (injectOperation *InjectOperation) WriteToDatabase(db *gorm.DB) {
	tx := db.Save(injectOperation)
	if tx.Error != nil {
		fmt.Printf("Error saving inject operation: %s\n", tx.Error)
	}
}

func (injectOperation *InjectOperation) ShouldFilter(str string) bool {
	return false
}

func (injectOperation *InjectOperation) parseURL() string {
	var requestData = make([]byte, 0)
	urlToReturn := ""

	for _, requestPart := range injectOperation.Request {
		decodedData, err := base64.StdEncoding.DecodeString(requestPart.RequestPart)
		if err != nil {
			fmt.Printf("Could not decode base64 encoded inject request, error: %s\n", err.Error())
		}
		requestData = append(requestData, decodedData...)
	}

	b := bytes.NewReader(requestData)
	httpRequest, err := http.ReadRequest(bufio.NewReader(b))

	if err != nil {
		fmt.Printf("Error occurred parsing inject operation URL: %s\n", err.Error())
	} else {
		urlToReturn, _ = url.QueryUnescape(httpRequest.URL.String())
	}

	return urlToReturn
}
