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
	"sync"
	"time"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
	"dev.forensant.com/pipeline/razor/proximitycore/proxy/request_queue"
)

var defaultConnectionPool *http.Client
var updateInjectOperationMutex sync.Mutex

// MakeRequestParameters contains the parameters which are parsed to the Make Request API call
type MakeRequestParameters struct {
	RequestBase64 string `json:"request" example:"<base64 encoded request>"`
	Host          string `json:"host"`
	SSL           bool   `json:"ssl"`
	ScanID        string `json:"scan_id"`
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
// @Param default body proxy.MakeRequestParameters true "Make Request Parameters in JSON format"
// @Success 200 {string} string Message
// @Failure 400 {string} string Error
// @Failure 500 {string} string Error
// @Router /proxy/make_request [post]
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
	updateConnectionPool(httpClient)

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

// TODO: These are the same as the parameters above
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
// @Param default body proxy.AddRequestToQueueParameters true "Request Details"
// @Success 200
// @Failure 500 {string} string Error
// @Router /proxy/add_request_to_queue [post]
func AddRequestToQueue(w http.ResponseWriter, r *http.Request) {
	var params AddRequestToQueueParameters
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		fmt.Println("Error decoding JSON")
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

			updateInjectOperationMutex.Lock()
			injectOp := project.InjectFromGUID(params.ScanID)

			if injectOp != nil {
				injectOp.TotalRequestCount -= 1
				injectOp.UpdateAndRecord()
			}
			project.ScriptDecrementTotalRequests(params.ScanID)

			updateInjectOperationMutex.Unlock()

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

func initConnectionPool() {
	defaultConnectionPool = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	updateConnectionPool(defaultConnectionPool)
}

func makeRequestToSite(ssl bool, hostname string, requestData []byte, httpClient *http.Client, httpContext context.Context) (*project.Request, error) {
	requestData = project.CorrectLengthHeaders(requestData)

	b := bytes.NewReader(requestData)
	httpRequest, err := http.ReadRequest(bufio.NewReader(b))

	if err != nil {
		return nil, err
	}

	httpRequest.Host = hostname
	protocol := "https"
	if !ssl {
		protocol = "http"
	}
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
		request.HandleResponse(response)
	}

	return request, nil
}

func updateConnectionPool(connectionPool *http.Client) {
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
			return
		}

		transport.Proxy = http.ProxyURL(proxyUrl)
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if settings.MaxConnectionsPerHost == 0 {
		settings.MaxConnectionsPerHost = 2
	}
	transport.MaxConnsPerHost = settings.MaxConnectionsPerHost
}
