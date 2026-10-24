package r18devdump

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/require"
)

// In-process handler tests exercise staging without opening a listener.
type stagingDeadlineRecorder struct{ *httptest.ResponseRecorder }

func (*stagingDeadlineRecorder) SetReadDeadline(time.Time) error  { return nil }
func (*stagingDeadlineRecorder) SetWriteDeadline(time.Time) error { return nil }

func stageRequest(t *testing.T, h *dumpHandler, payload uploadReq) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(&stagingDeadlineRecorder{rec})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/upload", bytes.NewReader(payload.body))
	c.Request.Header.Set("Content-Type", payload.contentType)
	h.startUpload(c)
	return rec
}

func assertNoUploadStages(t *testing.T, dumpPath string) {
	t.Helper()
	stages, err := filepath.Glob(filepath.Join(filepath.Dir(dumpPath), ".r18dev-dump-upload-*"))
	require.NoError(t, err)
	require.Empty(t, stages, "request-owned upload stage must be removed")
}

func TestUploadStagePreservesExistingPaths(t *testing.T) {
	for _, occupant := range []string{"regular", "symlink", "hardlink", "directory"} {
		t.Run(occupant, func(t *testing.T) {
			h, path := newTestHandler(t)
			t.Cleanup(h.rt.Shutdown)
			buildTestDump(t, path, "118ipx00535", "IPX-535")
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			legacy := path + ".upload"
			want := []byte("unowned pre-existing stage")
			switch occupant {
			case "regular":
				require.NoError(t, os.WriteFile(legacy, want, 0o600))
			case "symlink":
				if err := os.Symlink(path, legacy); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("Windows symlink privilege unavailable: %v", err)
					}
					require.NoError(t, err)
				}
				want = original
			case "hardlink":
				require.NoError(t, os.Link(path, legacy))
				want = original
			case "directory":
				require.NoError(t, os.Mkdir(legacy, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(legacy, "keep"), want, 0o600))
			}
			rec := stageRequest(t, h, buildUploadBody(t, []byte("synthetic invalid upload"), "junk.db"))
			require.Equal(t, http.StatusAccepted, rec.Code)
			awaitFixtureDone(t, h)
			lastErr, kind, running := handlerState(h)
			require.NotEmpty(t, lastErr)
			require.Equal(t, kindValidation, kind)
			require.False(t, running)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, after, "invalid upload must preserve the working dump")
			keep := legacy
			if occupant == "directory" {
				keep = filepath.Join(legacy, "keep")
			}
			after, err = os.ReadFile(keep)
			require.NoError(t, err)
			require.Equal(t, want, after, "unowned legacy stage must remain untouched")
			assertNoUploadStages(t, path)
		})
	}
}

func TestUploadStagePreflightPreservesExistingPath(t *testing.T) {
	h, path := newTestHandler(t)
	t.Cleanup(h.rt.Shutdown)
	legacy := path + ".upload"
	want := []byte("unowned stage")
	require.NoError(t, os.WriteFile(legacy, want, 0o600))
	h.uploadMaxBytes = 1
	rec := stageRequest(t, h, buildUploadBody(t, []byte("oversized"), "junk.db"))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	got, err := os.ReadFile(legacy)
	require.NoError(t, err)
	require.Equal(t, want, got)
	assertNoUploadStages(t, path)
}

func TestUploadOwnedStageCleanup(t *testing.T) {
	for _, outcome := range []string{"raw success", "sidecar success", "rename failure", "copy failure", "mkdir failure", "create failure"} {
		t.Run(outcome, func(t *testing.T) {
			h, path := newTestHandler(t)
			t.Cleanup(h.rt.Shutdown)
			h.reloadFn = func(*config.Config, bool) error { return nil }
			payload := buildUploadBody(t, gzBytes(t, uploadOneRowDump), "synthetic.sql.gz")
			switch outcome {
			case "sidecar success", "rename failure":
				payload = buildUploadBody(t, validSidecarFixture(t, "synthetic"), "synthetic.db")
				if outcome == "rename failure" {
					h.renameFn = func(string, string) error { return errors.New("synthetic rename failure") }
				}
			case "copy failure":
				h.openPartFn = func(*multipart.FileHeader) (multipart.File, error) { return erroringFile{}, nil }
			case "mkdir failure":
				blocker := filepath.Join(t.TempDir(), "blocker")
				require.NoError(t, os.WriteFile(blocker, []byte("keep"), 0o600))
				path = filepath.Join(blocker, "dump.db")
				cfg := &config.Config{}
				cfg.Metadata.R18DevDump.Path = path
				h.rt.SetConfig(cfg)
			case "create failure":
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("requires POSIX directory permission enforcement")
				}
				require.NoError(t, os.Chmod(filepath.Dir(path), 0o500))
				t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o700) })
			}
			rec := stageRequest(t, h, payload)
			if outcome == "copy failure" || outcome == "mkdir failure" || outcome == "create failure" {
				require.Equal(t, http.StatusInternalServerError, rec.Code)
				require.Nil(t, h.done)
			} else {
				require.Equal(t, http.StatusAccepted, rec.Code)
				awaitFixtureDone(t, h)
				lastErr, kind, _ := handlerState(h)
				if outcome == "rename failure" {
					require.NotEmpty(t, lastErr)
					require.Equal(t, kindStaging, kind)
				} else {
					require.Empty(t, lastErr)
					require.FileExists(t, path)
				}
			}
			assertNoUploadStages(t, path)
			require.True(t, h.rt.WaitBackgroundTasks(time.Second))
		})
	}
}

func TestUploadOwnedStageShutdownCleanup(t *testing.T) {
	h, path := newTestHandler(t)
	src := &stageShutdownFile{Reader: bytes.NewReader([]byte("synthetic")), entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(src.release) }) }
	t.Cleanup(func() { unblock(); h.rt.Shutdown() })
	h.openPartFn = func(*multipart.FileHeader) (multipart.File, error) { return src, nil }
	response := make(chan *httptest.ResponseRecorder, 1)
	payload := buildUploadBody(t, []byte("synthetic"), "synthetic.db")
	go func() { response <- stageRequest(t, h, payload) }()
	select {
	case <-src.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("stage read did not start")
	}
	stages, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".r18dev-dump-upload-*"))
	require.NoError(t, err)
	require.Len(t, stages, 1)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(stages[0])
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	stopped := make(chan struct{})
	go func() { h.rt.Shutdown(); close(stopped) }()
	require.Eventually(t, func() bool { return h.rt.ServerCtx().Err() != nil }, time.Second, time.Millisecond)
	select {
	case <-stopped:
		t.Fatal("shutdown returned before staging I/O finished")
	default:
	}
	unblock()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join staging cleanup")
	}
	select {
	case rec := <-response:
		require.Equal(t, http.StatusInternalServerError, rec.Code)
		require.Contains(t, rec.Body.String(), "stage upload: context canceled")
	case <-time.After(5 * time.Second):
		t.Fatal("receive handler did not finish")
	}
	require.True(t, src.closed)
	require.Nil(t, h.done)
	require.False(t, h.running)
	require.NoFileExists(t, path)
	assertNoUploadStages(t, path)
}
