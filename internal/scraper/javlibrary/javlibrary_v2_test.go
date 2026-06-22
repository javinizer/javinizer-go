package javlibrary

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

type jlRoundTripper struct {
	responses map[string]string
}

func (rt *jlRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for url, body := range rt.responses {
		if strings.Contains(req.URL.String(), url) || strings.Contains(url, req.URL.Path) {
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

// TestScrapeURLV2_Success tests ScrapeURL with mock transport
func TestScrapeURLV2_Success(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<body>
<div id="video_info">
<div id="video_title"><h3>IPX-123 Test Movie</h3></div>
<div id="video_date">2024-01-02</div>
<div id="video_length">120</div>
<div id="video_maker"><a>TestMaker</a></div>
<div id="video_label"><a>TestLabel</a></div>
<div id="video_genres"><a>Drama</a><a>School</a></div>
<div id="video_cast"><a>Actress One</a><a>Actress Two</a></div>
<div id="video_director"><a>Director</a></div>
</div>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&jlRoundTripper{
		responses: map[string]string{
			"javlibrary.com": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://www.javlibrary.com/ja/?v=123456")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "javlibrary", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-JavLibrary URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
}

// TestSearchV2_Disabled tests Search when disabled
func TestSearchV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV2_DirectDetail tests Search when search lands directly on detail page
func TestSearchV2_DirectDetail(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<body>
<div id="video_info">
<div id="video_title"><h3>IPX-789 Test Movie</h3></div>
<div id="video_date">2024-03-15</div>
<div id="video_length">90</div>
<div id="video_maker"><a>TestMaker</a></div>
</div>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&jlRoundTripper{
		responses: map[string]string{
			"vl_searchbyid.php": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "IPX-789")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "javlibrary", result.Source)
}

// TestFetchPageCtxV2_Success tests fetchPageCtx
func TestFetchPageCtxV2_Success(t *testing.T) {
	client := resty.New()
	client.SetTransport(&jlRoundTripper{
		responses: map[string]string{
			"javlibrary.com/test": "<html><body>hello</body></html>",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, err := scraper.fetchPageCtx(context.Background(), "https://www.javlibrary.com/test")
	require.NoError(t, err)
	assert.Contains(t, html, "hello")
}

// TestGetURLCtxV2 tests getURLCtx
func TestGetURLCtxV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	url, err := scraper.getURLCtx(context.Background(), "IPX-123")
	require.NoError(t, err)
	assert.Contains(t, url, "IPX-123")
	assert.Contains(t, url, "vl_searchbyid.php")
}

// TestExtractIDFromURLV2 tests ExtractIDFromURL
func TestExtractIDFromURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		language:    "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	id, err := scraper.ExtractIDFromURL("https://www.javlibrary.com/ja/?v=javme5likd")
	require.NoError(t, err)
	assert.Equal(t, "javme5likd", id)

	id, err = scraper.ExtractIDFromURL("https://www.javlibrary.com/ja/vl_searchbyid.php?keyword=IPX-123")
	require.NoError(t, err)
	assert.Equal(t, "IPX-123", id)
}

// TestCanHandleURLV2 tests CanHandleURL
func TestCanHandleURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.javlibrary.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://www.javlibrary.com/ja/?v=123"))
	assert.False(t, scraper.CanHandleURL("https://example.com/test"))
}
