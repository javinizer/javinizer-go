package r18devdump

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearDumpResponse(h *dumpHandler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/r18dev/dump", nil)
	h.clearDump(c)
	return w
}

func readDumpStatus(t *testing.T, h *dumpHandler) dumpStatusResponse {
	t.Helper()
	w := dumpStatusRecorder(h)
	require.Equal(t, http.StatusOK, w.Code)
	var status dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	return status
}

func dumpStatusRecorder(h *dumpHandler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)
	h.getStatus(c)
	return w
}

func TestClearDump_FailurePreservesPersistedError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		setup  func(*testing.T, *dumpHandler, string)
	}{
		{"missing", http.StatusNotFound, func(_ *testing.T, _ *dumpHandler, _ string) {}},
		{"invalid", http.StatusBadRequest, func(t *testing.T, _ *dumpHandler, path string) {
			require.NoError(t, os.WriteFile(path, []byte("synthetic invalid dump"), 0o600))
		}},
		{"delete failure", http.StatusInternalServerError, func(t *testing.T, h *dumpHandler, path string) {
			buildTestDump(t, path, "118abf030", "ABF-030")
			h.removeFn = func(string) error { return fmt.Errorf("synthetic delete failure") }
		}},
		{"conflict", http.StatusConflict, func(t *testing.T, h *dumpHandler, path string) {
			buildTestDump(t, path, "118abf030", "ABF-030")
			require.True(t, h.tryAcquireDumpOp())
			t.Cleanup(h.releaseDumpOp)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, dumpPath := newTestHandler(t)
			h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
			tc.setup(t, h, dumpPath)
			seedStatusError(h, "import")

			cleared := clearDumpResponse(h)
			require.Equal(t, tc.status, cleared.Code)
			after := readDumpStatus(t, h)
			assert.Equal(t, "previous failure", after.LastError)
			assert.Equal(t, "import", after.LastErrorKind)
			assert.Equal(t, tc.status == http.StatusConflict, after.Running)
			if tc.status != http.StatusNotFound {
				assert.FileExists(t, dumpPath)
			}
		})
	}
}

func TestClearDump_SuccessStatusControls(t *testing.T) {
	for _, reloadFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("reload_failure=%t", reloadFails), func(t *testing.T) {
			h, dumpPath := newTestHandler(t)
			buildTestDump(t, dumpPath, "118abf030", "ABF-030")
			h.reloadFn = func(_ *config.Config, _ bool) error {
				if reloadFails {
					return fmt.Errorf("synthetic reload warning")
				}
				return nil
			}
			if reloadFails {
				seedStatusError(h, "reload")
			}

			// A successful clear without a prior error stays clean. A hot-swap
			// warning retains the existing successful-clear response contract.
			require.Equal(t, http.StatusOK, clearDumpResponse(h).Code)
			require.NoFileExists(t, dumpPath)
			status := readDumpStatus(t, h)
			assert.Empty(t, status.LastError)
			assert.Empty(t, status.LastErrorKind)
			assert.False(t, status.Running)
			assert.False(t, status.Present)

			// Repeated clear is a no-op failure and cannot revive the old error.
			require.Equal(t, http.StatusNotFound, clearDumpResponse(h).Code)
			status = readDumpStatus(t, h)
			assert.Empty(t, status.LastError)
			assert.Empty(t, status.LastErrorKind)
		})
	}
}

func TestClearDump_AfterUploadFailureAndNextFailure(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")
	badUpload := buildUploadBody(t, gzBytes(t, "synthetic invalid SQL"), "synthetic.sql.gz")
	status, _, _ := srv.doUpload(t, badUpload)
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 5*time.Second)
	before := readDumpStatus(t, h)
	require.True(t, before.Present)
	require.NotEmpty(t, before.LastError)
	require.Equal(t, "validation", before.LastErrorKind)

	require.Equal(t, http.StatusOK, clearDumpResponse(h).Code)
	after := readDumpStatus(t, h)
	assert.False(t, after.Present)
	assert.Empty(t, after.LastError)
	assert.Empty(t, after.LastErrorKind)

	status, _, _ = srv.doUpload(t, badUpload)
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 5*time.Second)
	after = readDumpStatus(t, h)
	assert.False(t, after.Present)
	assert.NotEmpty(t, after.LastError, "a new failure must still be visible after clear")
	assert.Equal(t, "validation", after.LastErrorKind)
}

func TestClearDump_ConcurrentStatusKeepsErrorPairConsistent(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")
	seedStatusError(h, "import")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	h.reloadFn = func(_ *config.Config, _ bool) error {
		close(entered)
		<-release
		return nil
	}
	clearDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { clearDone <- clearDumpResponse(h) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("clear did not reach reload")
	}

	// Until successful completion, polling still sees the previous outcome.
	during := readDumpStatus(t, h)
	require.True(t, during.Running)
	require.Equal(t, "previous failure", during.LastError)
	require.Equal(t, "import", during.LastErrorKind)
	require.Equal(t, http.StatusConflict, clearDumpResponse(h).Code)

	const readers = 8
	start := make(chan struct{})
	errs := make(chan error, readers)
	for range readers {
		go func() {
			<-start
			for range 100 {
				w := dumpStatusRecorder(h)
				var status dumpStatusResponse
				if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
					errs <- err
					return
				}
				oldPair := status.LastError == "previous failure" && status.LastErrorKind == "import"
				newPair := status.LastError == "" && status.LastErrorKind == ""
				if w.Code != http.StatusOK || (!oldPair && !newPair) {
					errs <- fmt.Errorf("inconsistent status: %d %s", w.Code, w.Body.String())
					return
				}
			}
			errs <- nil
		}()
	}
	close(start)
	unblock()
	select {
	case cleared := <-clearDone:
		require.Equal(t, http.StatusOK, cleared.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("clear did not finish")
	}
	for range readers {
		select {
		case err := <-errs:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("status reader did not finish")
		}
	}
	after := readDumpStatus(t, h)
	assert.False(t, after.Running)
	assert.False(t, after.Present)
	assert.Empty(t, after.LastError)
	assert.Empty(t, after.LastErrorKind)
}

func TestClearDump_ClearsPersistedError(t *testing.T) {
	for _, kind := range []string{"", "validation", "import", "staging", "reload"} {
		name := kind
		if name == "" {
			name = "download"
		}
		t.Run(name, func(t *testing.T) {
			h, dumpPath := newTestHandler(t)
			buildTestDump(t, dumpPath, "118abf030", "ABF-030")
			h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
			seedStatusError(h, kind)

			before := readDumpStatus(t, h)
			require.True(t, before.Present)
			require.Equal(t, "previous failure", before.LastError)
			require.Equal(t, kind, before.LastErrorKind)

			cleared := clearDumpResponse(h)
			require.Equal(t, http.StatusOK, cleared.Code)
			require.NoFileExists(t, dumpPath)

			// Reopening Settings loads a fresh status response from this handler.
			after := readDumpStatus(t, h)
			assert.False(t, after.Present)
			assert.False(t, after.Running)
			assert.Empty(t, after.LastError, "successful clear must dismiss the persisted failure")
			assert.Empty(t, after.LastErrorKind, "successful clear must dismiss its persisted kind")
		})
	}
}
