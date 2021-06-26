package proxy

import (
	"bufio"
	"embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
)

//go:embed resources/fuzzdb
var fuzzdb embed.FS

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
// @Param default body project.InjectOperation true "Injection details in JSON format (not all fields can be set)"
// @Success 200 {string} string GUID
// @Failure 500 {string} string Error
// @Router /inject_operation/run [post]
func RunInjection(w http.ResponseWriter, r *http.Request) {
	var operation project.InjectOperation

	// Try to decode the request body into the struct. If there is an error,
	// respond to the client with the error message and a 400 status code.
	err := json.NewDecoder(r.Body).Decode(&operation)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

// GetPayloads godoc
// @Summary Gets injection payloads
// @Description gets all available payloads available for injection
// @Tags Injection Operations
// @Produce json
// @Security ApiKeyAuth
// @Success 200 {object} proxy.PayloadOptions
// @Failure 500 {string} string Error
// @Router /inject_operations/payloads [get]
func GetInjectPayloads(w http.ResponseWriter, r *http.Request) {
	attackFilenames, err := payloadsWithTitles("attacks")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	knownFileFilenames, err := payloadsWithTitles("file_lists")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	sort.Sort(attackFilenames)
	sort.Sort(knownFileFilenames)

	payloadOptions := PayloadOptions{
		Attack:     attackFilenames,
		KnownFiles: knownFileFilenames,
	}

	js, err := json.Marshal(payloadOptions)
	if err != nil {
		http.Error(w, "Cannot convert request to JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

func getPort(host string) string {
	portIdx := strings.LastIndex(host, ":")

	if portIdx == -1 {
		return ""
	}

	return host[portIdx+1:]
}

func payloadsWithTitles(directory string) (PayloadFileArray, error) {
	payloads := make([]PayloadFile, 0)
	dir := "resources/fuzzdb/" + directory

	files, err := fuzzdb.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, fileEntry := range files {
		filename := fileEntry.Name()
		payloads = append(payloads, PayloadFile{
			Filename:       filename,
			Title:          project.TitlizeName(filename),
			SamplePayloads: samplePayloads(dir + "/" + filename),
		})
	}

	return payloads, nil
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

// sorting functions for the payload files
func (p PayloadFileArray) Len() int {
	return len(p)
}

func (p PayloadFileArray) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

func (p PayloadFileArray) Less(i, j int) bool {
	return p[i].Title < p[j].Title
}
