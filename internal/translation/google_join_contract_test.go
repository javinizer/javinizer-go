package translation

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
)

type orderedGoogleResponse struct {
	release chan struct{}
	status  int
}

type orderedGoogleClient struct {
	responses map[string]orderedGoogleResponse
	requests  chan *http.Request
	closed    atomic.Int32
}

func (c *orderedGoogleClient) Do(req *http.Request) (*http.Response, error) {
	response := c.responses[req.URL.Query().Get("q")]
	c.requests <- req
	return &http.Response{
		StatusCode: response.status, Header: make(http.Header), Request: req,
		Body: &bufferedGoogleBody{Reader: strings.NewReader(`[[["translated"]]]`), release: response.release, closed: &c.closed},
	}, nil
}

func TestGoogleFreeJoinsAllWorkersAfterProviderFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first, second, last := make(chan struct{}), make(chan struct{}), make(chan struct{})
		defer func() {
			for _, release := range []chan struct{}{first, second, last} {
				select {
				case <-release:
				default:
					close(release)
				}
			}
		}()
		client := &orderedGoogleClient{
			responses: map[string]orderedGoogleResponse{
				"first":  {first, http.StatusServiceUnavailable},
				"second": {second, http.StatusUnprocessableEntity},
				"last":   {last, http.StatusOK},
			}, requests: make(chan *http.Request, 3),
		}
		provider := NewGoogleProvider(Config{}, client)
		done := make(chan error, 1)
		go func() {
			result, err := provider.Translate(t.Context(), "ja", "en", []string{"last", "second", "first"})
			if result != nil {
				done <- nil
				return
			}
			done <- err
		}()
		synctest.Wait()
		require.Len(t, client.requests, 3)
		requests := []*http.Request{<-client.requests, <-client.requests, <-client.requests}
		close(first)
		synctest.Wait()
		require.Equal(t, int32(1), client.closed.Load())
		select {
		case <-done:
			t.Fatal("returned before joining the remaining responses")
		default:
		}
		for _, req := range requests {
			require.NoError(t, req.Context().Err())
		}
		close(second)
		synctest.Wait()
		require.Equal(t, int32(2), client.closed.Load())
		select {
		case <-done:
			t.Fatal("returned before joining the final response")
		default:
		}
		close(last)
		synctest.Wait()
		err := <-done
		var providerErr *translationError
		require.ErrorAs(t, err, &providerErr)
		require.Equal(t, TranslationErrorHTTPStatus, providerErr.Kind)
		require.Equal(t, http.StatusServiceUnavailable, providerErr.StatusCode)
		require.Equal(t, int32(3), client.closed.Load())
		for _, req := range requests {
			require.ErrorIs(t, req.Context().Err(), context.Canceled)
		}
	})
}

func TestGoogleFreeAllAdmittedBufferedResultsSurviveCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		client := &bufferedGoogleClient{release: make(chan struct{})}
		defer func() {
			select {
			case <-client.release:
			default:
				close(client.release)
			}
		}()
		provider := NewGoogleProvider(Config{}, client)
		type outcome struct {
			result *translationResult
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			result, err := provider.Translate(ctx, "ja", "en", []string{"one", "two"})
			done <- outcome{result, err}
		}()
		synctest.Wait()
		require.Equal(t, int32(2), client.calls.Load())
		cancel()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("returned before joining buffered results")
		default:
		}
		close(client.release)
		synctest.Wait()
		got := <-done
		require.NoError(t, got.err)
		require.NotNil(t, got.result)
		require.Equal(t, []string{"translated: one", "translated: two"}, got.result.Texts)
		require.Equal(t, int32(2), client.closed.Load())
	})
}
