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

// TestScrapeURLV2_Success tests ScrapeURL with a mock HTTP server
func TestScrapeURLV2_Success(t *testing.T) {
	detailHTML := `
<html>
	<head><title>IPX-123 Test Movie - JavDB</title></head>
	<body>
		<h2 class="title is-4"><strong>IPX-123</strong> Test Movie Title</h2>
		<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>番號:</strong><span class="value">IPX-123</span></div>
			<div class="panel-block"><strong>日期:</strong><span class="value">2024-01-02</span></div>
			<div class="panel-block"><strong>時長:</strong><span class="value">120分鐘</span></div>
			<div class="panel-block"><strong>片商:</strong><span class="value"><a>Maker Name</a></span></div>
			<div class="panel-block"><strong>演員:</strong><span class="value"><a>Actress One</a></span></div>
		</div>
	</body>
</html>
`
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

	result, err := scraper.ScrapeURL(context.Background(), ts.URL+"/v/abc123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-123", result.ID)
	assert.Equal(t, "Test Movie Title", result.Title)
	assert.Equal(t, "Maker Name", result.Maker)
	assert.Equal(t, 120, result.Runtime)
}

// TestScrapeURLV2_Disabled tests ScrapeURL when scraper is disabled
func TestScrapeURLV2_Disabled(t *testing.T) {
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

// TestScrapeURLV2_URLNotHandled tests ScrapeURL with a non-JavDB URL
func TestScrapeURLV2_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://javdb.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/v/abc123")
	require.Error(t, err)
}

// TestScrapeURLV2_SparseDetailWithRetry tests ScrapeURL retry on sparse detail
func TestScrapeURLV2_SparseDetailWithRetry(t *testing.T) {
	callCount := 0
	sparseHTML := `<html><body><h2 class="title is-4">abc123</h2></body></html>`
	fullHTML := `
<html>
	<body>
		<h2 class="title is-4"><strong>ABC-123</strong> Full Movie Title</h2>
		<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>番號:</strong><span class="value">ABC-123</span></div>
		</div>
	</body>
</html>
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/html")
		if callCount <= 1 {
			fmt.Fprint(w, sparseHTML)
		} else {
			fmt.Fprint(w, fullHTML)
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

	result, err := scraper.ScrapeURL(context.Background(), ts.URL+"/v/abc123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "ABC-123", result.ID)
}

// TestSearchV2_DirectURLVideoCode tests Search with JavDB video code (direct URL path)
func TestSearchV2_DirectURLVideoCode(t *testing.T) {
	detailHTML := `
<html>
	<body>
		<h2 class="title is-4"><strong>ABCD1</strong> Direct Video</h2>
		<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>番號:</strong><span class="value">ABCD1</span></div>
			<div class="panel-block"><strong>片商:</strong><span class="value"><a>Test Maker</a></span></div>
		</div>
	</body>
</html>
`
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
}

// TestSearchV2_SearchWithHTTPServer tests Search with full mock server flow
func TestSearchV2_SearchWithHTTPServer(t *testing.T) {
	searchHTML := `
<html>
	<body>
		<div class="movie-list">
			<div class="item">
				<a href="/v/xyz789">
					<div class="video-title"><strong>IPX-456</strong> Fallback Movie</div>
					<div class="uid">IPX-456</div>
				</a>
			</div>
		</div>
	</body>
</html>
`
	detailHTML := `
<html>
	<body>
		<h2 class="title is-4"><strong>IPX-456</strong> Fallback Movie</h2>
		<div class="column-video-cover"><img class="video-cover" src="https://img.example.com/cover.jpg" /></div>
		<div class="movie-panel-info">
			<div class="panel-block"><strong>番號:</strong><span class="value">IPX-456</span></div>
			<div class="panel-block"><strong>片商:</strong><span class="value"><a>Fallback Maker</a></span></div>
		</div>
	</body>
</html>
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Search request
		if r.URL.Path == "/search" {
			fmt.Fprint(w, searchHTML)
			return
		}
		// Detail page
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

	result, err := scraper.Search(context.Background(), "IPX-456")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-456", result.ID)
	assert.Equal(t, "Fallback Maker", result.Maker)
}

// TestFetchPageCtxV2_Non200Status tests fetchPageCtx with non-200 status
func TestFetchPageCtxV2_Non200Status(t *testing.T) {
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

// TestFetchPageCtxV2_Success tests fetchPageCtx with 200 status
func TestFetchPageCtxV2_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>hello</body></html>")
	}))
	defer ts.Close()

	scraper := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, err := scraper.fetchPageCtx(context.Background(), ts.URL+"/test")
	require.NoError(t, err)
	assert.Contains(t, html, "hello")
}

// TestFetchPageDirectCtxV2_Success tests fetchPageDirectCtx
func TestFetchPageDirectCtxV2_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body>direct</body></html>")
	}))
	defer ts.Close()

	scraper := &scraper{
		client:      resty.New().SetBaseURL(ts.URL),
		enabled:     true,
		baseURL:     ts.URL,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, err := scraper.fetchPageDirectCtx(context.Background(), ts.URL+"/test")
	require.NoError(t, err)
	assert.Contains(t, html, "direct")
}
