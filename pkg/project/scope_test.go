package project

import (
	"testing"
)

func TestURLsInScopeWithPrefix(t *testing.T) {
	scopes := []struct {
		name           string
		prefix         string
		urlsInScope    []string
		urlsOutOfScope []string
	}{
		{
			"No prefix",
			"",
			[]string{
				"https://example.com/abc",
				"https://test.com/abc/def",
			},
			[]string{},
		},
		{
			"Prefix",
			"https://example.com",
			[]string{
				"https://example.com/abc",
				"https://example.com/abc/def",
			},
			[]string{
				"https://test.com/abc",
				"https://test.com/abc/def",
				"https://anothertest.com",
				"https://test.com/example.com",
				"https://test.com/https://example.com",
			},
		},
	}

	for _, scope := range scopes {
		t.Run(scope.name, func(t *testing.T) {
			scopeEntry := ScopeEntry{
				Prefix: scope.prefix,
			}

			for _, url := range scope.urlsInScope {
				inScope, err := scopeEntry.URLInScope(url)
				if err != nil {
					t.Errorf("Error when checking if URL %s is in scope: %s", url, err.Error())
				}
				if !inScope {
					t.Errorf("URL %s should be in scope", url)
				}
			}

			for _, url := range scope.urlsOutOfScope {
				inScope, err := scopeEntry.URLInScope(url)
				if err != nil {
					t.Errorf("Error when checking if URL %s is in scope: %s", url, err.Error())
				}
				if inScope {
					t.Errorf("URL %s should not be in scope", url)
				}
			}
		})
	}
}

func TestURLsInScopeWithRegex(t *testing.T) {
	scopes := []struct {
		name           string
		protocol       string
		hostRegex      string
		portRegex      string
		fileRegex      string
		urlsInScope    []string
		urlsOutOfScope []string
	}{
		{
			"Protocol",
			"https",
			"",
			"",
			"",
			[]string{
				"https://example.com/abc",
				"https://test.com/abc/def",
			},
			[]string{
				"http://example.com/abc",
				"http://test.com/abc/def",
			},
		},
		{
			"Host",
			"",
			"^example\\.com$",
			"",
			"",
			[]string{
				"https://example.com/abc",
				"https://example.com/abc/def",
				"http://example.com/abc",
				"http://example.com/abc/def",
			},
			[]string{
				"https://test.com/abc",
				"https://test.com/abc/def",
				"http://anothertest.com",
				"https://test.example.com/abc",
			},
		},
		{
			"Port",
			"",
			"",
			"^8080$",
			"",
			[]string{
				"https://example.com:8080/abc",
				"https://test.com:8080/abc/def",
				"http://example.com:8080/abc",
				"http://test.com:8080/abc/def",
			},
			[]string{
				"https://example.com:8081/abc",
				"https://test.com:8081/abc/def",
				"http://example.com/abc",
				"http://test.com/abc/def",
			},
		},
		{
			"File",
			"",
			"",
			"",
			"^/abc/.*$",
			[]string{
				"https://example.com/abc/def",
				"http://test.com/abc/def/ghi",
			},
			[]string{
				"https://example.com/abc",
				"http://test.com/abce/def",
			},
		},
		{
			"Host and Port",
			"",
			"^example\\.com$",
			"^8080$",
			"",
			[]string{
				"https://example.com:8080/abc",
				"https://example.com:8080/abc/def",
				"http://example.com:8080/abc",
				"http://example.com:8080/abc/def",
			},
			[]string{
				"https://example.com:8081/abc",
				"https://test.com:8080/abc/def",
				"http://example.com/abc",
				"http://test.com:8080/abc/def",
			},
		},
	}

	for _, scope := range scopes {
		t.Run(scope.name, func(t *testing.T) {
			scopeEntry := ScopeEntry{
				Protocol:  scope.protocol,
				HostRegex: scope.hostRegex,
				PortRegex: scope.portRegex,
				FileRegex: scope.fileRegex,
			}

			for _, url := range scope.urlsInScope {
				inScope, err := scopeEntry.URLInScope(url)
				if err != nil {
					t.Errorf("Error when checking if URL %s is in scope: %s", url, err.Error())
				}
				if !inScope {
					t.Errorf("URL %s should be in scope", url)
				}
			}

			for _, url := range scope.urlsOutOfScope {
				inScope, err := scopeEntry.URLInScope(url)
				if err != nil {
					t.Errorf("Error when checking if URL %s is in scope: %s", url, err.Error())
				}
				if inScope {
					t.Errorf("URL %s should not be in scope", url)
				}
			}
		})
	}
}
