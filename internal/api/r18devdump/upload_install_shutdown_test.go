package r18devdump

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

type installTestCloser struct{ calls atomic.Int32 }

func (c *installTestCloser) Close() error { c.calls.Add(1); return nil }

// Hold the real reload mutex until validation/import has finished and the
// tracked upload worker owns dumpMu. Shutdown must cancel and join that worker
// without allowing a later lock release to install its staged dump.
func TestUploadInstallLockWaitServerLifecycle(t *testing.T) {
	for _, format := range []string{"sidecar", "raw"} {
		for _, outcome := range []string{"healthy", "request canceled", "server shutdown"} {
			t.Run(format+"/"+outcome, func(t *testing.T) {
				h, path := newTestHandler(t)
				require.NoError(t, os.WriteFile(path, validSidecarFixture(t, "synthetic-old"), 0o600))
				before, err := os.ReadFile(path)
				require.NoError(t, err)
				old := &installTestCloser{}
				h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(old)
				var reloads, completed atomic.Int32
				h.reloadFn = func(*config.Config, bool) error { reloads.Add(1); return nil }
				h.broadcastProgressFn = func(phase string, _, _ int64, _ string) {
					if phase == "done" {
						completed.Add(1)
					}
				}
				unlock := h.rt.LockReload()
				var once sync.Once
				release := func() { once.Do(unlock) }
				t.Cleanup(func() { release(); h.rt.Shutdown() })
				payload := buildUploadBody(t, validSidecarFixture(t, "synthetic-new"), "synthetic.db")
				if format == "raw" {
					payload = buildUploadBody(t, gzBytes(t, uploadOneRowDump), "synthetic.sql.gz")
				}
				requestCtx, cancelRequest := context.WithCancel(t.Context())
				defer cancelRequest()
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(&stagingDeadlineRecorder{rec})
				c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/upload", bytes.NewReader(payload.body)).WithContext(requestCtx)
				c.Request.Header.Set("Content-Type", payload.contentType)
				h.startUpload(c)
				require.Equal(t, http.StatusAccepted, rec.Code)
				awaitFixtureReady(t, h, "import/validation to reach the install lock", func() bool {
					if h.dumpMu.TryLock() {
						h.dumpMu.Unlock()
						return false
					}
					return true
				})
				require.False(t, h.rt.WaitBackgroundTasks(time.Millisecond))
				require.Zero(t, old.calls.Load(), "the old handle stays attached while waiting")
				shutdownDone := make(chan struct{})
				switch outcome {
				case "request canceled":
					cancelRequest()
					require.NoError(t, h.rt.ServerCtx().Err(), "accepted worker has server lifetime")
				case "server shutdown":
					go func() { h.rt.Shutdown(); close(shutdownDone) }()
					require.Eventually(t, func() bool { return h.rt.ServerCtx().Err() != nil }, time.Second, time.Millisecond)
					select {
					case <-shutdownDone:
						t.Fatal("shutdown returned before worker and staging cleanup")
					default:
					}
				}
				release()
				if outcome == "server shutdown" {
					awaitDone(t, h, 5*time.Second)
				} else {
					awaitFixtureDone(t, h)
				}
				require.True(t, h.rt.WaitBackgroundTasks(time.Second))
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				lastErr, kind, running := handlerState(h)
				require.False(t, running)
				if outcome == "server shutdown" {
					select {
					case <-shutdownDone:
					case <-time.After(5 * time.Second):
						t.Fatal("shutdown failed to join upload cleanup")
					}
					require.True(t, bytes.Equal(before, after), "cancelled install must preserve working dump bytes")
					require.Zero(t, old.calls.Load(), "cancelled install must preserve the attached handle")
					require.Zero(t, reloads.Load())
					require.Zero(t, completed.Load())
					require.Contains(t, lastErr, context.Canceled.Error())
					require.Equal(t, map[string]string{"sidecar": kindStaging, "raw": kindImport}[format], kind)
				} else {
					require.False(t, bytes.Equal(before, after), "healthy install must replace the working dump")
					require.EqualValues(t, 1, old.calls.Load())
					require.EqualValues(t, 1, reloads.Load())
					require.EqualValues(t, 1, completed.Load())
					require.Empty(t, lastErr)
					require.Empty(t, kind)
				}
				assertNoUploadStages(t, path)
				for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
					require.NoFileExists(t, path+suffix)
				}
				require.True(t, h.dumpMu.TryLock(), "install must release dumpMu")
				h.dumpMu.Unlock()
				require.NoError(t, h.rt.WithReloadLock(func() error { return nil }))
			})
		}
	}
}

// Cancellation before detaching the working handle must not suppress the
// existing restore-on-import-error or installed-but-reload-failed behavior.
func TestUploadInstallFailureRecovery(t *testing.T) {
	for _, outcome := range []string{"import failure", "reload failure", "rename failure"} {
		t.Run(outcome, func(t *testing.T) {
			h, path := newTestHandler(t)
			t.Cleanup(h.rt.Shutdown)
			require.NoError(t, os.WriteFile(path, validSidecarFixture(t, "synthetic-old"), 0o600))
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			old := &installTestCloser{}
			h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(old)
			var reloads atomic.Int32
			h.reloadFn = func(*config.Config, bool) error {
				reloads.Add(1)
				return errors.New("synthetic reload failure")
			}
			payload := buildUploadBody(t, gzBytes(t, uploadOneRowDump), "synthetic.sql.gz")
			wantKind := kindReload
			switch outcome {
			case "import failure":
				payload = buildUploadBody(t, gzBytes(t, "<html>invalid dump</html>"), "synthetic.sql.gz")
				wantKind = kindValidation
			case "rename failure":
				payload = buildUploadBody(t, validSidecarFixture(t, "synthetic-new"), "synthetic.db")
				h.renameFn = func(string, string) error { return errors.New("synthetic rename failure") }
				wantKind = kindStaging
			}
			rec := stageRequest(t, h, payload)
			require.Equal(t, http.StatusAccepted, rec.Code)
			awaitFixtureDone(t, h)
			require.True(t, h.rt.WaitBackgroundTasks(time.Second))
			lastErr, kind, running := handlerState(h)
			require.NotEmpty(t, lastErr)
			require.Equal(t, wantKind, kind)
			require.False(t, running)
			require.EqualValues(t, 1, reloads.Load())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, outcome != "reload failure", bytes.Equal(before, after))
			if outcome == "import failure" {
				require.Zero(t, old.calls.Load())
			} else {
				require.EqualValues(t, 1, old.calls.Load())
			}
			assertNoUploadStages(t, path)
			for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
				require.NoFileExists(t, path+suffix)
			}
		})
	}
}
