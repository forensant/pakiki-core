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
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/pipeline/proximity-core/internal/scripting"
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

var getRequestWithURLInject = []project.InjectOperationRequestPart{
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

var getRequestWithURLAndBodyInject = []project.InjectOperationRequestPart{
	{
		RequestPart: base64.StdEncoding.EncodeToString([]byte("POST /")),
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
	{
		RequestPart: base64.StdEncoding.EncodeToString([]byte("a")),
		Inject:      true,
	},
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

func setHost(injectOperation project.InjectOperation, hostname string) project.InjectOperation {
	for i, reqPart := range injectOperation.Request {
		b, _ := base64.StdEncoding.DecodeString(reqPart.RequestPart)
		b = bytes.ReplaceAll(b, []byte("HOST"), []byte(hostname))
		injectOperation.Request[i].RequestPart = base64.StdEncoding.EncodeToString(b)
	}

	return injectOperation
}

func TestRunInjectScan(t *testing.T) {
	tests := []injectTestCase{
		{
			project.InjectOperation{
				Request:     getRequestWithURLInject,
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
				Request:     getRequestWithURLInject,
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
		{
			project.InjectOperation{
				Request:     getRequestWithURLAndBodyInject,
				IterateFrom: 1,
				IterateTo:   3,
			},
			[]expectedRequestResponse{
				{"/0", "a", 0}, // base request
				{"/1", "a", 0},
				{"/2", "a", 0},
				{"/0", "1", 0},
				{"/0", "2", 0},
			},
		},
	}

	proximityServerMux := http.NewServeMux()
	proximityServerMux.HandleFunc("/inject_operation/run", RunInjection)
	proximityServerMux.HandleFunc("/requests/bulk_queue", BulkRequestQueue)
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

		test.injectOperation = setHost(test.injectOperation, host)

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
				}
			case <-time.After(5 * time.Second):
				stop = true
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

func TestCancelInjectScan(t *testing.T) {
	op := project.InjectOperation{
		Request:     getRequestWithURLInject,
		IterateFrom: 1,
		IterateTo:   100,
	}

	proximityServerMux := mux.NewRouter()
	proximityServerMux.HandleFunc("/inject_operation/run", RunInjection)
	proximityServerMux.HandleFunc("/requests/bulk_queue", BulkRequestQueue)
	proximityServerMux.HandleFunc("/scripts/{guid}/cancel", scripting.CancelScript)
	proximityServerMux.HandleFunc("/requests", project.GetRequests)
	proximityServerMux.HandleFunc("/inject_operations/{guid}", project.GetInjectOperation)

	s := httptest.NewServer(proximityServerMux)
	defer s.Close()

	testServerMux := http.NewServeMux()
	reqChan := make(chan bool)
	receivedReqChan := make(chan bool)

	testServerMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		receivedReqChan <- true
		<-reqChan
		w.Write([]byte("OK"))
	})

	srv := &http.Server{
		Handler: testServerMux,
	}

	listener, _ := net.Listen("tcp4", "127.0.0.1:0")
	go srv.Serve(listener)
	host := listener.Addr().String()

	op.Host = host
	op.SSL = false

	op = setHost(op, host)

	reqBody, _ := json.Marshal(op)
	resp, err := http.Post(s.URL+"/inject_operation/run", "application/json", bytes.NewReader(reqBody))

	if err != nil {
		t.Fatalf("Error running inject scan: %s\n", err.Error())
	}

	var returnedOp project.InjectOperation
	err = json.NewDecoder(resp.Body).Decode(&returnedOp)
	if err != nil {
		t.Fatalf("Error parsing inject operation: %s\n", err.Error())
		return
	}

	observedReqCount := 0
	for {
		stop := false
		select {
		case <-receivedReqChan:
			observedReqCount += 1
			if observedReqCount == 2 {
				stop = true
			}
		case <-time.After(5 * time.Second):
			stop = true
		}

		if stop {
			break
		}
	}

	http.Post(s.URL+"/scripts/"+returnedOp.GUID+"/cancel", "application/json", strings.NewReader(""))

	// let the requests through
	reqChan <- true
	reqChan <- true

	// less than ideal, but I want to allow a bit of extra time in case extra requests come through
	// (rather than subscribing to the notifications)
	time.Sleep(50 * time.Millisecond)

	// get the requests and assert there's only been two
	resp, _ = http.Get(s.URL + "/requests?scanid=" + returnedOp.GUID)
	var returnedReqs []project.Request
	json.NewDecoder(resp.Body).Decode(&returnedReqs)
	if len(returnedReqs) != 2 {
		t.Errorf("Expected two requests to have been returned (had %d)\n", len(returnedReqs))
	}

	// assert that the inject operation was complete
	resp, _ = http.Get(s.URL + "/inject_operations/" + returnedOp.GUID)
	json.NewDecoder(resp.Body).Decode(&returnedOp)
	if returnedOp.PercentCompleted != 100 {
		t.Errorf("Inject operation was not marked as complete\n")
	}

	if returnedOp.RequestsMadeCount < returnedOp.TotalRequestCount {
		t.Errorf("Requests made (%d) isn't greater than or equal to total requests (%d)\n", returnedOp.RequestsMadeCount, returnedOp.TotalRequestCount)
	}

	err = srv.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Could not shut down mock HTTP server: %s\n", err.Error())
	}
}
