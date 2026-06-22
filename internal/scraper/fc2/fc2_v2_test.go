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

// fc2RoundTripper maps URLs to HTML responses for testing
type fc2RoundTripper struct {
	responses map[string]string
}

func (rt *fc2RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if body, ok := rt.responses[req.URL.String()]; ok {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	}
	// Match by path
	for url, body := range rt.responses {
		parsed, _ := req.URL.Parse(url)
		if parsed != nil && parsed.Path == req.URL.Path {
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
<head>
<meta property="og:title" content="FC2-PPV-12345678 Test Movie Title - FC2" />
<meta property="og:image" content="https://adult.contents.fc2.com/cover.jpg" />
<meta property="og:description" content="Test description" />
</head>
<body>
<div class="items_article_MainitemThumb">
<div class="items_article_info">60 minutes</div>
</div>
<div class="items_article_headerInfo"><a href="/users/12345">TestMaker</a></div>
<script type="application/ld+json">{"@type":"Product","name":"FC2-PPV-12345678","aggregateRating":{"ratingValue":"4.5","reviewCount":"10"}}</script>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&fc2RoundTripper{
		responses: map[string]string{
			"https://adult.contents.fc2.com/article/12345678/": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/article/12345678/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "FC2-PPV-12345678", result.ID)
	assert.Equal(t, "fc2", result.Source)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-FC2 URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/article/12345678/")
	require.Error(t, err)
}

// TestScrapeURLV2_NoArticleID tests ScrapeURL when URL doesn't contain article ID
func TestScrapeURLV2_NoArticleID(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://adult.contents.fc2.com/other-page")
	require.Error(t, err)
}

// TestSearchV2_Success tests Search with mock transport
func TestSearchV2_Success(t *testing.T) {
	detailHTML := `
<!DOCTYPE html>
<html>
<head>
<meta property="og:title" content="FC2-PPV-12345678 Test Movie Title - FC2" />
<meta property="og:image" content="https://adult.contents.fc2.com/cover.jpg" />
<meta property="og:description" content="Test description" />
</head>
<body>
<div class="items_article_MainitemThumb">
<div class="items_article_info">60 minutes</div>
</div>
<div class="items_article_headerInfo"><a href="/users/12345">TestMaker</a></div>
</body>
</html>
`

	client := resty.New()
	client.SetTransport(&fc2RoundTripper{
		responses: map[string]string{
			"https://adult.contents.fc2.com/article/12345678/": detailHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "FC2-PPV-12345678")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "FC2-PPV-12345678", result.ID)
}

// TestSearchV2_Disabled tests Search when scraper is disabled
func TestSearchV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "FC2-PPV-12345678")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV2_InvalidID tests Search with an invalid ID format
func TestSearchV2_InvalidID(t *testing.T) {
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

// TestFetchPageCtxV2_Success tests fetchPageCtx with successful response
func TestFetchPageCtxV2_Success(t *testing.T) {
	client := resty.New()
	client.SetTransport(&fc2RoundTripper{
		responses: map[string]string{
			"https://adult.contents.fc2.com/test": "<html><body>hello</body></html>",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	html, status, err := scraper.fetchPageCtx(context.Background(), "https://adult.contents.fc2.com/test")
	require.NoError(t, err)
	assert.Equal(t, 200, status)
	assert.Contains(t, html, "hello")
}

// TestFetchPageCtxV2_Non200 tests fetchPageCtx with non-200 status
func TestFetchPageCtxV2_Non200(t *testing.T) {
	client := resty.New()
	client.SetTransport(&fc2RoundTripper{
		responses: map[string]string{},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, status, err := scraper.fetchPageCtx(context.Background(), "https://adult.contents.fc2.com/nonexistent")
	require.NoError(t, err)
	assert.Equal(t, 404, status)
}

// TestSearchV2_NotFoundPage tests Search when the detail page is FC2 not-found
func TestSearchV2_NotFoundPage(t *testing.T) {
	notFoundHTML := `<html><body><div class="wrapper-not-found"><h1>お探しの商品が見つかりませんでした</h1></div></body></html>`

	client := resty.New()
	client.SetTransport(&fc2RoundTripper{
		responses: map[string]string{
			"https://adult.contents.fc2.com/article/99999999/": notFoundHTML,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://adult.contents.fc2.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "FC2-PPV-99999999")
	require.Error(t, err)
}
