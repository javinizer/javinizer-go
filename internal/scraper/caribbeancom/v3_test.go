package caribbeancom

import (
	"context"
	"errors"
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

// TestScrapeURLV3_FetchError verifies the direct-page fetch error without network.
// The enabled flag gates Search, so it cannot stand in for a response fixture.
func TestScrapeURLV3_FetchError(t *testing.T) {
	const target = "https://www.caribbeancom.com/moviepages/012345-001/"
	fetchErr := errors.New("synthetic detail fetch failure")
	transport := &ccMockTransport{err: fetchErr}
	scraper := &scraper{
		client:      resty.New().SetTransport(transport),
		enabled:     true,
		baseURL:     "https://www.caribbeancom.com",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}

	result, err := scraper.ScrapeURL(context.Background(), target)
	assert.Nil(t, result)
	require.ErrorIs(t, err, fetchErr)
	assert.Contains(t, err.Error(), "failed to fetch Caribbeancom detail page")
	require.Len(t, transport.requests, 1)
	assert.Equal(t, http.MethodGet, transport.requests[0].Method)
	assert.Equal(t, target, transport.requests[0].URL.String())
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
	err        error
	requests   []*http.Request
	response   string
	statusCode int
}

func (mt *ccMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	mt.requests = append(mt.requests, req)
	if mt.err != nil {
		return nil, mt.err
	}
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
