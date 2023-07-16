// This is using a forked and slightly modified version of the following library:
// https://github.com/elazarl/goproxy
//
// Basically, it works well enough for now, but at some point we may need to make significant contributions
// in order to support everything we need - including more robust error handling which is exposed to the
// intercept functions, better control of H2/H3, etc.

package proxy

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/forensant/goproxy"
	"github.com/forensant/pakiki-core/pkg/project"
	"github.com/google/uuid"
)

var passthroughUrls = []string{
	"https://accounts.google.com:443/ListAccounts?gpsia=1&source=ChromiumBrowser",
	"https://update.googleapis.com:443/service/update2/json",
	"http://edgedl.me.gvt1.com:80/edgedl/release2/chrome_component/",
	"http://edgedl.me.gvt1.com/edgedl/release2/chrome_component/",
	"https://www.google.com:443/complete/search?client=chrome-omni",
	"https://optimizationguide-pa.googleapis.com:443/v1:GetModels",
}

func onHttp11RequestReceived(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	if requestOnPassthroughList(req) {
		return req, nil
	}

	requestBytes, err := httputil.DumpRequestOut(req, true)

	if err != nil {
		fmt.Printf("Error reading body from request\nURL: %s\n", req.URL)
		return req, nil
	}

	request := project.NewRequestFromHttp(req, requestBytes)
	var response *http.Response

	hookResp := project.RunHooksOnRequest(request, requestBytes)
	hookRun := false

	if hookResp.Modified {
		<-hookResp.ResponseReady

		if !bytes.Equal(hookResp.ModifiedRequest, requestBytes) {
			requestBytes = hookResp.ModifiedRequest

			// in case we don't intercept
			modifiedRequest := bufio.NewReader(io.NopCloser(bytes.NewBuffer(requestBytes)))
			oldUrl := req.URL
			req, err = http.ReadRequest(modifiedRequest)
			if err == nil {
				req.URL.Scheme = oldUrl.Scheme
				req.URL.Host = oldUrl.Host
			} else {
				response = goproxy.NewResponse(req,
					goproxy.ContentTypeText, http.StatusBadRequest,
					"Pākiki Proxy could not read the modified request")
				request.Error = "Could not read modified request: " + err.Error()
			}

			hookRun = true

			if !interceptSettings.BrowserToServer {
				// if we're not doing other modifications, update the request
				dataPacket := project.DataPacket{
					Data:        requestBytes,
					Direction:   "Request",
					Modified:    true,
					GUID:        uuid.NewString(),
					Time:        time.Now().Unix(),
					StartOffset: 0,
					EndOffset:   int64(len(requestBytes)) - 1,
				}

				request.DataPackets = append(request.DataPackets, dataPacket)
			}
		}
	}

	if interceptSettings.BrowserToServer {
		interceptedRequest := interceptRequest(request, "", "browser_to_server", requestBytes, hookRun)
		<-interceptedRequest.ResponseReady

		modifiedRequestData := request.GetRequestResponseData("Request", true)
		if len(modifiedRequestData) == 0 {
			modifiedRequestData = request.GetRequestResponseData("Request", false)
		}

		modifiedRequest := bufio.NewReader(io.NopCloser(bytes.NewBuffer(modifiedRequestData)))
		forward := false

		switch interceptedRequest.RequestAction {
		case "forward":
			forward = true

		case "forward_and_intercept_response":
			forward = true
			request.InterceptResponse = true

		default:
			response = goproxy.NewResponse(req,
				goproxy.ContentTypeText, http.StatusForbidden,
				"Request dropped by Pākiki Proxy")
			response.ProtoMajor = req.ProtoMajor
			response.ProtoMinor = req.ProtoMinor
		}

		if forward {
			oldUrl := req.URL
			oldReq := req
			req, err = http.ReadRequest(modifiedRequest)
			if err == nil {
				req.URL.Scheme = oldUrl.Scheme
				req.URL.Host = oldUrl.Host
				request.RequestSize = int64(len(modifiedRequestData))
			} else {
				req = oldReq
				response = goproxy.NewResponse(req,
					goproxy.ContentTypeText, http.StatusBadRequest,
					"Pākiki Proxy could not read the modified request")
				response.ProtoMajor = req.ProtoMajor
				response.ProtoMinor = req.ProtoMinor
				request.Error = "Could not read modified request: " + err.Error() + ", original request sent"
			}
		}

		removeInterceptedRequest(interceptedRequest)
	}

	if ctx.Error != nil && request.Error == "" {
		fmt.Printf("Error: %v", ctx.Error.Error())
		request.Error = ctx.Error.Error()
	}

	request.Record()

	ctx.UserData = request

	return req, response
}

func onHttp11ResponseReceived(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	var errorToReport = ctx.Error

	request, typecastOK := ctx.UserData.(*project.Request)
	if !typecastOK {
		fmt.Printf("Could not convert the response's user context to a request\n")
		errorToReport = errors.New("could not convert the response's user context to a request")
	}

	if resp == nil || resp.Body == nil {
		if errorToReport != nil && request != nil {
			request.Error = errorToReport.Error()
			request.Record()
		}
		return resp
	}

	if request != nil {
		can_intercept := request.HandleResponse(resp, ctx, true)

		if can_intercept {
			responseBytes := request.GetRequestResponseData("Response", false)
			shouldIntercept := (interceptSettings.ServerToBrowser || request.InterceptResponse)

			hookResp := project.RunHooksOnResponse(request, responseBytes)

			hasBeenModified := false
			hookRun := false

			if hookResp.Modified {
				<-hookResp.ResponseReady

				if !bytes.Equal(hookResp.ModifiedRequest, responseBytes) {
					responseBytes = hookResp.ModifiedRequest
					hasBeenModified = true
					hookRun = true

					if !shouldIntercept {
						// if we're not doing other modifications, update the request
						dataPacket := project.DataPacket{
							Data:        responseBytes,
							Direction:   "Response",
							Modified:    true,
							GUID:        uuid.NewString(),
							Time:        time.Now().Unix(),
							StartOffset: 0,
							EndOffset:   int64(len(responseBytes)) - 1,
						}

						request.DataPackets = append(request.DataPackets, dataPacket)
					}
				}
			}

			if shouldIntercept {
				interceptedResponse := interceptRequest(request, "", "server_to_browser", responseBytes, hookRun)
				<-interceptedResponse.ResponseReady

				responseBytes = request.GetRequestResponseData("Response", true)
				if len(responseBytes) == 0 {
					responseBytes = request.GetRequestResponseData("Response", false)
				}
				hasBeenModified = true

				removeInterceptedRequest(interceptedResponse)
			}

			if hasBeenModified {
				responseBytes = project.CorrectLengthHeaders(responseBytes)
				modifiedResponse := bufio.NewReader(io.NopCloser(bytes.NewBuffer(responseBytes)))
				newResponse, err := http.ReadResponse(modifiedResponse, resp.Request)

				if err != nil {
					request.Error = "Error reading modified response: " + err.Error()
				} else {
					resp = newResponse
				}
			}
		}

		if errorToReport != nil {
			request.Error = errorToReport.Error()
		}

		request.Record()
	}

	return resp
}

func onWebsocketPacketReceived(data []byte, direction goproxy.WebsocketDirection, opcode string, ctx *goproxy.ProxyCtx) []byte {
	request, typecastOK := ctx.UserData.(*project.Request)
	if !typecastOK {
		fmt.Printf("Could not convert the response's user context to a request\n")
		return data
	}

	if request == nil {
		fmt.Printf("Cannot find original request corresponding to websocket packet\n")
		return data
	}

	if request.Protocol != "Websocket" {
		// this is the initial packet on the websocket, create the new request object
		newRequest := &project.Request{
			Protocol:            "Websocket",
			URL:                 request.URL,
			Verb:                request.Verb,
			ResponseStatusCode:  request.ResponseStatusCode,
			ResponseContentType: request.ResponseContentType,
			ScanID:              request.ScanID,
			SiteMapPathID:       request.SiteMapPathID,
			SiteMapPath:         request.SiteMapPath,
		}

		request = newRequest
		ctx.UserData = request
	}

	directionString := "Request"
	if direction == goproxy.ServerToClient {
		directionString = "Response"
	}

	dataPacketGuid := uuid.New().String()

	dataPacket := project.DataPacket{
		GUID:        dataPacketGuid,
		Time:        time.Now().Unix(),
		Data:        data,
		Direction:   directionString,
		Modified:    false,
		DisplayData: "{\"opcode\": \"" + opcode + "\"}",
	}

	request.DataPackets = append(request.DataPackets, dataPacket)
	request.Record()

	if (interceptSettings.BrowserToServer && direction == goproxy.ClientToServer) ||
		(interceptSettings.ServerToBrowser && direction == goproxy.ServerToClient) {

		interceptDirection := "browser_to_server"
		if direction == goproxy.ServerToClient {
			interceptDirection = "server_to_browser"
		}

		interceptedRequest := interceptRequest(request, dataPacketGuid, interceptDirection, data, false)
		<-interceptedRequest.ResponseReady

		for _, dataPacket := range request.DataPackets {
			if dataPacket.GUID == dataPacketGuid && dataPacket.Modified {
				data = dataPacket.Data
				request.Record()
			}
		}

		removeInterceptedRequest(interceptedRequest)
	}

	return data
}

func requestOnPassthroughList(req *http.Request) bool {
	urlStr := req.URL.String()
	for _, passthroughUrl := range passthroughUrls {
		if strings.HasPrefix(urlStr, passthroughUrl) {
			return true
		}
	}
	return false
}

func setCA(caCert, caKey []byte) error {
	goproxyCa, err := tls.X509KeyPair(caCert, caKey)
	if err != nil {
		return err
	}
	if goproxyCa.Leaf, err = x509.ParseCertificate(goproxyCa.Certificate[0]); err != nil {
		return err
	}
	goproxy.GoproxyCa = goproxyCa
	goproxy.OkConnect = &goproxy.ConnectAction{Action: goproxy.ConnectAccept, TLSConfig: goproxy.TLSConfigFromCA(&goproxyCa)}
	goproxy.MitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(&goproxyCa)}
	goproxy.HTTPMitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectHTTPMitm, TLSConfig: goproxy.TLSConfigFromCA(&goproxyCa)}
	goproxy.RejectConnect = &goproxy.ConnectAction{Action: goproxy.ConnectReject, TLSConfig: goproxy.TLSConfigFromCA(&goproxyCa)}
	return nil
}

func startHttp11BrowserProxy(wg *sync.WaitGroup, settings *ProxySettings) (*http.Server, error) {
	certificateRecord, err := getRootCertificate()

	if err != nil {
		fmt.Printf("Error getting root certificate: %s\n", err.Error())
		return nil, err
	}

	setCA(certificateRecord.CertificatePEM, certificateRecord.PrivateKey)
	proxy := goproxy.NewProxyHttpServer()

	if settings.Http11UpstreamProxyAddr != "" {
		proxy.Tr.Proxy = func(req *http.Request) (*url.URL, error) {
			var upstreamProxy = settings.Http11UpstreamProxyAddr
			if !strings.Contains(upstreamProxy, "://") {
				upstreamProxy = "http://" + upstreamProxy
			}
			return url.Parse(upstreamProxy)
		}
	}

	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	proxy.OnRequest().DoFunc(onHttp11RequestReceived)
	proxy.OnResponse().DoFunc(onHttp11ResponseReceived)
	proxy.AddWebsocketHandler(onWebsocketPacketReceived)

	proxy.Verbose = false

	srv := &http.Server{
		Handler: proxy,
	}

	log.Printf("Starting proxy listener: %s\n", settings.Http11ProxyAddr)
	listener, err := net.Listen("tcp4", settings.Http11ProxyAddr)
	if err != nil {
		error_str := err.Error()
		if strings.Contains(error_str, "address already in use") {
			error_str = "port already in use"
		}

		return nil, errors.New(error_str + " (" + settings.Http11ProxyAddr + ")")
	}

	go func() {
		defer wg.Done()
		err := srv.Serve(listener)
		if err != http.ErrServerClosed {
			log.Printf("HTTP/1.1 Proxy Listen and Serve failure: %v", err)
		}
	}()

	return srv, nil
}
