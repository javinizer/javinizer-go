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

type dlgRoundTripper struct {
	responses map[string]string
}

func (rt *dlgRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for path, body := range rt.responses {
		if strings.Contains(req.URL.String(), path) || strings.Contains(path, req.URL.Path) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=Shift_JIS"}},
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
<table>
<tr><td>作品ID：12345</td></tr>
<tr><td>テスト映画</td></tr>
</table>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&dlgRoundTripper{
		responses: map[string]string{
			"dl.getchu.com": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "http://dl.getchu.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "http://dl.getchu.com/index.php?action=article&id=12345")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "dlgetchu", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-DLGetchu URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "http://dl.getchu.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/article/12345")
	require.Error(t, err)
}

// TestSearchV2_Disabled tests Search when disabled
func TestSearchV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "http://dl.getchu.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestCanHandleURLV2 tests CanHandleURL
func TestCanHandleURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "http://dl.getchu.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("http://dl.getchu.com/index.php?action=article&id=12345"))
	assert.False(t, scraper.CanHandleURL("https://example.com/test"))
}

// TestExtractIDFromURLV2 tests ExtractIDFromURL
func TestExtractIDFromURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "http://dl.getchu.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	id, err := scraper.ExtractIDFromURL("http://dl.getchu.com/index.php?action=article&id=12345")
	require.NoError(t, err)
	assert.Equal(t, "12345", id)
}

// TestFetchPageCtxV2 tests fetchPageCtx
func TestFetchPageCtxV2(t *testing.T) {
	client := resty.New()
	client.SetTransport(&dlgRoundTripper{
		responses: map[string]string{
			"dl.getchu.com/test": "<html><body>hello</body></html>",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "http://dl.getchu.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "http://dl.getchu.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "hello")
}
