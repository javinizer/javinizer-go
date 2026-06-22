package libredmm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Note: ScrapeURL requires CanHandleURL to pass, which checks that the hostname
// is libredmm.com. Since httptest.NewServer uses 127.0.0.1, we use Search
// for HTTP-level integration tests. ScrapeURL-specific status code paths
// (404, 429, 502, etc.) share the same code as Search.

// --- ScrapeURL: not handled URL path ---

func TestScrapeURL_UnhandledURL(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://example.com/movies/IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- ScrapeURL: disabled scraper ---

func TestScrapeURL_Disabled(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	settings.Enabled = false
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- Search with httptest.NewServer: covers all status code paths ---

func TestSearch_MissTest_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := moviePayload{
			NormalizedID:  "IPX-535",
			Subtitle:      "ipx00535",
			Title:         "Test Movie",
			Description:   "A test movie description",
			Date:          "2024-01-15",
			Volume:        7200,
			Directors:     []string{"Director A"},
			Makers:        []string{"Maker A"},
			Labels:        []string{"Label A"},
			Genres:        []string{"Genre A"},
			Actresses:     []actressPayload{{Name: "Actress A", ImageURL: ""}},
			CoverImageURL: "https://example.com/cover.jpg",
			Review:        8.5,
			URL:           "https://www.libredmm.com/movies/IPX-535",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "libredmm", result.Source)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "Test Movie", result.Title)
	assert.Equal(t, 120, result.Runtime) // 7200/60
}

func TestSearch_MissTest_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"err":"not found"}`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "NOTFOUND")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSearch_MissTest_BadGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprint(w, `{"err":"bad gateway"}`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestSearch_MissTest_BadGateway_NoMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func TestSearch_MissTest_OtherErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `{"err":"maintenance mode"}`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestSearch_MissTest_OtherErrorStatus_NoMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestSearch_MissTest_Disabled(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	settings.Enabled = false
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestSearch_MissTest_JSONParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `this is not valid json`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse JSON")
}

func TestSearch_MissTest_PayloadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"err":"movie not indexed yet"}`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "movie not indexed yet")
}

// --- Search: 202 processing then success ---

func TestSearch_MissTest_ProcessingThenSuccess(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{"err":"processing"}`)
			return
		}
		payload := moviePayload{
			NormalizedID: "IPX-535",
			Subtitle:     "ipx00535",
			Title:        "Test Movie",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	s.pollInterval = 1 * time.Millisecond
	s.maxPollAttempts = 3

	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", result.ID)
	assert.True(t, callCount >= 2)
}

// --- Search: 202 with empty error message ---

func TestSearch_MissTest_ProcessingEmptyMessage(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.WriteHeader(http.StatusAccepted)
			_, _ = fmt.Fprint(w, `{}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"normalized_id":"IPX-535","title":"Test"}`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	s.pollInterval = 1 * time.Millisecond
	s.maxPollAttempts = 4

	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", result.ID)
}

// --- Search: cancelled context during poll ---

func TestSearch_MissTest_ContextCancelledDuringPoll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprint(w, `{"err":"processing"}`)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	s.pollInterval = 5 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := s.Search(ctx, "IPX-535")
	require.Error(t, err)
}

// --- Search: success path with full payload ---

func TestSearch_MissTest_FullPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := moviePayload{
			NormalizedID:  "IPX-535",
			Subtitle:      "ipx00535",
			Title:         "Great Movie",
			Description:   "A great movie",
			Date:          "2024-01-15",
			Volume:        7200,
			Directors:     []string{"Dir A"},
			Makers:        []string{"Maker A"},
			Labels:        []string{"Label A"},
			Genres:        []string{"Genre A", "Genre B"},
			Actresses:     []actressPayload{{Name: "花子", ImageURL: ""}},
			CoverImageURL: "https://pics.dmm.co.jp/digital/video/ipx00535/ipx00535pl.jpg",
			Review:        7.5,
			URL:           "https://www.libredmm.com/movies/IPX-535",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	settings := testSettings(server.URL)
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	result, err := s.Search(context.Background(), "IPX-535")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "Great Movie", result.Title)
	assert.Equal(t, 120, result.Runtime)
	assert.NotNil(t, result.ReleaseDate)
	assert.Equal(t, "Dir A", result.Director)
	assert.Equal(t, "Maker A", result.Maker)
	assert.Equal(t, "Label A", result.Label)
	assert.Len(t, result.Genres, 2)
	assert.Len(t, result.Actresses, 1)
	assert.Equal(t, "花子", result.Actresses[0].JapaneseName)
	assert.NotNil(t, result.Rating)
	assert.Equal(t, 7.5, result.Rating.Score)
}

// --- getURLCtx: HTTP URL input ---

func TestGetURL_MissTest_HTTPURLInput(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})

	url, err := s.GetURL(context.Background(), "https://libredmm.com/movies/IPX-535")
	require.NoError(t, err)
	assert.Contains(t, url, "libredmm.com")
}

// --- getURLCtx: URL with extractable ID ---

func TestGetURL_MissTest_ExtractIDFromURL(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})

	// URL that is not libredmm but contains cid/id query param
	url, err := s.GetURL(context.Background(), "https://example.com/search?cid=IPX-535")
	require.NoError(t, err)
	assert.Contains(t, url, "IPX-535")
}

// --- filterPlaceholderScreenshotsCtx ---

func TestFilterPlaceholderScreenshotsCtx_NoScreenshots(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	result := &models.ScraperResult{}
	s.filterPlaceholderScreenshotsCtx(context.Background(), result)
	assert.Empty(t, result.ScreenshotURL)
}

func TestFilterPlaceholderScreenshotsCtx_WithScreenshots(t *testing.T) {
	settings := testSettings("https://libredmm.com")
	settings.PlaceholderThresholdKB = 10
	s := newScraper(&settings, nil, models.FlareSolverrConfig{})
	result := &models.ScraperResult{
		ScreenshotURL: []string{"https://example.com/s1.jpg"},
	}
	// With screenshots present, filterPlaceholderScreenshotsCtx will attempt to filter
	s.filterPlaceholderScreenshotsCtx(context.Background(), result)
	// The placeholder filter may error on unreachable URLs, but should not panic
}

// --- normalizeMovieURL: edge cases ---

func TestNormalizeMovieURL_CIDPathEmpty(t *testing.T) {
	base := "https://libredmm.com"
	_, ok := normalizeMovieURL("https://libredmm.com/cid/", base)
	assert.False(t, ok, "empty cid should return false")
}

func TestNormalizeMovieURL_SearchWithEmptyQuery(t *testing.T) {
	base := "https://libredmm.com"
	_, ok := normalizeMovieURL("https://libredmm.com/search?q=", base)
	assert.False(t, ok, "empty search query should return false")
}

func TestNormalizeMovieURL_MoviesPathWithEmptyID(t *testing.T) {
	base := "https://libredmm.com"
	_, ok := normalizeMovieURL("https://libredmm.com/movies/", base)
	assert.False(t, ok, "empty movies id should return false")
}

// --- stripJSONSuffix edge case ---

func TestStripJSONSuffix_MissTest_InvalidURL(t *testing.T) {
	result := stripJSONSuffix("not-a-url.json")
	assert.Equal(t, "not-a-url", result)
}

// --- extractIDFromURL: query parameter variations ---

func TestExtractIDFromURL_MissTest_QueryParam(t *testing.T) {
	// Test with id= query parameter
	id := extractIDFromURL("https://example.com/search?id=IPX-535")
	assert.Equal(t, "IPX-535", id)
}
