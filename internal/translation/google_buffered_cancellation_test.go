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
	"time"

	"github.com/stretchr/testify/require"
)

type bufferedGoogleClient struct {
	release chan struct{}
	calls   atomic.Int32
	closed  atomic.Int32
}

type bufferedGoogleBody struct {
	io.Reader
	release <-chan struct{}
	closed  *atomic.Int32
}

func (b *bufferedGoogleBody) Read(p []byte) (int, error) {
	<-b.release
	return b.Reader.Read(p)
}

func (b *bufferedGoogleBody) Close() error {
	b.closed.Add(1)
	return nil
}

func (c *bufferedGoogleClient) Do(req *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	data, err := json.Marshal([][][]string{{{"translated: " + req.URL.Query().Get("q")}}})
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Request: req,
		Body: &bufferedGoogleBody{Reader: strings.NewReader(string(data)), release: c.release, closed: &c.closed},
	}, nil
}

func TestGoogleFreeCanceledAdmissionWithBufferedResponses(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				wantErr := context.Canceled
				if deadline {
					cancel()
					ctx, cancel = context.WithTimeout(t.Context(), time.Second)
					wantErr = context.DeadlineExceeded
				}
				defer cancel()
				client := &bufferedGoogleClient{release: make(chan struct{})}
				released := false
				defer func() {
					if !released {
						close(client.release)
					}
				}()
				provider := NewGoogleProvider(Config{}, client)
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
				synctest.Wait()
				require.Equal(t, int32(5), client.calls.Load())
				require.Zero(t, client.closed.Load())
				if deadline {
					time.Sleep(time.Second)
				} else {
					cancel()
				}
				synctest.Wait()
				require.ErrorIs(t, ctx.Err(), wantErr)
				select {
				case <-done:
					t.Fatal("translation returned before joining buffered response workers")
				default:
				}
				close(client.release)
				released = true
				synctest.Wait()
				got := <-done
				require.Nil(t, got.result)
				require.ErrorIs(t, got.err, wantErr)
				require.Equal(t, int32(5), client.calls.Load())
				require.Equal(t, int32(5), client.closed.Load())
				require.Equal(t, before, texts)
				retried, err := provider.Translate(t.Context(), "ja", "en", texts)
				require.NoError(t, err)
				want := make([]string, len(texts))
				for i, text := range texts {
					want[i] = "translated: " + text
				}
				require.Equal(t, want, retried.Texts)
				require.Equal(t, before, texts)
				require.Equal(t, int32(11), client.calls.Load())
				require.Equal(t, int32(11), client.closed.Load())
			})
		})
	}
}
