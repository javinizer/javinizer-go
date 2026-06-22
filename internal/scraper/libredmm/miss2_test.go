package libredmm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapeURL: disabled scraper ---

func TestMiss2_ScrapeURL_Disabled(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	settings.Enabled = false
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- ScrapeURL: unhandled URL ---

func TestMiss2_ScrapeURL_UnhandledURL(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://example.com/movies/IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- ScrapeURL: 202 processing status (then succeed) ---

func TestMiss2_ScrapeURL_Status202_Then200(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"err": "processing"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"normalized_id":"IPX-535","title":"Test Movie","url":"https://www.libredmm.com/movies/IPX-535"}`))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	settings.RateLimit = 0
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	s.pollInterval = 10 * time.Millisecond // Speed up polling

	result, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "libredmm", result.Source)
}

// --- ScrapeURL: 202 processing status then max attempts ---

func TestMiss2_ScrapeURL_Status202_MaxAttempts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"err": "processing"}`))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	settings.RateLimit = 0
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	s.pollInterval = 1 * time.Millisecond
	s.maxPollAttempts = 2

	_, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still processing")
}

// --- ScrapeURL: 502 status ---

func TestMiss2_ScrapeURL_Status502(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	settings.RateLimit = 0
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})

	_, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "502")
}

// --- ScrapeURL: 502 with error message ---

func TestMiss2_ScrapeURL_Status502_WithMsg(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"err": "gateway timeout"}`))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	settings.RateLimit = 0
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})

	_, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "gateway timeout")
}

// --- ScrapeURL: other non-200 status ---

func TestMiss2_ScrapeURL_OtherStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	settings.RateLimit = 0
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})

	_, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
}

// --- ScrapeURL: error in payload ---

func TestMiss2_ScrapeURL_PayloadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"err": "something went wrong"}`))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	settings.RateLimit = 0
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})

	_, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "something went wrong")
}

// --- normalizeMovieURL: non-HTTP input ---

func TestMiss2_NormalizeMovieURL_NonHTTP(t *testing.T) {
	url, ok := normalizeMovieURL("not-a-url", "https://libredmm.com")
	assert.False(t, ok)
	assert.Empty(t, url)
}

// --- normalizeMovieURL: search URL ---

func TestMiss2_NormalizeMovieURL_SearchURL(t *testing.T) {
	url, ok := normalizeMovieURL("https://libredmm.com/search?q=IPX-535", "https://libredmm.com")
	assert.True(t, ok)
	assert.Contains(t, url, "search")
}

// --- normalizeMovieURL: non-libredmm host ---

func TestMiss2_NormalizeMovieURL_WrongHost(t *testing.T) {
	url, ok := normalizeMovieURL("https://example.com/movies/IPX-535", "https://libredmm.com")
	assert.False(t, ok)
	assert.Empty(t, url)
}

// --- extractIDFromURL ---

func TestMiss2_ExtractIDFromURL(t *testing.T) {
	assert.Equal(t, "IPX-535", extractIDFromURL("https://libredmm.com/movies/IPX-535"))
	assert.Equal(t, "IPX-535", extractIDFromURL("https://libredmm.com/movies/IPX-535.json"))
	assert.Equal(t, "", extractIDFromURL(""))
	assert.Equal(t, "", extractIDFromURL("https://example.com/"))
}

// --- stripJSONSuffix ---

func TestMiss2_StripJSONSuffix(t *testing.T) {
	assert.Equal(t, "https://libredmm.com/movies/IPX-535", stripJSONSuffix("https://libredmm.com/movies/IPX-535.json"))
	assert.Equal(t, "plain", stripJSONSuffix("plain"))
}

// --- toHTTPS ---

func TestMiss2_ToHTTPS(t *testing.T) {
	assert.Equal(t, "https://example.com", toHTTPS("http://example.com"))
	assert.Equal(t, "https://example.com", toHTTPS("https://example.com"))
}

// --- stripANSICodes ---

func TestMiss2_StripANSICodes(t *testing.T) {
	// Strip ANSI escape sequences
	assert.Equal(t, "hello", stripANSICodes("\x1b[32mhello\x1b[0m"))
	assert.Equal(t, `{"key":"val"}`, stripANSICodes(`{"key":"val"}`))
}

// --- buildSearchURL ---

func TestMiss2_BuildSearchURL(t *testing.T) {
	url := buildSearchURL("https://libredmm.com", "IPX-535")
	assert.Contains(t, url, "search?q=IPX-535")
	assert.Contains(t, url, "format=json")
}

// --- ScrapeURL: 202 with context cancellation during poll ---

func TestMiss2_ScrapeURL_Status202_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"err": "processing"}`))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	settings.RateLimit = 0
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	s.pollInterval = 30 * time.Second // Long poll
	s.maxPollAttempts = 100

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := s.ScrapeURL(ctx, "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
}

// --- ScrapeURL: other status with error message ---

func TestMiss2_ScrapeURL_OtherStatus_WithMsg(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"err": "maintenance mode"}`))
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	settings.RateLimit = 0
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})

	_, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "maintenance mode")
}

// --- fetchMovieJSONCtx: network error ---

func TestMiss2_FetchMovieJSONCtx_NetworkError(t *testing.T) {
	settings := testSettings("http://127.0.0.1:1") // Port 1 is unreachable
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})

	_, _, _, err := s.fetchMovieJSONCtx(context.Background(), "http://127.0.0.1:1/movies/IPX-535.json")
	require.Error(t, err)
}
