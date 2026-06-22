package mgstage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mgsRoundTripper maps URLs to HTML responses for testing
type mgsRoundTripper struct {
	responses map[string]string
	statuses  map[string]int
}

func (rt *mgsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.String()
	// Try direct match
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
	// Try path match
	for url, body := range rt.responses {
		if strings.Contains(url, req.URL.Path) || strings.HasSuffix(url, req.URL.RawQuery) {
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
<head><title>TEST-123 Test Movie Title - MGS</title></head>
<body>
<table>
<tr><th>品番：</th><td>TEST-123</td></tr>
<tr><th>配信開始日：</th><td>2024/01/15</td></tr>
<tr><th>収録時間：</th><td>120分</td></tr>
<tr><th>出演：</th><td><a>Actress One</a> <a>Actress Two</a></td></tr>
<tr><th>ジャンル：</th><td><a>Drama</a> <a>School</a></td></tr>
</table>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&mgsRoundTripper{
		responses: map[string]string{
			"https://www.mgstage.com/product/product_detail/TEST-123/": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/TEST-123/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "TEST-123", result.ID)
	assert.Equal(t, "mgstage", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-MGStage URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/product/TEST-123/")
	require.Error(t, err)
}

// TestScrapeURLV2_404 tests ScrapeURL with 404 response
func TestScrapeURLV2_404(t *testing.T) {
	client := resty.New()
	client.SetTransport(&mgsRoundTripper{
		responses: map[string]string{},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.mgstage.com/product/product_detail/NOTFOUND-999/")
	require.Error(t, err)
}

// TestSearchV2_Success tests Search with mock transport
func TestSearchV2_Success(t *testing.T) {
	searchResultHTML := `
<!DOCTYPE html>
<html>
<body>
<a href="/product/product_detail/LUXU-1806/">LUXU-1806</a>
</body>
</html>
`
	detailHTML := `
<!DOCTYPE html>
<html>
<head><title>LUXU-1806 Test Movie - MGS</title></head>
<body>
<table>
<tr><th>品番：</th><td>LUXU-1806</td></tr>
<tr><th>配信開始日：</th><td>2024/01/15</td></tr>
<tr><th>収録時間：</th><td>90分</td></tr>
</table>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&mgsRoundTripper{
		responses: map[string]string{
			"https://www.mgstage.com/search/cSearch.php?search_word=LUXU-1806&type=top&page=1&list_cnt=120": searchResultHTML,
			"https://www.mgstage.com/product/product_detail/LUXU-1806/":                                     detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "LUXU-1806")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "LUXU-1806", result.ID)
}

// TestSearchV2_NotFound tests Search when movie not found
func TestSearchV2_NotFound(t *testing.T) {
	client := resty.New()
	client.SetTransport(&mgsRoundTripper{
		responses: map[string]string{},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Search with a short timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := scraper.Search(ctx, "NOTFOUND-99999")
	require.Error(t, err)
}

// TestCanHandleURLV2 tests CanHandleURL
func TestCanHandleURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://www.mgstage.com/product/product_detail/TEST-123/"))
	assert.False(t, scraper.CanHandleURL("https://example.com/test"))
	assert.False(t, scraper.CanHandleURL("not-a-url"))
}

// TestExtractIDFromURLV2 tests ExtractIDFromURL
func TestExtractIDFromURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	id, err := scraper.ExtractIDFromURL("https://www.mgstage.com/product/product_detail/TEST-123/")
	require.NoError(t, err)
	assert.Equal(t, "TEST-123", id)

	_, err = scraper.ExtractIDFromURL("https://www.mgstage.com/other")
	require.Error(t, err)
}

// TestResolveSearchQueryV2 tests ResolveSearchQuery
func TestResolveSearchQueryV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	// Test with MGStage-style ID
	id, ok := scraper.ResolveSearchQuery("LUXU-1806")
	assert.True(t, ok)
	assert.Equal(t, "LUXU-1806", id)

	// Test with empty input
	_, ok = scraper.ResolveSearchQuery("")
	assert.False(t, ok)
}

// TestNormalizeMGStageIDTokenV2 tests normalizeMGStageIDToken
func TestNormalizeMGStageIDTokenV2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"LUXU-1806", "LUXU-1806", true},
		{"luxu-1806", "LUXU-1806", true},
		{"259LUXU-1806", "259LUXU-1806", true},
		{"123", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, ok := normalizeMGStageIDToken(tt.input)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestSplitMGStageIDV2 tests splitMGStageID
func TestSplitMGStageIDV2(t *testing.T) {
	prefix, num := splitMGStageID("LUXU-1806")
	assert.Equal(t, "LUXU", prefix)
	assert.Equal(t, "1806", num)
}

// TestCleanTitleV2 tests cleanTitle
func TestCleanTitleV2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"「Test Title」 - MGS", "Test Title"},
		{"Simple Title", "Simple Title"},
		{"Title：MGS Site", "Title"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := cleanTitle(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
