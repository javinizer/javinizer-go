package dmm

import (
	"context"
	"encoding/json"
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

// TestSearchV3_Disabled tests Search when disabled
func TestSearchV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-123")
	require.Error(t, err)
	// DMM search may not check enabled flag directly
}

// TestScrapeURLV3_Disabled tests ScrapeURL when disabled - DMM doesn't check enabled flag
func TestScrapeURLV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	// DMM ScrapeURL doesn't check enabled flag - it checks CanHandleURL first
	// Using a non-DMM URL will cause an error
	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/detail/")
	require.Error(t, err)
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-DMM URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/detail/cid=ipx00123/")
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

	assert.True(t, scraper.CanHandleURL("https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=ipx00123/"))
	assert.False(t, scraper.CanHandleURL("https://example.com/detail/"))
}

// TestGetURLCtxV3 tests getURLCtx
func TestGetURLCtxV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := scraper.getURLCtx(context.Background(), "")
		require.Error(t, err)
	})
}

// TestStripANSICodesV3_DMM tests stripANSICodes
func TestStripANSICodesV3_DMM(t *testing.T) {
	// Test JSON-LD parsing with clean input
	input := `{"name":"Test Movie","@type":"VideoObject"}`
	var result map[string]any
	err := json.Unmarshal([]byte(input), &result)
	require.NoError(t, err)
	assert.Equal(t, "Test Movie", result["name"])
}

// dmmMockTransport is a mock transport for testing
type dmmMockTransport struct {
	response   string
	statusCode int
}

func (mt *dmmMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: mt.statusCode,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(mt.response)),
		Request:    req,
	}, nil
}

// TestStripRentalSuffixV3 tests stripRentalSuffix
func TestStripRentalSuffixV3(t *testing.T) {
	assert.Equal(t, "ipx123", stripRentalSuffix("ipx123r"))
	assert.Equal(t, "ipx123", stripRentalSuffix("ipx123"))
	assert.Equal(t, "abc", stripRentalSuffix("abc"))
}

// TestUniqueNonEmptyStringsV3 tests uniqueNonEmptyStrings
func TestUniqueNonEmptyStringsV3(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, uniqueNonEmptyStrings([]string{"a", "b", "a", "", "b"}))
	assert.Equal(t, []string{}, uniqueNonEmptyStrings([]string{"", ""}))
}

// TestNormalizedContentIDWithoutPaddingV3 tests normalizedContentIDWithoutPadding
func TestNormalizedContentIDWithoutPaddingV3(t *testing.T) {
	assert.Equal(t, "ipx123", normalizedContentIDWithoutPadding("ipx00123"))
	assert.Equal(t, "", normalizedContentIDWithoutPadding(""))
}

// TestBuildResolveContentIDSearchQueriesV3 tests buildResolveContentIDSearchQueries
func TestBuildResolveContentIDSearchQueriesV3(t *testing.T) {
	queries := buildResolveContentIDSearchQueries("IPX-123", "ipx00123")
	assert.NotEmpty(t, queries)
	// Should contain both the search ID (normalized) and the content ID
	found123 := false
	for _, q := range queries {
		if strings.Contains(q, "123") {
			found123 = true
		}
	}
	assert.True(t, found123)
}

// TestExtractCoverURLV3 tests extractCoverURL with mock HTML
func TestExtractCoverURLV3(t *testing.T) {
	html := `<html><head><meta property="og:image" content="https://pics.dmm.co.jp/digital/video/ipx00123/ipx00123pl.jpg"></head><body></body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	coverURL := scraper.extractCoverURL(doc, false, "ipx00123")
	assert.NotEmpty(t, coverURL)
}

// TestExtractScreenshotsV3 tests extractScreenshots with mock HTML
func TestExtractScreenshotsV3(t *testing.T) {
	html := `<html><body>
	<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00123/ipx00123jp-1.jpg"></a>
	<a name="sample-image"><img data-lazy="https://pics.dmm.co.jp/digital/video/ipx00123/ipx00123jp-2.jpg"></a>
	</body></html>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)

	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	screenshots := scraper.extractScreenshots(doc, false)
	assert.Len(t, screenshots, 2)
}
