package translation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// This client exercises the public HTTPClient dependency, without sockets or
// provider internals. During the first operation each request remains in flight
// until its real request context is cancelled; the retry returns local JSON.
type saturatedGoogleClient struct {
	retry  atomic.Bool
	calls  atomic.Int32
	active atomic.Int32
	closed atomic.Int32
}

func (c *saturatedGoogleClient) Do(req *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	c.active.Add(1)
	defer c.active.Add(-1)
	if !c.retry.Load() {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	data, err := json.Marshal([][][]string{{{"translated: " + req.URL.Query().Get("q")}}})
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       &saturatedGoogleBody{Reader: strings.NewReader(string(data)), closed: &c.closed},
		Header:     make(http.Header), Request: req,
	}, nil
}

type saturatedGoogleBody struct {
	io.Reader
	closed *atomic.Int32
}

func (b *saturatedGoogleBody) Close() error {
	b.closed.Add(1)
	return nil
}

func TestGoogleFreeCancellationWhileAdmissionSaturatedJoinsWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		client := &saturatedGoogleClient{}
		provider := NewGoogleProvider(Config{Google: googleConfig{
			Mode: models.GoogleModeFree, BaseURL: "https://translation.invalid",
		}}, client)
		texts := []string{"一", "二", "三", "四", "五", "queued sixth"}
		before := append([]string(nil), texts...)
		type outcome struct {
			result *translationResult
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			result, err := provider.Translate(ctx, "ja", "en", texts)
			done <- outcome{result, err}
		}()

		// All five requests and the sixth admission must be durably blocked.
		// No sleeps, scheduler lottery, or private semaphore inspection.
		synctest.Wait()
		require.Equal(t, int32(5), client.calls.Load())
		require.Equal(t, int32(5), client.active.Load())
		select {
		case <-done:
			t.Fatal("saturated operation returned before cancellation")
		default:
		}
		cancel()
		synctest.Wait()
		got := <-done
		require.Nil(t, got.result)
		// Transport errors are intentionally redacted by the provider rather
		// than wrapping context.Canceled or leaking source text in a URL.
		require.EqualError(t, got.err, "google free translation request failed")
		var providerErr *translationError
		require.ErrorAs(t, got.err, &providerErr)
		require.Equal(t, TranslationErrorProvider, providerErr.Kind)
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.Equal(t, int32(5), client.calls.Load(), "queued request must never reach HTTP")
		require.Zero(t, client.active.Load(), "return must join every in-flight worker")
		require.Equal(t, before, texts)

		client.retry.Store(true)
		retried, err := provider.Translate(t.Context(), "ja", "en", texts)
		require.NoError(t, err)
		require.NotNil(t, retried)
		want := make([]string, len(before))
		for i, text := range before {
			want[i] = "translated: " + text
		}
		require.Equal(t, want, retried.Texts, "retry preserves input order despite fanout")
		require.Equal(t, before, texts)
		require.Equal(t, int32(11), client.calls.Load())
		require.Zero(t, client.active.Load())
		require.Equal(t, int32(6), client.closed.Load(), "every successful response body is closed")
	})
}
