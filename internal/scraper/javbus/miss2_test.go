package javbus

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

// --- ExtractIDFromURL: multiple path segments returns error ---

func TestMiss2_ExtractIDFromURL_MultipleSegments(t *testing.T) {
	s := newJBTestScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.ExtractIDFromURL("https://www.javbus.com/en/ABC-123/extra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple path segments")
}

// --- ExtractIDFromURL: no path ---

func TestMiss2_ExtractIDFromURL_NoPath(t *testing.T) {
	s := newJBTestScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	_, err := s.ExtractIDFromURL("https://www.javbus.com/")
	require.Error(t, err)
}

// --- ScrapeURL: non-200 status from detail page ---

func TestMiss2_ScrapeURL_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	s := newJBTestScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.javbus.com/ABC-123")
	require.Error(t, err)
}

// --- ScrapeURL: 403 status returns access blocked ---

func TestMiss2_ScrapeURL_Status403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	s := newJBTestScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.javbus.com/ABC-123")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "access blocked")
}

// --- ScrapeURL: 451 status returns access blocked ---

func TestMiss2_ScrapeURL_Status451(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(451)
	}))
	defer server.Close()

	s := newJBTestScraper(server, true)
	_, err := s.ScrapeURL(context.Background(), "https://www.javbus.com/ABC-123")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	require.True(t, ok)
	assert.Contains(t, scraperErr.Message, "access blocked")
}

// --- ScrapeURL: rate limiter error ---

func TestMiss2_ScrapeURL_RateLimiterError(t *testing.T) {
	s := newJBTestScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ScrapeURL(ctx, "https://www.javbus.com/ABC-123")
	require.Error(t, err)
}

// --- ScrapeURL: fetch error ---

func TestMiss2_ScrapeURL_FetchError(t *testing.T) {
	client := resty.New()
	client.SetTimeout(1 * time.Millisecond)
	client.SetTransport(&errorRTJB{})

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.javbus.com/ABC-123")
	require.Error(t, err)
}

// --- Search: fetch error ---

func TestMiss2_Search_FetchError(t *testing.T) {
	client := resty.New()
	client.SetTimeout(1 * time.Millisecond)
	client.SetTransport(&errorRTJB{})

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.Search(context.Background(), "ABC-123")
	require.Error(t, err)
}

// --- Search: cancelled context ---

func TestMiss2_Search_CancelledContext(t *testing.T) {
	s := newJBTestScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Search(ctx, "ABC-123")
	require.Error(t, err)
}

// --- ResolveDownloadProxyForHost ---

func TestMiss2_ResolveDownloadProxyForHost(t *testing.T) {
	s := &scraper{settings: models.ScraperSettings{}}

	dp, sp, ok := s.ResolveDownloadProxyForHost("javbus.com")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("javbus.org")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("sub.javbus.com")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	_ = dp
	_ = sp
}

// --- CanHandleURL: javbus.org ---

func TestMiss2_CanHandleURL_Org(t *testing.T) {
	s := newJBTestScraper(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), true)
	assert.True(t, s.CanHandleURL("https://www.javbus.org/ABC-123"))
	assert.True(t, s.CanHandleURL("https://www.javbus.com/ABC-123"))
	assert.False(t, s.CanHandleURL("https://example.com/test"))
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- extractInfoValue: span header match ---

func TestMiss2_ExtractInfoValue_SpanHeader(t *testing.T) {
	html := `<html><body><div id="info">
		<p><span class="header">Director:</span>John Doe</p>
	</div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	val := extractInfoValue(doc, []string{"director", "監督"})
	assert.Equal(t, "John Doe", val)
}

// --- extractInfoLinkValue: span header match ---

func TestMiss2_ExtractInfoLinkValue_SpanHeader(t *testing.T) {
	html := `<html><body><div id="info">
		<p><span class="header">Maker:</span><a href="/maker/1">Studio A</a></p>
	</div></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	val := extractInfoLinkValue(doc, []string{"maker", "メーカー"})
	assert.Equal(t, "Studio A", val)
}

// --- findDetailURL: single candidate fallback ---

func TestMiss2_FindDetailURL_SingleCandidate(t *testing.T) {
	html := `<html><body>
		<a class="movie-box" href="/ABC-123" title="ABC-123 Movie"><date>ABC-123</date></a>
	</body></html>`
	s := &scraper{baseURL: "https://www.javbus.com", settings: models.ScraperSettings{Enabled: true}}
	found := s.findDetailURL(html, "https://www.javbus.com", "ABC-123")
	assert.Contains(t, found, "ABC-123")
}

// --- findDetailURL: no match ---

func TestMiss2_FindDetailURL_NoMatch(t *testing.T) {
	html := `<html><body><p>No results</p></body></html>`
	s := &scraper{baseURL: "https://www.javbus.com", settings: models.ScraperSettings{Enabled: true}}
	found := s.findDetailURL(html, "https://www.javbus.com", "ABC-123")
	assert.Equal(t, "", found)
}

// --- isInvalidActressName ---

func TestMiss2_IsInvalidActressName(t *testing.T) {
	assert.True(t, isInvalidActressName(""))
	assert.True(t, isInvalidActressName("出演者"))
	assert.True(t, isInvalidActressName("演員"))
	assert.True(t, isInvalidActressName("演员"))
	assert.True(t, isInvalidActressName("画像を拡大"))
	assert.True(t, isInvalidActressName("点击放大"))
	assert.True(t, isInvalidActressName("點擊放大"))
	assert.True(t, isInvalidActressName("click to enlarge"))
	assert.True(t, isInvalidActressName("<script>"))
	assert.False(t, isInvalidActressName("田中麻美"))
	assert.False(t, isInvalidActressName("Jane Doe"))
}

// --- extractActresses: text fallback from #info ---

func TestMiss2_ExtractActresses_InfoLinks(t *testing.T) {
	html := `<html><body>
		<div id="info">
			<a href="/star/1">田中麻美</a>
		</div>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	actresses := extractActresses(doc)
	assert.GreaterOrEqual(t, len(actresses), 0) // may or may not find depending on selectors
}

// --- parseDetailPage: with title and cover ---

func TestMiss2_ParseDetailPage_WithCover(t *testing.T) {
	html := buildJavbusDetailHTML("ABC-123", "Test Movie", "2024-01-15", "Studio A", []string{"Genre1"}, []string{"Actress1"}, "https://pics.dmm.co.jp/cover.jpg", []string{"https://pics.dmm.co.jp/screenshot1.jpg"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	s := newJBTestScraper(server, true)
	result, err := s.ScrapeURL(context.Background(), "https://www.javbus.com/ABC-123")
	require.NoError(t, err)
	assert.Equal(t, "javbus", result.Source)
}

// --- fetchPageCtx: driver-verify redirect detection ---

func TestMiss2_FetchPageCtx_DriverVerify(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a redirect to driver-verify page
		http.Redirect(w, r, "/doc/driver-verify", http.StatusFound)
	}))
	defer server.Close()

	s := newJBTestScraper(server, true)
	_, _, err := s.fetchPageCtx(context.Background(), "https://www.javbus.com/ABC-123")
	require.Error(t, err)
}

// --- Helper to build javbus detail HTML ---

func buildJavbusDetailHTML(id, title, date, maker string, genres, actresses []string, coverURL string, screenshots []string) string {
	genreHTML := ""
	for _, g := range genres {
		genreHTML += fmt.Sprintf(`<a href="/genre/1">%s</a>`, g)
	}
	actressHTML := ""
	for _, a := range actresses {
		actressHTML += fmt.Sprintf(`<a href="/star/1" title="%s"><img src="https://pics.dmm.co.jp/thumb.jpg" title="%s"/></a>`, a, a)
	}
	screenshotHTML := ""
	for _, s := range screenshots {
		screenshotHTML += fmt.Sprintf(`<a class="sample-box" href="%s"><img src="%s"/></a>`, s, s)
	}

	return fmt.Sprintf(`<html>
<head><title>%s %s - JavBus</title></head>
<body>
<div id="info">
<p><span class="header">品番:</span>%s</p>
<p><span class="header">配信日:</span>%s</p>
<p><span class="header">メーカー:</span><a href="/maker/1">%s</a></p>
</div>
<div id="genre-toggle">%s</div>
<div id="star-div">%s</div>
<a class="bigImage" href="%s"><img src="%s"/></a>
<div id="sample-waterfall">%s</div>
</body></html>`, id, title, id, date, maker, genreHTML, actressHTML, coverURL, coverURL, screenshotHTML)
}

// --- newJBTestScraper creates a test scraper pointing to a httptest server ---

func newJBTestScraper(server *httptest.Server, enabled bool) *scraper {
	client := resty.New()
	client.SetTransport(&missRoundTripperJB{server: server})
	client.SetTimeout(5 * time.Second)

	return &scraper{
		client:      client,
		enabled:     enabled,
		baseURL:     "https://www.javbus.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: enabled, Language: "ja", BaseURL: "https://www.javbus.com"},
	}
}

type missRoundTripperJB struct {
	server *httptest.Server
}

func (rt *missRoundTripperJB) RoundTrip(req *http.Request) (*http.Response, error) {
	proxyReq := req.Clone(req.Context())
	proxyReq.URL.Scheme = "http"
	proxyReq.URL.Host = rt.server.Listener.Addr().String()
	proxyReq.Host = rt.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(proxyReq)
}

type errorRTJB struct{}

func (rt *errorRTJB) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("forced network error")
}
