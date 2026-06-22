package r18dev

import (
	"context"
	"encoding/json"
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

// TestSearchV3_Disabled tests Search when disabled - R18dev doesn't check enabled in Search,
// so it will fail trying to make requests
func TestSearchV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-123")
	require.Error(t, err)
}

// TestScrapeURLV3_Disabled tests ScrapeURL when disabled
func TestScrapeURLV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/combined=IPX-123/json")
	require.Error(t, err)
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-r18dev URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/videos/IPX-123")
	require.Error(t, err)
}

// TestCanHandleURLV3 tests CanHandleURL
func TestCanHandleURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://r18.dev/videos/vod/movies/detail/-/combined=IPX-123"))
	assert.False(t, scraper.CanHandleURL("https://example.com/videos/123"))
}

// TestContentIDMatchesExpectedV3 tests contentIDCoreMatch
func TestContentIDMatchesExpectedV3(t *testing.T) {
	// contentIDCoreMatch expects content_id format like "ipx00123" vs dvd_id "IPX-123"
	assert.True(t, contentIDCoreMatch("ipx00123", "ipx123"))
	assert.False(t, contentIDCoreMatch("", "ipx123"))
	assert.False(t, contentIDCoreMatch("abc00123", "ipx123"))
}

// TestSearchV3_WithMockServer tests Search with a mock JSON server
func TestSearchV3_WithMockServer(t *testing.T) {
	mockResponse := map[string]any{
		"data": map[string]any{
			"id":              "ipx00123",
			"content_id":      "ipx00123",
			"dmm_id":          "IPX-123",
			"title":           "Test Movie Title",
			"release_date":    "2024-01-15",
			"runtime_minutes": 120,
			"maker":           map[string]any{"name": "Test Maker"},
			"label":           map[string]any{"name": "Test Label"},
			"actresses":       []map[string]any{{"name": "Test Actress"}},
			"genres":          []map[string]any{{"name": "Drama"}},
			"cover_image_url": "https://example.com/cover.jpg",
		},
	}
	mockJSON, _ := json.Marshal(mockResponse)

	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &r18MockTransport{
		response:   string(mockJSON),
		statusCode: 200,
	}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "IPX-123")
	require.NoError(t, err)
	require.NotNil(t, result)
}

// r18MockTransport is a mock transport
type r18MockTransport struct {
	response   string
	statusCode int
}

func (mt *r18MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: mt.statusCode,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(mt.response)),
		Request:    req,
	}, nil
}
