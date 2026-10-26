package javdb

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
	"github.com/stretchr/testify/require"
)

type offlineEdgesTransport func(*http.Request) (*http.Response, error)

func (f offlineEdgesTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOfflineEdgesConstructorInvalidProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://environment.invalid:8080")
	proxy := &models.ProxyConfig{Enabled: true, DefaultProfile: "broken", Profiles: map[string]models.ProxyProfile{"broken": {URL: "http://%invalid"}}}
	s := newScraper(&models.ScraperSettings{Enabled: true, Timeout: 3}, proxy, models.FlareSolverrConfig{})
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	require.NotNil(t, s.client)
	require.Nil(t, s.flaresolverr)
	transport, ok := s.client.GetClient().Transport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, transport.Proxy, "invalid configured proxy must not fall back to environment proxy")
	require.Equal(t, int64(3), int64(s.client.GetClient().Timeout.Seconds()))
}

func TestOfflineEdgesSettingsValidation(t *testing.T) {
	for _, base := range []string{"", "https://javdb.com", "http://127.0.0.1:8080"} {
		require.NoError(t, validateScraperSettings(&models.ScraperSettings{BaseURL: base}))
	}
	require.Error(t, validateScraperSettings(&models.ScraperSettings{BaseURL: "ftp://javdb.com"}))
}

func TestOfflineEdgesScrapeTransportFailure(t *testing.T) {
	calls := 0
	s := newMarkerTestScraper(nil)
	s.client = resty.New().SetTransport(offlineEdgesTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "/v/fixture", req.URL.Path)
		return nil, io.ErrUnexpectedEOF
	}))
	result, err := s.ScrapeURL(context.Background(), "https://javdb.test/v/fixture")
	require.Nil(t, result)
	require.ErrorContains(t, err, "unexpected EOF")
	classified, ok := models.AsScraperError(err)
	require.True(t, ok)
	require.Equal(t, models.ScraperErrorKindUnknown, classified.Kind)
	require.Equal(t, 1, calls)
}

func TestOfflineEdgesVideoLookupFallsBackToSearch(t *testing.T) {
	var paths []string
	s := &scraper{enabled: true, baseURL: "https://javdb.test", rateLimiter: ratelimit.NewLimiter(0)}
	s.client = resty.New().SetTransport(offlineEdgesTransport(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		body := ""
		switch req.URL.Path {
		case "/v/ABC123":
			body = `<html>not a detail page</html>`
		case "/search":
			require.Equal(t, "ABC123", req.URL.Query().Get("q"))
			body = `<div class="movie-list"><div class="item"><span class="uid">ABC-123</span></div><div class="item"><a href="/v/chosen"><span class="uid">ABC-123</span></a></div></div>`
		case "/v/chosen":
			body = remasterDetailPage("ABC-123")
		default:
			t.Errorf("unexpected request %s", req.URL)
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	result, err := s.Search(context.Background(), "ABC123")
	require.NoError(t, err)
	require.Equal(t, "ABC-123", result.ID)
	require.Equal(t, "Some Title", result.Title)
	require.Equal(t, "https://javdb.test/v/chosen", result.SourceURL)
	require.Equal(t, []string{"/v/ABC123", "/search", "/v/chosen"}, paths)
}

func TestOfflineEdgesScrapeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := newMarkerTestScraper(nil)
	s.rateLimiter = ratelimit.NewLimiter(time.Hour)
	s.client.SetTransport(offlineEdgesTransport(func(*http.Request) (*http.Response, error) {
		t.Error("cancelled request dispatched")
		return nil, io.ErrUnexpectedEOF
	}))
	result, err := s.ScrapeURL(ctx, "https://javdb.test/v/fixture")
	require.Nil(t, result)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "failed to fetch JavDB page")
}
