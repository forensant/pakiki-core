// This is used by the frontends to render previews of the content

package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/pipeline/goproxy"
	"github.com/pipeline/proximity-core/pkg/project"
)

func onPreviewProxyRequestReceived(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	var url_string = req.URL.Scheme + "://" + req.URL.Hostname()
	if (req.URL.Port() != "443" && req.URL.Scheme == "https") || (req.URL.Port() != "80" && req.URL.Scheme == "http") {
		if req.URL.Port() != "" {
			url_string += ":" + req.URL.Port()
		}
	}
	url_string += req.URL.Path
	if req.URL.RawQuery != "" {
		url_string += "?" + req.URL.RawQuery
	}

	responseBytes, err := project.GetLastResponseOfURL(url_string)

	errResp := goproxy.NewResponse(req,
		goproxy.ContentTypeText, http.StatusInternalServerError,
		"Error occurred when fetching response from the proxy database.")

	if err != nil || responseBytes == nil {
		fmt.Printf("Error gathering preview proxy response from request URL: %s, error: %s\n", req.URL, err)
		return req, errResp
	}

	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(responseBytes)), req)

	if err != nil {
		fmt.Printf("Error gathering preview proxy response from request URL: %s, error: %s\n", req.URL, err)
		return req, errResp
	}

	return req, resp
}

func StartHttpPreviewProxy(listener net.Listener) error {
	certificateRecord, err := getRootCertificate()

	if err != nil {
		fmt.Printf("Error getting root certificate: %s\n", err.Error())
		return err
	}

	setCA(certificateRecord.CertificatePEM, certificateRecord.PrivateKey)
	proxy := goproxy.NewProxyHttpServer()

	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.OnRequest().DoFunc(onPreviewProxyRequestReceived)

	proxy.Verbose = false

	srv := &http.Server{
		Handler: proxy,
	}

	go func() {
		err := srv.Serve(listener)
		if err != http.ErrServerClosed {
			log.Printf("HTTP/1.1 Proxy Listen and Serve failure for the preview proxy: %v", err)
		}
	}()

	return nil
}
