package libredmm

import (
	"context"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type pollingFixtureTransport func(*http.Request) (*http.Response, error)

func (f pollingFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestScrapeURLCancellationDuringPendingPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	s := &scraper{enabled: true, baseURL: "https://www.libredmm.com", maxPollAttempts: 2, pollInterval: time.Hour, rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(pollingFixtureTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "/movies/abc00123.json", req.URL.Path)
		cancel()
		return &http.Response{StatusCode: 202, Header: make(http.Header), Request: req, Body: io.NopCloser(strings.NewReader(`{"err":"queued"}`))}, nil
	}))}
	result, err := s.ScrapeURL(ctx, "https://www.libredmm.com/movies/abc00123")
	require.Nil(t, result)
	require.Equal(t, context.Canceled, err, "poll cancellation is returned directly, not wrapped as a fetch error")
	require.Equal(t, 1, calls)
}

func TestPollingLimiterCancellationPreventsRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &scraper{rateLimiter: ratelimit.NewLimiter(time.Hour)}
	payload, final, status, err := s.fetchMovieJSONCtx(ctx, "https://www.libredmm.com/movies/abc00123.json")
	require.Nil(t, payload)
	require.Empty(t, final)
	require.Zero(t, status)
	require.ErrorIs(t, err, context.Canceled)
}

func TestNormalizationRejectsEmptyMovieIdentifiers(t *testing.T) {
	for _, raw := range []string{"https://www.libredmm.com/movies/.json", "https://www.libredmm.com/cid/.json"} {
		got, ok := normalizeMovieURL(raw, defaultBaseURL)
		require.False(t, ok)
		require.Empty(t, got)
	}
	require.Equal(t, "%zz", extractIDFromURL("https://www.libredmm.com/?cid=%zz"))
	require.Equal(t, "http://%", stripJSONSuffix("http://%.json"))
}
