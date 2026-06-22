package libredmm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchV3_SuccessWithMockServer tests Search with a mock JSON server
func TestSearchV3_SuccessWithMockServer(t *testing.T) {
	payload := moviePayload{
		NormalizedID:  "AB-123",
		Title:         "Test Movie Title",
		Date:          "2024-01-15",
		Volume:        7200,
		Directors:     []string{"Director A"},
		Makers:        []string{"Studio X"},
		Labels:        []string{"Label Y"},
		Genres:        []string{"Drama", "Romance"},
		CoverImageURL: "https://example.com/cover.jpg",
		Actresses: []actressPayload{
			{Name: "山田花子", ImageURL: "https://example.com/actress.jpg"},
		},
		Review: 4.5,
	}
	payloadJSON, _ := json.Marshal(payload)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, string(payloadJSON))
	}))
	defer ts.Close()

	scraper := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "AB-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "AB-123", result.ID)
	assert.Equal(t, "Test Movie Title", result.Title)
	assert.Equal(t, "Studio X", result.Maker)
	assert.Equal(t, "Director A", result.Director)
	assert.Equal(t, 120, result.Runtime) // 7200 / 60 = 120
}

// TestScrapeURLV3_SuccessWithMockServer tests ScrapeURL with a mock JSON server
func TestScrapeURLV3_SuccessWithMockServer(t *testing.T) {
	payload := moviePayload{
		NormalizedID:  "CD-456",
		Title:         "ScrapeURL Movie",
		Date:          "2024-02-20",
		Volume:        5400,
		Makers:        []string{"Studio Z"},
		Genres:        []string{"Action"},
		CoverImageURL: "https://example.com/cover2.jpg",
	}
	payloadJSON, _ := json.Marshal(payload)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, string(payloadJSON))
	}))
	defer ts.Close()

	scraper := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	// Use a libredmm.com URL so CanHandleURL passes
	result, err := scraper.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/CD-456")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "CD-456", result.ID)
}

// TestScrapeURLV3_DisabledScraper tests ScrapeURL with disabled scraper
func TestScrapeURLV3_DisabledScraper(t *testing.T) {
	scraper := &scraper{
		client:          resty.New(),
		enabled:         false,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/AB-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV3_DisabledScraper tests Search with disabled scraper
func TestSearchV3_DisabledScraper(t *testing.T) {
	scraper := &scraper{
		client:          resty.New(),
		enabled:         false,
		baseURL:         "https://www.libredmm.com",
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "AB-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV3_404Response tests Search when server returns 404
func TestSearchV3_404Response(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"err":"not found"}`)
	}))
	defer ts.Close()

	scraper := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "MISSING-001")
	require.Error(t, err)
}

// TestSearchV3_502Response tests Search when server returns 502
func TestSearchV3_502Response(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, `{"err":"bad gateway"}`)
	}))
	defer ts.Close()

	scraper := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "AB-123")
	require.Error(t, err)
}

// TestSearchV3_202Polling tests Search when server returns 202 (processing)
func TestSearchV3_202Polling(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount < 2 {
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"err":"processing"}`)
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"normalized_id":"AB-789","title":"Polled Movie","makers":["Studio P"]}`)
		}
	}))
	defer ts.Close()

	scraper := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 3,
		settings:        models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "AB-789")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "AB-789", result.ID)
	assert.Equal(t, "Polled Movie", result.Title)
}

// TestSearchV3_PollTimeout tests Search when 202 polling exhausts attempts
func TestSearchV3_PollTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprint(w, `{"err":"still processing"}`)
	}))
	defer ts.Close()

	scraper := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 2,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "AB-999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still processing")
}

// TestSearchV3_ErrorInPayload tests Search when payload contains an error message
func TestSearchV3_ErrorInPayload(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"err":"movie not found in database"}`)
	}))
	defer ts.Close()

	scraper := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "AB-404")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "movie not found in database")
}

// TestSearchV3_OtherStatus tests Search with unexpected status code
func TestSearchV3_OtherStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"err":"internal error"}`)
	}))
	defer ts.Close()

	scraper := &scraper{
		client:          resty.New().SetBaseURL(ts.URL),
		enabled:         true,
		baseURL:         ts.URL,
		rateLimiter:     ratelimit.NewLimiter(0),
		pollInterval:    0,
		maxPollAttempts: 1,
		settings:        models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "AB-500")
	require.Error(t, err)
}

// TestGetURLCtxV3_Various tests getURLCtx with various inputs
func TestGetURLCtxV3_Various(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.libredmm.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := scraper.getURLCtx(context.Background(), "")
		require.Error(t, err)
	})

	t.Run("libredmm URL input", func(t *testing.T) {
		url, err := scraper.getURLCtx(context.Background(), "https://www.libredmm.com/movies/AB-123")
		require.NoError(t, err)
		assert.Contains(t, url, "AB-123")
	})

	t.Run("plain ID", func(t *testing.T) {
		url, err := scraper.getURLCtx(context.Background(), "AB-123")
		require.NoError(t, err)
		assert.Contains(t, url, "AB-123")
	})
}

// TestNormalizeMovieURLV3 tests normalizeMovieURL with various inputs
func TestNormalizeMovieURLV3(t *testing.T) {
	t.Run("non-URL input", func(t *testing.T) {
		_, ok := normalizeMovieURL("AB-123", "https://www.libredmm.com")
		assert.False(t, ok)
	})

	t.Run("different host", func(t *testing.T) {
		_, ok := normalizeMovieURL("https://example.com/movies/AB-123", "https://www.libredmm.com")
		assert.False(t, ok)
	})

	t.Run("search URL with q param", func(t *testing.T) {
		result, ok := normalizeMovieURL("https://www.libredmm.com/search?q=AB-123", "https://www.libredmm.com")
		assert.True(t, ok)
		assert.Contains(t, result, "AB-123")
	})

	t.Run("movies path", func(t *testing.T) {
		result, ok := normalizeMovieURL("https://www.libredmm.com/movies/AB-123", "https://www.libredmm.com")
		assert.True(t, ok)
		assert.Contains(t, result, "AB-123")
	})

	t.Run("cid path", func(t *testing.T) {
		result, ok := normalizeMovieURL("https://www.libredmm.com/cid/AB-123", "https://www.libredmm.com")
		assert.True(t, ok)
		assert.Contains(t, result, "AB-123")
	})

	t.Run("movies path with empty id", func(t *testing.T) {
		_, ok := normalizeMovieURL("https://www.libredmm.com/movies/", "https://www.libredmm.com")
		assert.False(t, ok)
	})
}

// TestStripANSICodesV3 tests stripANSICodes with various inputs
func TestStripANSICodesV3(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"clean JSON", `{"key":"value"}`, `{"key":"value"}`},
		{"ANSI prefix", "\x1b[32m{\"key\":\"value\"}", `{"key":"value"}`},
		{"bare ESC", "\x1b{\"key\":\"value\"}", `{"key":"value"}`},
		{"control chars", "\x00\x01{\"key\":\"value\"}", `{"key":"value"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripANSICodes(tt.input))
		})
	}
}

// TestStripJSONSuffixV3 tests stripJSONSuffix
func TestStripJSONSuffixV3(t *testing.T) {
	assert.Equal(t, "https://www.libredmm.com/movies/AB-123", stripJSONSuffix("https://www.libredmm.com/movies/AB-123.json"))
	assert.Equal(t, "not-a-url", stripJSONSuffix("not-a-url.json"))
	assert.Equal(t, "https://www.libredmm.com/movies/AB-123", stripJSONSuffix("https://www.libredmm.com/movies/AB-123"))
}

// TestExtractIDFromURLV3 tests extractIDFromURL with various inputs
func TestExtractIDFromURLV3(t *testing.T) {
	assert.Equal(t, "AB-123", extractIDFromURL("https://www.libredmm.com/movies/AB-123"))
	assert.Equal(t, "AB-123", extractIDFromURL("https://www.libredmm.com/movies/AB-123.json"))
	assert.Equal(t, "AB-123", extractIDFromURL("https://www.libredmm.com/cid/AB-123"))
	assert.Equal(t, "AB-123", extractIDFromURL("https://www.libredmm.com/cid/AB-123?cid=AB-123"))
	assert.Equal(t, "", extractIDFromURL(""))
}
