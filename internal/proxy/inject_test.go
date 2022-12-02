package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/pipeline/proximity-core/internal/testing_init"
	"github.com/pipeline/proximity-core/pkg/project"
)

type injectTestCase struct {
	injectOperation project.InjectOperation
	responses       []expectedRequestResponse
}

type expectedRequestResponse struct {
	path  string
	body  string
	count int
}

func checkRequestExists(testCase *injectTestCase, t *testing.T, w http.ResponseWriter, r *http.Request) {
	found := false
	b, _ := ioutil.ReadAll(r.Body)
	body := string(b)

	for i, resp := range testCase.responses {
		if resp.path == r.RequestURI && resp.body == body {
			testCase.responses[i].count += 1
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Request was not expected - URI: %s, Body: %s\n", r.RequestURI, body)
	} else {
		w.Write([]byte("OK"))
	}
}

func setHost(testCase injectTestCase, hostname string) injectTestCase {
	for i, reqPart := range testCase.injectOperation.Request {
		b, _ := base64.StdEncoding.DecodeString(reqPart.RequestPart)
		b = bytes.ReplaceAll(b, []byte("HOST"), []byte(hostname))
		testCase.injectOperation.Request[i].RequestPart = base64.StdEncoding.EncodeToString(b)
	}

	return testCase
}

func TestRunInjectScan(t *testing.T) {
	getRequest := []project.InjectOperationRequestPart{
		{
			RequestPart: base64.StdEncoding.EncodeToString([]byte("GET /")),
			Inject:      false,
		},
		{
			RequestPart: base64.StdEncoding.EncodeToString([]byte("0")),
			Inject:      true,
		},
		{
			RequestPart: base64.StdEncoding.EncodeToString([]byte(" HTTP/1.1\r\nHost: HOST\r\nUser-Agent: Golang\r\n\r\n")),
			Inject:      false,
		},
	}

	tests := []injectTestCase{
		{
			project.InjectOperation{
				Request:     getRequest,
				IterateFrom: 1,
				IterateTo:   3,
			},
			[]expectedRequestResponse{
				{"/0", "", 0}, // base request
				{"/1", "", 0},
				{"/2", "", 0},
			},
		},
		{
			project.InjectOperation{
				Request:     getRequest,
				IterateFrom: 1,
				IterateTo:   2,
				CustomPayloads: []string{
					"test",
					"abc",
				},
			},
			[]expectedRequestResponse{
				{"/0", "", 0}, // base request
				{"/1", "", 0},
				{"/test", "", 0},
				{"/abc", "", 0},
			},
		},
	}

	proximityServerMux := http.NewServeMux()
	proximityServerMux.HandleFunc("/inject_operation/run", RunInjection)
	proximityServerMux.HandleFunc("/requests/queue", AddRequestToQueue)
	s := httptest.NewServer(proximityServerMux)
	defer s.Close()

	for _, test := range tests {
		testServerMux := http.NewServeMux()
		reqChan := make(chan bool)

		testServerMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			checkRequestExists(&test, t, w, r)
			reqChan <- true
		})

		srv := &http.Server{
			Handler: testServerMux,
		}

		listener, _ := net.Listen("tcp4", "127.0.0.1:0")
		go srv.Serve(listener)
		host := listener.Addr().String()

		test.injectOperation.Host = host
		test.injectOperation.SSL = false

		test = setHost(test, host)

		reqBody, _ := json.Marshal(test.injectOperation)
		_, err := http.Post(s.URL+"/inject_operation/run", "application/json", bytes.NewReader(reqBody))

		if err != nil {
			t.Fatalf("Error running inject scan: %s\n", err.Error())
		}

		observedReqCount := 0
		for {
			stop := false
			select {
			case <-reqChan:
				observedReqCount += 1
				if observedReqCount >= len(test.responses) {
					stop = true
					break
				}
			case <-time.After(5 * time.Second):
				stop = true
				break
			}

			if stop {
				break
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Second*10))
		defer cancel()

		err = srv.Shutdown(ctx)
		if err != nil {
			t.Fatalf("Could not shut down mock HTTP server: %s\n", err.Error())
		}

		for _, rr := range test.responses {
			if rr.count != 1 {
				t.Errorf("Expected URL \"%s\" with body \"%s\" to have been encountered exactly once", rr.path, rr.body)
			}
		}
	}
}
