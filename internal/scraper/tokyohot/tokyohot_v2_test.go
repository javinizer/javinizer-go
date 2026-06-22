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

type thRoundTripper struct {
	responses map[string]string
}

func (rt *thRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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
<div class="player">
<h1>TEST-123 Test Movie</h1>
<div class="detail">
<table>
<tr><th>品番：</th><td>TEST-123</td></tr>
<tr><th>配信日：</th><td>2024-01-02</td></tr>
<tr><th>収録時間：</th><td>90分</td></tr>
<tr><th>メーカー：</th><td>TestMaker</td></tr>
<tr><th>ジャンル：</th><td><a>Drama</a></td></tr>
<tr><th>出演者：</th><td><a>Actress One</a></td></tr>
</table>
</div>
</div>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&thRoundTripper{
		responses: map[string]string{
			"tokyo-hot.com": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://www.tokyo-hot.com/product/TEST-123/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "tokyohot", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-TokyoHot URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/product/TEST-123/")
	require.Error(t, err)
}

// TestSearchV2_Disabled tests Search when disabled
func TestSearchV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.tokyo-hot.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "TEST-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestCanHandleURLV2 tests CanHandleURL
func TestCanHandleURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://www.tokyo-hot.com/product/test/"))
	assert.False(t, scraper.CanHandleURL("https://example.com/test"))
}

// TestFetchPageCtxV2 tests fetchPageCtx
func TestFetchPageCtxV2(t *testing.T) {
	client := resty.New()
	client.SetTransport(&thRoundTripper{
		responses: map[string]string{
			"tokyo-hot.com/test": "<html><body>hello</body></html>",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "https://www.tokyo-hot.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "hello")
}

// TestExtractIDFromURLV2 tests ExtractIDFromURL
func TestExtractIDFromURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.tokyo-hot.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ExtractIDFromURL("https://www.tokyo-hot.com/product/TEST-123/")
	require.NoError(t, err)

	_, err = scraper.ExtractIDFromURL("https://www.tokyo-hot.com/invalid")
	require.Error(t, err)
}
