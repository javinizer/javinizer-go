package dlgetchu

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

// TestSearchV3_Disabled tests Search when disabled
func TestSearchV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.dlsite.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "RJ123456")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestScrapeURLV3_Disabled tests ScrapeURL when disabled
func TestScrapeURLV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.dlsite.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.dlsite.com/maniax/work/=/product_id/RJ123456.html")
	require.Error(t, err)
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-DLsite URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.dlsite.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/work/RJ123456.html")
	require.Error(t, err)
}

// TestCanHandleURLV3 tests CanHandleURL
func TestCanHandleURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.dlsite.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://dl.getchu.com/item/123456"))
	assert.False(t, scraper.CanHandleURL("https://example.com/work/RJ123456.html"))
}

// TestFetchPageCtxV3 tests fetchPageCtx
func TestFetchPageCtxV3(t *testing.T) {
	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &dlgMockTransport{statusCode: 200, response: "<html>ok</html>"}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.dlsite.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "https://www.dlsite.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "ok")
}

// TestExtractIDFromURLV3 tests ExtractIDFromURL
func TestExtractIDFromURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.dlsite.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("item URL", func(t *testing.T) {
		id, err := scraper.ExtractIDFromURL("https://www.dlsite.com/i/item123456")
		require.NoError(t, err)
		assert.NotEmpty(t, id)
	})

	t.Run("invalid URL", func(t *testing.T) {
		_, err := scraper.ExtractIDFromURL("://invalid")
		require.Error(t, err)
	})
}

// dlgMockTransport is a mock transport
type dlgMockTransport struct {
	response   string
	statusCode int
}

func (mt *dlgMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: mt.statusCode,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(mt.response)),
		Request:    req,
	}, nil
}
