package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/forensant/pakiki-core/internal/scripting"
	"github.com/forensant/pakiki-core/pkg/project"
)

func TestScriptRequestAPIs(t *testing.T) {
	baseScript := `
import base64
import json

req_parts = [
	{
		'Inject': False,
		'RequestPart': base64.b64encode(b'GET /').decode('utf-8')
	},
	{
		'Inject': True,
		'RequestPart': base64.b64encode(b'a').decode('utf-8')
	},
	{
		'Inject': False,
		'RequestPart': base64.b64encode(b' HTTP/1.1\r\nHost: HOSTNAME\r\n\r\n').decode('utf-8')
	}
]

req = InjectableRequest('HOSTNAME', False, json.dumps(req_parts))

`

	tests := []struct {
		name   string
		script string
	}{
		{
			name: "Bulk queue",
			script: baseScript + `
req.bulk_queue([['Yg=='], ['Yw==']])
`,
		},
		{
			name: "Manual queue",
			script: baseScript + `
req.replace_injection_point(0, 'a')
req.queue()
req.replace_injection_point(0, 'b')
req.queue()
req.replace_injection_point(0, 'c')
req.queue()
`,
		},
	}

	pakikiServerMux := http.NewServeMux()
	pakikiServerMux.HandleFunc("/scripts/run", project.RunScript)
	pakikiServerMux.HandleFunc("/requests/bulk_queue", BulkRequestQueue)
	pakikiServerMux.HandleFunc("/requests/queue", AddRequestToQueue)
	s := httptest.NewServer(pakikiServerMux)
	defer s.Close()

	for _, test := range tests {
		testServerMux := http.NewServeMux()
		reqChan := make(chan bool)

		tc := injectTestCase{
			responses: []expectedRequestResponse{
				{"/a", "", 0}, // base request
				{"/b", "", 0},
				{"/c", "", 0},
			},
		}

		testServerMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			checkRequestExists(&tc, t, w, r)
			reqChan <- true
		})

		srv := &http.Server{
			Handler: testServerMux,
		}

		listener, _ := net.Listen("tcp4", "127.0.0.1:0")
		go srv.Serve(listener)
		host := listener.Addr().String()

		scriptParams := project.RunScriptParameters{
			Code: []scripting.ScriptCode{{
				Code:       strings.ReplaceAll(test.script, "HOSTNAME", host),
				Filename:   "script.py",
				MainScript: true,
			}},
			Title:       test.name,
			Development: true,
		}

		reqBody, _ := json.Marshal(scriptParams)
		_, err := http.Post(s.URL+"/scripts/run", "application/json", bytes.NewReader(reqBody))

		if err != nil {
			t.Fatalf("Error running inject scan: %s\n", err.Error())
		}

		observedReqCount := 0
		for {
			stop := false
			select {
			case <-reqChan:
				observedReqCount += 1
				if observedReqCount >= len(tc.responses) {
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

		for _, rr := range tc.responses {
			if rr.count != 1 {
				t.Errorf("Expected URL \"%s\" with body \"%s\" to have been encountered exactly once", rr.path, rr.body)
			}
		}
	}
}
