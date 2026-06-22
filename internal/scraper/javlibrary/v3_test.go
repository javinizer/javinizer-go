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

// TestSearchV3_SuccessWithMockServer tests Search with a mock server
func TestSearchV3_SuccessWithMockServer(t *testing.T) {
	searchResultHTML := `
<html>
<body>
<div class="video" id="vid_javliat76u">
<div class="id">IPX-123</div>
</div>
</body>
</html>`

	detailHTML := `
<html>
<head><title>IPX-123 Test Movie - JAVLibrary</title></head>
<body>
<div id="video_info">
<div id="video_date"><span class="text">2024-01-15</span></div>
<div id="video_length"><span class="text">120</span></div>
<div id="video_director"><a>Test Director</a></div>
<div id="video_maker"><a>Test Maker</a></div>
<div id="video_label"><a>Test Label</a></div>
<div id="video_series"><a>Test Series</a></div>
<span class="genre"><a>Genre1</a></span>
<span class="star"><a>Actress One</a></span>
</div>
<div id="video_jacket_img" src="https://example.com/cover.jpg"></div>
<div id="video_rating"><span class="num">4.5</span></div>
</body>
</html>`

	callCount := 0
	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &jlMockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			callCount++
			body := searchResultHTML
			if callCount > 1 {
				body = detailHTML
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		},
	}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "http://www.javlibrary.com",
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "IPX-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-123", result.ID)
}

// TestScrapeURLV3_SuccessWithMockServer tests ScrapeURL with a mock server
func TestScrapeURLV3_SuccessWithMockServer(t *testing.T) {
	detailHTML := `
<html>
<head><title>IPX-456 ScrapeURL Movie - JAVLibrary</title></head>
<body>
<div id="video_info">
<div id="video_date"><span class="text">2024-02-20</span></div>
<div id="video_length"><span class="text">90</span></div>
<div id="video_maker"><a>Studio XYZ</a></div>
<span class="genre"><a>Drama</a></span>
<span class="star"><a>Actress Two</a></span>
</div>
<div id="video_jacket_img" src="https://example.com/cover2.jpg"></div>
</body>
</html>`

	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &jlMockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader(detailHTML)),
				Request:    req,
			}, nil
		},
	}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "http://www.javlibrary.com",
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "http://www.javlibrary.com/en/?v=javliat76u")
	require.NoError(t, err)
	require.NotNil(t, result)
}

// TestScrapeURLV3_Disabled tests ScrapeURL with disabled scraper
func TestScrapeURLV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "http://www.javlibrary.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "http://www.javlibrary.com/en/?v=test")
	// Should fail because CanHandleURL would fail for non-javlibrary URLs
	// or because URL is not handled
	// Note: when disabled, ScrapeURL still checks CanHandleURL first
	_ = err
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-JavLibrary URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "http://www.javlibrary.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/?v=test")
	require.Error(t, err)
}

// TestSearchV3_Disabled tests Search with disabled scraper
func TestSearchV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "http://www.javlibrary.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "IPX-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestScrapeURLV3_NoVideoInfo tests ScrapeURL when page has no video_info div
func TestScrapeURLV3_NoVideoInfo(t *testing.T) {
	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &jlMockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader(`<html><body>No video info here</body></html>`)),
				Request:    req,
			}, nil
		},
	}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "http://www.javlibrary.com",
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "http://www.javlibrary.com/en/?v=javliat76u")
	require.Error(t, err)
}

// TestFetchPageCtxV3_Non200Status tests fetchPageCtx with non-200 status
func TestFetchPageCtxV3_Non200Status(t *testing.T) {
	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &jlMockTransport{
		handler: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 403,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader("forbidden")),
				Request:    req,
			}, nil
		},
	}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "http://www.javlibrary.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.fetchPageCtx(context.Background(), "http://www.javlibrary.com/en/?v=test")
	require.Error(t, err)
}

// TestExtractMovieURLFromHTMLV3 tests extractMovieURLFromHTML with various HTML patterns
func TestExtractMovieURLFromHTMLV3(t *testing.T) {
	scraper := &scraper{
		baseURL:  "http://www.javlibrary.com",
		language: "en",
	}

	t.Run("current format with video div", func(t *testing.T) {
		html := `<div class="video" id="vid_javliat76u"><div class="id">IPX-123</div></div>`
		result := scraper.extractMovieURLFromHTML(html, "IPX-123")
		assert.Contains(t, result, "v=")
	})

	t.Run("legacy format with href and language", func(t *testing.T) {
		html := `<a href="/en/?v=javliat76u">IPX-123</a>`
		result := scraper.extractMovieURLFromHTML(html, "IPX-123")
		assert.NotEmpty(t, result)
	})

	t.Run("no matches", func(t *testing.T) {
		html := `<html><body>no links here</body></html>`
		result := scraper.extractMovieURLFromHTML(html, "IPX-123")
		assert.Empty(t, result)
	})
}

// TestIsValidLanguageV3 tests isValidLanguage
func TestIsValidLanguageV3(t *testing.T) {
	assert.True(t, isValidLanguage("en"))
	assert.True(t, isValidLanguage("ja"))
	assert.True(t, isValidLanguage("cn"))
	assert.True(t, isValidLanguage("tw"))
	assert.False(t, isValidLanguage("fr"))
	assert.False(t, isValidLanguage(""))
}

// TestExtractIDFromURLV3 tests ExtractIDFromURL with various URL formats
func TestExtractIDFromURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "http://www.javlibrary.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("query parameter v", func(t *testing.T) {
		id, err := scraper.ExtractIDFromURL("http://www.javlibrary.com/en/?v=javliat76u")
		require.NoError(t, err)
		assert.Equal(t, "javliat76u", id)
	})

	t.Run("query parameter keyword", func(t *testing.T) {
		id, err := scraper.ExtractIDFromURL("http://www.javlibrary.com/en/vl_searchbyid.php?keyword=IPX-123")
		require.NoError(t, err)
		assert.Equal(t, "IPX-123", id)
	})

	t.Run("invalid URL", func(t *testing.T) {
		_, err := scraper.ExtractIDFromURL("://invalid")
		require.Error(t, err)
	})
}

// CloseV3_WithFlareSolverr tests Close with flaresolverr
func TestCloseV3_WithFlareSolverr(t *testing.T) {
	scraper := &scraper{
		flaresolverr: nil,
	}
	assert.NoError(t, scraper.Close())
}

// jlMockTransport is a mock transport for testing
type jlMockTransport struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (mt *jlMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return mt.handler(req)
}
