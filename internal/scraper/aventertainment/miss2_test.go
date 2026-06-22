package aventertainment

import (
	"context"
	"fmt"
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

// --- CanHandleURL: edge cases ---

func TestMiss2_CanHandleURL(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	assert.True(t, s.CanHandleURL("https://www.aventertainments.com/ppv/detail/123"))
	assert.True(t, s.CanHandleURL("https://aventertainments.com/test"))
	assert.False(t, s.CanHandleURL("https://example.com/test"))
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- ExtractIDFromURL: query param ---

func TestMiss2_ExtractIDFromURL_QueryParam(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	id, err := s.ExtractIDFromURL("https://www.aventertainments.com/ppv/detail?item_no=ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "ABC-123", id)
}

// --- ExtractIDFromURL: path-based ---

func TestMiss2_ExtractIDFromURL_PathBased(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	id, err := s.ExtractIDFromURL("https://www.aventertainments.com/ppv/detail/12345")
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

// --- ExtractIDFromURL: failed ---

func TestMiss2_ExtractIDFromURL_Failed(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.ExtractIDFromURL("https://www.aventertainments.com/")
	require.Error(t, err)
}

// --- ScrapeURL: unhandled URL ---

func TestMiss2_ScrapeURL_UnhandledURL(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- ScrapeURL: 404 status ---

func TestMiss2_ScrapeURL_Status404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newAVEHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.aventertainments.com/ppv/detail/99999/1/1/new_detail")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// --- ScrapeURL: 429 status ---

func TestMiss2_ScrapeURL_Status429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	s := newAVEHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.aventertainments.com/ppv/detail/99999/1/1/new_detail")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusTooManyRequests, scraperErr.StatusCode)
}

// --- ScrapeURL: rate limiter error ---

func TestMiss2_ScrapeURL_RateLimiterError(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ScrapeURL(ctx, "https://www.aventertainments.com/ppv/detail/123/1/1/new_detail")
	require.Error(t, err)
}

// --- Search: disabled scraper ---

func TestMiss2_Search_Disabled(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), false)
	_, err := s.Search(context.Background(), "ABC-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- getURLCtx: empty ID ---

func TestMiss2_GetURLCtx_EmptyID(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.getURLCtx(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// --- getURLCtx: URL input ---

func TestMiss2_GetURLCtx_URLInput(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	url, err := s.getURLCtx(context.Background(), "https://www.aventertainments.com/ppv/detail/123/1/1/new_detail")
	require.NoError(t, err)
	assert.Contains(t, url, "aventertainments.com")
}

// --- getURLCtx: not found ---

func TestMiss2_GetURLCtx_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><p>No results</p></body></html>`))
	}))
	defer server.Close()

	s := newAVEHTTPTScraper(server, true)
	_, err := s.getURLCtx(context.Background(), "NOTFOUND-99999")
	require.Error(t, err)
}

// --- stripSiteSuffix ---

func TestMiss2_StripSiteSuffix(t *testing.T) {
	assert.Equal(t, "My Movie", stripSiteSuffix("My Movie - AV Entertainment"))
	assert.Equal(t, "Test Title", stripSiteSuffix("Test Title | AV Entertainment"))
	assert.Equal(t, "Clean Title", stripSiteSuffix("Clean Title"))
}

// --- normalizeComparableID ---

func TestMiss2_NormalizeComparableID(t *testing.T) {
	// Basic normalization
	result := normalizeComparableID("ABC-123")
	assert.NotEmpty(t, result)
}

// --- extractCandidateID ---

func TestMiss2_ExtractCandidateID(t *testing.T) {
	html := `<span class="tag-title">ABC-123</span>`
	assert.Equal(t, "ABC-123", extractCandidateID(html))
	assert.Equal(t, "", extractCandidateID("<html></html>"))
}

// --- extractDetailLinks ---

func TestMiss2_ExtractDetailLinks(t *testing.T) {
	html := `<html><body><a href="/ppv/detail/123/1/1/new_detail">Link</a></body></html>`
	links := extractDetailLinks(html, "https://www.aventertainments.com")
	assert.GreaterOrEqual(t, len(links), 1)
}

// --- findDate ---

func TestMiss2_FindDate(t *testing.T) {
	assert.Equal(t, "2024/01/15", findDate(`発売日</span><span class="value">2024/01/15</span>`))
	assert.Equal(t, "", findDate("no date here"))
}

// --- findRuntime ---

func TestMiss2_FindRuntime(t *testing.T) {
	assert.NotEmpty(t, findRuntime(`収録時間</span><span class="value">120 Min</span>`))
	assert.Equal(t, "", findRuntime("no runtime here"))
}

// --- parseRuntime ---

func TestMiss2_ParseRuntime(t *testing.T) {
	assert.Equal(t, 120, parseRuntime("120 Min"))
	assert.Equal(t, 90, parseRuntime("1:30:00"))
	assert.Equal(t, 0, parseRuntime(""))
}

// --- findMaker ---

func TestMiss2_FindMaker(t *testing.T) {
	html := `<span class="title">Studio</span><span class="value"><a href="/studio/1">Test Studio</a></span>`
	assert.Equal(t, "Test Studio", findMaker(html))
	assert.Equal(t, "", findMaker("no maker here"))
}

// --- fetchPageCtx: error ---

func TestMiss2_FetchPageCtx_Error(t *testing.T) {
	client := resty.New()
	client.SetTimeout(1 * time.Millisecond)
	client.SetTransport(&errorRTAVE{})

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.aventertainments.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, _, err := s.fetchPageCtx(context.Background(), "https://www.aventertainments.com/test")
	require.Error(t, err)
}

// --- fetchPageCtx: cancelled context ---

func TestMiss2_FetchPageCtx_CancelledContext(t *testing.T) {
	s := newAVEHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := s.fetchPageCtx(ctx, "https://www.aventertainments.com/test")
	require.Error(t, err)
}

// --- applyLanguage: modifies URL ---

func TestMiss2_ApplyLanguage(t *testing.T) {
	s := &scraper{language: "en", baseURL: "https://www.aventertainments.com"}
	result := s.applyLanguage("https://www.aventertainments.com/ppv/detail/123/1/1/new_detail")
	assert.Contains(t, result, "lang=1")

	s2 := &scraper{language: "ja", baseURL: "https://www.aventertainments.com"}
	result2 := s2.applyLanguage("https://www.aventertainments.com/ppv/detail/123/1/1/new_detail")
	assert.Contains(t, result2, "lang=2")
}

type errorRTAVE struct{}

func (rt *errorRTAVE) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("forced network error")
}
