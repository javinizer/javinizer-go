package r18devdump

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
	"net/textproto"
)

func TestClassifyImportError_Table(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		fsFault  bool
		gzFault  bool
		wantKind string
	}{
		{"fs fault wins first", fmt.Errorf("wrap: %w", r18devdump.ErrDumpNoRows), true, true, "staging"},
		{"zero rows is validation", fmt.Errorf("parse dump: %w", r18devdump.ErrDumpNoRows), false, false, "validation"},
		{"gzip-layer fault is validation", errors.New("unexpected EOF in stream"), false, true, "validation"},
		{"plain sqlite exec is import", errors.New("exec batch: constraint failed"), false, false, "import"},
		{"typed value is validation", fmt.Errorf("parse dump: %w", r18devdump.ErrDumpTypedValue), false, false, "validation"},
		{"swap failure is staging", fmt.Errorf("rename tmp db: %w: %w", r18devdump.ErrDumpSwap, errors.New("destination locked")), false, false, "staging"},
		{"truncated dump is validation", fmt.Errorf("parse dump: %w", r18devdump.ErrTruncatedDump), false, false, "validation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantKind, classifyImportError(tc.err, tc.fsFault, tc.gzFault))
		})
	}
}

type deadlineErrReader struct{ n int }

func (r *deadlineErrReader) Read(p []byte) (int, error) {
	if r.n > 0 {
		r.n--
		p[0] = 'x'
		return 1, nil
	}
	return 0, fmt.Errorf("read: %w", os.ErrDeadlineExceeded)
}

func (r *deadlineErrReader) Close() error { return nil }

func TestReceiveWrapper_Read_DeadlineClassification(t *testing.T) {
	t.Run("fresh deadline error claims serverTimedOut", func(t *testing.T) {
		w := newReceiveWrapper(&deadlineErrReader{n: 1}, nil, nil)
		buf := make([]byte, 4)
		n, err := w.Read(buf)
		assert.Equal(t, 1, n)
		assert.NoError(t, err)
		_, err = w.Read(buf)
		assert.ErrorIs(t, err, ErrReceiveTimeout)
		assert.Equal(t, dispServerTimeout, w.classify(fmt.Errorf("parse: %w", err)))
	})

	t.Run("deadline error after watchdog claim is stall", func(t *testing.T) {
		w := newReceiveWrapper(&deadlineErrReader{n: 0}, nil, nil)
		w.onStall(func() {}) // watchdog claims first
		buf := make([]byte, 4)
		_, err := w.Read(buf)
		assert.ErrorIs(t, err, ErrReceiveStalled)
		assert.Equal(t, dispStalled, w.classify(fmt.Errorf("parse: %w", err)))
	})

	t.Run("deadline passthrough in completed state", func(t *testing.T) {
		w := newReceiveWrapper(&deadlineErrReader{n: 0}, nil, nil)
		require.Equal(t, dispStaging, w.classify(nil)) // completed
		_ = w
	})

	t.Run("Close delegates", func(t *testing.T) {
		w := newReceiveWrapper(&deadlineErrReader{n: 0}, nil, nil)
		assert.NoError(t, w.Close())
	})

	t.Run("classify unreachable default", func(t *testing.T) {
		w := newReceiveWrapper(io.NopCloser(strings.NewReader("x")), nil, nil)
		w.state.Store(99)
		assert.Equal(t, dispServerTimeout, w.classify(errors.New("x")))
	})

	t.Run("onBytes nil watchdog nil safe", func(t *testing.T) {
		w := newReceiveWrapper(&deadlineErrReader{n: 2}, nil, nil)
		buf := make([]byte, 8)
		n, err := w.Read(buf)
		assert.Equal(t, 1, n)
		assert.NoError(t, err)
	})
}

func TestFirstErr(t *testing.T) {
	assert.NoError(t, firstErr())
	assert.NoError(t, firstErr(nil, nil))
	e1 := errors.New("first")
	e2 := errors.New("second")
	assert.Equal(t, e1, firstErr(nil, e1, e2))
	assert.Equal(t, e2, firstErr(nil, nil, e2))
}

func TestRawFilenameParam_BrokenDisposition(t *testing.T) {
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", "\x80\x81garbage")
	fh := &multipart.FileHeader{Header: hdr}
	assert.Equal(t, "", rawFilenameParam(fh))

	hdr2 := textproto.MIMEHeader{}
	hdr2.Set("Content-Disposition", `form-data; name="file"`) // no filename param
	fh2 := &multipart.FileHeader{Header: hdr2}
	assert.Equal(t, "", rawFilenameParam(fh2))

	hdr3 := textproto.MIMEHeader{}
	hdr3.Set("Content-Disposition", `form-data; name="file"; filename="ok.sql.gz"`)
	fh3 := &multipart.FileHeader{Header: hdr3}
	assert.Equal(t, "ok.sql.gz", rawFilenameParam(fh3))
}

func TestReceiveStallTimeout_Default(t *testing.T) {
	h := &dumpHandler{}
	assert.Equal(t, dumpStallTimeout, h.receiveStallTimeout())
	h2 := &dumpHandler{stallTimeout: time.Second}
	assert.Equal(t, time.Second, h2.receiveStallTimeout())
}

// Synthetic handler-level server-timeout 408: a deadline-capable writer whose
// body errors with os.ErrDeadlineExceeded before any watchdog exists.
type deadlineCapableRecorder struct {
	*httptest.ResponseRecorder
}

func (w *deadlineCapableRecorder) SetReadDeadline(time.Time) error  { return nil }
func (w *deadlineCapableRecorder) SetWriteDeadline(time.Time) error { return nil }

func TestUpload_Handler408ServerTimeout(t *testing.T) {
	h, _, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(&deadlineCapableRecorder{w})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/upload", &deadlineErrReader{n: 1})
	req.Header.Set("Content-Type", "multipart/form-data; boundary=zzz")
	req.ContentLength = -1
	c.Request = req

	h.startUpload(c)
	assert.Equal(t, http.StatusRequestTimeout, w.Code)
	assert.Contains(t, w.Body.String(), ErrReceiveTimeout.Error())
	assert.Equal(t, "close", w.Header().Get("Connection"))
	_, _, running := handlerState(h)
	assert.False(t, running)
}

func TestUpload_OpenPartError_500(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	h.openPartFn = func(*multipart.FileHeader) (multipart.File, error) {
		return nil, errors.New("spool vanished")
	}
	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Contains(t, body, "stage upload")
	_, _, running := handlerState(h)
	assert.False(t, running)
}

func TestUpload_OpenPartPanic_Recovered(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	seedStatusError(h, "import")
	h.openPartFn = func(*multipart.FileHeader) (multipart.File, error) {
		panic("injected panic")
	}
	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Contains(t, body, "internal error during receive")
	lastErr, kind, running := handlerState(h)
	assert.Equal(t, "previous failure", lastErr, "panic path must not touch status")
	assert.Equal(t, "import", kind)
	assert.False(t, running, "guard released after panic")
	// Guard works again after the panic.
	h.openPartFn = nil
	status, _, _ = srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
}

func TestUpload_RawDump_ReloadFailureAfterSwap_FileKeptAndReloadKind(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return errors.New("simulated reload failure") }
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "reload", kind)
	assert.Contains(t, lastErr, "reload")
	_, err := os.Stat(dumpPath)
	require.NoError(t, err, "installed dump stays on disk after a reload failure")
}

func TestUpload_RawDump_ImportFailure_RestoresHandleWarning(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	var calls int32
	h.reloadFn = func(_ *config.Config, _ bool) error {
		calls++
		return errors.New("restore also fails")
	}
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, "<html>nope</html>"), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	_, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind)
	assert.Greater(t, calls, int32(0), "failure path attempts a handle restore")
}

func TestRunUploadJob_StagedOpenFailure_Staging(t *testing.T) {
	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	h.mu.Lock()
	h.running = true
	h.done = make(chan struct{})
	done := h.done
	h.mu.Unlock()

	h.runUploadJob(context.Background(), dumpPath, dumpPath+".upload", provenanceCapture{}, done)
	lastErr, kind, running := handlerState(h)
	assert.Equal(t, "staging", kind)
	assert.Contains(t, lastErr, "staged")
	assert.False(t, running)
	_, err := os.Stat(dumpPath + ".upload")
	assert.True(t, os.IsNotExist(err))
}
