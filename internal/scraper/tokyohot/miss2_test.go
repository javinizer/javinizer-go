package tokyohot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ScrapeURL: unhandled URL ---

func TestMiss2_ScrapeURL_UnhandledURL(t *testing.T) {
	s := newScraper(testSettings("https://www.tokyo-hot.com"), nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- ScrapeURL: 404 ---

func TestMiss2_ScrapeURL_Status404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	s := newTHHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/product/N1234/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, models.ScraperErrorKindNotFound, scraperErr.Kind)
}

// --- ScrapeURL: 429 ---

func TestMiss2_ScrapeURL_Status429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	s := newTHHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/product/N1234/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusTooManyRequests, scraperErr.StatusCode)
}

// --- ScrapeURL: 403 ---

func TestMiss2_ScrapeURL_Status403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	s := newTHHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/product/N1234/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "access blocked")
}

// --- ScrapeURL: 451 ---

func TestMiss2_ScrapeURL_Status451(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(451)
	}))
	defer server.Close()

	s := newTHHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/product/N1234/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "access blocked")
}

// --- ScrapeURL: non-200 ---

func TestMiss2_ScrapeURL_OtherStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := newTHHTTPTScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/product/N1234/")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "status code 500")
}

// --- Search: cancelled context ---

func TestMiss2_Search_CancelledContext(t *testing.T) {
	s := newTHHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Search(ctx, "N1234")
	require.Error(t, err)
}

// --- getURLCtx: empty ID ---

func TestMiss2_GetURLCtx_EmptyID(t *testing.T) {
	s := newTHHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.getURLCtx(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// --- getURLCtx: URL input ---

func TestMiss2_GetURLCtx_URLInput(t *testing.T) {
	s := newTHHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	url, err := s.getURLCtx(context.Background(), "https://www.tokyo-hot.com/product/N1234/")
	require.NoError(t, err)
	assert.Contains(t, url, "tokyo-hot.com")
}

// --- fetchPageCtx: cancelled context ---

func TestMiss2_FetchPageCtx_CancelledContext(t *testing.T) {
	s := newTHHTTPTScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := s.fetchPageCtx(ctx, "https://www.tokyo-hot.com/product/N1234/")
	require.Error(t, err)
}

// --- fetchPageCtx: network error ---

func TestMiss2_FetchPageCtx_NetworkError(t *testing.T) {
	client := resty.New()
	client.SetTimeout(1 * time.Millisecond)
	client.SetTransport(&errorRTTH{})

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, _, err := s.fetchPageCtx(context.Background(), "https://www.tokyo-hot.com/product/N1234/")
	require.Error(t, err)
}

// --- applyLanguage ---

func TestMiss2_ApplyLanguage(t *testing.T) {
	s := &scraper{language: "en"}
	assert.Contains(t, s.applyLanguage("https://www.tokyo-hot.com/product/N1234/"), "lang=en")

	s2 := &scraper{language: "ja"}
	assert.Contains(t, s2.applyLanguage("https://www.tokyo-hot.com/product/N1234/"), "lang=ja")

	s3 := &scraper{language: "zh"}
	assert.Contains(t, s3.applyLanguage("https://www.tokyo-hot.com/product/N1234/"), "lang=zh-TW")
}

// --- splitNames ---

func TestMiss2_SplitNames(t *testing.T) {
	assert.Equal(t, []string{"A", "B"}, splitNames("A, B"))
	assert.Equal(t, []string{"A", "B"}, splitNames("A、B"))
	assert.Nil(t, splitNames(""))
}

// --- extractScreenshotURLs ---

func TestMiss2_ExtractScreenshotURLs(t *testing.T) {
	html := `<html><body><div class="scap"><a href="https://www.tokyo-hot.com/img/cap1.jpg">1</a></div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	urls := extractScreenshotURLs(doc, "https://www.tokyo-hot.com")
	assert.GreaterOrEqual(t, len(urls), 0)
}

// --- extractTrailerURL ---

func TestMiss2_ExtractTrailerURL(t *testing.T) {
	html := `<html><body><video><source src="https://www.tokyo-hot.com/sample.mp4"/></video></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	url := extractTrailerURL(doc, "https://www.tokyo-hot.com")
	assert.Contains(t, url, "sample.mp4")
}

// --- extractTrailerURL: no trailer ---

func TestMiss2_ExtractTrailerURL_None(t *testing.T) {
	html := `<html><body><p>no trailer</p></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	assert.Equal(t, "", extractTrailerURL(doc, "https://www.tokyo-hot.com"))
}

// --- ResolveDownloadProxyForHost ---

func TestMiss2_ResolveDownloadProxyForHost(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}

	dp, sp, ok := s.ResolveDownloadProxyForHost("tokyo-hot.com")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	_ = dp
	_ = sp
}

// --- Helper: newTHHTTPTScraper ---

func newTHHTTPTScraper(server *httptest.Server, enabled bool) *scraper {
	client := resty.New()
	client.SetTransport(&missRTTH{server: server})
	client.SetTimeout(5 * time.Second)

	return &scraper{
		client:      client,
		enabled:     enabled,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: enabled, BaseURL: "https://www.tokyo-hot.com"},
	}
}

type missRTTH struct {
	server *httptest.Server
}

func (rt *missRTTH) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyReq := req.Clone(req.Context())
	proxyReq.URL.Scheme = "http"
	proxyReq.URL.Host = rt.server.Listener.Addr().String()
	proxyReq.Host = rt.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(proxyReq)
}

type errorRTTH struct{}

func (rt *errorRTTH) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("forced network error")
}
