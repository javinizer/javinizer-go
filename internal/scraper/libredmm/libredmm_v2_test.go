package libredmm

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

// libredmmRoundTripper maps URLs to responses for testing
type libredmmRoundTripper struct {
	responses map[string]string
	statuses  map[string]int
}

func (rt *libredmmRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.String()
	// Also try path-only matching
	if status, ok := rt.statuses[key]; ok {
		body := rt.responses[key]
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}
	for url, body := range rt.responses {
		parsedURL := url
		if strings.Contains(parsedURL, req.URL.Path) || strings.HasSuffix(parsedURL, req.URL.Path) {
			status := 200
			if s, ok := rt.statuses[url]; ok {
				status = s
			}
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("not found")),
		Request:    req,
	}, nil
}

// TestScrapeURLV2_Success tests ScrapeURL with mock transport
func TestScrapeURLV2_Success(t *testing.T) {
	payload := moviePayload{
		NormalizedID:  "IPX-123",
		Title:         "Test Movie Title",
		CoverImageURL: "https://example.com/cover.jpg",
		Date:          "2024-01-02",
		Description:   "Test description",
		Review:        4.5,
		Makers:        []string{"TestMaker"},
		Actresses:     []actressPayload{{Name: "Actress1", ImageURL: "https://example.com/actress1.jpg"}},
		Genres:        []string{"Drama"},
		Directors:     []string{"Director1"},
		Labels:        []string{"Label1"},
		URL:           "https://www.libredmm.com/movies/IPX-123",
	}
	body, _ := json.Marshal(payload)

	client := resty.New()
	client.SetTransport(&libredmmRoundTripper{
		responses: map[string]string{
			"https://www.libredmm.com/movies/IPX-123.json": string(body),
		},
	})

	scraper := &scraper{
		client:          client,
		enabled:         true,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/IPX-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "libredmm", result.Source)
	assert.Equal(t, "IPX-123", result.ID)
}

// TestScrapeURLV2_Disabled tests ScrapeURL when disabled
func TestScrapeURLV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:          resty.New(),
		enabled:         false,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/IPX-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-LibreDMM URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/movies/IPX-123")
	require.Error(t, err)
}

// TestScrapeURLV2_404 tests ScrapeURL when API returns 404
func TestScrapeURLV2_404(t *testing.T) {
	client := resty.New()
	client.SetTransport(&libredmmRoundTripper{
		responses: map[string]string{},
		statuses:  map[string]int{},
	})

	scraper := &scraper{
		client:          client,
		enabled:         true,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/NOTFOUND-999")
	require.Error(t, err)
}

// TestScrapeURLV2_502 tests ScrapeURL when API returns 502
func TestScrapeURLV2_502(t *testing.T) {
	client := resty.New()
	client.SetTransport(&libredmmRoundTripper{
		responses: map[string]string{
			"https://www.libredmm.com/movies/IPX-502.json": `{"err":"bad gateway"}`,
		},
		statuses: map[string]int{
			"https://www.libredmm.com/movies/IPX-502.json": 502,
		},
	})

	scraper := &scraper{
		client:          client,
		enabled:         true,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/IPX-502")
	require.Error(t, err)
}

// TestSearchV2_Success tests Search with mock transport
func TestSearchV2_Success(t *testing.T) {
	payload := moviePayload{
		NormalizedID:  "IPX-123",
		Title:         "Test Movie Title",
		CoverImageURL: "https://example.com/cover.jpg",
		Date:          "2024-01-02",
		Description:   "Test description",
		Review:        4.5,
		Makers:        []string{"TestMaker"},
		Actresses:     []actressPayload{{Name: "Actress1"}},
		Genres:        []string{"Drama"},
		URL:           "https://www.libredmm.com/movies/IPX-123",
	}
	body, _ := json.Marshal(payload)

	client := resty.New()
	client.SetTransport(&libredmmRoundTripper{
		responses: map[string]string{
			"https://www.libredmm.com/search?q=IPX-123&format=json": string(body),
		},
	})

	scraper := &scraper{
		client:          client,
		enabled:         true,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "IPX-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-123", result.ID)
	assert.Equal(t, "libredmm", result.Source)
}

// TestSearchV2_Disabled tests Search when scraper is disabled
func TestSearchV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV2_404 tests Search when movie not found
func TestSearchV2_404(t *testing.T) {
	client := resty.New()
	client.SetTransport(&libredmmRoundTripper{
		responses: map[string]string{},
	})

	scraper := &scraper{
		client:          client,
		enabled:         true,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "NOTFOUND-999")
	require.Error(t, err)
}

// TestFetchMovieJSONCtxV2_Success tests fetchMovieJSONCtx
func TestFetchMovieJSONCtxV2_Success(t *testing.T) {
	payload := moviePayload{NormalizedID: "IPX-123", Title: "Test"}
	body, _ := json.Marshal(payload)

	client := resty.New()
	client.SetTransport(&libredmmRoundTripper{
		responses: map[string]string{
			"https://www.libredmm.com/movies/IPX-123.json": string(body),
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	p, _, status, err := scraper.fetchMovieJSONCtx(context.Background(), "https://www.libredmm.com/movies/IPX-123.json")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Equal(t, "IPX-123", p.NormalizedID)
}

// TestScrapeURLV2_PayloadError tests ScrapeURL when payload has error
func TestScrapeURLV2_PayloadError(t *testing.T) {
	client := resty.New()
	client.SetTransport(&libredmmRoundTripper{
		responses: map[string]string{
			"https://www.libredmm.com/movies/IPX-ERR.json": `{"err":"movie not found"}`,
		},
	})

	scraper := &scraper{
		client:          client,
		enabled:         true,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/IPX-ERR")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "movie not found")
}

// TestGetURLCtxV2 tests getURLCtx
func TestGetURLCtxV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	url, err := scraper.getURLCtx(context.Background(), "IPX-123")
	require.NoError(t, err)
	assert.Contains(t, url, "IPX-123")
}

// TestGetURLCtxV2_EmptyID tests getURLCtx with empty ID
func TestGetURLCtxV2_EmptyID(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.getURLCtx(context.Background(), "")
	require.Error(t, err)
}

// TestExtractIDFromURLV2 tests ExtractIDFromURL with various URL formats
func TestExtractIDFromURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		name     string
		url      string
		expected string
		hasError bool
	}{
		{"movies path", "https://www.libredmm.com/movies/IPX-123", "IPX-123", false},
		{"movies json", "https://www.libredmm.com/movies/IPX-123.json", "IPX-123", false},
		{"cid path", "https://www.libredmm.com/cid/IPX-456", "IPX-456", false},
		{"query param", "https://www.libredmm.com/?q=SSIS-789", "SSIS-789", false},
		{"no id", "https://www.libredmm.com/", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := scraper.ExtractIDFromURL(tt.url)
			if tt.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, id)
			}
		})
	}
}

// TestFilterPlaceholderScreenshotsCtxV2 tests filterPlaceholderScreenshotsCtx
func TestFilterPlaceholderScreenshotsCtxV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Test with a result that has screenshot URLs
	result := &models.ScraperResult{
		ScreenshotURL: []string{"https://example.com/shot1.jpg", "https://example.com/shot2.jpg"},
	}
	scraper.filterPlaceholderScreenshotsCtx(context.Background(), result)
	// Should not panic; screenshots remain if not placeholder
	assert.Len(t, result.ScreenshotURL, 2)
}

// TestNormalizeMovieURLV2 tests normalizeMovieURL with various inputs
func TestNormalizeMovieURLV2(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expectOK  bool
		expectURL string
	}{
		{"libredmm movies URL", "https://www.libredmm.com/movies/IPX-123", true, "https://www.libredmm.com/movies/IPX-123.json"},
		{"libredmm movies URL with json", "https://www.libredmm.com/movies/IPX-123.json", true, "https://www.libredmm.com/movies/IPX-123.json"},
		{"other site", "https://example.com/movies/IPX-123", false, ""},
		{"not a URL", "IPX-123", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, ok := normalizeMovieURL(tt.input, "https://www.libredmm.com")
			assert.Equal(t, tt.expectOK, ok)
			if tt.expectOK {
				assert.Equal(t, tt.expectURL, url)
			}
		})
	}
}

// TestStripANSICodesV2 tests stripANSICodes with various inputs
func TestStripANSICodesV2(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"clean string", `{"title":"test"}`, `{"title":"test"}`},
		{"ansi escape", "\x1b[31mred\x1b[0m", "red"},
		{"bare ESC", "test\x1bstring", "teststring"},
		{"control char", "test\x00string", "teststring"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripANSICodes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestScrapeURLV2_202Processing tests ScrapeURL with 202 status (processing)
func TestScrapeURLV2_202Processing(t *testing.T) {
	client := resty.New()
	client.SetTransport(&libredmmRoundTripper{
		responses: map[string]string{
			"https://www.libredmm.com/movies/IPX-202.json": `{"err":"processing"}`,
		},
		statuses: map[string]int{
			"https://www.libredmm.com/movies/IPX-202.json": 202,
		},
	})

	scraper := &scraper{
		client:          client,
		enabled:         true,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/IPX-202")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "processing")
}
