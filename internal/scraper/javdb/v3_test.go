package javdb

import (
	"context"
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

// TestSearchV3_VideoCodeDirectURL tests Search with a JavDB video code going through direct URL path
func TestSearchV3_VideoCodeDirectURL(t *testing.T) {
	detailHTML := `
<html>
<head><title>AbJEe Test - JavDB</title></head>
<body>
<h2 class="title is-4"><strong>AbJEe</strong> Direct URL Movie</h2>
<div class="column-video-cover"><img class="video-cover" src="https://pics.dmm.co.jp/cover.jpg" /></div>
<div class="movie-panel-info">
	<div class="panel-block"><strong>番號:</strong><span class="value">AbJEe</span></div>
	<div class="panel-block"><strong>日期:</strong><span class="value">2024-03-15</span></div>
	<div class="panel-block"><strong>時長:</strong><span class="value">90分鐘</span></div>
	<div class="panel-block"><strong>片商:</strong><span class="value"><a>Test Studio</a></span></div>
	<div class="panel-block"><strong>女優:</strong><span class="value"><a>Actress A</a></span></div>
</div>
</body>
</html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, detailHTML)
	}))
	defer ts.Close()

	scraper := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "AbJEe")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "AbJEe", result.ID)
	assert.Equal(t, "Direct URL Movie", result.Title)
	assert.Equal(t, "Test Studio", result.Maker)
}

// TestSearchV3_FallbackToSearchPage tests Search falling back from failed direct URL to search
func TestSearchV3_FallbackToSearchPage(t *testing.T) {
	searchHTML := `
<html><body>
<div class="movie-list">
<div class="item">
	<a href="/v/xAbJEe">
		<span class="uid">AB-456</span>
		<div class="video-title"><strong>AB-456</strong> Search Result Title</div>
	</a>
</div>
</div>
</body></html>`

	detailHTML := `
<html>
<head><title>AB-456 Test - JavDB</title></head>
<body>
<h2 class="title is-4"><strong>AB-456</strong> Search Result Movie</h2>
<div class="column-video-cover"><img class="video-cover" src="https://pics.dmm.co.jp/cover2.jpg" /></div>
<div class="movie-panel-info">
	<div class="panel-block"><strong>番號:</strong><span class="value">AB-456</span></div>
	<div class="panel-block"><strong>片商:</strong><span class="value"><a>Studio B</a></span></div>
</div>
</body>
</html>`

	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/html")
		// First call is direct URL which returns a sparse page, then search, then detail
		if r.URL.Path == "/v/AB-456" || r.URL.Path == "/v/xAbJEe" {
			fmt.Fprint(w, detailHTML)
		} else {
			fmt.Fprint(w, searchHTML)
		}
	}))
	defer ts.Close()

	scraper := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "AB-456")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "AB-456", result.ID)
}

// TestScrapeURLV3_CloudflareChallenge tests ScrapeURL when a Cloudflare challenge page is returned
func TestScrapeURLV3_CloudflareChallenge(t *testing.T) {
	challengeHTML := `<html><head><title>Just a moment...</title><script>challenge-platform</script></head><body>Please enable JavaScript</body></html>`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, challengeHTML)
	}))
	defer ts.Close()

	scraper := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), ts.URL+"/v/abc123")
	require.Error(t, err)
}

// TestFetchPageCtxV3_Non200Status tests fetchPageCtx with non-200 status codes
func TestFetchPageCtxV3_Non200Status(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "forbidden")
	}))
	defer ts.Close()

	scraper := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.fetchPageCtx(context.Background(), ts.URL+"/test")
	require.Error(t, err)
}

// TestFetchPageDirectCtxV3 tests the direct fetch path
func TestFetchPageDirectCtxV3(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>direct response</body></html>")
	}))
	defer ts.Close()

	scraper := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, err := scraper.fetchPageDirectCtx(context.Background(), ts.URL+"/direct")
	require.NoError(t, err)
	assert.Contains(t, html, "direct response")
}

// TestFetchPageDirectCtxV3_Non200 tests direct fetch with non-200 status
func TestFetchPageDirectCtxV3_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found")
	}))
	defer ts.Close()

	scraper := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.fetchPageDirectCtx(context.Background(), ts.URL+"/missing")
	require.Error(t, err)
}

// TestScrapeURLV3_DisabledScraper tests ScrapeURL with disabled scraper
func TestScrapeURLV3_DisabledScraper(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://javdb.com/v/abc123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV3_DisabledScraper tests Search with disabled scraper
func TestSearchV3_DisabledScraper(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "AB-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestTrimVariantSuffixV3 tests trimVariantSuffix with various inputs
func TestTrimVariantSuffixV3(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"AB123C", "AB123"},
		{"AB123", "AB123"},
		{"AB12", "AB12"},
		{"A", "A"},
		{"AB123AB", "AB123AB"}, // last char not A-Z or prev not 0-9
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, trimVariantSuffix(tt.input))
	}
}

// TestCloseV3_WithFlareSolverr tests Close when flaresolverr is nil
func TestCloseV3_WithFlareSolverr(t *testing.T) {
	scraper := &scraper{
		flaresolverr: nil,
	}
	assert.NoError(t, scraper.Close())
}
