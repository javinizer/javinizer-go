package dmm

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

func TestCleanupDirectProbeQueuedCancellationStopsHTTP(t *testing.T) {
	limiter := ratelimit.NewLimiter(time.Hour)
	require.NoError(t, limiter.Wait(context.Background())) // Reserve the immediate slot.
	ctx, cancel := context.WithCancel(context.Background())
	observed := &dmmObservedWaitContext{Context: ctx, waiting: make(chan struct{})}
	var requests atomic.Int32
	s := &scraper{
		rateLimiter: limiter,
		client: resty.New().SetRetryCount(0).SetTransport(dmmCancellationTransport(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			return dmmCancellationResponse(req, http.StatusOK), nil
		})),
	}
	result := make(chan []urlCandidate, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result <- s.tryDirectURLs(observed, "ssis00200")
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
	})
	select {
	case <-observed.waiting:
		// This caller has not reached HTTP: its real limiter is selecting on
		// the embedded context's Done channel behind the reserved slot.
	case <-time.After(2 * time.Second):
		t.Fatal("direct probe did not queue behind the reserved limiter slot")
	}
	cancel()
	select {
	case got := <-result:
		require.Nil(t, got)
		require.Zero(t, requests.Load())
		require.ErrorIs(t, observed.Err(), context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("queued limiter cancellation did not terminate the probe")
	}
}

func TestCleanupSearchLimiterCancellationPreservesErrorAndCache(t *testing.T) {
	s, assertCacheUnchanged := newDMMCachedSearchContainmentScraper(t)
	s.rateLimiter = ratelimit.NewLimiter(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requested []string
	s.client.SetTransport(dmmCancellationTransport(func(req *http.Request) (*http.Response, error) {
		requested = append(requested, req.URL.String())
		// Complete the first search response, then cancel before the next
		// query's limiter admission. The resolver used the real cached row.
		cancel()
		return dmmCancellationResponse(req, http.StatusOK), nil
	}))
	got, err := s.GetURL(ctx, "SSIS-200")
	require.Empty(t, got)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualError(t, err, "DMM search cancelled: context canceled")
	require.Equal(t, []string{searchURLFor("ssis200")}, requested)
	assertCacheUnchanged()
}
