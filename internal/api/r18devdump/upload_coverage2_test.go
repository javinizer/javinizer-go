package r18devdump

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Writer where SetReadDeadline succeeds but SetWriteDeadline fails: covers the
// second fail-closed deadline-lift branch.
type writeDeadlineFailRecorder struct {
	*httptest.ResponseRecorder
}

func (w *writeDeadlineFailRecorder) SetReadDeadline(time.Time) error { return nil }
func (w *writeDeadlineFailRecorder) SetWriteDeadline(time.Time) error {
	return errors.New("unsupported")
}

func TestUpload_WriteDeadlineLiftUnsupported_500(t *testing.T) {
	h, _, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(&writeDeadlineFailRecorder{w})
	req := buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/upload", bytes.NewReader(req.body))
	c.Request.Header.Set("Content-Type", req.contentType)
	c.Request.ContentLength = req.contentLength
	h.startUpload(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	_, _, running := handlerState(h)
	assert.False(t, running)
}

// Staged-file create failure: dump dir read-only. POSIX-only by design: a
// read-only directory attribute does not block file creation on Windows. The
// Windows-covered variant is TestUpload_StagingMkdirBlockedByFile_500, which
// blocks the dump parent with a regular file (fails portably).
func TestUpload_StagingCreateFailure_500(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory attribute is not enforced on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not constrain the root user")
	}
	h, dumpPath, srv := newUploadHandler(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(dumpPath), 0o755))
	require.NoError(t, os.Chmod(filepath.Dir(dumpPath), 0o500))
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(dumpPath), 0o755) })

	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Contains(t, body, "stage upload")
	_, _, running := handlerState(h)
	assert.False(t, running)
}

// Parent path of the staged file is blocked by a regular file: MkdirAll
// must fail (can't mkdir over a file) and the response must be a staging 500.
func TestUpload_StagingMkdirBlockedByFile_500(t *testing.T) {
	h, _, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	blockerParent := t.TempDir()
	require.NoError(t, os.WriteFile(blockerParent+"/blocker", []byte("file"), 0o600))
	blocked := blockerParent + "/blocker/sub/r18dev_dump.db"
	cfg := &config.Config{}
	cfg.Metadata.R18DevDump.Path = blocked
	cfg.Metadata.R18DevDump.Enabled = true
	h.rt.SetConfig(cfg)
	srv := newUploadTestServer(t, h)

	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Contains(t, body, "stage upload")
	_, _, running := handlerState(h)
	assert.False(t, running)
}

// erroringFile is a multipart.File whose Copy/reads fail deterministically.
type erroringFile struct{}

func (erroringFile) Read([]byte) (int, error)          { return 0, errors.New("spool read error") }
func (erroringFile) ReadAt([]byte, int64) (int, error) { return 0, errors.New("spool read error") }
func (erroringFile) Seek(int64, int) (int64, error)    { return 0, nil }
func (erroringFile) Close() error                      { return nil }

func TestUpload_StagingCopyFailure_500(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	h.openPartFn = func(*multipart.FileHeader) (multipart.File, error) { return erroringFile{}, nil }
	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Contains(t, body, "stage upload")
}

// Gzip magic present but the stream itself is broken at the header.
func TestUpload_GzipHeaderBroken_Validation(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, []byte{0x1f, 0x8b, 0x00, 0x01, 0x02}, "broken.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind)
	assert.Contains(t, lastErr, "gzip")
}

// fsFault during import classifies as staging (direct job invocation).
type gzipThenFsFail struct {
	chunks [][]byte
	err    error
}

func (r *gzipThenFsFail) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, r.err
	}
	chunk := r.chunks[0]
	r.chunks = r.chunks[1:]
	return copy(p, chunk), nil
}

func TestRunRawDumpJob_FsFaultMidImport_Staging(t *testing.T) {
	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	// First two reads deliver a valid gzip header + partial deflate data, then
	// the filesystem faults mid-parse.
	full := gzBytes(t, uploadOneRowDump)
	fsFault := &atomic.Bool{}
	inner := &gzipThenFsFail{chunks: [][]byte{full[:len(full)/2]}, err: errors.New("disk read error")}
	br := bufio.NewReader(&fsTagReader{inner: inner, failed: fsFault})

	var kind, ferr string
	failErr := error(nil)
	succeeded := false
	kindP, errP := &kind, &failErr
	_ = ferr
	h.runRawDumpJob(context.Background(), br, dumpPath, provenanceCapture{raw: "r18dotdev_dump_2026-09-20.sql.gz"}, fsFault, kindP, errP, &succeeded)
	assert.False(t, succeeded)
	assert.Equal(t, "staging", kind)
	require.Error(t, failErr)
	assert.Contains(t, failErr.Error(), "staged file read failed")
	assert.True(t, fsFault.Load())
}

// completed-state deadline passthrough (third line of the deadline branch).
type lateDeadlineReader struct{ done bool }

func (r *lateDeadlineReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, fmt.Errorf("late: %w", os.ErrDeadlineExceeded)
	}
	r.done = true
	p[0] = 'x'
	return 1, nil
}
func (r *lateDeadlineReader) Close() error { return nil }

func TestReceiveWrapper_DeadlineAfterCompleted_Passthrough(t *testing.T) {
	w := newReceiveWrapper(&lateDeadlineReader{}, nil, nil)
	buf := make([]byte, 4)
	_, err := w.Read(buf)
	require.NoError(t, err)
	require.Equal(t, dispStaging, w.classify(nil)) // completed
	_, err = w.Read(buf)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrReceiveTimeout)
	assert.NotErrorIs(t, err, ErrReceiveStalled)
	assert.ErrorContains(t, err, "late")
}

func TestUpload_SidecarSwapSuccess_PathAndHandleActive(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, validSidecarFixture(t, "https://example/x.sql.gz"), "good.db"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.RowCount)
}
