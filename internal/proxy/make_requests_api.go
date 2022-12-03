package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"

	"github.com/pipeline/proximity-core/internal/request_queue"
	"github.com/pipeline/proximity-core/pkg/project"
)

var defaultConnectionPool *http.Client

// MakeRequestParameters contains the parameters which are passed to the Make Request API call
type MakeRequestParameters struct {
	RequestBase64 string `json:"request" example:"<base64 encoded request>"`
	Host          string `json:"host"`
	SSL           bool   `json:"ssl"`
	ScanID        string `json:"scan_id"`
	ClientCert    string `json:"client_cert"`
	ClientCertKey string `json:"client_cert_key"`
}

// BulkRequestQueueParameters contains the parameters which are passed to the Bulk Request API call
type BulkRequestQueueParameters struct {
	Host         string                               `json:"host"`
	SSL          bool                                 `json:"ssl"`
	ScanID       string                               `json:"scan_id"`
	Replacements [][]string                           `json:"replacements"`
	Request      []project.InjectOperationRequestPart `json:"request"`
}

// Request returns the base64 decoded request
func (params *MakeRequestParameters) Request() []byte {
	data, err := base64.StdEncoding.DecodeString(params.RequestBase64)
	if err != nil {
		fmt.Println("Error base64 decoding request:", err)
		return nil
	}
	return data
}

// hostWithPort returns the host with the appropriate port number (if it doesn't contain one)
func (params *MakeRequestParameters) hostWithPort() string {
	if strings.Contains(params.Host, ":") {
		return params.Host
	}

	if params.SSL {
		return params.Host + ":443"
	}

	return params.Host + ":80"
}

// MakeRequest godoc
// @Summary Make a single request
// @Description makes a single request to a given server
// @Tags Requests
// @Accept json
// @Produce  json
// @Security ApiKeyAuth
// @Param body body proxy.MakeRequestParameters true "Make Request Parameters in JSON format"
// @Success 200 {string} string Message
// @Failure 400 {string} string Error
// @Failure 500 {string} string Error
// @Router /requests/make [post]
func MakeRequest(w http.ResponseWriter, r *http.Request) {
	var params MakeRequestParameters

	// Try to decode the request body into the struct. If there is an error,
	// respond to the client with the error message and a 400 status code.
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	httpClient := &http.Client{}

	if params.ClientCert != "" && params.ClientCertKey != "" {
		cert, err := tls.X509KeyPair([]byte(params.ClientCert), []byte(params.ClientCertKey))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		updateConnectionPool(httpClient, &cert)
	} else {
		updateConnectionPool(httpClient, nil)
	}

	request, err := makeRequestToSite(params.SSL, params.hostWithPort(), params.Request(), httpClient, nil)
	if err != nil {
		http.Error(w, "Cannot make request to site: "+err.Error(), http.StatusInternalServerError)
		return
	}

	request.ScanID = params.ScanID
	request.Record()

	js, err := json.Marshal(request)
	if err != nil {
		http.Error(w, "Cannot convert request to JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(js)
}

type AddRequestToQueueParameters struct {
	Request  string `json:"request" example:"<base64 encoded request>"`
	Host     string `json:"host"`
	SSL      bool   `json:"ssl"`
	ScanID   string `json:"scan_id"`
	Payloads string `json:"payloads"`
}

// AddRequestToQueue godoc
// @Summary Add Request to Queue
// @Description add a request to the queue for scanning sites
// @Tags Requests
// @Security ApiKeyAuth
// @Param body body proxy.AddRequestToQueueParameters true "Request Details"
// @Success 200
// @Failure 500 {string} string Error
// @Router /requests/queue [post]
func AddRequestToQueue(w http.ResponseWriter, r *http.Request) {
	if defaultConnectionPool == nil {
		initConnectionPool()
	}

	var params AddRequestToQueueParameters
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		fmt.Println("Error decoding JSON: " + err.Error())
		http.Error(w, "Error decoding JSON:"+err.Error(), http.StatusBadRequest)
		return
	}

	requestData, err := base64.StdEncoding.DecodeString(params.Request)
	if err != nil {
		fmt.Println("Error decoding request")
		http.Error(w, "Error decoding request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	httpContext, cancel := context.WithCancel(context.Background())

	request_queue.Increment(params.ScanID)
	requestFinishedChannel := make(chan bool)
	project.ScriptIncrementTotalRequests(params.ScanID)
	errorThrown := false

	go func() {
		request, err := makeRequestToSite(params.SSL, params.Host, requestData, defaultConnectionPool, httpContext)

		if err != nil {
			errorStr := "Error making request to the site: " + err.Error()

			project.ScriptDecrementTotalRequests(params.ScanID)

			if request != nil {
				request.Error = errorStr
			}
			errorThrown = true
		}

		if request != nil {
			request.ScanID = params.ScanID
			request.Payloads = params.Payloads
			request.Record()
		}

		close(requestFinishedChannel)
	}()

	go func() {
		select {
		case <-requestFinishedChannel:
			request_queue.Decrement(params.ScanID)
			if !errorThrown {
				project.ScriptIncrementRequestCount(params.ScanID)
			}
			return
		case _, ok := <-request_queue.Channel(params.ScanID):

			if !ok {
				project.ScriptDecrementRequestCount(params.ScanID)
				cancel()
			}
		}
	}()

	w.Header().Set("Content-Type", "text/text")
	w.Write([]byte("OK"))
}

func bulkRequestWorker(ssl bool, hostname string, scanId string, requestParts []project.InjectOperationRequestPart, payloads <-chan []string, done chan<- bool, httpClient *http.Client) {
	for payloadList := range payloads {
		// check if the scan has been cancelled
		if !request_queue.Contains(scanId) {
			done <- true
			return
		}

		requestData := make([]byte, 0)
		injectI := 0
		differingPayloads := make(map[string]string)

		injectionPointCount := 0
		for _, part := range requestParts {
			if part.Inject {
				injectionPointCount += 1
			}
		}

		if injectionPointCount != len(payloadList) {
			fmt.Printf("Injection point parameter list didn't have the same number of injection points as the request - skipping\n")
			request_queue.Decrement(scanId)
			project.ScriptDecrementTotalRequests(scanId)
			continue
		}

		for _, part := range requestParts {
			if part.Inject {
				payload, _ := base64.StdEncoding.DecodeString(payloadList[injectI])
				origPart, _ := base64.StdEncoding.DecodeString(part.RequestPart)
				if part.RequestPart != payloadList[injectI] {
					differingPayloads[string(origPart)] = string(payload)
				}

				requestData = append(requestData, payload...)
			} else {
				reqData, _ := base64.StdEncoding.DecodeString(part.RequestPart)
				requestData = append(requestData, reqData...)
			}
		}
		req, err := makeRequestToSite(ssl, hostname, requestData, httpClient, nil)

		if err != nil {
			fmt.Println("Error making request: " + err.Error())
		} else {
			jsonPayloads, _ := json.Marshal(differingPayloads)

			req.ScanID = scanId
			req.Payloads = string(jsonPayloads)

			req.Record()
		}

		request_queue.Decrement(scanId)
		project.ScriptDecrementTotalRequests(scanId)
	}
	done <- true
}

// BulkRequestQueue godoc
// @Summary Add Multiple Requests to the Qeueue
// @Description add multiple requests to the queue for scanning sites
// @Tags Requests
// @Security ApiKeyAuth
// @Param body body proxy.BulkRequestQueueParameters true "Request Details"
// @Success 200
// @Failure 500 {string} string Error
// @Router /requests/bulk_queue [post]
func BulkRequestQueue(w http.ResponseWriter, r *http.Request) {
	if defaultConnectionPool == nil {
		initConnectionPool()
	}

	var params BulkRequestQueueParameters
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		fmt.Println("Error decoding JSON: " + err.Error())
		http.Error(w, "Error decoding JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	settings, err := GetSettings()
	if err != nil {
		fmt.Printf("Could not get settings: " + err.Error())
		http.Error(w, "Could not get settings: "+err.Error(), http.StatusBadRequest)
		return
	}

	injectScan := project.InjectFromGUID(params.ScanID)
	script := project.ScriptRunFromGUID(params.ScanID)
	if injectScan == nil && script == nil {
		fmt.Printf("Inject scan or script doesn't exist: %s", params.ScanID)
		http.Error(w, "Could not find inject scan or script: "+params.ScanID, http.StatusBadRequest)
		return
	}

	if injectScan != nil {
		request_queue.Add(injectScan)
	} else if script != nil {
		request_queue.IncrementBy(params.ScanID, len(params.Replacements)+1)
		project.ScriptIncrementTotalRequestsBy(params.ScanID, len(params.Replacements)+1)
	}

	go func() {
		// make the base request
		baseReq := make([]byte, 0)
		for _, part := range params.Request {
			reqData, _ := base64.StdEncoding.DecodeString(part.RequestPart)
			baseReq = append(baseReq, reqData...)
		}
		req, err := makeRequestToSite(params.SSL, params.Host, baseReq, defaultConnectionPool, nil)

		if err != nil {
			fmt.Println("Error making request: " + err.Error())
		} else {
			req.ScanID = params.ScanID
			req.Record()
			request_queue.Decrement(params.ScanID)
		}

		// now create workers to actually send the requests
		payloads := make(chan []string, len(params.Replacements))
		complete := make(chan bool, settings.MaxConnectionsPerHost)
		for i := 0; i < settings.MaxConnectionsPerHost; i++ {
			go bulkRequestWorker(params.SSL, params.Host, params.ScanID, params.Request, payloads, complete, defaultConnectionPool)
		}

		for _, payload := range params.Replacements {
			payloads <- payload
		}

		close(payloads)

		// wait for the workers to finish
		for i := 0; i < settings.MaxConnectionsPerHost; i++ {
			<-complete
		}

		if injectScan != nil {
			injectScan.Record()
		}
	}()

	w.Header().Set("Content-Type", "text/text")
	w.Write([]byte("OK"))
}

func initConnectionPool() {
	defaultConnectionPool = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	updateConnectionPool(defaultConnectionPool, nil)
}

func makeRequestToSite(ssl bool, hostname string, requestData []byte, httpClient *http.Client, httpContext context.Context) (*project.Request, error) {
	requestData = project.CorrectLengthHeaders(requestData)

	b := bytes.NewReader(requestData)
	httpRequest, err := http.ReadRequest(bufio.NewReader(b))

	if err != nil {
		return nil, err
	}

	protocol := "https"
	if !ssl {
		protocol = "http"
	}
	if strings.HasSuffix(hostname, ":443") && protocol == "https" {
		hostname = strings.Replace(hostname, ":443", "", 1)
	}
	if strings.HasSuffix(hostname, ":80") && protocol == "http" {
		hostname = strings.Replace(hostname, ":80", "", 1)
	}
	httpRequest.Host = hostname

	url, err := url.Parse(protocol + "://" + hostname + httpRequest.URL.RequestURI())

	if err != nil {
		return nil, err
	}

	httpRequest.URL = url
	httpRequest.RequestURI = ""

	request, err := project.NewRequestFromHttpWithoutBytes(httpRequest)
	if err != nil {
		return nil, err
	}

	if httpContext != nil {
		httpRequest = httpRequest.WithContext(httpContext)
	}

	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			request.Time = time.Now().Unix()
		},
	}

	httpRequest = httpRequest.WithContext(httptrace.WithClientTrace(httpRequest.Context(), trace))

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		request.Error = "Error making request to site: " + err.Error()
		return request, err
	} else {
		request.HandleResponse(response, nil, false)
	}

	return request, nil
}

func updateConnectionPool(connectionPool *http.Client, clientCert *tls.Certificate) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxConnsPerHost = 2
	transport.MaxIdleConnsPerHost = 2
	connectionPool.Transport = transport

	settings, err := GetSettings()
	if err != nil {
		return
	}

	if settings.Http11UpstreamProxyAddr != "" {
		proxyUrl, err := url.Parse(settings.Http11UpstreamProxyAddr)
		if err != nil {
			fmt.Printf("Error parsing proxy address: %s\n", err.Error())
			return
		}
		transport.Proxy = http.ProxyURL(proxyUrl)
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	if clientCert != nil {
		tlsConfig.Certificates = []tls.Certificate{*clientCert}
	}

	transport.TLSClientConfig = tlsConfig
	transport.MaxConnsPerHost = settings.MaxConnectionsPerHost
}
