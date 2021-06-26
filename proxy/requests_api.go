package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"dev.forensant.com/pipeline/razor/proximitycore/project"
)

// MakeRequestParameters contains the parameters which are parsed to the Make Request API call
type MakeRequestParameters struct {
	RequestBase64 string `json:"request" example:"<base64 encoded request>"`
	Host          string `json:"host"`
	SSL           bool   `json:"ssl"`
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

	request, err := makeRequestToSite(params.SSL, params.hostWithPort(), params.Request(), http.DefaultClient)
	if err != nil {
		http.Error(w, "Cannot make request to site: "+err.Error(), http.StatusInternalServerError)
		return
	}

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
	Request string `json:"request" example:"<base64 encoded request>"`
	Host    string `json:"host"`
	SSL     bool   `json:"ssl"`
	ScanID  string `json:"scan_id"`
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
func AddRequestToQueue(w http.ResponseWriter, r *http.Request, httpClient *http.Client) {
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

	request, err := makeRequestToSite(params.SSL, params.Host, requestData, httpClient)
	if err != nil {
		errorStr := "Error making request to the site: " + err.Error()
		if request == nil {
			injectOp := project.InjectFromGUID(params.ScanID)
			if injectOp != nil {
				injectOp.TotalRequestCount -= 1
				injectOp.UpdateAndRecord()
			}

			fmt.Println(errorStr)
			return
		}
		request.Error = errorStr
	}

	request.ScanID = params.ScanID
	request.Record()

	w.Header().Set("Content-Type", "text/text")
	w.Write([]byte("OK"))
}

func makeRequestToSite(ssl bool, hostname string, requestData []byte, httpClient *http.Client) (*project.Request, error) {
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
		return request, err
	}

	// set the proxy, if necessary
	settings, err := GetSettings()
	if err != nil {
		return request, err
	}

	if settings.Http11UpstreamProxyAddr == "" {
		httpClient.Transport = http.DefaultTransport
	} else {
		proxyUrl, err := url.Parse(settings.Http11UpstreamProxyAddr)
		if err != nil {
			return request, err
		}

		httpClient.Transport = &http.Transport{
			Proxy:           http.ProxyURL(proxyUrl),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	response, err := httpClient.Do(httpRequest)
	if err != nil {
		request.Error = "Error making request to site: " + err.Error()
	} else {
		request.HandleResponse(response)
	}

	return request, nil
}
