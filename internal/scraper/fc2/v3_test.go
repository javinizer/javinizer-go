package fc2

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

// TestScrapeURLV3_Success tests ScrapeURL with a mock server
func TestScrapeURLV3_Success(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<head>
<meta property="og:title" content="FC2 PPV 123456 Test FC2 Movie | FC2">
<meta property="og:description" content="Test description for FC2 movie">
<meta property="og:image" content="https://example.com/cover.jpg">
</head>
<body>
<div class="items_article_softDevice">
<p>販売日: 2024/01/15</p>
</div>
<div class="items_article_headerInfo">
<a href="/users/123">Test Maker</a>
</div>
<div class="items_article_TagArea">
<a class="tagTag">Tag1</a>
<a class="tagTag">Tag2</a>
</div>
</body>
</html>`

	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &fc2MockTransport{response: detailHTML, statusCode: 200}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/123456/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "FC2-PPV-123456", result.ID)
}

// fc2MockTransport returns a canned response for all requests
type fc2MockTransport struct {
	response   string
	statusCode int
}

func (mt *fc2MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: mt.statusCode,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(mt.response)),
		Request:    req,
	}, nil
}

// TestScrapeURLV3_HTTPStatusErrors tests ScrapeURL with various HTTP errors.
// Uses fc2MockTransport (custom RoundTripper) instead of httptest.Server
// because ScrapeURL receives an absolute FC2 URL and resty ignores
// SetBaseURL for absolute URLs — the request would hit the real FC2 site
// instead of the test server, making the test network-dependent.
func TestScrapeURLV3_HTTPStatusErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"404", 404},
		{"429", 429},
		{"403", 403},
		{"451", 451},
		{"500", 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := resty.New()
			httpClient := client.GetClient()
			httpClient.Transport = &fc2MockTransport{response: "", statusCode: tt.statusCode}

			scraper := &scraper{
				client:      client,
				enabled:     true,
				baseURL:     "https://adult.contents.fc2.com",
				rateLimiter: ratelimit.NewLimiter(0),
				settings:    models.ScraperSettings{Enabled: true},
			}

			_, err := scraper.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/123456/")
			require.Error(t, err)
		})
	}
}

// TestScrapeURLV3_NotFoundPage tests ScrapeURL when page shows not found content.
// Uses fc2MockTransport (custom RoundTripper) instead of httptest.Server
// for the same reason as TestScrapeURLV3_HTTPStatusErrors.
func TestScrapeURLV3_NotFoundPage(t *testing.T) {
	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &fc2MockTransport{
		response:   `<html><body>お探しの商品が見つかりませんでした</body></html>`,
		statusCode: 200,
	}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/999999/")
	require.Error(t, err)
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-FC2 URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/article/123456/")
	require.Error(t, err)
}

// TestSearchV3_DisabledScraper tests Search with disabled scraper
func TestSearchV3_DisabledScraper(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "FC2-PPV-123456")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV3_InvalidID tests Search with invalid ID format
func TestSearchV3_InvalidID(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "INVALID-ID")
	require.Error(t, err)
}

// TestGetURLCtxV3_Various tests getURLCtx with various inputs
func TestGetURLCtxV3_Various(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := scraper.getURLCtx(context.Background(), "")
		require.Error(t, err)
	})

	t.Run("FC2 PPV ID", func(t *testing.T) {
		url, err := scraper.getURLCtx(context.Background(), "FC2-PPV-123456")
		require.NoError(t, err)
		assert.Contains(t, url, "123456")
	})

	t.Run("URL with article ID", func(t *testing.T) {
		url, err := scraper.getURLCtx(context.Background(), "https://adult.contents.fc2.com/article/123456/")
		require.NoError(t, err)
		assert.Contains(t, url, "123456")
	})

	t.Run("non-FC2 URL fails", func(t *testing.T) {
		_, err := scraper.getURLCtx(context.Background(), "https://example.com/not-fc2")
		require.Error(t, err)
	})
}

// TestExtractArticleIDV3 tests extractArticleID with various inputs
func TestExtractArticleIDV3(t *testing.T) {
	assert.Equal(t, "123456", extractArticleID("FC2-PPV-123456"))
	assert.Equal(t, "789012", extractArticleID("fc2_ppv_789012"))
	assert.Equal(t, "345678", extractArticleID("PPV-345678"))
	assert.Equal(t, "111111", extractArticleID("111111"))
	assert.Equal(t, "", extractArticleID(""))
	assert.Equal(t, "", extractArticleID("invalid"))
}

// TestCanonicalFC2IDV3 tests canonicalFC2ID
func TestCanonicalFC2IDV3(t *testing.T) {
	assert.Equal(t, "FC2-PPV-123456", canonicalFC2ID("123456"))
	assert.Equal(t, "FC2-PPV-789012", canonicalFC2ID("789012"))
}

// TestStripFC2IDPrefixV3 tests stripFC2IDPrefix
func TestStripFC2IDPrefixV3(t *testing.T) {
	assert.Equal(t, "Test Movie", stripFC2IDPrefix("FC2-PPV-123456 Test Movie"))
	assert.Equal(t, "No Prefix", stripFC2IDPrefix("No Prefix"))
}

// TestParseRuntimeV3 tests parseRuntime with various formats
func TestParseRuntimeV3(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1:30:00", 90},
		{"45:30", 46},
		{"120 minutes", 120},
		{"90min", 90},
		{"", 0},
		{"invalid", 0},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, parseRuntime(tt.input))
	}
}

// TestParseReleaseDateV3 tests parseReleaseDate
func TestParseReleaseDateV3(t *testing.T) {
	t.Run("valid date", func(t *testing.T) {
		result := parseReleaseDate("2024/01/15")
		require.NotNil(t, result)
		assert.Equal(t, 2024, result.Year())
	})

	t.Run("dash format", func(t *testing.T) {
		result := parseReleaseDate("2024-01-15")
		require.NotNil(t, result)
	})

	t.Run("empty", func(t *testing.T) {
		assert.Nil(t, parseReleaseDate(""))
	})

	t.Run("invalid", func(t *testing.T) {
		assert.Nil(t, parseReleaseDate("not-a-date"))
	})
}

// TestIsFC2NotFoundPageV3 tests isFC2NotFoundPage
func TestIsFC2NotFoundPageV3(t *testing.T) {
	assert.True(t, isFC2NotFoundPage(`<html>お探しの商品が見つかりませんでした</html>`))
	assert.True(t, isFC2NotFoundPage(`<html>this page may have been deleted</html>`))
	assert.False(t, isFC2NotFoundPage(`<html>Normal page</html>`))
}

// TestStripSiteSuffixV3 tests stripSiteSuffix
func TestStripSiteSuffixV3(t *testing.T) {
	assert.Equal(t, "Test Movie", stripSiteSuffix("Test Movie | FC2"))
	assert.Equal(t, "No Suffix", stripSiteSuffix("No Suffix"))
	assert.Equal(t, "", stripSiteSuffix(""))
}

// TestNormalizeURLV3 tests normalizeURL
func TestNormalizeURLV3(t *testing.T) {
	assert.Equal(t, "https://example.com/img.jpg", normalizeURL("//example.com/img.jpg", "https://base.com"))
	assert.Equal(t, "https://example.com/img.jpg", normalizeURL("https://example.com/img.jpg", "https://base.com"))
	assert.Equal(t, "", normalizeURL("", "https://base.com"))
}
