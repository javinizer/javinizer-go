package r18devdump

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// Shutdown joins accepted jobs, including after Fatal/Goexit in a test. Do not
// return on a cleanup timeout and let TempDir race a still-running worker. The
// test command's deadline remains the last-resort bound for a broken join.
func shutdownTestHandler(h *dumpHandler) {
	h.rt.Shutdown()
	if old := h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(nil); old != nil {
		_ = old.Close()
	}
}

const dumpTimeoutStackBytes = 32 << 10

// Snapshot before cancellation changes the outcome. Avoid payload/error text,
// filesystem reads, and blocking on a mutex which may itself explain a timeout.
func dumpTimeoutDiagnostics(h *dumpHandler) string {
	state := "handler state unavailable (mutex held)"
	if h.mu.TryLock() {
		doneClosed := false
		select {
		case <-h.done:
			doneClosed = true
		default:
		}
		kind := h.lastErrorKind
		if len(kind) > 64 {
			kind = kind[:64] + "..."
		}
		state = fmt.Sprintf("running=%t done_present=%t done_closed=%t error_kind=%q", h.running, h.done != nil, doneClosed, kind)
		h.mu.Unlock()
	}
	stack := make([]byte, dumpTimeoutStackBytes)
	n := runtime.Stack(stack, true)
	return fmt.Sprintf("%s server_canceled=%t\ngoroutine stacks (bounded to %d bytes, truncated=%t):\n%s",
		state, h.rt.ServerCtx().Err() != nil, len(stack), n == len(stack), stack[:n])
}

type harnessCleanupCloser struct{ closed chan struct{} }

func (c *harnessCleanupCloser) Close() error {
	close(c.closed)
	return nil
}

func TestHarnessShutdownJoinsBeforeClosingDump(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h, _ := newTestHandler(t)
		closer := &harnessCleanupCloser{closed: make(chan struct{})}
		h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(closer)
		taskDone := h.rt.TrackServerTask()
		require.NotNil(t, taskDone)
		canceled, release, exited := make(chan struct{}), make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(release) })
		t.Cleanup(unblock) // Release a synthetic gate before any failure-path join.
		go func() {
			defer taskDone()
			defer close(exited)
			<-h.rt.ServerCtx().Done()
			close(canceled)
			<-release
		}()
		stopped := make(chan struct{})
		go func() { shutdownTestHandler(h); close(stopped) }()
		synctest.Wait()
		select {
		case <-canceled:
		default:
			t.Fatal("harness shutdown did not cancel the accepted task")
		}
		select {
		case <-stopped:
			t.Fatal("harness shutdown returned before task cleanup")
		default:
		}
		select {
		case <-closer.closed:
			t.Fatal("harness closed the dump before task cleanup")
		default:
		}
		unblock()
		synctest.Wait()
		for name, ch := range map[string]<-chan struct{}{"task": exited, "shutdown": stopped, "dump closer": closer.closed} {
			select {
			case <-ch:
			default:
				t.Errorf("%s did not finish after release", name)
			}
		}
		shutdownTestHandler(h) // Repeated helper cleanup must not close twice.
	})
}

func TestHarnessEarlyReturnJoinsAcceptedUpload(t *testing.T) {
	for _, mode := range []string{"handler", "hub", "http1", "http2"} {
		t.Run(mode, func(t *testing.T) {
			var h *dumpHandler
			// Keep the stage directory alive after child cleanup so absence below
			// must come from the upload worker, not testing.T's RemoveAll.
			path := filepath.Join(t.TempDir(), "dump.db")
			t.Run("accepted worker left to cleanup", func(t *testing.T) {
				if mode == "handler" {
					h, _ = newTestHandler(t)
				} else {
					h, _, _ = newTestHandlerWithHub(t)
				}
				cfg := h.rt.Deps().CoreDeps.GetConfig().Clone()
				cfg.Metadata.R18DevDump.Path = path
				h.rt.SetConfig(cfg)
				started := make(chan struct{})
				h.broadcastProgressFn = func(phase string, _, _ int64, _ string) {
					if phase == "importing" {
						close(started)
						<-h.rt.ServerCtx().Done()
					}
				}
				payload := buildUploadBody(t, []byte("synthetic invalid upload"), "synthetic.db")
				var status int
				switch mode {
				case "http1":
					status, _, _ = newUploadTestServer(t, h).doUpload(t, payload)
				case "http2":
					status, _, _ = newUploadTestServerH2(t, h).doUpload(t, payload)
				default:
					status = stageRequest(t, h, payload).Code
				}
				require.Equal(t, http.StatusAccepted, status)
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("accepted worker did not reach the cleanup gate")
				}
				stages, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".r18dev-dump-upload-*"))
				require.NoError(t, err)
				require.Len(t, stages, 1, "accepted worker must own a stage before cleanup")
				// Deliberately omit awaitDone: registered cleanup owns this worker.
			})
			require.NotNil(t, h)
			require.ErrorIs(t, h.rt.ServerCtx().Err(), context.Canceled)
			select {
			case <-h.done:
			default:
				t.Fatal("child test returned before accepted upload cleanup")
			}
			require.True(t, h.rt.WaitBackgroundTasks(time.Second))
			require.False(t, h.running)
			require.DirExists(t, filepath.Dir(path))
			_, err := os.Stat(path)
			require.True(t, os.IsNotExist(err), "cleanup must leave no installed dump")
			assertNoUploadStages(t, path)
		})
	}
}

func TestHarnessTimeoutDiagnosticsAreBoundedAndNonBlocking(t *testing.T) {
	h, _ := newTestHandler(t)
	h.running = true
	h.done = make(chan struct{})
	h.lastError = "synthetic payload must not appear in diagnostics"
	h.lastErrorKind = kindImport
	diagnostic := dumpTimeoutDiagnostics(h)
	require.Contains(t, diagnostic, "running=true done_present=true done_closed=false")
	require.Contains(t, diagnostic, `error_kind="import"`)
	require.Contains(t, diagnostic, "server_canceled=false")
	require.Contains(t, diagnostic, "goroutine stacks (bounded to 32768 bytes")
	require.NotContains(t, diagnostic, h.lastError)
	require.Less(t, len(diagnostic), dumpTimeoutStackBytes+1024)

	h.mu.Lock()
	diagnostic = dumpTimeoutDiagnostics(h)
	h.mu.Unlock()
	require.Contains(t, diagnostic, "handler state unavailable (mutex held)")
	close(h.done)
	h.rt.Shutdown()
	diagnostic = dumpTimeoutDiagnostics(h)
	require.Contains(t, diagnostic, "done_closed=true")
	require.Contains(t, diagnostic, "server_canceled=true")
}
