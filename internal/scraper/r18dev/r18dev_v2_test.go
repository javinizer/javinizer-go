package r18dev

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

type r18RoundTripper struct {
	responses map[string]string
	statuses  map[string]int
	headers   map[string]string
}

func (rt *r18RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for path, body := range rt.responses {
		if strings.Contains(req.URL.String(), path) || strings.Contains(path, req.URL.Path) {
			status := 200
			if s, ok := rt.statuses[path]; ok {
				status = s
			}
			hdr := http.Header{"Content-Type": []string{"application/json"}}
			if ct, ok := rt.headers[path]; ok {
				hdr = http.Header{"Content-Type": []string{ct}}
			}
			return &http.Response{
				StatusCode: status,
				Header:     hdr,
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
	respJSON := `{
		"dvd_id": "IPX-123",
		"content_id": "ipx00123",
		"title_ja": "テスト映画タイトル",
		"title_en": "Test Movie Title",
		"release_date": "2024-01-02",
		"runtime_mins": 120,
		"jacket_full_url": "https://example.com/cover.jpg",
		"maker": {"name": "TestMaker"},
		"label": {"name": "TestLabel"},
		"series": {"name": "TestSeries"},
		"director": "Director Name",
		"actresses": [{"id": 12345, "name_kanji": "女優一", "name_romaji": "Actress One"}],
		"categories": [{"name": "Drama"}]
	}`

	client := resty.New()
	client.SetTransport(&r18RoundTripper{
		responses: map[string]string{
			"r18.dev": respJSON,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), "https://r18.dev/videos/vod/movies/detail/-/id=ipx00123/")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "r18dev", result.Source)
	assert.Equal(t, "IPX-123", result.ID)
}

// TestScrapeURLV2_NotHandled tests ScrapeURL with non-R18.dev URL
func TestScrapeURLV2_NotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/videos/test/")
	require.Error(t, err)
}

// TestScrapeURLV2_Disabled tests ScrapeURL when disabled
func TestScrapeURLV2_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://r18.dev/videos/test/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestSearchV2_Success tests Search
func TestSearchV2_Success(t *testing.T) {
	respJSON := `{
		"dvd_id": "IPX-456",
		"content_id": "ipx00456",
		"title_ja": "サーチ映画",
		"title_en": "Search Movie Title",
		"release_date": "2024-03-15",
		"runtime_mins": 90,
		"maker": {"name": "SearchMaker"},
		"actresses": [],
		"categories": []
	}`

	client := resty.New()
	client.SetTransport(&r18RoundTripper{
		responses: map[string]string{
			"r18.dev": respJSON,
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.Search(context.Background(), "IPX-456")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "IPX-456", result.ID)
}

// TestSearchV2_NotFound tests Search when movie is not found
func TestSearchV2_NotFound(t *testing.T) {
	client := resty.New()
	client.SetTransport(&r18RoundTripper{
		responses: map[string]string{},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.Search(context.Background(), "NOTFOUND-99999")
	require.Error(t, err)
}

// TestScrapeURLV2_HTMLResponse tests ScrapeURL when response is HTML
func TestScrapeURLV2_HTMLResponse(t *testing.T) {
	client := resty.New()
	client.SetTransport(&r18RoundTripper{
		responses: map[string]string{
			"r18.dev/notfound": "<html><body>not found</body></html>",
		},
		headers: map[string]string{
			"r18.dev/notfound": "text/html",
		},
	})

	scraper := &scraper{
		client:      client,
		enabled:     true,
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://r18.dev/videos/notfound/")
	require.Error(t, err)
}

// TestCanHandleURLV2 tests CanHandleURL
func TestCanHandleURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://r18.dev/videos/ipx00123/"))
	assert.False(t, scraper.CanHandleURL("https://example.com/test"))
}

// TestExtractIDFromURLV2 tests ExtractIDFromURL
func TestExtractIDFromURLV2(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		language:    "en",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	id, err := scraper.ExtractIDFromURL("https://r18.dev/videos/vod/movies/detail/-/id=ipx00123/")
	require.NoError(t, err)
	assert.Equal(t, "ipx00123", id)
}
