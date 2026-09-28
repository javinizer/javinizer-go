package r18devdump

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Seed the runtime's dump closer so swap paths take the old-handle-close
// branch, then run one raw upload and one sidecar upload.
func TestUpload_SwapClosesPreviousHandle(t *testing.T) {
	t.Run("raw import BeforeSwap", func(t *testing.T) {
		h, dumpPath, srv := newUploadHandler(t)
		prev := &fakeCloser{}
		h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(prev)

		status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
		require.Equal(t, http.StatusAccepted, status)
		awaitDone(t, h, 10*time.Second)
		lastErr, _, _ := handlerState(h)
		require.Empty(t, lastErr)
		assert.True(t, prev.closed, "swap must close the previous dump handle before rename")
		_ = dumpPath
	})

	t.Run("sidecar swap", func(t *testing.T) {
		h, _, srv := newUploadHandler(t)
		prev := &fakeCloser{}
		h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(prev)

		status, _, _ := srv.doUpload(t, buildUploadBody(t, validSidecarFixture(t, "https://example/x.sql.gz"), "new.db"))
		require.Equal(t, http.StatusAccepted, status)
		awaitDone(t, h, 10*time.Second)
		lastErr, _, _ := handlerState(h)
		require.Empty(t, lastErr)
		assert.True(t, prev.closed, "sidecar swap must close the previous dump handle")
	})
}

func TestUpload_Sidecar_RenameFailure_RestoreAlsoFails_LogsAndReports(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")
	h.renameFn = func(_, _ string) error { return errors.New("simulated rename failure") }
	calls := atomic.Int32{}
	h.reloadFn = func(_ *config.Config, _ bool) error {
		calls.Add(1)
		return errors.New("simulated restore failure")
	}

	status, _, _ := srv.doUpload(t, buildUploadBody(t, validSidecarFixture(t, "https://example/new.sql.gz"), "new.db"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "staging", kind)
	assert.Contains(t, lastErr, "rename")
	assert.Greater(t, calls.Load(), int32(0), "restore attempt ran even though it also failed")
}
