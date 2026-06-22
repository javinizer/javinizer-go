package tokyohot

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

// TestSearchV3_SuccessWithMockServer tests Search with a mock server
func TestSearchV3_SuccessWithMockServer(t *testing.T) {
	searchHTML := `
<!DOCTYPE html>
<html><body>
<a href="/product/KB-123/">KB-123 Test</a>
</body></html>`

	detailHTML := `
<!DOCTYPE html>
<html>
<head><title>KB-123 Test Movie | Tokyo Hot</title></head>
<body>
<div class="detail-spec">
<dl><dt>Release Date</dt><dd>2024/01/15</dd></dl>
<dl><dt>Duration</dt><dd>01:30:00</dd></dl>
</div>
<div class="actor"><a>Actress One</a></div>
<img src="https://example.com/cover.jpg" />
</body>
</html>`

	callCount := 0
	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &thMockTransportHandler{
		handler: func(req *http.Request) (*http.Response, error) {
			callCount++
			body := searchHTML
			if callCount > 1 {
				body = detailHTML
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		},
	}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "KB-123")
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestSearchV3_Disabled tests Search when disabled
func TestSearchV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "KB-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestScrapeURLV3_SuccessWithMockServer tests ScrapeURL with a mock server
func TestScrapeURLV3_SuccessWithMockServer(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<head><title>KB-456 ScrapeURL Movie - Tokyo Hot</title></head>
<body>
<div class="product-detail">
<h1>KB-456 ScrapeURL Movie Title</h1>
<div class="detail-spec">
<dl><dt>Release Date</dt><dd>2024/02/20</dd></dl>
<dl><dt>Duration</dt><dd>00:45:00</dd></dl>
</div>
<div class="actor"><a>Actress Two</a></div>
<img src="https://example.com/cover2.jpg" />
</div>
</body>
</html>`

	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &thMockTransport{
		response:   detailHTML,
		statusCode: 200,
	}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/product/KB-456/")
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-TokyoHot URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/product/KB-123/")
	require.Error(t, err)
}

// TestScrapeURLV3_Disabled tests ScrapeURL when disabled
func TestScrapeURLV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/product/KB-123/")
	require.Error(t, err)
}

// TestGetURLCtxV3 tests getURLCtx with various inputs
func TestGetURLCtxV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := scraper.getURLCtx(context.Background(), "")
		require.Error(t, err)
	})

	t.Run("URL input", func(t *testing.T) {
		url, err := scraper.getURLCtx(context.Background(), "https://www.tokyo-hot.com/product/KB-123/")
		require.NoError(t, err)
		assert.Contains(t, url, "KB-123")
	})
}

// TestResolveSearchQueryV3 tests ResolveSearchQuery
func TestResolveSearchQueryV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("valid TokyoHot URL", func(t *testing.T) {
		result, ok := scraper.ResolveSearchQuery("https://www.tokyo-hot.com/product/KB-123/")
		assert.True(t, ok)
		assert.NotEmpty(t, result)
	})

	t.Run("non-TokyoHot input", func(t *testing.T) {
		_, ok := scraper.ResolveSearchQuery("IPX-123")
		assert.False(t, ok)
	})
}

// TestCanHandleURLV3 tests CanHandleURL
func TestCanHandleURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://www.tokyo-hot.com/product/KB-123/"))
	assert.False(t, scraper.CanHandleURL("https://example.com/product/KB-123/"))
}

// thMockTransport is a mock transport for testing
type thMockTransport struct {
	response   string
	statusCode int
}

func (mt *thMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: mt.statusCode,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(mt.response)),
		Request:    req,
	}, nil
}

// thMockTransportHandler is a mock transport with a custom handler
type thMockTransportHandler struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (mt *thMockTransportHandler) RoundTrip(req *http.Request) (*http.Response, error) {
	return mt.handler(req)
}
