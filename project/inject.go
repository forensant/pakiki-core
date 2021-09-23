package project

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
	ID          uint `json:"-"`
	GUID        string
	Title       string
	Request     []InjectOperationRequestPart
	Host        string
	SSL         bool
	FuzzDB      []string `gorm:"-"`
	KnownFiles  []string `gorm:"-"`
	IterateFrom int
	IterateTo   int
	Archived    bool `gorm:"default:false"`
	Error       string

	// Parts of the object which cannot be set by JSON
	PercentCompleted  int    `gorm:"-"`
	ObjectType        string `gorm:"-"`
	FuzzDBGorm        string `json:"-"`
	KnownFilesGorm    string `json:"-"`
	URL               string
	InjectDescription string `gorm:"-"`
	RequestsMadeCount int    `gorm:"-"`
	TotalRequestCount int
	DoNotRecord       bool `gorm:"-"`
}

func InjectFromGUID(guid string) *InjectOperation {
	var operation InjectOperation
	tx := readableDatabase.Preload(clause.Associations).Where("guid = ?", guid).First(&operation)
	if tx.Error != nil {
		return nil
	}

	return &operation
}

// Record sends the inject operation to the user interface and/or records it in the database
func (injectOperation *InjectOperation) Record() {
	injectOperation.ObjectType = "Inject Operation"
	injectOperation.FuzzDBGorm = strings.Join(injectOperation.FuzzDB[:], ";")
	injectOperation.KnownFilesGorm = strings.Join(injectOperation.KnownFiles[:], ";")
	if injectOperation.GUID == "" {
		injectOperation.GUID = uuid.NewString()
	}

	if !injectOperation.DoNotRecord {
		ioHub.databaseWriter <- injectOperation
	}

	injectOperation.UpdateForDisplay()
	ioHub.broadcast <- injectOperation
}

// RecordError updates the error field and transmits notification of the error to the GUI
func (injectOperation *InjectOperation) RecordError(err string) {
	fmt.Println(err)
	injectOperation.TotalRequestCount = 0
	injectOperation.Error = err
	injectOperation.UpdateAndRecord()
}

func TitlizeName(filename string) string {
	filename = strings.Split(filename, ".")[0]
	filename = strings.ReplaceAll(filename, "-", " ")
	filename = strings.ReplaceAll(filename, "_", " ")
	filename = strings.Title(filename)
	return filename
}

func (injectOperation *InjectOperation) UpdateAndRecord() {
	injectOperation.updatePercentCompleted()
	injectOperation.UpdateForDisplay()
	injectOperation.Record()
}

func (injectOperation *InjectOperation) UpdateForDisplay() {
	injectOperation.FuzzDB = strings.Split(injectOperation.FuzzDBGorm, ";")
	injectOperation.KnownFiles = strings.Split(injectOperation.KnownFilesGorm, ";")

	payloads := make([]string, 0)
	if injectOperation.IterateFrom != 0 || injectOperation.IterateTo != 0 {
		payloads = append(payloads, "Iteration from "+strconv.Itoa(injectOperation.IterateFrom)+" to "+strconv.Itoa(injectOperation.IterateTo))
	}

	for _, filename := range injectOperation.FuzzDB {
		if filename == "" {
			continue
		}
		payloads = append(payloads, "FuzzDB: "+TitlizeName(filename))
	}

	for _, filename := range injectOperation.KnownFiles {
		if filename == "" {
			continue
		}
		payloads = append(payloads, "Known files: "+TitlizeName(filename))
	}

	injectOperation.InjectDescription = strings.Join(payloads, ", ")
	injectOperation.URL = injectOperation.parseURL()
}

func (injectOperation *InjectOperation) updatePercentCompleted() {
	var requestCount int64
	tx := readableDatabase.Model(&Request{}).Where("scan_id = ?", injectOperation.GUID).Count(&requestCount)
	if tx.Error != nil {
		return
	}

	injectOperation.RequestsMadeCount = int(requestCount)

	if requestCount >= int64(injectOperation.TotalRequestCount) {
		injectOperation.PercentCompleted = 100
	} else {
		injectOperation.PercentCompleted = int((float32(requestCount) / float32(injectOperation.TotalRequestCount)) * 100.0)
	}
}

func updateRequestCountForScan(scanId string) {
	var scan InjectOperation
	tx := readableDatabase.Preload(clause.Associations).Where("guid = ?", scanId).Take(&scan)
	if tx.Error != nil {
		return
	}

	scan.updatePercentCompleted()
	scan.DoNotRecord = true

	scan.UpdateForDisplay()
	scan.Record()
}

func (injectOperation *InjectOperation) WriteToDatabase(db *gorm.DB) {
	db.Save(injectOperation)
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
		fmt.Printf("Error occurred: %s\n", err.Error())
	} else {
		urlToReturn, _ = url.QueryUnescape(httpRequest.URL.String())
	}

	return urlToReturn
}
