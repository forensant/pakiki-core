// This is using the following library
// https://github.com/elazarl/goproxy

// At some point, we may need to refactor this so that H2 will work, but the library does support websockets
// I'm also not sure how/if it handles streaming
//
// Basically, it works well enough for now, but at some point we may need to make significant contributions or a fork
// to the goproxy library in order to support everything we need - including more robust error handling which is exposed to the
// intercept functions.

package proxy

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"dev.forensant.com/pipeline/razor/proximitycore/ca"
	"dev.forensant.com/pipeline/razor/proximitycore/project"
	"github.com/elazarl/goproxy"
)

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

func onRequestReceived(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	requestBytes, err := httputil.DumpRequest(req, true)

	if err != nil {
		fmt.Printf("Error reading body from request\nURL: %s\n", req.URL)
		return req, nil
	}

	request := project.NewRequestFromHttp(req, requestBytes)
	if ctx.Error != nil {
		fmt.Printf("Error: %v", ctx.Error.Error())
		request.Error = ctx.Error.Error()
	}
	request.Record()

	ctx.UserData = request

	return req, nil
}

func onResponseReceived(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}
	bodyBytes, err := io.ReadAll(resp.Body)

	if err != nil {
		fmt.Printf("Could not read response body: %s\n", err)
		return resp
	}

	resp.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var errorToReport = ctx.Error

	if err != nil {
		fmt.Printf("Error reading body from response\nURL: %s\n", resp.Request.URL)
		errorToReport = err
	}

	request, typecastOK := ctx.UserData.(*project.Request)
	if !typecastOK {
		fmt.Printf("Could not convert the response's user context to a request\n")
		errorToReport = err
	}

	request.HandleResponse(resp)

	if errorToReport != nil {
		request.Error = errorToReport.Error()
	}

	request.Record()

	return resp
}

func startHttp11BrowserProxy(wg *sync.WaitGroup, settings *ProxySettings) (*http.Server, error) {
	certificateRecord, err := ca.CertificateForDomain("CA Root")

	if err != nil {
		fmt.Printf("Error getting root certificate: %s\n", err.Error())
		return nil, err
	}

	setCA([]byte(certificateRecord.CertificatePEM), certificateRecord.PrivateKey)
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

	proxy.OnRequest().DoFunc(onRequestReceived)
	proxy.OnResponse().DoFunc(onResponseReceived)

	proxy.Verbose = false

	srv := &http.Server{
		Handler: proxy,
	}

	log.Printf("Starting proxy listener: %s\n", settings.Http11ProxyAddr)
	listener, err := net.Listen("tcp4", settings.Http11ProxyAddr)
	if err != nil {
		return nil, err
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
