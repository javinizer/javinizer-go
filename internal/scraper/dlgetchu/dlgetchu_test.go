package dlgetchu

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func testSettings(baseURL string) config.ScraperSettings {
	return config.ScraperSettings{
		Enabled:   true,
		RateLimit: 0,
		BaseURL:   baseURL,
	}
}

func TestSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/") && r.URL.RawQuery == "search_keyword=ABC-123":
			_, _ = fmt.Fprint(w, `<html><body><a href="/i/item12345">Result</a></body></html>`)
		case r.URL.Path == "/i/item12345":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, `<html><head>
<meta property="og:title" content="DLgetchu Sample Title">
<meta name="description" content="Fallback description">
</head><body>
<table>
<tr><td>作品内容</td><td>Long <b>description</b> for the DLgetchu parser.</td></tr>
</table>
<div>作品ID: 12345</div>
<div>発売日 2026/02/13</div>
<div>収録時間 ９０分</div>
<a href="dojin_circle_detail.php?id=44">Test Circle</a>
<a href="genre_id=1">Drama</a>
<a href="genre_id=2">Romance</a>
<img src="/data/item_img/demo/12345top.jpg">
"/data/item_img/demo/shot1.jpg" class="highslide"
"/data/item_img/demo/shot2.webp" class="highslide"
</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := New(testSettings(server.URL), nil, config.FlareSolverrConfig{})
	result, err := s.Search("ABC-123")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if result.Source != "dlgetchu" {
		t.Fatalf("Source = %q, want dlgetchu", result.Source)
	}
	if result.SourceURL != server.URL+"/i/item12345" {
		t.Fatalf("SourceURL = %q", result.SourceURL)
	}
	if result.ID != "12345" || result.ContentID != "12345" {
		t.Fatalf("unexpected IDs: %q %q", result.ID, result.ContentID)
	}
	if result.Title != "DLgetchu Sample Title" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.Description != "Long description for the DLgetchu parser." {
		t.Fatalf("Description = %q", result.Description)
	}
	if result.Maker != "Test Circle" {
		t.Fatalf("Maker = %q", result.Maker)
	}
	if result.Runtime != 90 {
		t.Fatalf("Runtime = %d, want 90", result.Runtime)
	}
	if result.ReleaseDate == nil {
		t.Fatal("ReleaseDate is nil")
	}
	wantDate := time.Date(2026, 2, 13, 0, 0, 0, 0, time.UTC)
	if !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("ReleaseDate = %v, want %v", result.ReleaseDate, wantDate)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "Drama" || result.Genres[1] != "Romance" {
		t.Fatalf("Genres = %#v", result.Genres)
	}
	if len(result.ScreenshotURL) != 2 {
		t.Fatalf("ScreenshotURL len = %d, want 2", len(result.ScreenshotURL))
	}
	if result.CoverURL != server.URL+"/data/item_img/demo/12345top.jpg" || result.PosterURL != result.CoverURL {
		t.Fatalf("unexpected cover URLs: %q %q", result.CoverURL, result.PosterURL)
	}
}

func TestParseDetailPage_Fallbacks(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head>
<title>Fallback Title</title>
<meta name="description" content="Meta fallback description">
</head><body></body></html>`))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	result := parseDetailPage(doc, `<html><body><div>id=98765</div></body></html>`, "https://dl.getchu.com/i/item98765", "RJ-1")
	if result.ID != "98765" {
		t.Fatalf("ID = %q, want 98765", result.ID)
	}
	if result.Title != "Fallback Title" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.Description != "Meta fallback description" {
		t.Fatalf("Description = %q", result.Description)
	}
}

func TestHelpers(t *testing.T) {
	if got := findFirstDetailLink(`<a href="/i/item12345">x</a>`, "https://dl.getchu.com"); got != "https://dl.getchu.com/i/item12345" {
		t.Fatalf("findFirstDetailLink = %q", got)
	}
	if got := normalizeFullWidthDigits("１２３ ４５"); got != "123 45" {
		t.Fatalf("normalizeFullWidthDigits = %q", got)
	}
	if got := extractNumericID("作品ID: 54321"); got != "54321" {
		t.Fatalf("extractNumericID = %q", got)
	}
	if got := resolveURL("https://dl.getchu.com/i/item12345", "/x/y.jpg"); got != "https://dl.getchu.com/x/y.jpg" {
		t.Fatalf("resolveURL = %q", got)
	}
	if !isHTTPURL("https://dl.getchu.com/i/item12345") {
		t.Fatal("expected HTTP URL")
	}
}

// TestExtractGenres tests genre extraction with table-driven tests
func TestExtractGenres(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected []string
	}{
		{
			name:     "Single genre",
			html:     `<a href="genre_id=1">Action</a>`,
			expected: []string{"Action"},
		},
		{
			name:     "Multiple genres",
			html:     `<a href="genre_id=1">Action</a><a href="genre_id=2">Romance</a><a href="genre_id=3">Drama</a>`,
			expected: []string{"Action", "Romance", "Drama"},
		},
		{
			name:     "No genres",
			html:     `<html><body></body></html>`,
			expected: []string{},
		},
		{
			name:     "Empty HTML",
			html:     ``,
			expected: []string{},
		},
		{
			name:     "Malformed HTML",
			html:     `<a href="genre_id=1">Action`,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGenres(tt.html)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractScreenshots tests screenshot extraction with table-driven tests
// Note: regex pattern is: "(/data/item_img/[^\"']+\.(?:jpg|jpeg|webp))"\s+class="highslide"
// The URL must be in quotes directly followed by class="highslide"
func TestExtractScreenshots(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		baseURL  string
		expected []string
	}{
		{
			name:     "Single screenshot",
			html:     `"/data/item_img/demo/s1.jpg" class="highslide"`,
			baseURL:  "https://dl.getchu.com",
			expected: []string{"https://dl.getchu.com/data/item_img/demo/s1.jpg"},
		},
		{
			name:     "Multiple screenshots",
			html:     `"/data/item_img/demo/s1.jpg" class="highslide" "/data/item_img/demo/s2.jpg" class="highslide"`,
			baseURL:  "https://dl.getchu.com",
			expected: []string{"https://dl.getchu.com/data/item_img/demo/s1.jpg", "https://dl.getchu.com/data/item_img/demo/s2.jpg"},
		},
		{
			name:     "No screenshots",
			html:     `<html><body></body></html>`,
			baseURL:  "https://dl.getchu.com",
			expected: []string{},
		},
		{
			name:     "Non-highslide links",
			html:     `"/data/item_img/demo/s1.jpg"`,
			baseURL:  "https://dl.getchu.com",
			expected: []string{},
		},
		{
			name:     "Empty HTML",
			html:     ``,
			baseURL:  "https://dl.getchu.com",
			expected: []string{},
		},
		{
			name:     "WebP format",
			html:     `"/data/item_img/demo/s1.webp" class="highslide"`,
			baseURL:  "https://dl.getchu.com",
			expected: []string{"https://dl.getchu.com/data/item_img/demo/s1.webp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractScreenshots(tt.html, tt.baseURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFetchPage tests HTTP request handling with table-driven tests
// Note: fetchPage returns status code for HTTP errors, not an error - only network errors return error
func TestFetchPage(t *testing.T) {
	tests := []struct {
		name         string
		handler      http.HandlerFunc
		expectedCode int
		expectError  bool
	}{
		{
			name: "Success 200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("<html><body>test content</body></html>"))
			},
			expectedCode: 200,
			expectError:  false,
		},
		{
			name: "Not found 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			},
			expectedCode: 404,
			expectError:  false, // fetchPage returns status, not error for HTTP codes
		},
		{
			name: "Server error 500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("Internal Server Error"))
			},
			expectedCode: 500,
			expectError:  false, // fetchPage returns status, not error for HTTP codes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			settings := testSettings(server.URL)
			s := New(settings, nil, config.FlareSolverrConfig{})

			result, status, err := s.fetchPage(server.URL)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedCode, status)
			// fetchPage returns the response body regardless of status code
			assert.NotEmpty(t, result)
		})
	}
}

// TestDecodeBody tests response body decoding
func TestDecodeBody(t *testing.T) {
	// Test with empty body (http.NoBody)
	resp := &resty.Response{RawResponse: &http.Response{Body: http.NoBody}}
	result, err := decodeBody(resp)
	assert.NoError(t, err)
	assert.Empty(t, result)
}

func TestResolveURL(t *testing.T) {
	assert.Equal(t, "https://example.com/x/y.jpg", resolveURL("https://example.com/i/item1", "/x/y.jpg"))
}

func TestCleanString(t *testing.T) {
	assert.Equal(t, "hello world", cleanString("hello   world"))
}

func TestStripTags(t *testing.T) {
	assert.Equal(t, "Hello world", stripTags("Hello <b>world</b>"))
}

// TestIsHTTPURL tests URL validation with table-driven tests
func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "HTTP URL",
			input:    "http://example.com",
			expected: true,
		},
		{
			name:     "HTTPS URL",
			input:    "https://example.com",
			expected: true,
		},
		{
			name:     "Not a URL",
			input:    "example.com",
			expected: false,
		},
		{
			name:     "Empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "FTP protocol",
			input:    "ftp://example.com/file",
			expected: false,
		},
		{
			name:     "File protocol",
			input:    "file:///path/to/file",
			expected: false,
		},
		{
			name:     "Invalid URL with special chars",
			input:    "javascript:alert(1)",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHTTPURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestScraper_IsEnabled tests the IsEnabled method with various configurations
func TestScraper_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{"Enabled scraper", true},
		{"Disabled scraper", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.ScraperSettings{Enabled: tt.enabled}
			scraper := New(settings, nil, config.FlareSolverrConfig{})
			assert.Equal(t, tt.enabled, scraper.IsEnabled(), "IsEnabled should match config")
		})
	}
}

// TestScraper_Name tests the Name method
func TestScraper_Name(t *testing.T) {
	settings := testSettings("https://dl.getchu.com")
	scraper := New(settings, nil, config.FlareSolverrConfig{})
	assert.Equal(t, "dlgetchu", scraper.Name())
}

// TestScraper_GetURL tests URL generation for various scenarios
func TestScraper_GetURL(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		expectedErr bool
		expectedURL string
	}{
		{
			name:        "URL input - returns as-is",
			id:          "https://dl.getchu.com/i/item12345",
			expectedErr: false,
			expectedURL: "https://dl.getchu.com/i/item12345",
		},
		{
			name:        "Empty ID - returns error",
			id:          "",
			expectedErr: true,
			expectedURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := testSettings("https://dl.getchu.com")
			scraper := New(settings, nil, config.FlareSolverrConfig{})
			url, err := scraper.GetURL(tt.id)
			if tt.expectedErr {
				assert.Error(t, err, "GetURL should fail for empty ID")
				assert.Empty(t, url)
			} else {
				assert.NoError(t, err, "GetURL should succeed for valid URL")
				assert.Equal(t, tt.expectedURL, url)
			}
		})
	}
}

// TestScraper_GetURLNumeric tests numeric ID URL generation (requires network)
func TestScraper_GetURLNumeric(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-dependent test")
	}

	settings := testSettings("https://dl.getchu.com")
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	// This test may fail if the URL doesn't exist
	// It's designed to test the actual network behavior
	url, err := scraper.GetURL("12345")
	// Just check that it doesn't panic
	_ = url
	_ = err
}

// TestScraper_GetURL_Search tests GetURL with search fallback
func TestScraper_GetURL_Search(t *testing.T) {
	// Create server that returns search results
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		query := r.URL.RawQuery

		// Search keyword responses
		if strings.Contains(query, "search_keyword") {
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><body><a href="/i/item12345">Result</a></body></html>`)
			return
		}

		// Item detail page
		if strings.Contains(path, "/i/item") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = fmt.Fprint(w, `<html><body>
<div>作品ID: 12345</div>
<div>発売日 2026/02/13</div>
<a href="dojin_circle_detail.php?id=44">Test Circle</a>
<img src="/data/item_img/demo/12345top.jpg">
</body></html>`)
			return
		}

		// Root path
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<html><body>Root</body></html>`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := New(settings, nil, config.FlareSolverrConfig{})

	// Test search fallback
	url, err := s.GetURL("ABC-123")
	assert.NoError(t, err, "GetURL should succeed with search fallback")
	assert.Contains(t, url, "/i/item12345")
}

// TestFindFirstDetailLink tests findFirstDetailLink utility
func TestFindFirstDetailLink(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		baseURL  string
		expected string
	}{
		{
			name:     "Basic link",
			html:     `<a href="/i/item12345">Result</a>`,
			baseURL:  "https://dl.getchu.com",
			expected: "https://dl.getchu.com/i/item12345",
		},
		{
			name:     "Multiple links",
			html:     `<a href="/i/item12345">Result 1</a><a href="/i/item67890">Result 2</a>`,
			baseURL:  "https://dl.getchu.com",
			expected: "https://dl.getchu.com/i/item12345",
		},
		{
			name:     "No links",
			html:     `<html><body>no links</body></html>`,
			baseURL:  "https://dl.getchu.com",
			expected: "",
		},
		{
			name:     "External link",
			html:     `<a href="https://other.com/item">External</a>`,
			baseURL:  "https://dl.getchu.com",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findFirstDetailLink(tt.html, tt.baseURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractNumericID tests extractNumericID utility
func TestExtractNumericID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "With label",
			input:    "作品ID: 12345",
			expected: "12345",
		},
		{
			name:     "With fullwidth colon",
			input:    "作品ID：67890",
			expected: "67890",
		},
		{
			name:     "URL pattern",
			input:    "/item99999",
			expected: "99999",
		},
		{
			name:     "No match",
			input:    "ABC-123",
			expected: "",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractNumericID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeFullWidthDigits tests normalizeFullWidthDigits utility
func TestNormalizeFullWidthDigits(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Halfwidth and fullwidth",
			input:    "１２３ ４５６",
			expected: "123 456",
		},
		{
			name:     "Halfwidth only",
			input:    "123 456",
			expected: "123 456",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeFullWidthDigits(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveDownloadProxyForHost tests proxy resolution
func TestResolveDownloadProxyForHost(t *testing.T) {
	settings := testSettings("https://dl.getchu.com")
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	tests := []struct {
		name     string
		host     string
		expected bool
	}{
		{
			name:     "DLGetchu host",
			host:     "dl.getchu.com",
			expected: true,
		},
		{
			name:     "Getchu host",
			host:     "www.getchu.com",
			expected: true,
		},
		{
			name:     "Empty host",
			host:     "",
			expected: false,
		},
		{
			name:     "Non-getchu host",
			host:     "example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := scraper.ResolveDownloadProxyForHost(tt.host)
			assert.Equal(t, tt.expected, ok, "Should match expected result for %s", tt.host)
		})
	}
}

// TestExtractGenresEdgeCases tests edge cases in genre extraction
func TestExtractGenresEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected int
	}{
		{
			name:     "Multiple genres",
			html:     `<a href="genre_id=1">Action</a><a href="genre_id=2">Romance</a><a href="genre_id=3">Drama</a>`,
			expected: 3,
		},
		{
			name:     "Single genre",
			html:     `<a href="genre_id=1">Action</a>`,
			expected: 1,
		},
		{
			name:     "No genres",
			html:     `<html><body></body></html>`,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGenres(tt.html)
			assert.Len(t, result, tt.expected)
		})
	}
}

// TestExtractScreenshotsEdgeCases tests edge cases in screenshot extraction
func TestExtractScreenshotsEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected int
	}{
		{
			name:     "Multiple screenshots",
			html:     `<a href="/data/item_img/demo/shot1.jpg" class="highslide"></a><a href="/data/item_img/demo/shot2.jpg" class="highslide"></a>`,
			expected: 2,
		},
		{
			name:     "Single screenshot",
			html:     `<a href="/data/item_img/demo/shot1.jpg" class="highslide"></a>`,
			expected: 1,
		},
		{
			name:     "No screenshots",
			html:     `<html><body></body></html>`,
			expected: 0,
		},
		{
			name:     "Non-highslide links",
			html:     `<a href="/data/item_img/demo/shot1.jpg"></a><a href="/data/item_img/demo/shot2.jpg"></a>`,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractScreenshots(tt.html, "https://dl.getchu.com")
			assert.Len(t, result, tt.expected)
		})
	}
}

// TestCleanStringEdgeCases tests edge cases in string cleaning
func TestCleanStringEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Multiple spaces",
			input:    "hello   world",
			expected: "hello world",
		},
		{
			name:     "Leading/trailing spaces",
			input:    "  hello world  ",
			expected: "hello world",
		},
		{
			name:     "Newlines",
			input:    "hello\nworld",
			expected: "hello world",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStripTagsEdgeCases tests edge cases in tag stripping
func TestStripTagsEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Bold tags",
			input:    "Hello <b>world</b>",
			expected: "Hello world",
		},
		{
			name:     "Multiple tags",
			input:    "<div><span>Text</span></div>",
			expected: "Text",
		},
		{
			name:     "No tags",
			input:    "Plain text",
			expected: "Plain text",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripTags(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveURLEdgeCases tests edge cases in URL resolution
func TestResolveURLEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		relative string
		expected string
	}{
		{
			name:     "Absolute path",
			base:     "https://example.com/i/item1",
			relative: "/x/y.jpg",
			expected: "https://example.com/x/y.jpg",
		},
		{
			name:     "Relative path",
			base:     "https://example.com/i/item1",
			relative: "x/y.jpg",
			expected: "https://example.com/i/x/y.jpg",
		},
		{
			name:     "Empty relative",
			base:     "https://example.com/i/item1",
			relative: "",
			expected: "", // resolveURL returns empty string for empty relative URL
		},
		{
			name:     "Full URL",
			base:     "https://example.com",
			relative: "https://other.com/file.jpg",
			expected: "https://other.com/file.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveURL(tt.base, tt.relative)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSearchDisabled tests Search behavior when scraper is disabled
func TestSearchDisabled(t *testing.T) {
	settings := testSettings("https://dl.getchu.com")
	settings.Enabled = false
	s := New(settings, nil, config.FlareSolverrConfig{})

	result, err := s.Search("12345")

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DLgetchu scraper is disabled")
}

// TestSearchWithHTTPError tests Search behavior when HTTP fails
func TestSearchWithHTTPError(t *testing.T) {
	// Create server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := New(settings, nil, config.FlareSolverrConfig{})

	result, err := s.Search("12345")

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found on DLgetchu")
}

func TestCanHandleURL(t *testing.T) {
	s := New(testSettings("https://dl.getchu.com"), nil, config.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"dl.getchu.com", "https://dl.getchu.com/i/item12345", true},
		{"getchu.com", "https://www.getchu.com/i/item12345", true},
		{"with path", "http://dl.getchu.com/i/item12345", true},
		{"other site", "https://www.example.com/item/12345", false},
		{"malformed URL", "not-a-url", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.CanHandleURL(tt.url)
			assert.Equal(t, tt.expected, got, "CanHandleURL(%q) = %v, want %v", tt.url, got, tt.expected)
		})
	}
}

func TestExtractIDFromURL(t *testing.T) {
	s := New(testSettings("https://dl.getchu.com"), nil, config.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{"standard path", "https://dl.getchu.com/i/item12345", "12345", false},
		{"getchu.com path", "https://www.getchu.com/i/item67890", "67890", false},
		{"with trailing slash", "https://dl.getchu.com/i/item12345/", "12345", false},
		{"item ID in HTML context", "https://dl.getchu.com/i/item 作品ID: 54321", "54321", false},
		{"invalid URL", "not-a-url", "", true},
		{"non-item path", "https://dl.getchu.com/other", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ExtractIDFromURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err, "ExtractIDFromURL(%q) expected error, got nil", tt.url)
			} else {
				assert.NoError(t, err, "ExtractIDFromURL(%q) unexpected error: %v", tt.url, err)
				assert.Equal(t, tt.expected, got, "ExtractIDFromURL(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestScraperInterfaceCompliance_DLgetchu(t *testing.T) {
	s := New(testSettings("https://dl.getchu.com"), nil, config.FlareSolverrConfig{})
	var _ models.Scraper = s
	var _ models.URLHandler = s
	var _ models.DirectURLScraper = s
}
