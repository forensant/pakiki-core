package proxy

import (
	"bufio"
	"embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/forensant/pakiki-core/pkg/project"
)

//go:embed resources/fuzzdb
var fuzzdb embed.FS

// PayloadEntry contains a single entry within the payloads list
type PayloadEntry struct {
	Filename       string
	IsDirectory    bool
	ResourcePath   string
	SamplePayloads []string
	SubEntries     []PayloadEntry
	Title          string
	PayloadCount   int
}

type PayloadFile struct {
	Title          string
	Filename       string
	SamplePayloads []string
}
type PayloadFileArray []PayloadFile

// PayloadOptions contains maps of filename[title] for each type of payload for injection
type PayloadOptions struct {
	Attack     PayloadFileArray
	KnownFiles PayloadFileArray
}

// RunInjection godoc
// @Summary Run an Injection Operation
// @Description creates and runs an injection operation
// @Tags Injection Operations
// @Accept json
// @Produce  json
// @Security ApiKeyAuth
// @Param body body project.InjectOperation true "Injection details in JSON format (not all fields can be set)"
// @Success 200 {string} string GUID
// @Failure 500 {string} string Error
// @Router /inject_operations/run [post]
func RunInjection(w http.ResponseWriter, r *http.Request) {
	var operation project.InjectOperation

	// Try to decode the request body into the struct. If there is an error,
	// respond to the client with the error message and a 400 status code.
	err := json.NewDecoder(r.Body).Decode(&operation)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if operation.Host == "" {
		http.Error(w, "Please specify a host to target", http.StatusBadRequest)
		return
	}

	if len(operation.FuzzDB) == 0 && len(operation.CustomPayloads) == 0 && operation.IterateFrom == operation.IterateTo {
		http.Error(w, "Please specify a payload to run", http.StatusBadRequest)
		return
	}

	has_fuzz_points := false
	for _, requestPart := range operation.Request {
		if requestPart.Inject {
			has_fuzz_points = true
		}
	}

	if !has_fuzz_points {
		http.Error(w, "Please specify at least one part of the request to fuzz", http.StatusBadRequest)
		return
	}

	port := getPort(r.Host)
	runInjection(&operation, port, r.Header.Get("X-API-Key"))

	js, err := json.Marshal(operation)
	if err != nil {
		http.Error(w, "Cannot convert request to JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

// GetFuzzDB Payload godoc
// @Summary Get a fuzzdb file
// @Description gets a specific fuzzdb file
// @Tags Injection Operations
// @Produce json
// @Security ApiKeyAuth
// @Param file query string true "The file path of the fuzzdb file to fetch the payload for"
// @Success 200 {array} string
// @Failure 500 {string} string Error
// @Router /inject_operations/fuzzdb_payload [get]
func GetFuzzdbPayload(w http.ResponseWriter, r *http.Request) {
	payloads := make([]string, 0)
	filename := r.FormValue("file")

	if filename == "" {
		http.Error(w, "No filename specified", http.StatusBadRequest)
		return
	}

	file, err := fuzzdb.Open(filename)
	if err != nil {
		http.Error(w, "Could not open file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := strings.Map(func(r rune) rune {
			if unicode.IsGraphic(r) {
				return r
			}
			return -1
		}, scanner.Text())
		payloads = append(payloads, text)
	}

	js, err := json.Marshal(payloads)
	if err != nil {
		http.Error(w, "Cannot convert request to JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

// GetPayloads godoc
// @Summary Gets injection payloads
// @Description gets all available payloads available for injection
// @Tags Injection Operations
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} proxy.PayloadEntry
// @Failure 500 {string} string Error
// @Router /inject_operations/payloads [get]
func GetInjectPayloads(w http.ResponseWriter, r *http.Request) {
	rootEntry := PayloadEntry{
		Title:        "root",
		Filename:     "",
		ResourcePath: "/",
		IsDirectory:  true,
		SubEntries:   make([]PayloadEntry, 0),
	}

	readPayloadDirectory("resources/fuzzdb", &rootEntry)

	js, err := json.Marshal(rootEntry)
	if err != nil {
		http.Error(w, "Cannot convert request to JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

func countLines(filename string) int {
	lineCounterFile, _ := fuzzdb.Open(filename)
	defer lineCounterFile.Close()

	scanner := bufio.NewScanner(lineCounterFile)
	line_count := 0
	for scanner.Scan() {
		line_count += 1
	}
	return line_count
}

func getPort(host string) string {
	portIdx := strings.LastIndex(host, ":")

	if portIdx == -1 {
		return ""
	}

	return host[portIdx+1:]
}

func readPayloadDirectory(path string, entry *PayloadEntry) {
	files, err := fuzzdb.ReadDir(path)
	if err != nil {
		return
	}

	if len(files) == 0 {
		return
	}

	for _, fileEntry := range files {
		filename := fileEntry.Name()
		fileEntryPath := path + "/" + filename

		if strings.HasSuffix(filename, ".md") {
			continue
		}

		fileSamplePayloads := make([]string, 0)
		payloadCount := 0
		if !fileEntry.IsDir() {
			payloadCount = countLines(fileEntryPath)
			fileSamplePayloads = samplePayloads(fileEntryPath)
			fileSamplePayloads = append(fileSamplePayloads, "")
			fileSamplePayloads = append(fileSamplePayloads, "Total entries: "+strconv.Itoa(payloadCount))
		}

		newEntry := PayloadEntry{
			Filename:       filename,
			Title:          project.TitlizeName(filename),
			ResourcePath:   fileEntryPath,
			SamplePayloads: fileSamplePayloads,
			IsDirectory:    fileEntry.Type().IsDir(),
			SubEntries:     make([]PayloadEntry, 0),
			PayloadCount:   payloadCount,
		}

		if fileEntry.IsDir() {
			readPayloadDirectory(fileEntryPath, &newEntry)
		}

		entry.SubEntries = append(entry.SubEntries, newEntry)
	}
}

func samplePayloads(filename string) []string {
	payloads := make([]string, 0)

	file, err := fuzzdb.Open(filename)
	if err != nil {
		return payloads
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for i := 0; i < 5 && scanner.Scan(); i++ {
		text := scanner.Text()
		if len(text) > 30 {
			text = text[0:30] + "..."
		}
		payloads = append(payloads, text)
	}

	return payloads
}
