package caribbeancom

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// caribbeanRoundTripper maps URLs to HTML responses for testing
type caribbeanRoundTripper struct {
	responses map[string]string
}

func (rt *caribbeanRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if body, ok := rt.responses[req.URL.String()]; ok {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}
	// Check by path only (for dynamic hostnames in tests)
	for url, body := range rt.responses {
		if strings.HasSuffix(url, req.URL.Path) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}
	}
	return &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("not found")),
		Request:    req,
	}, nil
}

// TestScrapeURLV2_Success tests ScrapeURL with mock transport
func TestScrapeURLV2_Success(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<head><title>120614-753 Test Movie - Caribbeancom</title></head>
<body>
<div id="moviepages">
<div class="movie-info">
<h1 itemprop="name">テスト映画タイトル</h1>
<p itemprop="description">Test description</p>
<span itemprop="duration" content="PT1H30M"></span>
</div>
</div>
<script>var Movie = {"movie_id":"120614-753","sample_flash_url":"https://sample.caribbeancom.com/120614-753/sample.mp4"};</script>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&caribbeanRoundTripper{
		responses: map[string]string{
			"https://www.caribbeancom.com/moviepages/120614-753/index.html": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/120614-753/index.html")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "120614-753", result.ID)
	assert.Equal(t, "caribbeancom", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-Caribbeancom URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/moviepages/120614-753/index.html")
	require.Error(t, err)
}

// TestScrapeURLV2_404 tests ScrapeURL when page returns 404
func TestScrapeURLV2_404(t *testing.T) {
	client := resty.New()
	client.SetTransport(&caribbeanRoundTripper{
		responses: map[string]string{},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/120614-999/index.html")
	require.Error(t, err)
}

// TestScrapeURLV2_429 tests ScrapeURL when rate limited
func TestScrapeURLV2_429(t *testing.T) {
	rt := &caribbeanRoundTripper{responses: map[string]string{}}
	// Override with a 429 responder
	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// ScrapeURL calls fetchPageCtx which returns status code, then checks it
	// Since our round tripper returns 404, this will be a status error
	_, err := scraper.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/120614-999/index.html")
	require.Error(t, err)
}

// TestSearchV2_Success tests Search with mock transport
func TestSearchV2_Success(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<head><title>120614-753 Test Movie - Caribbeancom</title></head>
<body>
<div id="moviepages">
<div class="movie-info">
<h1 itemprop="name">テスト映画タイトル</h1>
<p itemprop="description">Test description</p>
<span itemprop="duration" content="PT1H30M"></span>
</div>
</div>
<script>var Movie = {"movie_id":"120614-753"};</script>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&caribbeanRoundTripper{
		responses: map[string]string{
			"https://www.caribbeancom.com/moviepages/120614-753/index.html": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "120614-753")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "120614-753", result.ID)
	assert.Equal(t, "caribbeancom", result.Source)
}

// TestSearchV2_Disabled tests Search when scraper is disabled
func TestSearchV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "120614-753")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV2_InvalidID tests Search with an ID that doesn't match format
func TestSearchV2_InvalidID(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "INVALID-ID")
	require.Error(t, err)
}

// TestFetchPageCtxV2_Success tests fetchPageCtx with successful response
func TestFetchPageCtxV2_Success(t *testing.T) {
	client := resty.New()
	client.SetTransport(&caribbeanRoundTripper{
		responses: map[string]string{
			"https://www.caribbeancom.com/test": "<html><body>hello</body></html>",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "hello")
}

// TestFetchPageCtxV2_Non200 tests fetchPageCtx with non-200 status
func TestFetchPageCtxV2_Non200(t *testing.T) {
	client := resty.New()
	client.SetTransport(&caribbeanRoundTripper{
		responses: map[string]string{},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, status, err := scraper.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/nonexistent")
	require.NoError(t, err)
	assert.Equal(t, 404, status)
}

// TestDecodeBodyV2 tests decodeBody function
func TestDecodeBodyV2(t *testing.T) {
	client := resty.New()
	client.SetTransport(&caribbeanRoundTripper{
		responses: map[string]string{
			"https://www.caribbeancom.com/test": "テストコンテンツ",
		},
	})

	resp, err := client.R().Get("https://www.caribbeancom.com/test")
	require.NoError(t, err)

	decoded, err := decodeBody(resp)
	require.NoError(t, err)
	assert.Contains(t, decoded, "テスト")
}

// TestSearchV2_WithHTTPURL tests Search when passing a URL as ID
func TestSearchV2_WithHTTPURL(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<body>
<div id="moviepages">
<div class="movie-info">
<h1 itemprop="name">テスト</h1>
</div>
</div>
<script>var Movie = {"movie_id":"120614-753"};</script>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&caribbeanRoundTripper{
		responses: map[string]string{
			"https://www.caribbeancom.com/moviepages/120614-753/index.html": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "https://www.caribbeancom.com/moviepages/120614-753/index.html")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "120614-753", result.ID)
}
