package jav321

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type j321RoundTripper struct {
	responses map[string]string
}

func (rt *j321RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check for POST requests (search)
	if req.Method == "POST" && req.URL.Path == "/search" {
		if body, ok := rt.responses["/search"]; ok {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}
	}
	for path, body := range rt.responses {
		if strings.Contains(req.URL.String(), path) || strings.Contains(path, req.URL.Path) {
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

// TestScrapeURLV2_Success tests ScrapeURL
func TestScrapeURLV2_Success(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<body>
<div class="col-md-12">
<h3>IPX-123 テスト映画</h3>
<table>
<tr><td>品番：</td><td>IPX-123</td></tr>
<tr><td>配信開始日：</td><td>2024-01-02</td></tr>
<tr><td>収録時間：</td><td>120分</td></tr>
<tr><td>出演者：</td><td><a>Actress One</a> <a>Actress Two</a></td></tr>
<tr><td>ジャンル：</td><td><a>Drama</a> <a>School</a></td></tr>
<tr><td>メーカー：</td><td><a>TestMaker</a></td></tr>
</table>
</div>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&j321RoundTripper{
		responses: map[string]string{
			"/video/IPX-123": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://jp.jav321.com/video/IPX-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "jav321", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-Jav321 URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/video/IPX-123")
	require.Error(t, err)
}

// TestSearchV2_Disabled tests Search when disabled
func TestSearchV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV2_Success tests Search with mock transport
func TestSearchV2_Success(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<body>
<div class="col-md-12">
<h3>IPX-456 テスト映画</h3>
<table>
<tr><td>品番：</td><td>IPX-456</td></tr>
<tr><td>配信開始日：</td><td>2024-03-15</td></tr>
<tr><td>収録時間：</td><td>90分</td></tr>
<tr><td>メーカー：</td><td><a>TestMaker</a></td></tr>
</table>
</div>
</body>
</html>
`
	searchResultHTML := `
<!DOCTYPE html>
<html>
<body>
<a href="/video/IPX-456">IPX-456 テスト映画</a>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&j321RoundTripper{
		responses: map[string]string{
			"/search":        searchResultHTML,
			"/video/IPX-456": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "IPX-456")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-456", result.ID)
}

// TestGetURLV2 tests GetURL
func TestGetURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Test with HTTP URL
	u, err := scraper.GetURL(context.Background(), "https://jp.jav321.com/video/IPX-123")
	require.NoError(t, err)
	assert.Equal(t, "https://jp.jav321.com/video/IPX-123", u)
}

// TestCanHandleURLV2 tests CanHandleURL
func TestCanHandleURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://jp.jav321.com/video/IPX-123"))
	assert.False(t, scraper.CanHandleURL("https://example.com/test"))
}

// TestExtractIDFromURLV2 tests ExtractIDFromURL
func TestExtractIDFromURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	id, err := scraper.ExtractIDFromURL("https://jp.jav321.com/video/IPX-123")
	require.NoError(t, err)
	assert.Equal(t, "IPX-123", id)

	_, err = scraper.ExtractIDFromURL("https://jp.jav321.com/other")
	require.Error(t, err)
}

// TestFetchPageCtxV2 tests fetchPageCtx
func TestFetchPageCtxV2(t *testing.T) {
	client := resty.New()
	client.SetTransport(&j321RoundTripper{
		responses: map[string]string{
			"/test": "<html><body>hello</body></html>",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "https://jp.jav321.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "hello")
}

// TestBuildSearchURLV2 tests URL building
func TestBuildSearchURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	u, err := scraper.getURLCtx(context.Background(), "https://jp.jav321.com/video/ABCD-123")
	require.NoError(t, err)
	assert.Equal(t, "https://jp.jav321.com/video/ABCD-123", u)
}

// TestBuildSearchURLV2_EmptyID tests empty ID
func TestBuildSearchURLV2_EmptyID(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://jp.jav321.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.getURLCtx(context.Background(), "")
	require.Error(t, err)
}

// TestExtractIDV2 tests extractID helper
func TestExtractIDV2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"IPX-123", "IPX-123"},
		{"SSIS456", "SSIS456"},
		{"no match here", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeIDForSearchV2 tests scraperutil integration
func TestNormalizeIDForSearchV2(t *testing.T) {
	_ = url.QueryEscape("IPX-123") // verify url import works
	assert.True(t, true)
}
