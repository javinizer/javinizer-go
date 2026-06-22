package mgstage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearchV3_Disabled tests Search when disabled - MGStage doesn't check enabled in Search, so it will try to fetch
func TestSearchV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "AB-123")
	// Search doesn't check enabled flag in MGStage, it will fail with a network error or not found
	require.Error(t, err)
}

// TestScrapeURLV3_Disabled tests ScrapeURL when disabled
func TestScrapeURLV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/AB-123/")
	require.Error(t, err)
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-MGStage URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/product/AB-123/")
	require.Error(t, err)
}

// TestCanHandleURLV3 tests CanHandleURL
func TestCanHandleURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://www.mgstage.com/product/product_detail/AB-123/"))
	assert.False(t, scraper.CanHandleURL("https://example.com/product/AB-123/"))
}

// TestResolveSearchQueryV3 tests ResolveSearchQuery
func TestResolveSearchQueryV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("MGStage URL", func(t *testing.T) {
		result, ok := scraper.ResolveSearchQuery("https://www.mgstage.com/product/product_detail/AB-123/")
		assert.True(t, ok)
		assert.NotEmpty(t, result)
	})

	t.Run("MGStage-style ID", func(t *testing.T) {
		result, ok := scraper.ResolveSearchQuery("GANA-2850")
		assert.True(t, ok)
		assert.NotEmpty(t, result)
	})

	t.Run("non-MGStage input", func(t *testing.T) {
		_, ok := scraper.ResolveSearchQuery("completely_invalid!!")
		assert.False(t, ok)
	})
}

// TestExtractTableValueV3 tests extractTableValue with HTML input
func TestExtractTableValueV3(t *testing.T) {
	html := `<html><body><table><tr><th>Label</th><td>Value Text</td></tr></table></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractTableValue(doc, "Label")
	assert.Equal(t, "Value Text", result)
}

// TestExtractTableLinkValueV3 tests extractTableLinkValue
func TestExtractTableLinkValueV3(t *testing.T) {
	html := `<html><body><table><tr><th>Maker</th><td><a href="/maker/1">Test Maker</a></td></tr></table></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractTableLinkValue(doc, "Maker")
	assert.Equal(t, "Test Maker", result)
}

// TestExtractGenresV3 tests extractGenres
func TestExtractGenresV3(t *testing.T) {
	html := `<html><body><table><tr><th>ジャンル：</th><td><a href="/genre/1">Drama</a><a href="/genre/2">Romance</a></td></tr></table></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	result := extractGenres(doc)
	assert.Contains(t, result, "Drama")
	assert.Contains(t, result, "Romance")
}

// TestFetchPageCtxV3_Removed removed - mgstage doesn't have fetchPageCtx

// mgsMockTransport is a mock transport
type mgsMockTransport struct {
	response   string
	statusCode int
}

func (mt *mgsMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: mt.statusCode,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(mt.response)),
		Request:    req,
	}, nil
}
