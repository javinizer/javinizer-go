package r18devdump

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	dumpstore "github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/require"
)

// A complete chunked gzip response is healthy. A mirror can serve that GET
// while not answering Range requests. An optional EOF probe must not cancel
// the already-complete SQL import through its stall watchdog.
func TestStartDownload_CompleteChunkedDumpSurvivesStalledEOFProbe(t *testing.T) {
	rangeSeen := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			once.Do(func() { close(rangeSeen) })
			<-r.Context().Done()
			return
		}
		w.Header().Set("ETag", `"stable-complete-object"`)
		w.Header().Set("Content-Type", "application/gzip")
		w.(http.Flusher).Flush() // force unknown Content-Length
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"))
		_ = gz.Close()
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	h, path := newTestHandler(t)
	// Shorter than the probe header budget: this also proves the real handler
	// pauses payload accounting while optional headers are pending.
	h.stallTimeout = time.Second
	h.httpClient = srv.Client()
	h.reloadFn = func(*config.Config, bool) error { return nil }
	t.Setenv("JAVINIZER_R18DEV_DUMP_URL", srv.URL+"/complete.sql.gz")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/download", nil)
	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)
	select {
	case <-h.done:
	case <-time.After(20 * time.Second):
		t.Fatal("download did not finish")
	}
	h.mu.Lock()
	lastError := h.lastError
	h.mu.Unlock()
	require.Empty(t, lastError, "a complete valid dump should survive an optional EOF probe failure")
	require.FileExists(t, path)
	store, err := dumpstore.Open(path)
	require.NoError(t, err)
	defer store.Close()
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.RowCount)
}

type probeTestTransport func(*http.Request) (*http.Response, error)

func (f probeTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type delayedProbeEOF struct {
	io.Reader
	delay   time.Duration
	delayed bool
}

func (r *delayedProbeEOF) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n == 0 && err == io.EOF && !r.delayed {
		r.delayed = true
		time.Sleep(r.delay)
	}
	return n, err
}
func (*delayedProbeEOF) Close() error { return nil }

type stalledProbeBody struct{ ctx context.Context }

func (r *stalledProbeBody) Read([]byte) (int, error) { <-r.ctx.Done(); return 0, r.ctx.Err() }
func (*stalledProbeBody) Close() error               { return nil }

// Even when a clean HTTP EOF consumes nearly all of the payload idle budget,
// optional header probing must use its own budget. A resumed payload still
// consumes the original remaining idle time, with no fabricated progress.
func TestEOFProbe_PreservesNearlySpentPayloadBudget(t *testing.T) {
	var payload bytes.Buffer
	gz := gzip.NewWriter(&payload)
	_, err := gz.Write([]byte("complete member\n"))
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	for _, adoptBody := range []bool{false, true} {
		name := "stalled optional headers"
		if adoptBody {
			name = "stalled adopted payload"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				watchdog := newStallWatchdog(5 * time.Second)
				defer watchdog.Stop()
				go watchdog.run(ctx, cancel)
				probes := 0
				client := &http.Client{Transport: probeTestTransport(func(req *http.Request) (*http.Response, error) {
					if req.Header.Get("Range") == "" {
						return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Request: req,
							Header: http.Header{"Etag": {`"stable-v1"`}},
							Body:   &delayedProbeEOF{Reader: bytes.NewReader(payload.Bytes()), delay: 4 * time.Second}}, nil
					}
					probes++
					if !adoptBody {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					size := payload.Len()
					return &http.Response{StatusCode: http.StatusPartialContent, Request: req,
						Header: http.Header{"Content-Range": {fmt.Sprintf("bytes %d-%d/%d", size, size+19, size+20)}},
						Body:   &stalledProbeBody{ctx: req.Context()}}, nil
				})}
				started := time.Now()
				var received []byte
				_, err := dumpstore.DownloadWithOptions(ctx, client, "", func(int64, int64) { watchdog.Ping() }, func(r io.Reader, _ dumpstore.DownloadResult) error {
					r = &eofDetectReader{r: watchdog.reader(r), onEOF: watchdog.Stop}
					var readErr error
					received, readErr = io.ReadAll(r)
					return readErr
				}, dumpstore.DownloadOptions{OnEOFProbeStart: watchdog.Pause, OnEOFProbeDone: watchdog.Resume})
				require.Equal(t, 1, probes)
				if adoptBody {
					require.ErrorIs(t, err, context.Canceled)
					require.True(t, watchdog.Fired(), "adopted payload must remain subject to stall cancellation")
					require.Equal(t, 5*time.Second, time.Since(started), "probe must not reset the original idle budget")
				} else {
					require.NoError(t, err)
					require.NoError(t, ctx.Err())
					require.False(t, watchdog.Fired())
					require.Equal(t, "complete member\n", string(received))
					require.Greater(t, time.Since(started), 5*time.Second, "fixture must outlast the payload watchdog without resetting it")
				}
			})
		})
	}
}
