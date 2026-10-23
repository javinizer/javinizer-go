package libredmm

import (
	"context"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type pollingFixtureTransport func(*http.Request) (*http.Response, error)

func (f pollingFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestScrapeURLCancellationDuringPendingPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responseRead := make(chan struct{})
	calls := 0
	requestedPath := ""
	s := &scraper{enabled: true, baseURL: "https://www.libredmm.com", maxPollAttempts: 2, pollInterval: time.Hour, rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(pollingFixtureTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		requestedPath = req.URL.Path
		return &http.Response{StatusCode: 202, Header: make(http.Header), Request: req, Body: &pollingResponseBody{Reader: strings.NewReader(`{"err":"queued"}`), closed: responseRead}}, nil
	}))}
	type outcome struct {
		result *models.ScraperResult
		err    error
	}
	done := make(chan outcome, 1)
	returned := false
	t.Cleanup(func() {
		cancel()
		if !returned {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("poll worker did not finish during cleanup")
			}
		}
	})
	go func() {
		result, err := s.ScrapeURL(ctx, "https://www.libredmm.com/movies/abc00123")
		done <- outcome{result, err}
	}()
	select {
	case <-responseRead:
	case <-time.After(5 * time.Second):
		t.Fatal("response was not consumed")
	}
	// The synchronous transport and body have finished. The only remaining
	// blocking operation is the hour-long pending-poll wait; ensure the call
	// remains blocked before cancellation arrives from this goroutine.
	select {
	case got := <-done:
		returned = true
		t.Fatalf("poll returned before cancellation: %v", got.err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	var got outcome
	select {
	case got = <-done:
		returned = true
	case <-time.After(5 * time.Second):
		t.Fatal("pending poll did not respond to cancellation")
	}
	require.Nil(t, got.result)
	require.Equal(t, context.Canceled, got.err, "poll cancellation is returned directly, not wrapped as a fetch error")
	require.Equal(t, 1, calls)
	require.Equal(t, "/movies/abc00123.json", requestedPath)
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

type pollingResponseBody struct {
	*strings.Reader
	closed chan struct{}
	once   sync.Once
}

func (b *pollingResponseBody) Close() error { b.once.Do(func() { close(b.closed) }); return nil }
