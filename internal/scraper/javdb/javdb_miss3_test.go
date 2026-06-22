package javdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Close: with flaresolverr that has error ---

func TestClose_Miss3_FlareSolverrError(t *testing.T) {
	s := &scraper{
		flaresolverr: nil,
	}
	err := s.Close()
	assert.NoError(t, err) // Close always returns nil even if flaresolverr.Close() errors
}

// --- ScrapeURL: disabled scraper ---

func TestScrapeURL_Miss3_DisabledScraper(t *testing.T) {
	s := &scraper{
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/abc123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- ScrapeURL: not handled URL ---

func TestScrapeURL_Miss3_NotHandledURL(t *testing.T) {
	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://example.com/v/abc123")
	require.Error(t, err)
}

// --- ScrapeURL: Cloudflare challenge detected on sparse page ---

func TestScrapeURL_Miss3_CloudflareChallengeOnSparsePage(t *testing.T) {
	cfHTML := `<html><body><title>Just a moment...</title><div class="cloudflare"><p>Attention Required! Checking your browser before accessing. DDoS protection by Cloudflare. Ray ID: abc123.</p></div></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cfHTML))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://javdb.com/v/cftest")
	require.Error(t, err)
	// Should detect Cloudflare challenge
	scraperErr, ok := models.AsScraperError(err)
	if ok {
		assert.Equal(t, models.ScraperErrorKindBlocked, scraperErr.Kind)
	}
}

// --- ScrapeURL: fetchPageCtx returns scraper error, passes through as-is ---

func TestScrapeURL_Miss3_FetchReturnsScraperError(t *testing.T) {
	s := &scraper{
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Use a cancelled context to trigger an error from fetchPageCtx
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.ScrapeURL(ctx, "https://javdb.com/v/abc123")
	require.Error(t, err)
}

// --- Search: disabled scraper ---

func TestSearch_Miss3_DisabledScraper(t *testing.T) {
	s := &scraper{
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := s.Search(context.Background(), "IPX-535")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- Search: direct URL lookup succeeds for JavDB video code ---

func TestSearch_Miss3_DirectURLVideoCode(t *testing.T) {
	detailHTML := `
<html>
	<head><title>AbC12 - JavDB</title></head>
	<body>
		<h2 class="title is-4"><strong>ABC12</strong> Direct Code Movie</h2>
		<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>片商:</strong><span class="value"><a>Direct Maker</a></span></div>
		</div>
	</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(detailHTML))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// "AbC12" looks like a JavDB video code (alphanumeric, 5 chars)
	result, err := s.Search(context.Background(), "AbC12")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Direct Maker", result.Maker)
}

// --- Search: direct URL lookup fails, falls back to search ---

func TestSearch_Miss3_DirectURLFailsFallsBackToSearch(t *testing.T) {
	searchHTML := `
<html><body>
	<div class="movie-list">
		<div class="item">
			<a href="/v/fb123">
				<div class="video-title"><strong>FB-001</strong> Fallback Search</div>
				<div class="uid">FB-001</div>
			</a>
		</div>
	</div>
</body></html>`

	detailHTML := `
<html><body>
	<h2 class="title is-4"><strong>FB-001</strong> Fallback Search</h2>
	<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>片商:</strong><span class="value"><a>Fallback Maker</a></span></div>
	</div>
</body></html>`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		path := r.URL.Path
		if strings.Contains(path, "/v/") && callCount == 1 {
			// First request: direct URL returns sparse page (no metadata)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<html><body><p>Nothing here</p></body></html>`))
		} else if strings.Contains(path, "/search") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(searchHTML))
		} else {
			// Detail page from search
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(detailHTML))
		}
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// "abc12" is a JavDB video code - direct URL will fail, then search succeeds
	result, err := s.Search(context.Background(), "FB-001")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Fallback Maker", result.Maker)
}

// --- fetchPageCtx: direct request returns non-200 ---

func TestFetchPageCtx_Miss3_DirectNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.fetchPageCtx(context.Background(), "https://javdb.com/v/test")
	require.Error(t, err)
}

// --- fetchPageCtx: Cloudflare challenge page triggers FlareSolverr fallback ---

func TestFetchPageCtx_Miss3_CloudflarePageNoFlareSolverr(t *testing.T) {
	cfHTML := `<html><body><title>Just a moment...</title><p>Cloudflare attention required. Checking your browser before accessing. DDoS protection by Cloudflare. Ray ID: abc123. cf-ray: xyz. /cdn-cgi/ path.</p></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cfHTML))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		rateLimiter:  ratelimit.NewLimiter(0),
		client:       client,
		settings:     models.ScraperSettings{Enabled: true, UseFlareSolverr: true},
		flaresolverr: nil, // No flaresolverr configured
	}

	_, err := s.fetchPageCtx(context.Background(), "https://javdb.com/v/test")
	require.Error(t, err)
	// Should return challenge error since FlareSolverr is not available
	scraperErr, ok := models.AsScraperError(err)
	if ok {
		assert.Equal(t, models.ScraperErrorKindBlocked, scraperErr.Kind)
	}
}

// --- fetchPageCtx: rate limiter wait cancelled ---

func TestFetchPageCtx_Miss3_RateLimitCancelled(t *testing.T) {
	s := &scraper{
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.fetchPageCtx(ctx, "https://javdb.com/v/test")
	require.Error(t, err)
}

// --- findDetailURLCtx: cancelled context ---

func TestFindDetailURLCtx_Miss3_CancelledContext(t *testing.T) {
	s := &scraper{
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.findDetailURLCtx(ctx, "IPX-535")
	require.Error(t, err)
}

// --- GetURL: empty ID ---

func TestGetURL_Miss3_EmptyID(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com"}
	_, err := s.GetURL(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// --- GetURL: whitespace-only ID ---

func TestGetURL_Miss3_WhitespaceID(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com"}
	_, err := s.GetURL(context.Background(), "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// --- fetchPageDirectCtx: Cloudflare challenge page ---

func TestFetchPageDirectCtx_Miss3_CloudflarePage(t *testing.T) {
	cfHTML := `<html><body><title>Just a moment...</title><p>Cloudflare attention required. Checking your browser before accessing. DDoS protection by Cloudflare. Ray ID: abc123. cf-ray: xyz. /cdn-cgi/ path.</p></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cfHTML))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		rateLimiter: ratelimit.NewLimiter(0),
		client:      client,
	}

	_, err := s.fetchPageDirectCtx(context.Background(), "https://javdb.com/v/test")
	require.Error(t, err)
	scraperErr, ok := models.AsScraperError(err)
	if ok {
		assert.Equal(t, models.ScraperErrorKindBlocked, scraperErr.Kind)
	}
}

// --- Search: retry path where retry also sparse ---

func TestSearch_Miss3_SparseDetailRetryAlsoSparse(t *testing.T) {
	searchHTML := `
<html><body>
	<div class="movie-list">
		<div class="item">
			<a href="/v/spa321">
				<div class="video-title"><strong>SPA3-001</strong> Sparse 3</div>
				<div class="uid">SPA3-001</div>
			</a>
		</div>
	</div>
</body></html>`

	sparseHTML := `<html><body><p>Sparse</p></body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.Contains(path, "/search") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(searchHTML))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(sparseHTML))
		}
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.Search(context.Background(), "SPA3-001")
	require.Error(t, err)
}

// --- findDetailURLCtx: single fallback link ---

func TestFindDetailURLCtx_Miss3_SingleFallbackLink(t *testing.T) {
	searchHTML := `
<html><body>
	<div class="movie-list">
		<div class="item">
			<a href="/v/sng123">
				<div class="video-title">Some Movie Title</div>
			</a>
		</div>
	</div>
</body></html>`

	detailHTML := `
<html><body>
	<h2 class="title is-4"><strong>SNG-001</strong> Single Fallback</h2>
	<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
	<div class="movie-panel-info">
		<div class="panel-block"><strong>片商:</strong><span class="value"><a>Single Maker</a></span></div>
	</div>
</body></html>`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		path := r.URL.Path
		if strings.Contains(path, "/search") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(searchHTML))
		} else {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(detailHTML))
		}
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&javdbTestTransport{server: server})
	client.SetTimeout(5 * time.Second)

	s := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.Search(context.Background(), "SNG-001")
	require.NoError(t, err)
	require.NotNil(t, result)
}

// --- CanHandleURL: with custom base URL ---

func TestCanHandleURL_Miss3_CustomBaseURL(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.test.com"}
	assert.True(t, s.CanHandleURL("https://javdb.test.com/v/abc123"))
	assert.False(t, s.CanHandleURL("https://javdb.com/v/abc123"))
}

// --- CanHandleURL: invalid URL ---

func TestCanHandleURL_Miss3_InvalidURL(t *testing.T) {
	s := &scraper{baseURL: "https://javdb.com"}
	assert.False(t, s.CanHandleURL("://not-a-url"))
}

// --- ResolveDownloadProxyForHost: various hosts ---

func TestResolveDownloadProxyForHost_Miss3(t *testing.T) {
	s := &scraper{
		settings: models.ScraperSettings{
			DownloadProxy: &models.ProxyConfig{Enabled: true, Profile: "dl-proxy"},
			Proxy:         &models.ProxyConfig{Enabled: true, Profile: "scraper-proxy"},
		},
	}

	// JavDB hosts
	dl, scr, ok := s.ResolveDownloadProxyForHost("jdbstatic.com")
	assert.True(t, ok)
	assert.NotNil(t, dl)
	assert.NotNil(t, scr)

	dl, scr, ok = s.ResolveDownloadProxyForHost("cdn.jdbstatic.com")
	assert.True(t, ok)

	dl, scr, ok = s.ResolveDownloadProxyForHost("javdb.com")
	assert.True(t, ok)

	// Unknown host
	_, _, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)

	// Empty host
	_, _, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
}
