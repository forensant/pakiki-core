package project

import (
	"encoding/base64"
	"testing"
)

func TestInjectPointIdentification(t *testing.T) {
	requests := []struct {
		name    string
		request string
		parts   []InjectOperationRequestPart
	}{
		{"No parameters", "GET /test HTTP/1.1\r\nHost: example.com\r\n\r\n", []InjectOperationRequestPart{
			{RequestPart: "GET /test HTTP/1.1\r\nHost: example.com\r\n\r\n", Inject: false},
		}},
		{"No parameters with values", "GET /test?abc HTTP/1.1\r\nHost: example.com\r\n\r\n", []InjectOperationRequestPart{
			{RequestPart: "GET /test?abc HTTP/1.1\r\nHost: example.com\r\n\r\n", Inject: false},
		}},
		{"Dangling parameter", "GET /test?1=2&abc HTTP/1.1\r\nHost: example.com\r\n\r\n", []InjectOperationRequestPart{
			{RequestPart: "GET /test?1=", Inject: false},
			{RequestPart: "2", Inject: true},
			{RequestPart: "&abc HTTP/1.1\r\nHost: example.com\r\n\r\n", Inject: false},
		}},
		{"One URL parameter", "GET /test?test=a HTTP/1.1\r\nHost: example.com\r\n\r\n", []InjectOperationRequestPart{
			{RequestPart: "GET /test?test=", Inject: false},
			{RequestPart: "a", Inject: true},
			{RequestPart: " HTTP/1.1\r\nHost: example.com\r\n\r\n", Inject: false},
		}},
		{"Two URL parameters", "GET /test?test=abc&example=xyz HTTP/1.1\r\nHost: example.com\r\n\r\n", []InjectOperationRequestPart{
			{RequestPart: "GET /test?test=", Inject: false},
			{RequestPart: "abc", Inject: true},
			{RequestPart: "&example=", Inject: false},
			{RequestPart: "xyz", Inject: true},
			{RequestPart: " HTTP/1.1\r\nHost: example.com\r\n\r\n", Inject: false},
		}},
		{"One URL parameter, unknown content type", "GET /test?test=abc HTTP/1.1\r\nContent-Type: text/json\r\nHost: example.com\r\n\r\n{'Test':'ABC'}", []InjectOperationRequestPart{
			{RequestPart: "GET /test?test=", Inject: false},
			{RequestPart: "abc", Inject: true},
			{RequestPart: " HTTP/1.1\r\nContent-Type: text/json\r\nHost: example.com\r\n\r\n{'Test':'ABC'}", Inject: false},
		}},
		{"No URL parameters, one body parameter", "GET /test HTTP/1.1\r\nContent-Type: application/x-www-form-urlencoded\r\nHost: example.com\r\n\r\ntest=abc", []InjectOperationRequestPart{
			{RequestPart: "GET /test HTTP/1.1\r\nContent-Type: application/x-www-form-urlencoded\r\nHost: example.com\r\n\r\ntest=", Inject: false},
			{RequestPart: "abc", Inject: true},
		}},
		{"One URL parameter, two body parameters", "GET /test?test=abc HTTP/1.1\r\nContent-Type: application/x-www-form-urlencoded\r\nHost: example.com\r\n\r\ntest=abc&other=xyz", []InjectOperationRequestPart{
			{RequestPart: "GET /test?test=", Inject: false},
			{RequestPart: "abc", Inject: true},
			{RequestPart: " HTTP/1.1\r\nContent-Type: application/x-www-form-urlencoded\r\nHost: example.com\r\n\r\ntest=", Inject: false},
			{RequestPart: "abc", Inject: true},
			{RequestPart: "&other=", Inject: false},
			{RequestPart: "xyz", Inject: true},
		}},
	}

	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			actualParts := findInjectPoints([]byte(request.request))

			if len(actualParts) != len(request.parts) {
				t.Errorf("Expected %d parts but got %d", len(request.parts), len(actualParts))
			}

			for i, part := range actualParts {
				b, _ := base64.StdEncoding.DecodeString(part.RequestPart)
				actualPart := string(b)

				if i < len(request.parts) {
					if actualPart != request.parts[i].RequestPart {
						t.Errorf("Expected part '%s' but got '%s'", request.parts[i].RequestPart, actualPart)
					}

					if part.Inject != request.parts[i].Inject {
						t.Errorf("Expected part '%s' to have inject set to: %t but got %t", actualPart, part.Inject, request.parts[i].Inject)
					}
				}
			}
		})
	}
}
