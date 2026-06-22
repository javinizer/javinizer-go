package javbus

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

// javbusRoundTripper maps URLs to HTML responses for testing
type javbusRoundTripper struct {
	responses map[string]string
	statuses  map[string]int
	lastReqs  []string
}

func (rt *javbusRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.lastReqs = append(rt.lastReqs, req.URL.String())
	key := req.URL.String()
	if body, ok := rt.responses[key]; ok {
		status := 200
		if s, ok := rt.statuses[key]; ok {
			status = s
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}
	// Path-based fallback
	for url, body := range rt.responses {
		if strings.Contains(url, req.URL.Path) || strings.Contains(req.URL.String(), url) {
			status := 200
			if s, ok := rt.statuses[url]; ok {
				status = s
			}
			return &http.Response{
				StatusCode: status,
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
<head><title>IPX-123 Test Movie - JavBus</title></head>
<body>
<div class="container">
<h3>IPX-123 Test Movie Title</h3>
<p><span class="header">品番:</span> IPX-123</p>
<p><span class="header">發行日期:</span> 2024-01-02</p>
<p><span class="header">長度:</span> 120分鐘</p>
<p><span class="header">導演:</span> <a>Director Name</a></p>
<p><span class="header">製作商:</span> <a>Maker Name</a></p>
<p><span class="header">發行商:</span> <a>Label Name</a></p>
<p><span class="header">系列:</span> <a>Series Name</a></p>
<p><span class="header">演員:</span> <a>Actress One</a> <a>Actress Two</a></p>
<p><span class="header">類別:</span> <a>Drama</a> <a>School</a></p>
<a class="bigImage" href="https://img.example.com/cover.jpg"><img src="https://img.example.com/thumb.jpg"></a>
</div>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&javbusRoundTripper{
		responses: map[string]string{
			"https://www.javbus.com/IPX-123": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://www.javbus.com/IPX-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "javbus", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-JavBus URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/IPX-123")
	require.Error(t, err)
}

// TestSearchV2_Success tests Search with mock transport
func TestSearchV2_Success(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<head><title>IPX-456 Test Movie - JavBus</title></head>
<body>
<div class="container">
<h3>IPX-456 Test Movie Title</h3>
<p><span class="header">品番:</span> IPX-456</p>
<p><span class="header">製作商:</span> <a>Test Maker</a></p>
<a class="bigImage" href="https://img.example.com/cover.jpg"><img src="https://img.example.com/thumb.jpg"></a>
</div>
</body>
</html>
`
	searchHTML := `
<!DOCTYPE html>
<html>
<body>
<a class="movie-box" href="/IPX-456" title="IPX-456"><date>IPX-456</date></a>
</body>
</html>
`

	rt := &javbusRoundTripper{
		responses: map[string]string{
			"/search/": searchHTML,
			"/IPX-456": detailHTML,
		},
	}

	client := resty.New()
	client.SetTransport(rt)

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "IPX-456")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-456", result.ID)
}

// TestSearchV2_Disabled tests Search when disabled
func TestSearchV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-456")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestFetchPageCtxV2_Success tests fetchPageCtx
func TestFetchPageCtxV2_Success(t *testing.T) {
	client := resty.New()
	client.SetTransport(&javbusRoundTripper{
		responses: map[string]string{
			"https://www.javbus.com/test": "<html><body>hello</body></html>",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javbus.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "https://www.javbus.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "hello")
}
