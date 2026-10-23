package r18dev

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

type retryCancellationTransport func(*http.Request) (*http.Response, error)

func (f retryCancellationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type retryResponseBody struct {
	*strings.Reader
	consumed chan struct{}
	once     sync.Once
}

func (b *retryResponseBody) Close() error { b.once.Do(func() { close(b.consumed) }); return nil }

func TestOfflineRetryBackoffHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	consumed := make(chan struct{})
	calls := 0
	s := &scraper{maxRetries: 1, respectRetryAfter: true, rateLimiter: ratelimit.NewLimiter(0), client: resty.New().SetTransport(retryCancellationTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"3600"}}, Request: req, Body: &retryResponseBody{Reader: strings.NewReader("busy"), consumed: consumed}}, nil
	}))}
	type outcome struct {
		response *resty.Response
		err      error
	}
	done := make(chan outcome, 1)
	returned := false
	t.Cleanup(func() {
		cancel()
		if !returned {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("retry worker did not stop during cleanup")
			}
		}
	})
	go func() {
		response, err := s.doRequestWithRetryCtx(ctx, "https://r18.dev/fixture")
		done <- outcome{response, err}
	}()
	select {
	case <-consumed:
	case <-time.After(5 * time.Second):
		t.Fatal("rate-limit response was not consumed")
	}
	// Cancel only after the first response is consumed and the call remains
	// blocked on its hour-long retry wait, not during request setup.
	select {
	case got := <-done:
		returned = true
		t.Fatalf("retry returned before cancellation: %v", got.err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	var got outcome
	select {
	case got = <-done:
		returned = true
	case <-time.After(5 * time.Second):
		t.Fatal("backoff did not respond to cancellation")
	}
	require.Nil(t, got.response)
	require.Equal(t, context.Canceled, got.err, "backoff must propagate cancellation directly, not wrap a fetch failure")
	require.Equal(t, 1, calls, "cancellation during backoff must prevent a second attempt")
}

func TestOfflineGlobalProxyIsConfigured(t *testing.T) {
	proxy := &models.ProxyConfig{Enabled: true, DefaultProfile: "fixture", Profiles: map[string]models.ProxyProfile{"fixture": {URL: "http://127.0.0.1:1"}}}
	s := newScraper(&models.ScraperSettings{Enabled: true}, proxy, models.FlareSolverrConfig{}, nil)
	transport, ok := s.client.GetClient().Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.Proxy)
	request, err := http.NewRequest(http.MethodGet, "https://r18.dev/", nil)
	require.NoError(t, err)
	selected, err := transport.Proxy(request)
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:1", selected.String())
}
