package aventertainment

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

type aveRoundTripper struct {
	responses map[string]string
}

func (rt *aveRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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
<head><title>TEST-123 Test Movie - AVEntertainment</title></head>
<body>
<div class="detail">
<h1>TEST-123 Test Movie</h1>
<table>
<tr><th>品番：</th><td>TEST-123</td></tr>
<tr><th>配信日：</th><td>2024-01-02</td></tr>
<tr><th>収録時間：</th><td>90分</td></tr>
<tr><th>メーカー：</th><td>TestMaker</td></tr>
</table>
</div>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&aveRoundTripper{
		responses: map[string]string{
			"aventertainments.com": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.aventertainments.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://www.aventertainments.com/product/TEST-123/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "aventertainment", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-AVE URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.aventertainments.com",
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
		baseURL:     "https://www.aventertainments.com",
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
		baseURL:     "https://www.aventertainments.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://www.aventertainments.com/product/test/"))
	assert.False(t, scraper.CanHandleURL("https://example.com/test"))
}

// TestFetchPageCtxV2 tests fetchPageCtx
func TestFetchPageCtxV2(t *testing.T) {
	client := resty.New()
	client.SetTransport(&aveRoundTripper{
		responses: map[string]string{
			"aventertainments.com/test": "<html><body>hello</body></html>",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.aventertainments.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "https://www.aventertainments.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "hello")
}
