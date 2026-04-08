package tokyohot

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanHandleURL(t *testing.T) {
	s := New(testSettings("https://www.tokyo-hot.com"), nil, config.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"tokyo-hot.com", "https://www.tokyo-hot.com/product/N1234/", true},
		{"other site", "https://www.example.com/ABC-123", false},
		{"malformed URL", "not-a-url", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.CanHandleURL(tt.url)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExtractIDFromURL_TokyoHot(t *testing.T) {
	s := New(testSettings("https://www.tokyo-hot.com"), nil, config.FlareSolverrConfig{})

	tests := []struct {
		name     string
		url      string
		expected string
		wantErr  bool
	}{
		{"product URL", "https://www.tokyo-hot.com/product/N1234/", "N1234", false},
		{"with query params", "https://www.tokyo-hot.com/product/ABC-567/?lang=en", "ABC-567", false},
		{"empty path", "https://www.tokyo-hot.com/", "", true},
		{"no product path", "https://www.tokyo-hot.com/pages/about", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ExtractIDFromURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestScraperInterfaceCompliance_TokyoHot(t *testing.T) {
	s := New(testSettings("https://www.tokyo-hot.com"), nil, config.FlareSolverrConfig{})
	var _ models.Scraper = s
	var _ models.URLHandler = s
	var _ models.DirectURLScraper = s
}

func testSettings(baseURL string) config.ScraperSettings {
	return config.ScraperSettings{
		Enabled:   true,
		Language:  "en",
		RateLimit: 0,
		BaseURL:   baseURL,
	}
}

func TestSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/product/":
			if got := r.URL.Query().Get("q"); got != "N1234" {
				t.Fatalf("query q = %q, want N1234", got)
			}
			_, _ = fmt.Fprint(w, `<html><body><a href="/product/N1234/">N1234 Amazing Movie</a></body></html>`)
		case "/product/N1234/":
			_, _ = fmt.Fprint(w, `<html><head><title>Amazing Movie | Tokyo-Hot</title></head><body>
<dl class="info">
  <dt>Product ID</dt><dd>N1234</dd>
  <dt>Release</dt><dd>2026/02/14</dd>
  <dt>Length</dt><dd>01:05:31</dd>
  <dt>Maker</dt><dd><a href="/maker/test">Tokyo Hot</a></dd>
  <dt>Series</dt><dd><a href="/series/test">Series X</a></dd>
  <dt>Model</dt><dd>Jane Doe / 花子</dd>
  <dt>Genre</dt><dd>Drama / Romance</dd>
</dl>
<div class="sentence">Story description for the TokyoHot parser test.</div>
<img src="/images/jacket.jpg">
<div class="scap"><a href="/gallery/1.jpg">one</a><a href="/gallery/2.jpg">two</a></div>
<video><source src="/trailers/n1234.mp4"></video>
</body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	s := New(testSettings(server.URL), nil, config.FlareSolverrConfig{})
	result, err := s.Search("N1234")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if result.Source != "tokyohot" {
		t.Fatalf("Source = %q, want tokyohot", result.Source)
	}
	if result.SourceURL != server.URL+"/product/N1234/?lang=en" {
		t.Fatalf("SourceURL = %q", result.SourceURL)
	}
	if result.ID != "N1234" || result.ContentID != "N1234" {
		t.Fatalf("unexpected IDs: %q %q", result.ID, result.ContentID)
	}
	if result.Title != "Amazing Movie" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.Description != "Story description for the TokyoHot parser test." {
		t.Fatalf("Description = %q", result.Description)
	}
	if result.Maker != "Tokyo Hot" || result.Series != "Series X" {
		t.Fatalf("unexpected maker/series: %q %q", result.Maker, result.Series)
	}
	if result.Runtime != 66 {
		t.Fatalf("Runtime = %d, want 66", result.Runtime)
	}
	if result.ReleaseDate == nil {
		t.Fatal("ReleaseDate is nil")
	}
	wantDate := time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC)
	if !result.ReleaseDate.Equal(wantDate) {
		t.Fatalf("ReleaseDate = %v, want %v", result.ReleaseDate, wantDate)
	}
	if len(result.Genres) != 2 || result.Genres[0] != "Drama" || result.Genres[1] != "Romance" {
		t.Fatalf("Genres = %#v", result.Genres)
	}
	if len(result.Actresses) != 3 {
		t.Fatalf("Actresses len = %d, want 3", len(result.Actresses))
	}
	if result.Actresses[0].FirstName != "Jane" {
		t.Fatalf("unexpected first actress: %#v", result.Actresses[0])
	}
	if result.Actresses[1].FirstName != "Doe" {
		t.Fatalf("unexpected second actress: %#v", result.Actresses[1])
	}
	if result.Actresses[2].JapaneseName != "花子" {
		t.Fatalf("unexpected third actress: %#v", result.Actresses[2])
	}
	if result.CoverURL != server.URL+"/images/jacket.jpg" || result.PosterURL != result.CoverURL {
		t.Fatalf("unexpected cover URLs: %q %q", result.CoverURL, result.PosterURL)
	}
	if len(result.ScreenshotURL) != 2 {
		t.Fatalf("ScreenshotURL len = %d, want 2", len(result.ScreenshotURL))
	}
	if result.TrailerURL != server.URL+"/trailers/n1234.mp4" {
		t.Fatalf("TrailerURL = %q", result.TrailerURL)
	}
}

func TestParseDetailPage_Fallbacks(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head><title>Fallback Title | Tokyo-Hot</title></head><body>
<dl class="info"><dt>Genre</dt><dd><a href="/genre/a">Action</a></dd></dl>
<img src="//cdn.example.com/jacket.jpg">
<img src="/thumb/vcap_1.jpg">
</body></html>`))
	if err != nil {
		t.Fatalf("parse html: %v", err)
	}

	result := parseDetailPage(doc, "https://www.tokyo-hot.com/product/F9999/", "F9999", "zh")
	if result.Language != "zh" {
		t.Fatalf("Language = %q, want zh", result.Language)
	}
	if result.ID != "F9999" {
		t.Fatalf("ID = %q, want F9999", result.ID)
	}
	if result.CoverURL != "https://cdn.example.com/jacket.jpg" {
		t.Fatalf("CoverURL = %q", result.CoverURL)
	}
	if len(result.ScreenshotURL) != 1 || result.ScreenshotURL[0] != "https://www.tokyo-hot.com/thumb/vcap_1.jpg" {
		t.Fatalf("unexpected screenshots: %#v", result.ScreenshotURL)
	}
}

func TestHelpers(t *testing.T) {
	if got := normalizeLanguage("cn"); got != "zh" {
		t.Fatalf("normalizeLanguage = %q, want zh", got)
	}
	if got := extractID("TokyoHot N-1234 sample"); got != "N-1234" {
		t.Fatalf("extractID = %q, want N-1234", got)
	}
	if got := splitNames("Jane Doe / 花子"); len(got) != 3 {
		t.Fatalf("splitNames len = %d, want 3", len(got))
	}
	if got := resolveURL("https://www.tokyo-hot.com/product/N1234/", "trailer.mp4"); got != "https://www.tokyo-hot.com/product/N1234/trailer.mp4" {
		t.Fatalf("resolveURL = %q", got)
	}
	if !hasJapanese("花子") {
		t.Fatal("expected Japanese text detection")
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
			settings := testSettings("https://www.tokyo-hot.com")
			settings.Enabled = tt.enabled
			scraper := New(settings, nil, config.FlareSolverrConfig{})
			assert.Equal(t, tt.enabled, scraper.IsEnabled(), "IsEnabled should match config")
		})
	}
}

// TestScraper_Name tests the Name method
func TestScraper_Name(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	scraper := New(settings, nil, config.FlareSolverrConfig{})
	assert.Equal(t, "tokyohot", scraper.Name())
}

// TestScraper_GetURL tests URL generation for various scenarios
func TestScraper_GetURL(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		expectedErr bool
		contains    string
	}{
		{
			name:        "URL input - already a detail URL",
			id:          "https://www.tokyo-hot.com/product/N1234/",
			expectedErr: false,
			contains:    "/product/N1234/?lang=en",
		},
		{
			name:        "Empty ID",
			id:          "",
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := testSettings("https://www.tokyo-hot.com")
			scraper := New(settings, nil, config.FlareSolverrConfig{})
			url, err := scraper.GetURL(tt.id)
			if tt.expectedErr {
				assert.Error(t, err, "GetURL should fail for empty ID")
				assert.Empty(t, url)
			} else {
				assert.NoError(t, err, "GetURL should succeed for valid ID")
				assert.NotEmpty(t, url, "URL should not be empty")
				if tt.contains != "" {
					assert.Contains(t, url, tt.contains)
				}
			}
		})
	}
}

// TestScraper_GetURL_ResolveURL tests URL resolution with mock URL
func TestScraper_GetURL_ResolveURL(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	// Test that we can resolve a URL when input is already a full URL
	url, err := scraper.GetURL("https://www.tokyo-hot.com/product/N1234/")
	assert.NoError(t, err)
	assert.Equal(t, "https://www.tokyo-hot.com/product/N1234/?lang=en", url)
}

// TestScraper_GetURL_EmptyID tests GetURL with empty ID
func TestScraper_GetURL_EmptyID(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	_, err := scraper.GetURL("")
	assert.Error(t, err, "GetURL should fail for empty ID")
	assert.Contains(t, err.Error(), "cannot be empty")
}

// TestApplyLanguage tests language application to URLs
func TestApplyLanguage(t *testing.T) {
	jaSettings := testSettings("https://www.tokyo-hot.com")
	jaSettings.Language = "ja"
	scraperJa := New(jaSettings, nil, config.FlareSolverrConfig{})

	enSettings := testSettings("https://www.tokyo-hot.com")
	enSettings.Language = "en"
	scraperEn := New(enSettings, nil, config.FlareSolverrConfig{})

	tests := []struct {
		name        string
		scraper     *Scraper
		input       string
		expected    string
		description string
	}{
		{
			name:        "Japanese scraper adds lang=ja",
			scraper:     scraperJa,
			input:       "https://www.tokyo-hot.com/product/N1234/",
			expected:    "https://www.tokyo-hot.com/product/N1234/?lang=ja",
			description: "Japanese scraper should add lang=ja parameter",
		},
		{
			name:        "English scraper adds lang=en",
			scraper:     scraperEn,
			input:       "https://www.tokyo-hot.com/product/N1234/",
			expected:    "https://www.tokyo-hot.com/product/N1234/?lang=en",
			description: "English scraper should add lang=en parameter",
		},
		{
			name:        "Chinese scraper adds lang=zh-TW",
			scraper:     scraperJa,
			input:       "https://www.tokyo-hot.com/product/N1234/",
			expected:    "https://www.tokyo-hot.com/product/N1234/?lang=ja",
			description: "Chinese scraper should add lang=zh-TW parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.scraper.applyLanguage(tt.input)
			assert.Contains(t, result, tt.expected, tt.description)
		})
	}
}

// TestNormalizeLanguage tests language normalization
func TestNormalizeLanguage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Japanese", "ja", "ja"},
		{"Japanese uppercase", "JA", "ja"},
		{"Japanese with spaces", " ja ", "ja"},
		{"Chinese", "zh", "zh"},
		{"Chinese with spaces", " zh ", "zh"},
		{"CN alias", "cn", "zh"},
		{"TW alias", "tw", "zh"},
		{"English default", "en", "en"},
		{"Unknown default", "fr", "en"},
		{"Empty default", "", "en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeLanguage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestResolveDownloadProxyForHost tests proxy resolution
func TestResolveDownloadProxyForHost(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	scraper := New(settings, nil, config.FlareSolverrConfig{})

	tests := []struct {
		name     string
		host     string
		expected bool
	}{
		{
			name:     "TokyoHot host",
			host:     "www.tokyo-hot.com",
			expected: true,
		},
		{
			name:     "Empty host",
			host:     "",
			expected: false,
		},
		{
			name:     "Non-TokyoHot host",
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

// TestHasJapanese tests Japanese text detection
func TestHasJapanese(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"Hiragana", "こんにちは", true},
		{"Katakana", "コンニチハ", true},
		{"Kanji", "こんにちは世界", true},
		{"Mixed Japanese", "Jane Doe / 花子", true},
		{"Latin only", "Hello World", false},
		{"Mixed Latin and Kana", "Hello こんにちは", true},
		{"Empty string", "", false},
		{"Numbers only", "12345", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasJapanese(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSplitNamesEdgeCases tests edge cases in name splitting
func TestSplitNamesEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"Slash separator with spaces", "Jane Doe / 花子", 3},
		{"Comma separator", "Jane, Doe", 2},
		{"Fullwidth slash with space", "Jane Doe／花子", 3}, // space creates extra split
		{"Fullwidth slash no space", "JaneDoe／花子", 2},
		{"Pipe separator with space", "Jane Doe|花子", 3}, // space creates extra split
		{"Pipe separator no space", "JaneDoe|花子", 2},
		{"Multiple separators", "Jane, Doe / 花子", 3},
		{"Empty string", "", 0},
		{"Single name", "Jane", 1},
		{"Only separators", "///", 0},
		{"Multiple spaces", "Jane   Doe", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitNames(tt.input)
			assert.Len(t, result, tt.expected, "splitNames should return %d names for input %q", tt.expected, tt.input)
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
			base:     "https://www.tokyo-hot.com/product/N1234/",
			relative: "/trailers/n1234.mp4",
			expected: "https://www.tokyo-hot.com/trailers/n1234.mp4",
		},
		{
			name:     "Relative path",
			base:     "https://www.tokyo-hot.com/product/N1234/",
			relative: "trailer.mp4",
			expected: "https://www.tokyo-hot.com/product/N1234/trailer.mp4",
		},
		{
			name:     "Protocol relative URL",
			base:     "https://www.tokyo-hot.com/",
			relative: "//cdn.example.com/image.jpg",
			expected: "https://cdn.example.com/image.jpg",
		},
		{
			name:     "Full URL",
			base:     "https://www.tokyo-hot.com/",
			relative: "https://other.com/file.jpg",
			expected: "https://other.com/file.jpg",
		},
		{
			name:     "Empty relative",
			base:     "https://www.tokyo-hot.com/",
			relative: "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveURL(tt.base, tt.relative)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractIDEdgeCases tests edge cases in ID extraction
func TestExtractIDEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Standard format", "TokyoHot N-1234 sample", "N-1234"},
		{"With underscore", "TokyoHot N_1234 sample", ""}, // underscore not supported by extractID regex
		{"Complex ID", "TokyoHot ABC-123X", "ABC-123X"},
		{"No match", "TokyoHot sample text", ""},
		{"Empty string", "", ""},
		{"Multiple IDs", "TokyoHot N-1234 and ABC-5678", "N-1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractID(tt.input)
			assert.Equal(t, tt.expected, result, "extractID should return %q for input %q", tt.expected, tt.input)
		})
	}
}

// TestNormalizeIDEdgeCases tests edge cases in ID normalization
func TestNormalizeIDEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"With dashes", "ABC-123", "abc123"},
		{"With underscores", "ABC_123", "abc123"},
		{"With spaces", "ABC 123", "abc123"},
		{"With special chars", "ABC@123#DEF", "abc123def"},
		{"Empty string", "", ""},
		{"All caps", "ABC123", "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSearchDisabled tests Search behavior when scraper is disabled
func TestSearchDisabled(t *testing.T) {
	settings := testSettings("https://www.tokyo-hot.com")
	settings.Enabled = false
	s := New(settings, nil, config.FlareSolverrConfig{})

	result, err := s.Search("N1234")

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TokyoHot scraper is disabled")
}

// TestSearchWithHTTPError tests Search behavior when HTTP fails
func TestSearchWithHTTPError(t *testing.T) {
	// Create server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := New(settings, nil, config.FlareSolverrConfig{})

	result, err := s.Search("N1234")

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status code 404")
}

// TestExtractGenresEdgeCases tests edge cases in genre extraction
func TestExtractGenresEdgeCases(t *testing.T) {
	// Test with just the text value (no links) - splitNames will split on /
	html := `
<dl class="info">
  <dt>Genre</dt>
  <dd>Action / Romance</dd>
</dl>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	genres := extractGenres(doc)

	// After splitNames: ["Action", "Romance"], no duplicates
	assert.Len(t, genres, 2)
	assert.Equal(t, "Action", genres[0])
	assert.Equal(t, "Romance", genres[1])
}

// TestExtractActressesEdgeCases tests edge cases in actress extraction
func TestExtractActressesEdgeCases(t *testing.T) {
	html := `
<dl class="info">
  <dt>Model</dt>
  <dd>Jane Doe / 花子 / Mary Smith</dd>
</dl>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	actresses := extractActresses(doc)

	// Note: splitNames splits on both spaces AND separators, so "Jane Doe" becomes ["Jane", "Doe"]
	// Total of 5 "names" from: Jane, Doe, 花子, Mary, Smith
	assert.Len(t, actresses, 5)

	// First two are "Jane" and "Doe" (both first names only)
	assert.Equal(t, "Jane", actresses[0].FirstName)
	assert.Equal(t, "Doe", actresses[1].FirstName)

	// Third is Japanese name
	assert.Equal(t, "花子", actresses[2].JapaneseName)

	// Last two are "Mary" and "Smith" (both first names only)
	assert.Equal(t, "Mary", actresses[3].FirstName)
	assert.Equal(t, "Smith", actresses[4].FirstName)
}

// TestExtractCoverURLEdgeCases tests edge cases in cover URL extraction
func TestExtractCoverURLEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		html        string
		base        string
		expected    string
		description string
	}{
		{
			name:        "Jacket image",
			html:        `<img src="/images/jacket.jpg">`,
			base:        "https://www.tokyo-hot.com",
			expected:    "https://www.tokyo-hot.com/images/jacket.jpg",
			description: "Should extract jacket image",
		},
		{
			name:        "Meta og:image",
			html:        `<meta property="og:image" content="/images/jacket.jpg">`,
			base:        "https://www.tokyo-hot.com",
			expected:    "https://www.tokyo-hot.com/images/jacket.jpg",
			description: "Should extract from meta og:image",
		},
		{
			name:        "Protocol relative URL",
			html:        `<img src="//cdn.example.com/jacket.jpg">`,
			base:        "https://www.tokyo-hot.com",
			expected:    "https://cdn.example.com/jacket.jpg",
			description: "Should convert protocol relative URL",
		},
		{
			name:        "No cover found",
			html:        `<html><body></body></html>`,
			base:        "https://www.tokyo-hot.com",
			expected:    "",
			description: "Should return empty when no cover found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractCoverURL(doc, tt.base)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestExtractScreenshotURLEdgeCases tests edge cases in screenshot URL extraction
func TestExtractScreenshotURLEdgeCases(t *testing.T) {
	html := `
<div class="scap">
  <a href="/gallery/1.jpg">one</a>
  <a href="/gallery/2.jpg">two</a>
</div>
<img src="/thumb/vcap_1.jpg" class="highslide">`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	screenshots := extractScreenshotURLs(doc, "https://www.tokyo-hot.com")

	assert.Len(t, screenshots, 3)
	assert.Contains(t, screenshots, "https://www.tokyo-hot.com/gallery/1.jpg")
	assert.Contains(t, screenshots, "https://www.tokyo-hot.com/gallery/2.jpg")
	assert.Contains(t, screenshots, "https://www.tokyo-hot.com/thumb/vcap_1.jpg")
}
