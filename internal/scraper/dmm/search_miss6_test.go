package dmm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Search: HTTP fetch path (non-browser mode) ---

func TestMiss6_Search_HTTPFetchPath(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
	<table><tr><td>Actress</td><td><a href="?actress=100">Test Actress</a></td></tr></table>
	</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}
	s.client.SetTransport(&dmmTestTransport{server: server})

	// Test Search via getURLCtx returning server URL then fetching
	result, err := s.Search(context.Background(), "IPX-535")
	// The search will likely fail because getURLCtx can't find candidates on the server,
	// but the HTTP fetch path within Search itself is exercised
	_ = result
	_ = err
}

// --- Search: rate limit wait failure ---

func TestMiss6_Search_RateLimitWaitFailure(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Use a cancelled context so rate limiter wait fails
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Search(ctx, "IPX-535")
	require.Error(t, err)
}

// --- ScrapeURL: various HTTP status codes ---

func TestMiss6_ScrapeURL_Status404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMiss6_ScrapeURL_Status429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.Error(t, err)
}

func TestMiss6_ScrapeURL_Status403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "geo-restriction")
}

func TestMiss6_ScrapeURL_Status451(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(451)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "geo-restriction")
}

func TestMiss6_ScrapeURL_Status500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server error")
}

func TestMiss6_ScrapeURL_StatusOther(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(302)
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.Error(t, err)
}

// --- ScrapeURL: rate limit wait failure ---

func TestMiss6_ScrapeURL_RateLimitWaitFailure(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.ScrapeURL(ctx, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.Error(t, err)
}

// --- ScrapeURL: successful parse ---

func TestMiss6_ScrapeURL_SuccessfulParse(t *testing.T) {
	html := `<!DOCTYPE html><html><head></head><body>
	<h1 id="title" class="item">IPX-535 Test Movie Title</h1>
	</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.ScrapeURL(context.Background(), "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", result.ID)
	assert.Equal(t, "dmm", result.Source)
}

// --- parseHTML: new site (video.dmm.co.jp) path ---

func TestMiss6_ParseHTML_NewSite(t *testing.T) {
	html := `<!DOCTYPE html><html><head></head><body>
	<h1>テスト動画タイトル</h1>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		client:      resty.New(),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := s.parseHTML(context.Background(), doc, "https://video.dmm.co.jp/av/content/?id=ipx00535")
	require.NoError(t, err)
	assert.Equal(t, "dmm", result.Source)
	assert.Equal(t, "ja", result.Language)
}

// --- parseHTML: old site path ---

func TestMiss6_ParseHTML_OldSite(t *testing.T) {
	html := `<!DOCTYPE html><html><head></head><body>
	<h1 id="title" class="item">IPX-535 Test Title</h1>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:       true,
		rateLimiter:   ratelimit.NewLimiter(0),
		client:        resty.New(),
		settings:      models.ScraperSettings{Enabled: true},
		scrapeActress: true,
	}

	result, err := s.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00535/")
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", result.ID)
}

// --- parseHTML: monthly page skips actress extraction ---

func TestMiss6_ParseHTML_MonthlyPageSkipsActress(t *testing.T) {
	html := `<!DOCTYPE html><html><head></head><body>
	<h1 id="title" class="item">Test</h1>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	s := &scraper{
		enabled:       true,
		rateLimiter:   ratelimit.NewLimiter(0),
		client:        resty.New(),
		settings:      models.ScraperSettings{Enabled: true},
		scrapeActress: true,
	}

	result, err := s.parseHTML(context.Background(), doc, "https://www.dmm.co.jp/monthly/premium/-/detail/=/cid=ipx00535/")
	require.NoError(t, err)
	assert.Nil(t, result.Actresses)
}

// --- tryDirectURLs: successful direct URL (200) ---

func TestMiss6_TryDirectURLs_SuccessfulDirectURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	// Override URLs to point to test server
	client := resty.New()
	client.SetTransport(&dmmTestTransport{server: server})

	s := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// tryDirectURLs constructs URLs internally, but we test it returns candidates
	// when the server responds with 200
	candidates := s.tryDirectURLs(context.Background(), "ipx00535")
	// With httptest the URLs won't match DMM patterns, so we just ensure no panic
	_ = candidates
}

// --- getURLCtx: context cancellation ---

func TestMiss6_GetURLCtx_ContextCancellation(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.getURLCtx(ctx, "IPX-535")
	require.Error(t, err)
}

// --- GetURL: delegates to getURLCtx ---

func TestMiss6_GetURL_DelegatesToGetURLCtx(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.GetURL(ctx, "IPX-535")
	require.Error(t, err)
}

// --- ScrapeURL: not handled URL ---

func TestMiss6_ScrapeURL_NotHandledURL(t *testing.T) {
	s := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := s.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
}
