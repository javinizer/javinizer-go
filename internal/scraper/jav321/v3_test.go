package jav321

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchV3_Disabled tests Search when disabled
func TestSearchV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestScrapeURLV3_Disabled tests ScrapeURL when disabled
func TestScrapeURLV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://jp.jav321.com/search?sn=IPX-123")
	require.Error(t, err)
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-jav321 URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/search?sn=IPX-123")
	require.Error(t, err)
}

// TestCanHandleURLV3 tests CanHandleURL
func TestCanHandleURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://jp.jav321.com/search?sn=IPX-123"))
	assert.False(t, scraper.CanHandleURL("https://example.com/search"))
}

// TestGetURLCtxV3 tests getURLCtx
func TestGetURLCtxV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := scraper.getURLCtx(context.Background(), "")
		require.Error(t, err)
	})

	t.Run("URL input", func(t *testing.T) {
		url, err := scraper.getURLCtx(context.Background(), "https://jp.jav321.com/search?sn=IPX-123")
		require.NoError(t, err)
		assert.NotEmpty(t, url)
	})
}

// TestFetchPageCtxV3 tests fetchPageCtx
func TestFetchPageCtxV3(t *testing.T) {
	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &j321MockTransport{statusCode: 200, response: "<html>ok</html>"}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "https://jp.jav321.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "ok")
}

// TestExtractIDFromURLV3 tests ExtractIDFromURL
func TestExtractIDFromURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("video URL", func(t *testing.T) {
		id, err := scraper.ExtractIDFromURL("https://jp.jav321.com/video/IPX-123/")
		require.NoError(t, err)
		assert.Equal(t, "IPX-123", id)
	})

	t.Run("invalid URL", func(t *testing.T) {
		_, err := scraper.ExtractIDFromURL("://invalid")
		require.Error(t, err)
	})
}

// j321MockTransport is a mock transport
type j321MockTransport struct {
	response   string
	statusCode int
}

func (mt *j321MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: mt.statusCode,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(mt.response)),
		Request:    req,
	}, nil
}
