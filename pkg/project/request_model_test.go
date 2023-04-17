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

func TestURLScoping(t *testing.T) {
	scopes := []struct {
		name           string
		entries        []ScopeEntry
		urlsInScope    []string
		urlsOutOfScope []string
	}{
		{
			"No entries",
			[]ScopeEntry{},
			[]string{"https://example.com", "https://example.com/test", "https://test.com/test?abc=123"},
			[]string{},
		},
		{
			"Single entry",
			[]ScopeEntry{
				{
					Prefix:         "https://example.com",
					IncludeInScope: true,
				},
			},
			[]string{"https://example.com", "https://example.com/test", "https://example.com/test?abc=123"},
			[]string{"https://test.com", "https://test.com/test", "https://test.com/test?abc=123"},
		},
		{
			"Single entry, host only",
			[]ScopeEntry{
				{
					HostRegex:      "^example\\.com$",
					IncludeInScope: true,
				},
			},
			[]string{"https://example.com", "https://example.com/test", "https://example.com/test?abc=123"},
			[]string{"https://test.com", "https://test.com/test", "https://test.com/test?abc=123"},
		},
		{
			"Mixed entries, only includes",
			[]ScopeEntry{
				{
					Prefix:         "https://example.com",
					IncludeInScope: true,
				},
				{
					HostRegex:      "^test\\.com$",
					PortRegex:      "^443$",
					IncludeInScope: true,
				},
			},
			[]string{"https://example.com", "https://example.com/test", "https://example.com/test?abc=123", "https://test.com", "https://test.com/test", "https://test.com/test?abc=123"},
			[]string{"https://test.com:8080", "https://test.com:8080/test", "https://test.com:8080/test?abc=123", "http://anotherdomain.com"},
		},
		{
			"Mixed entries",
			[]ScopeEntry{
				{
					Prefix:         "https://example.com/abc/def",
					IncludeInScope: false,
				},
				{
					HostRegex:      "^example\\.com$",
					IncludeInScope: true,
				},
			},
			[]string{"https://example.com", "https://example.com/test", "https://example.com/test?abc=123"},
			[]string{"https://example.com/abc/def", "https://example.com/abc/def/test", "https://example.com/abc/def/test?abc=123", "https://test.com", "https://test.com/test", "https://test.com/test?abc=123"},
		},
	}

	for _, s := range scopes {
		t.Run(s.name, func(t *testing.T) {
			scope = s.entries

			for _, url := range s.urlsInScope {
				if !urlMatchesScope(url) {
					t.Errorf("Expected URL '%s' to be in scope", url)
				}
			}

			for _, url := range s.urlsOutOfScope {
				if urlMatchesScope(url) {
					t.Errorf("Expected URL '%s' to be out of scope", url)
				}
			}
		})
	}
}
