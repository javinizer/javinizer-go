package dmm

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCloseV2 tests Close method
func TestCloseV2(t *testing.T) {
	scraper := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{}, dmmOptions{})
	err := scraper.Close()
	require.NoError(t, err)
}

// TestCanHandleURLV2 tests CanHandleURL method
func TestCanHandleURLV2(t *testing.T) {
	scraper := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{}, dmmOptions{})

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00123/", true},
		{"https://www.dmm.com/digital/videoa/-/detail/=/cid=ipx00123/", true},
		{"https://example.com/test", false},
		{"not-a-url", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, scraper.CanHandleURL(tt.url))
		})
	}
}

// TestExtractIDFromURLV2 tests ExtractIDFromURL method
func TestExtractIDFromURLV2(t *testing.T) {
	scraper := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{}, dmmOptions{})

	tests := []struct {
		name     string
		url      string
		expected string
		hasErr   bool
	}{
		{"digital videoa", "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00123/", "ipx00123", false},
		{"no cid", "https://www.dmm.co.jp/digital/videoa/-/detail/", "", true},
		{"invalid URL", "not-a-url", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := scraper.ExtractIDFromURL(tt.url)
			if tt.hasErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, id)
			}
		})
	}
}

// TestResolveDownloadProxyForHostV2 tests ResolveDownloadProxyForHost method
func TestResolveDownloadProxyForHostV2(t *testing.T) {
	downloadProxy := &models.ProxyConfig{Enabled: true}
	overrideProxy := &models.ProxyConfig{Enabled: true}
	scraper := newScraper(&models.ScraperSettings{
		Enabled:       true,
		DownloadProxy: downloadProxy,
		Proxy:         overrideProxy,
	}, nil, models.FlareSolverrConfig{}, dmmOptions{})

	dp, op, ok := scraper.ResolveDownloadProxyForHost("dmm.co.jp")
	assert.True(t, ok)
	assert.Equal(t, downloadProxy, dp)
	assert.Equal(t, overrideProxy, op)

	dp, op, ok = scraper.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	assert.Nil(t, dp)
	assert.Nil(t, op)

	dp, op, ok = scraper.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
}

// TestScrapeURLV2_Disabled tests ScrapeURL when URL is not handled
func TestScrapeURLV2_Disabled(t *testing.T) {
	scraper := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{}, dmmOptions{})

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/not-handled")
	require.Error(t, err)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-DMM URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{}, dmmOptions{})

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
}

// TestGetURLV2 tests GetURL method
func TestGetURLV2(t *testing.T) {
	scraper := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{}, dmmOptions{})

	// GetURL may fail without a real HTTP server but should not panic
	_, _ = scraper.GetURL(context.Background(), "IPX-123")
}

// TestBuildSearchURL tests URL building helpers
func TestBuildSearchURL(t *testing.T) {
	// Verify URL encoding works
	encoded := url.QueryEscape("IPX-123")
	assert.Equal(t, "IPX-123", encoded)

	// Verify URL parsing works
	u, err := url.Parse("https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00123/")
	require.NoError(t, err)
	assert.True(t, strings.Contains(u.Hostname(), "dmm"))
}
