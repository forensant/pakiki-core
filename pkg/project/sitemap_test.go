package project

import (
	"testing"
)

func TestSitemapPathDetermination(t *testing.T) {
	urls := []struct {
		url          string
		expectedPath string
	}{
		{"https://example.com/accounts/summary", "https://example.com/accounts/"},
		{"https://example.com/accounts/products/purchased", "https://example.com/accounts/products/"},
	}

	for _, url := range urls {
		t.Run(url.url, func(t *testing.T) {
			if path := sitemapPathFromUrl(url.url); path != url.expectedPath {
				t.Errorf("Expected path '%s' but got '%s'", url.expectedPath, path)
			}
		})
	}
}
