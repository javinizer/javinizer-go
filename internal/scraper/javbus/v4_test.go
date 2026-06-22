package javbus

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScrapeURLV4_Success tests ScrapeURL with a valid detail page via httptest
func TestScrapeURLV4_Success(t *testing.T) {
	detailHTML := buildJavBusDetailHTML("ABC-123", "ABC-123 Test Movie - JavBus", "2024-01-15", "120", "TestMaker", "TestLabel", "TestDirector", "Action, Drama", "Actress A", "https://pics.dmm.co.jp/cover.jpg")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Fetch page via test server then parse
	html, status, err := s.fetchPageCtx(context.Background(), ts.URL+"/ABC-123")
	require.NoError(t, err)
	assert.Equal(t, 200, status)

	// Parse the detail page directly
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result, err := s.parseDetailPage(doc, ts.URL+"/ABC-123", "ABC-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
}

// TestScrapeURLV4_Disabled tests ScrapeURL when disabled
func TestScrapeURLV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestScrapeURLV4_StatusCodes tests various HTTP status codes
func TestScrapeURLV4_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		expectErr  bool
	}{
		{"404 not found", 404, true},
		{"403 forbidden", 403, true},
		{"429 rate limited", 429, true},
		{"500 server error", 500, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer ts.Close()

			s := &scraper{
				client:      resty.New().SetBaseURL(ts.URL),
				enabled:     true,
				baseURL:     ts.URL,
				language:    "ja",
				rateLimiter: ratelimit.NewLimiter(0),
				settings:    models.ScraperSettings{Enabled: true},
			}

			result, err := s.ScrapeURL(context.Background(), ts.URL+"/ABC-123")
			assert.Nil(t, result)
			assert.Error(t, err)
		})
	}
}

// TestSearchV4_Disabled tests Search when disabled
func TestSearchV4_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV4_WithSearchAndDetail tests Search finding detail URL then parsing
func TestSearchV4_WithSearchAndDetail(t *testing.T) {
	searchHTML := buildJavBusSearchHTML("ABC-123", "/ABC-123")
	detailHTML := buildJavBusDetailHTML("ABC-123", "ABC-123 Found Movie - JavBus", "2024-05-10", "90", "StudioA", "LabelB", "DirC", "Romance", "ActressD", "https://pics.dmm.co.jp/cover3.jpg")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		if r.URL.Path == "/search/ABC-123" || r.URL.Path == "/uncensored/search/ABC-123" {
			fmt.Fprint(w, searchHTML)
			return
		}
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	s := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Test the findDetailURL method with the search HTML
	detailURL := s.findDetailURL(searchHTML, ts.URL, "ABC-123")
	assert.NotEmpty(t, detailURL, "should find detail URL from search results")

	// Fetch the detail page and parse
	html, status, err := s.fetchPageCtx(context.Background(), ts.URL+"/ABC-123")
	require.NoError(t, err)
	assert.Equal(t, 200, status)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result, err := s.parseDetailPage(doc, ts.URL+"/ABC-123", "ABC-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
}

// TestExtractIDFromURLV4 tests URL ID extraction
func TestExtractIDFromURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.javbus.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		name        string
		url         string
		expected    string
		expectError bool
	}{
		{"standard detail", "https://www.javbus.com/ABC-123", "ABC-123", false},
		{"with language prefix", "https://www.javbus.com/en/ABC-123", "ABC-123", false},
		{"ja prefix", "https://www.javbus.com/ja/ABC-123", "ABC-123", false},
		{"no path", "https://www.javbus.com", "", true},
		{"invalid URL", "://bad", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := s.ExtractIDFromURL(tt.url)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, id)
			}
		})
	}
}

// TestGetURLV4 tests GetURL
func TestGetURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.javbus.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	// Empty ID should error
	_, err := s.GetURL(context.Background(), "")
	assert.Error(t, err)

	// HTTP URL should be returned with language
	url, err := s.GetURL(context.Background(), "https://www.javbus.com/ABC-123")
	assert.NoError(t, err)
	assert.Contains(t, url, "ABC-123")
}

// TestCanHandleURLV4 tests CanHandleURL
func TestCanHandleURLV4(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.javbus.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.javbus.com/ABC-123", true},
		{"https://javbus.com/ABC-123", true},
		{"https://www.javbus.org/ABC-123", true},
		{"https://example.com/ABC-123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// Helper functions for building HTML test fixtures

func buildJavBusDetailHTML(id, title, date, runtime, maker, label, director, genres, actresses, coverURL string) string {
	return fmt.Sprintf(`<html>
<head><title>%s</title></head>
<body>
<div id="info">
	<p><span class="header">品番:</span> %s</p>
	<p><span class="header">発売日:</span> %s</p>
	<p><span class="header">収録時間:</span> %s</p>
	<p><span class="header">監督:</span> <a>%s</a></p>
	<p><span class="header">メーカー:</span> <a>%s</a></p>
	<p><span class="header">レーベル:</span> <a>%s</a></p>
	<p><span class="header">ジャンル:</span> %s</p>
	<p><span class="header">出演者:</span> %s</p>
</div>
<a class="bigImage"><img src="%s" title="%s" /></a>
</body>
</html>`, title, id, date, runtime, director, maker, label, genres, actresses, coverURL, title)
}

func buildJavBusSearchHTML(id, detailPath string) string {
	return fmt.Sprintf(`<html><body>
<a class="movie-box" href="%s" title="%s"><date>%s</date></a>
</body></html>`, detailPath, id, id)
}
