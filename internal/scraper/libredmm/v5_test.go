package libredmm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchV5_FullE2E tests Search with httptest JSON server
func TestSearchV5_FullE2E(t *testing.T) {
	payload := moviePayload{
		NormalizedID:  "IPX-999",
		Title:         "Test Movie",
		Date:          "2024-01-15T00:00:00Z",
		Description:   "A great movie",
		Volume:        7200, // 120 min
		Directors:     []string{"DirectorA"},
		Makers:        []string{"MakerA"},
		Labels:        []string{"LabelA"},
		Genres:        []string{"Action", "Drama"},
		CoverImageURL: "https://pics.dmm.co.jp/cover.jpg",
		Actresses: []actressPayload{
			{Name: "Actress A", ImageURL: "https://pics.dmm.co.jp/actress.jpg"},
		},
		Review: 4.5,
		SampleImageURLs: []string{
			"https://pics.dmm.co.jp/sample1.jpg",
			"https://pics.dmm.co.jp/sample2.jpg",
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		data, _ := json.Marshal(payload)
		w.Write(data)
	}))
	defer ts.Close()

	s := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    100 * time.Millisecond,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-999", result.ID)
	assert.Equal(t, "Test Movie", result.Title)
	assert.Equal(t, "MakerA", result.Maker)
	assert.Equal(t, "DirectorA", result.Director)
	assert.Equal(t, "LabelA", result.Label)
	assert.Equal(t, 120, result.Runtime)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 1)
	assert.NotNil(t, result.Rating)
	assert.Equal(t, 4.5, result.Rating.Score)
}

// TestSearchV5_Disabled tests Search when disabled
func TestSearchV5_Disabled(t *testing.T) {
	s := &scraper{
		client:          resty.New(),
		enabled:         false,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    100 * time.Millisecond,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: false},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV5_NotFound tests Search with 404
func TestSearchV5_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		w.Write([]byte(`{"err":"not found"}`))
	}))
	defer ts.Close()

	s := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    100 * time.Millisecond,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "NOTFOUND-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestSearchV5_Processing202 tests Search with 202 processing status
func TestSearchV5_Processing202(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount <= 2 {
			w.WriteHeader(202)
			w.Write([]byte(`{"err":"processing"}`))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"normalized_id":"IPX-001","title":"Found Movie","date":"2024-01-15","makers":["TestMaker"],"volume":5400}`))
	}))
	defer ts.Close()

	s := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    10 * time.Millisecond,
		maxPollAttempts: 3,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "IPX-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-001", result.ID)
}

// TestSearchV5_502 tests Search with 502 error
func TestSearchV5_502(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		w.Write([]byte(`{"err":"bad gateway"}`))
	}))
	defer ts.Close()

	s := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    100 * time.Millisecond,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestScrapeURLV5_Disabled tests ScrapeURL when disabled
func TestScrapeURLV5_Disabled(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	result, err := s.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}

// TestGetURLV5 tests GetURL
func TestGetURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		baseURL:  "https://www.libredmm.com",
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := s.GetURL(context.Background(), "")
		assert.Error(t, err)
	})

	t.Run("valid ID builds search URL", func(t *testing.T) {
		url, err := s.GetURL(context.Background(), "IPX-999")
		require.NoError(t, err)
		assert.Contains(t, url, "search")
		assert.Contains(t, url, "IPX-999")
	})

	t.Run("HTTP URL normalizes", func(t *testing.T) {
		url, err := s.GetURL(context.Background(), "https://www.libredmm.com/movies/IPX-999")
		require.NoError(t, err)
		assert.Contains(t, url, "/movies/")
	})
}

// TestCanHandleURLV5 tests CanHandleURL
func TestCanHandleURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.libredmm.com/movies/IPX-999", true},
		{"https://libredmm.com/search?q=test", true},
		{"https://example.com/movies/IPX-999", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandleURL(tt.url))
		})
	}
}

// TestExtractIDFromURLV5 tests URL ID extraction
func TestExtractIDFromURLV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	tests := []struct {
		name        string
		url         string
		expected    string
		expectError bool
	}{
		{"movies path", "https://www.libredmm.com/movies/IPX-999", "IPX-999", false},
		{"search query", "https://www.libredmm.com/search?q=IPX-999", "IPX-999", false},
		{"cid path", "https://www.libredmm.com/cid/IPX-999", "IPX-999", false},
		{"no extractable ID", "https://www.libredmm.com/", "", true},
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

// TestPayloadToResultV5 tests payload to result conversion
func TestPayloadToResultV5(t *testing.T) {
	payload := &moviePayload{
		NormalizedID:  "IPX-001",
		Title:         "Payload Movie",
		Date:          "2024-06-01",
		Description:   "Test description",
		Volume:        5400, // 90 min
		Directors:     []string{"DirA"},
		Makers:        []string{"MakerA"},
		Labels:        []string{"LabelA"},
		Genres:        []string{"Action", "Drama"},
		CoverImageURL: "https://pics.dmm.co.jp/cover.jpg",
		Actresses: []actressPayload{
			{Name: "Actress A"},
		},
		Review:          3.5,
		SampleImageURLs: []string{"https://pics.dmm.co.jp/s1.jpg"},
	}

	result := payloadToResult(payload, "https://www.libredmm.com/movies/IPX-001", "IPX-001", nil)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-001", result.ID)
	assert.Equal(t, "Payload Movie", result.Title)
	assert.Equal(t, "MakerA", result.Maker)
	assert.Equal(t, "DirA", result.Director)
	assert.Equal(t, "LabelA", result.Label)
	assert.Equal(t, 90, result.Runtime)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 1)
	assert.NotNil(t, result.Rating)
}

// TestPayloadToResultV5_NilPayload tests nil payload handling
func TestPayloadToResultV5_NilPayload(t *testing.T) {
	result := payloadToResult(nil, "https://www.libredmm.com/movies/IPX-001", "IPX-001", nil)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-001", result.ID)
}

// TestPayloadToResultV5_EmptyPayload tests empty payload
func TestPayloadToResultV5_EmptyPayload(t *testing.T) {
	result := payloadToResult(&moviePayload{}, "https://www.libredmm.com/movies/EMPTY-001", "EMPTY-001", nil)
	require.NotNil(t, result)
	assert.Equal(t, "EMPTY-001", result.ID)
	assert.Equal(t, "EMPTY-001", result.Title) // Falls back to ID
}

// TestParseReleaseDateV5 tests release date parsing
func TestParseReleaseDateV5(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"2024-01-15T00:00:00Z", true},
		{"2024-01-15", true},
		{"", false},
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseReleaseDate(tt.input)
			if tt.expected {
				assert.NotNil(t, result)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

// TestParseActressesV5 tests actress parsing
func TestParseActressesV5(t *testing.T) {
	tests := []struct {
		name     string
		entries  []actressPayload
		expected int
	}{
		{"with entries", []actressPayload{{Name: "Actress A"}, {Name: "Actress B"}}, 2},
		{"empty name filtered", []actressPayload{{Name: ""}, {Name: "Valid"}}, 1},
		{"duplicates filtered", []actressPayload{{Name: "Same"}, {Name: "Same"}}, 1},
		{"nil entries", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseActresses(tt.entries, "https://www.libredmm.com")
			assert.Equal(t, tt.expected, len(result))
		})
	}
}

// TestDedupeStringsV5 tests string deduplication
func TestDedupeStringsV5(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected int
	}{
		{"dedup", []string{"Action", "Drama", "Action"}, 2},
		{"empty filtered", []string{"", "Valid", ""}, 1},
		{"nil input", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dedupeStrings(tt.input)
			assert.Equal(t, tt.expected, len(result))
		})
	}
}

// TestFirstNonEmptyV5 tests first non-empty extraction
func TestFirstNonEmptyV5(t *testing.T) {
	assert.Equal(t, "first", firstNonEmpty([]string{"first", "second"}))
	assert.Equal(t, "second", firstNonEmpty([]string{"", "second"}))
	assert.Equal(t, "", firstNonEmpty([]string{}))
	assert.Equal(t, "", firstNonEmpty(nil))
}

// TestNormalizeMovieURLV5 tests URL normalization
func TestNormalizeMovieURLV5(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"libredmm movies URL", "https://www.libredmm.com/movies/IPX-999", true},
		{"libredmm cid URL", "https://www.libredmm.com/cid/IPX-999", true},
		{"libredmm search URL", "https://www.libredmm.com/search?q=IPX-999", true},
		{"non-http", "not-a-url", false},
		{"other domain", "https://example.com/movies/IPX-999", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := normalizeMovieURL(tt.input, "https://www.libredmm.com")
			assert.Equal(t, tt.expected, ok)
		})
	}
}

// TestBuildSearchURLV5 tests search URL building
func TestBuildSearchURLV5(t *testing.T) {
	result := buildSearchURL("https://www.libredmm.com", "IPX-999")
	assert.Contains(t, result, "/search")
	assert.Contains(t, result, "IPX-999")
	assert.Contains(t, result, "format=json")
}

// TestExtractIDFromURLV5_Function tests the extractIDFromURL function
func TestExtractIDFromURLV5_Function(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://www.libredmm.com/movies/IPX-999", "IPX-999"},
		{"https://www.libredmm.com/cid/ABC-123", "ABC-123"},
		{"", ""},
		{"not-a-url", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractIDFromURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestStripJSONSuffixV5 tests JSON suffix stripping
func TestStripJSONSuffixV5(t *testing.T) {
	result := stripJSONSuffix("https://www.libredmm.com/movies/IPX-999.json")
	assert.NotContains(t, result, ".json")
	assert.Contains(t, result, "IPX-999")
}

// TestToHTTPSV5 tests HTTP to HTTPS conversion
func TestToHTTPSV5(t *testing.T) {
	assert.Equal(t, "https://example.com", toHTTPS("http://example.com"))
	assert.Equal(t, "https://example.com", toHTTPS("https://example.com"))
	assert.Equal(t, "", toHTTPS(""))
}

// TestStripANSICodesV5 tests ANSI code stripping
func TestStripANSICodesV5(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"ANSI escape", "\x1b[32m{\"key\":\"value\"}\x1b[0m", "{\"key\":\"value\"}"},
		{"bare ESC", "\x1b{\"key\":\"value\"}", "{\"key\":\"value\"}"},
		{"control chars", "prefix\x00{\"key\":\"value\"}", "{\"key\":\"value\"}"},
		{"clean JSON", `{"key":"value"}`, `{"key":"value"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripANSICodes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNewScraperV5 tests scraper creation
func TestNewScraperV5(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "https://www.libredmm.com",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.True(t, s.enabled)
	assert.Equal(t, "https://www.libredmm.com", s.baseURL)
}

// TestNewScraperV5_DefaultBaseURL tests default base URL
func TestNewScraperV5_DefaultBaseURL(t *testing.T) {
	settings := &models.ScraperSettings{
		Enabled: true,
		BaseURL: "",
	}

	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	require.NotNil(t, s)
	assert.Equal(t, defaultBaseURL, s.baseURL)
}

// TestResolveDownloadProxyForHostV5 tests proxy resolution
func TestResolveDownloadProxyForHostV5(t *testing.T) {
	s := &scraper{
		enabled:  true,
		settings: models.ScraperSettings{Enabled: true},
	}

	t.Run("libredmm.com", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("libredmm.com")
		assert.True(t, ok)
	})

	t.Run("empty", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("")
		assert.False(t, ok)
	})

	t.Run("unrelated", func(t *testing.T) {
		_, _, ok := s.ResolveDownloadProxyForHost("example.com")
		assert.False(t, ok)
	})
}

// TestDedupeResolvedURLsV5 tests URL deduplication with resolution
func TestDedupeResolvedURLsV5(t *testing.T) {
	urls := []string{
		"https://pics.dmm.co.jp/s1.jpg",
		"https://pics.dmm.co.jp/s2.jpg",
		"https://pics.dmm.co.jp/s1.jpg", // duplicate
	}

	result := dedupeResolvedURLs(urls, "https://www.libredmm.com")
	assert.Equal(t, 2, len(result))
}

// TestSearchV5_PollingTimeout tests Search when polling exceeds max attempts
func TestSearchV5_PollingTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		w.Write([]byte(`{"err":"still processing"}`))
	}))
	defer ts.Close()

	s := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    10 * time.Millisecond,
		maxPollAttempts: 2,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "still processing")
}

// TestSearchV5_ErrorInPayload tests Search when payload has error message
func TestSearchV5_ErrorInPayload(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"err":"Something went wrong"}`))
	}))
	defer ts.Close()

	s := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    100 * time.Millisecond,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Something went wrong")
}

// TestScrapeURLV5_NonLibreDMMURL tests ScrapeURL rejecting non-LibreDMM URLs
func TestScrapeURLV5_NonLibreDMMURL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://example.com/movies/IPX-999")
	assert.Nil(t, result)
	assert.Error(t, err)
}
