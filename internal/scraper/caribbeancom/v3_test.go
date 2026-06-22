package caribbeancom

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

// TestSearchV3_Disabled tests Search when disabled
func TestSearchV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.Search(context.Background(), "012345-001")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// TestScrapeURLV3_Disabled tests ScrapeURL when disabled
func TestScrapeURLV3_Disabled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     false,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: false},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/012345-001/")
	require.Error(t, err)
}

// TestScrapeURLV3_URLNotHandled tests ScrapeURL with non-Caribbeancom URL
func TestScrapeURLV3_URLNotHandled(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, err := scraper.ScrapeURL(context.Background(), "https://example.com/moviepages/012345-001/")
	require.Error(t, err)
}

// TestFetchPageCtxV3_Non200 tests fetchPageCtx with non-200 status
func TestFetchPageCtxV3_Non200(t *testing.T) {
	client := resty.New()
	httpClient := client.GetClient()
	httpClient.Transport = &ccMockTransport{statusCode: 404}

	scraper := &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	_, status, err := scraper.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/test")
	require.NoError(t, err)
	assert.Equal(t, 404, status)
}

// TestNormalizeMovieIDV3 tests normalizeMovieID
func TestNormalizeMovieIDV3(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"012345_001", "012345-001"},
		{"012345-001", "012345-001"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, normalizeMovieID(tt.input))
	}
}

// TestParseReleaseDateFromIDV3 tests parseReleaseDateFromID
func TestParseReleaseDateFromIDV3(t *testing.T) {
	t.Run("valid date from ID", func(t *testing.T) {
		result := parseReleaseDateFromID("011524-001")
		require.NotNil(t, result)
		assert.Equal(t, 2024, result.Year())
		assert.Equal(t, 1, int(result.Month()))
		assert.Equal(t, 15, result.Day())
	})

	t.Run("short ID", func(t *testing.T) {
		result := parseReleaseDateFromID("12345-001")
		assert.Nil(t, result)
	})
}

// TestCanHandleURLV3 tests CanHandleURL
func TestCanHandleURLV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	assert.True(t, scraper.CanHandleURL("https://www.caribbeancom.com/moviepages/012345-001/"))
	assert.False(t, scraper.CanHandleURL("https://example.com/moviepages/012345-001/"))
}

// TestGetURLCtxV3 tests getURLCtx
func TestGetURLCtxV3(t *testing.T) {
	scraper := &scraper{
		client:      resty.New(),
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	t.Run("empty ID", func(t *testing.T) {
		_, err := scraper.getURLCtx(context.Background(), "")
		require.Error(t, err)
	})

	t.Run("URL input", func(t *testing.T) {
		url, err := scraper.getURLCtx(context.Background(), "https://www.caribbeancom.com/moviepages/012345-001/")
		require.NoError(t, err)
		assert.Contains(t, url, "012345-001")
	})
}

// ccMockTransport is a mock transport for testing
type ccMockTransport struct {
	response   string
	statusCode int
}

func (mt *ccMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := mt.response
	if body == "" {
		body = "<html><body>mock response</body></html>"
	}
	return &http.Response{
		StatusCode: mt.statusCode,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}
