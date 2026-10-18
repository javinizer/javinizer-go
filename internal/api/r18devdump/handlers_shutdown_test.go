package r18devdump

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

func startShutdownTestDownload(h *dumpHandler, ctx context.Context, update bool) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/dump", nil).WithContext(ctx)
	h.startDownloadOrUpdate(c, update)
	return w
}

func TestDumpShutdownCancelsAndJoinsAfterRequestEnds(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(map[bool]string{false: "download", true: "update"}[update], func(t *testing.T) {
			h, _ := newTestHandler(t)
			h.stallTimeout = time.Hour
			started := make(chan context.Context, 1)
			canceled := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(func() { unblock(); h.rt.Shutdown() })
			h.httpClient = &http.Client{Transport: probeTestTransport(func(req *http.Request) (*http.Response, error) {
				started <- req.Context()
				<-req.Context().Done()
				close(canceled)
				<-release // model transport/import cleanup after cancellation
				return nil, req.Context().Err()
			})}
			t.Setenv("JAVINIZER_R18DEV_DUMP_URL", "https://offline.invalid/dump.sql.gz")
			requestCtx, cancelRequest := context.WithCancel(context.Background())
			defer cancelRequest()
			require.Equal(t, http.StatusAccepted, startShutdownTestDownload(h, requestCtx, update).Code)
			var workerCtx context.Context
			select {
			case workerCtx = <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("worker not started")
			}
			cancelRequest()
			require.NoError(t, workerCtx.Err(), "202 worker must survive initiating request")
			_, deadline := workerCtx.Deadline()
			require.False(t, deadline, "healthy slow transfer has no wall clock deadline")
			require.False(t, h.rt.WaitBackgroundTasks(time.Millisecond))
			require.Equal(t, http.StatusConflict, startShutdownTestDownload(h, context.Background(), update).Code)
			shutdownDone := make(chan struct{})
			go func() { h.rt.Shutdown(); close(shutdownDone) }()
			select {
			case <-canceled:
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown failed to cancel transport")
			}
			select {
			case <-shutdownDone:
				t.Fatal("shutdown returned before worker cleanup")
			default:
			}
			unblock()
			select {
			case <-shutdownDone:
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown failed to join worker")
			}
			select {
			case <-h.done:
			default:
				t.Fatal("worker not joined")
			}
			require.True(t, h.rt.WaitBackgroundTasks(time.Second))
			h.mu.Lock()
			require.False(t, h.running)
			require.Contains(t, h.lastError, "context canceled")
			h.mu.Unlock()
			require.Equal(t, http.StatusServiceUnavailable, startShutdownTestDownload(h, context.Background(), update).Code)
		})
	}
}

func TestDumpShutdownRejectsBeforeLaunching(t *testing.T) {
	h, _ := newTestHandler(t)
	h.rt.Shutdown()
	h.httpClient = &http.Client{Transport: probeTestTransport(func(*http.Request) (*http.Response, error) {
		t.Error("closed runtime launched a download")
		return nil, errors.New("unexpected transport")
	})}
	require.Equal(t, http.StatusServiceUnavailable, startShutdownTestDownload(h, context.Background(), false).Code)
	require.Nil(t, h.done)
	require.False(t, h.running)
}

func TestDumpStallCancellationAllowsRetry(t *testing.T) {
	h, _ := newTestHandler(t)
	defer h.rt.Shutdown()
	h.stallTimeout = 10 * time.Millisecond
	t.Setenv("JAVINIZER_R18DEV_DUMP_URL", "https://offline.invalid/dump.sql.gz")
	h.httpClient = &http.Client{Transport: probeTestTransport(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	for range 2 {
		require.Equal(t, http.StatusAccepted, startShutdownTestDownload(h, context.Background(), false).Code)
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			t.Fatal("watchdog failed to cancel")
		}
		require.True(t, h.rt.WaitBackgroundTasks(time.Second))
		require.Contains(t, h.lastError, "download stalled")
		require.NoError(t, h.rt.ServerCtx().Err(), "stall only cancels this attempt")
	}
}

// This fixture is generated entirely in memory; no real dump or network is used.
func TestDumpShutdownDuringSyntheticImport(t *testing.T) {
	h, path := newTestHandler(t)
	h.stallTimeout = time.Hour
	t.Cleanup(h.rt.Shutdown)
	var prefix bytes.Buffer
	gz := gzip.NewWriter(&prefix)
	_, err := gz.Write([]byte("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n"))
	require.NoError(t, err)
	require.NoError(t, gz.Flush())
	h.httpClient = &http.Client{Transport: probeTestTransport(func(req *http.Request) (*http.Response, error) {
		body := &shutdownBody{Reader: bytes.NewReader(prefix.Bytes()), ctx: req.Context()}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(body), ContentLength: -1, Request: req}, nil
	})}
	t.Setenv("JAVINIZER_R18DEV_DUMP_URL", "https://offline.invalid/dump.sql.gz")
	require.Equal(t, http.StatusAccepted, startShutdownTestDownload(h, context.Background(), false).Code)
	require.Eventually(t, func() bool { _, err := os.Stat(path + ".tmp"); return err == nil }, 5*time.Second, time.Millisecond)
	h.rt.Shutdown()
	require.Contains(t, h.lastError, "context canceled")
	require.NoFileExists(t, path)
	for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
		require.NoFileExists(t, path+suffix)
	}
	select {
	case <-h.done:
	default:
		t.Fatal("import cleanup not joined")
	}
}

type shutdownBody struct {
	*bytes.Reader
	ctx context.Context
}

func (r *shutdownBody) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		<-r.ctx.Done()
		return 0, r.ctx.Err()
	}
	return n, err
}

func TestDumpSyntheticSuccessAndRetryControls(t *testing.T) {
	h, path := newTestHandler(t)
	defer h.rt.Shutdown()
	h.reloadFn = func(*config.Config, bool) error { return nil }
	payload := gzipped("COPY public.derived_video (content_id, dvd_id) FROM stdin;\nsynthetic001\tTEST-001\n\\.\n")
	h.httpClient = &http.Client{Transport: probeTestTransport(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload)), Request: req}, nil
	})}
	t.Setenv("JAVINIZER_R18DEV_DUMP_URL", "https://offline.invalid/dump.sql.gz")
	for range 2 {
		require.Equal(t, http.StatusAccepted, startShutdownTestDownload(h, context.Background(), false).Code)
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			t.Fatal("synthetic import did not finish")
		}
		require.Empty(t, h.lastError)
		require.FileExists(t, path)
		require.True(t, h.rt.WaitBackgroundTasks(time.Second))
	}
}
