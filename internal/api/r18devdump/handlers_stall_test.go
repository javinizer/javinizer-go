package r18devdump

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: the dump download previously used a fixed 30-minute wall-clock
// deadline (context.WithTimeout), which aborted slow-but-healthy transfers
// mid-import and rolled back all progress. It was replaced with a stall
// watchdog that only cancels when no bytes arrive for stallTimeout.

func TestStartDownload_StallAborts(t *testing.T) {
	// Serve a valid gzip prefix (COPY header, no rows, no terminator), then
	// block without sending bytes. The watchdog must abort the download with a
	// clear stall error instead of hanging forever (and locking retries out
	// with 409s until server restart).
	serverBlocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		gw := gzip.NewWriter(w)
		_, _ = gw.Write([]byte("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n"))
		_ = gw.Flush()
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(serverBlocked)
		<-r.Context().Done()
	}))
	defer srv.Close()

	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	h.stallTimeout = 100 * time.Millisecond

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/dump.sql.gz"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	select {
	case <-serverBlocked:
	case <-time.After(5 * time.Second):
		t.Fatal("test server should have received the request")
	}

	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("stalled download must abort promptly via the watchdog")
	}

	h.mu.Lock()
	running := h.running
	lastErr := h.lastError
	h.mu.Unlock()
	assert.False(t, running, "handler must release the download lock so retries return 202, not 409")
	assert.Contains(t, lastErr, "download stalled (no data received for 100ms)")

	_, err := os.Stat(dumpPath)
	assert.True(t, os.IsNotExist(err), "stalled download must not leave a dump DB behind")
}

func TestStartDownload_SlowButSteadySucceeds(t *testing.T) {
	// Trickle a complete dump one line at a time, slower than the watchdog
	// tick but faster than the stall timeout, for a total duration well beyond
	// the stall timeout. The old fixed deadline would abort this; the watchdog
	// must not, because bytes keep arriving.
	var dumpBody strings.Builder
	dumpBody.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&dumpBody, "content%03d\tDVD-%03d\n", i, i)
	}
	dumpBody.WriteString("\\.\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		gw := gzip.NewWriter(w)
		flusher, _ := w.(http.Flusher)
		for _, line := range strings.SplitAfter(dumpBody.String(), "\n") {
			if line == "" {
				continue
			}
			_, _ = gw.Write([]byte(line))
			_ = gw.Flush()
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(60 * time.Millisecond)
		}
		_ = gw.Close()
	}))
	defer srv.Close()

	// 10 lines x 60ms ~= 600ms total, >2x the stall timeout: proves no
	// wall-clock cutoff, while 60ms gaps keep the watchdog fed. The stall
	// timeout stays comfortably above the pre-import local setup window
	// (schema creation etc.) even on heavily loaded Windows CI runners.
	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.httpClient = srv.Client()
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	h.stallTimeout = 250 * time.Millisecond

	orig := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/dump.sql.gz"
	defer func() { r18devdump.LatestDumpURL = orig }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)

	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code)

	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		t.Fatal("slow-but-steady download should complete")
	}

	h.mu.Lock()
	lastErr := h.lastError
	h.mu.Unlock()
	assert.Empty(t, lastErr, "slow-but-steady download must not be aborted: %s", lastErr)

	_, err := os.Stat(dumpPath)
	require.NoError(t, err, "dump DB should be imported after a slow-but-healthy transfer")
}
