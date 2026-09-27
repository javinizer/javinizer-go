package r18devdump

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

const (
	defaultUploadMaxBytes  int64 = 2 << 30  // 2 GiB
	defaultUploadMaxMemory int64 = 32 << 20 // 32 MiB multipart memory; larger parts disk-spool
)

// Asynchronous upload failure kinds, surfaced via last_error_kind.
const (
	kindValidation = "validation"
	kindImport     = "import"
	kindStaging    = "staging"
	kindReload     = "reload"
)

// startUpload godoc
// @Summary Upload the r18.dev dump manually
// @Description Accepts one uploaded file: a pre-built dump sidecar (.db, validated then swapped in) or the raw upstream gzipped pg_dump (.sql.gz, gunzipped and imported through the standard pipeline). The body is received and staged synchronously (deterministic 400/408/409/413/500 failures), then validation/import/install runs asynchronously with progress over the dump WebSocket channel. Returns 409 while any dump operation (download/update/upload/clear) is running.
// @Tags r18dev
// @Accept mpfd
// @Produce json
// @Param file formData file true "dump file (.db sidecar or .sql.gz raw dump)"
// @Success 202 {object} map[string]string
// @Failure 400 {object} map[string]string "malformed multipart envelope or no/multiple/empty file part (incl. empty filename)"
// @Failure 408 {object} map[string]string "receive stall or server read-timer timeout (connection disposed; retry on a fresh connection)"
// @Failure 409 {object} map[string]string
// @Failure 413 {object} map[string]string "body exceeds the upload size limit"
// @Failure 500 {object} map[string]string "deadline lift unsupported, staging failure, or panic"
// @Router /api/v1/r18dev/dump/upload [post]
func (h *dumpHandler) startUpload(c *gin.Context) {
	path := resolveDumpPath(h.rt.Deps().CoreDeps.GetConfig())
	staged := path + ".upload"

	if !h.tryAcquireDumpOp() {
		c.JSON(http.StatusConflict, gin.H{errorResponseKey: "another dump operation is already in progress"})
		return
	}
	handedOff := false
	defer func() {
		if handedOff {
			return
		}
		_ = os.Remove(staged)
		if rec := recover(); rec != nil {
			logging.Warnf("r18dev dump upload: panic during receive: %v", rec)
			h.releaseDumpOp()
			if !c.Writer.Written() {
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{errorResponseKey: "internal error during receive"})
			}
			return
		}
		h.releaseDumpOp()
	}()

	maxBytes := h.uploadMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultUploadMaxBytes
	}
	maxMemory := h.uploadMaxMemory
	if maxMemory <= 0 {
		maxMemory = defaultUploadMaxMemory
	}

	if c.Request.ContentLength > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{errorResponseKey: fmt.Sprintf("request body exceeds the %d-byte upload limit", maxBytes)})
		return
	}

	rc := http.NewResponseController(c.Writer)
	if err := rc.SetReadDeadline(time.Time{}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorResponseKey: "response writer does not support deadline control"})
		return
	}
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorResponseKey: "response writer does not support deadline control"})
		return
	}

	watchdog := newStallWatchdog(h.receiveStallTimeout())
	var bodyBytes atomic.Int64
	wrapped := newReceiveWrapper(
		http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes),
		watchdog,
		func(n int64) {
			bodyBytes.Add(n)
			h.broadcastProgress("downloading", bodyBytes.Load(), c.Request.ContentLength, "")
		},
	)
	c.Request.Body = wrapped
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go watchdog.run(runCtx, func() {
		wrapped.onStall(func() { _ = rc.SetReadDeadline(time.Now()) })
	})
	defer watchdog.Stop()

	parseErr := c.Request.ParseMultipartForm(maxMemory)
	if c.Request.MultipartForm != nil {
		defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	}

	switch wrapped.classify(parseErr) {
	case dispOversize:
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{errorResponseKey: fmt.Sprintf("request body exceeds the %d-byte upload limit", maxBytes)})
		return
	case dispStalled:
		c.Header("Connection", "close")
		c.JSON(http.StatusRequestTimeout, gin.H{errorResponseKey: ErrReceiveStalled.Error()})
		return
	case dispServerTimeout:
		c.Header("Connection", "close")
		c.JSON(http.StatusRequestTimeout, gin.H{errorResponseKey: ErrReceiveTimeout.Error()})
		return
	case dispEnvelope:
		c.JSON(http.StatusBadRequest, gin.H{errorResponseKey: "malformed multipart body: expected exactly one file part"})
		return
	case dispStaging:
	}

	watchdog.Stop()

	files := c.Request.MultipartForm.File["file"]
	if len(files) != 1 {
		c.JSON(http.StatusBadRequest, gin.H{errorResponseKey: "expected exactly one 'file' part"})
		return
	}
	fh := files[0]
	if fh.Size == 0 {
		c.JSON(http.StatusBadRequest, gin.H{errorResponseKey: "uploaded file is empty"})
		return
	}
	prov := captureProvenance(rawFilenameParam(fh))

	src, err := h.openPart(fh)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorResponseKey: fmt.Sprintf("stage upload: %v", err)})
		return
	}
	// The dump's parent dir may not exist on a fresh install (nothing else
	// creates it before the first upload).
	if err := os.MkdirAll(filepath.Dir(staged), 0o750); err != nil {
		_ = src.Close()
		c.JSON(http.StatusInternalServerError, gin.H{errorResponseKey: fmt.Sprintf("stage upload: %v", err)})
		return
	}
	dst, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		_ = src.Close()
		c.JSON(http.StatusInternalServerError, gin.H{errorResponseKey: fmt.Sprintf("stage upload: %v", err)})
		return
	}
	_, copyErr := io.Copy(dst, src)
	closeSrcErr := src.Close()
	closeDstErr := dst.Close()
	if err := firstErr(copyErr, closeSrcErr, closeDstErr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{errorResponseKey: fmt.Sprintf("stage upload: %v", err)})
		return
	}

	h.mu.Lock()
	h.lastError = ""
	h.lastErrorKind = ""
	h.done = make(chan struct{})
	done := h.done
	h.mu.Unlock()
	handedOff = true
	go h.runUploadJob(context.Background(), path, staged, prov, done)
	c.JSON(http.StatusAccepted, gin.H{"message": "upload staged; validation and install running"})
}

func (h *dumpHandler) openPart(fh *multipart.FileHeader) (multipart.File, error) {
	if h.openPartFn != nil {
		return h.openPartFn(fh)
	}
	return fh.Open()
}

func (h *dumpHandler) receiveStallTimeout() time.Duration {
	if h.stallTimeout > 0 {
		return h.stallTimeout
	}
	return dumpStallTimeout
}

// classifyImportError attributes an Import failure to exactly one layer:
// staged filesystem (staging), gzip-layer problems incl. truncation and the
// zero-rows invariant (validation), or genuine parser/SQLite errors (import).
func classifyImportError(importErr error, fsFault, gzFault bool) string {
	switch {
	case fsFault:
		return kindStaging
	case errors.Is(importErr, r18devdump.ErrDumpNoRows) || gzFault:
		return kindValidation
	default:
		return kindImport
	}
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func rawFilenameParam(fh *multipart.FileHeader) string {
	_, params, err := mime.ParseMediaType(fh.Header.Get("Content-Disposition"))
	if err != nil {
		return ""
	}
	return params["filename"]
}

// fsTagReader records non-EOF read failures of the staged file so job error
// classification can attribute them to the 'staging' (filesystem) layer even
// when they propagate through gzip untouched.
type fsTagReader struct {
	inner  io.Reader
	failed *atomic.Bool
}

func (r *fsTagReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		r.failed.Store(true)
	}
	return n, err
}

const sniffSize = 16

var sqliteMagic = []byte("SQLite format 3\x00")

// runUploadJob performs async payload detection, validation/import, swap, and
// hot-reload for an accepted (202) upload. It owns the staged file and the
// done channel; both are released on every exit path, under a detached
// context that outlives the 202 response.
func (h *dumpHandler) runUploadJob(ctx context.Context, path, staged string, prov provenanceCapture, done chan struct{}) {
	failKind := ""
	failErr := error(nil)
	succeeded := false
	defer func() {
		_ = os.Remove(staged)
		h.mu.Lock()
		h.running = false
		if succeeded {
			h.lastError = ""
			h.lastErrorKind = ""
		} else if failErr != nil {
			h.lastError = failErr.Error()
			h.lastErrorKind = failKind
		}
		h.mu.Unlock()
		close(done)
	}()

	h.broadcastProgress("importing", 0, 0, "")

	stagedFile, err := os.Open(staged)
	if err != nil {
		failKind, failErr = kindStaging, fmt.Errorf("open staged upload: %w", err)
		h.broadcastProgress(errorResponseKey, 0, 0, failErr.Error())
		return
	}
	defer func() { _ = stagedFile.Close() }()
	var fsFault atomic.Bool
	br := bufio.NewReader(&fsTagReader{inner: stagedFile, failed: &fsFault})
	head, _ := br.Peek(sniffSize)

	switch {
	case len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b:
		h.runRawDumpJob(ctx, br, path, prov, &fsFault, &failKind, &failErr, &succeeded)
	case len(head) >= sniffSize && string(head) == string(sqliteMagic):
		h.runSidecarJob(ctx, staged, path, &failKind, &failErr, &succeeded)
	default:
		failKind = kindValidation
		failErr = errors.New("uploaded file is neither a gzipped r18.dev dump nor a dump sidecar database")
		h.broadcastProgress(errorResponseKey, 0, 0, failErr.Error())
	}

	if succeeded {
		h.broadcastProgress("done", 0, 0, "")
	}
}

func (h *dumpHandler) runRawDumpJob(ctx context.Context, br *bufio.Reader, path string, prov provenanceCapture, fsFault *atomic.Bool, failKind *string, failErr *error, succeeded *bool) {
	gz, err := gzip.NewReader(br)
	if err != nil {
		*failKind = kindValidation
		*failErr = fmt.Errorf("invalid gzip stream: %w", err)
		h.broadcastProgress(errorResponseKey, 0, 0, (*failErr).Error())
		return
	}

	token, err := prov.token()
	if err != nil {
		*failKind = kindValidation
		*failErr = err
		h.broadcastProgress(errorResponseKey, 0, 0, err.Error())
		return
	}
	date := r18devdump.ParseFilenameSourceDate(token)

	gzFault := &atomic.Bool{}
	taggedGz := &fsTagReader{inner: gz, failed: gzFault}
	var unlockReload func()

	impRes, importErr := r18devdump.Import(ctx, taggedGz, path, r18devdump.ImportOptions{
		SourceURL:  token,
		SourceDate: date,
		BeforeSwap: func() error {
			h.dumpMu.Lock()
			unlockReload = h.rt.LockReload()
			old := h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(nil)
			if old != nil {
				_ = old.Close()
			}
			return nil
		},
		AfterSwap: func() {
			defer h.dumpMu.Unlock()
			defer unlockReload()
			if reloadErr := h.reloadDumpLocked(path); reloadErr != nil {
				logging.Warnf("r18dev dump upload: hot-swap failed after install: %v", reloadErr)
				*failKind = kindReload
				*failErr = fmt.Errorf("dump installed but registry reload failed: %v", reloadErr)
				h.broadcastProgress(errorResponseKey, 0, 0, (*failErr).Error())
			}
		},
	})
	_ = impRes
	if importErr != nil {
		*failKind = classifyImportError(importErr, fsFault.Load(), gzFault.Load())
		if *failKind == kindStaging {
			*failErr = fmt.Errorf("staged file read failed: %w", importErr)
		} else {
			*failErr = importErr
		}
		h.broadcastProgress(errorResponseKey, 0, 0, (*failErr).Error())
		// Import failure path restores the previous dump handle like download.
		if reloadErr := h.reloadDump(path); reloadErr != nil {
			logging.Warnf("r18dev dump upload: failed to restore handle after failed import: %v", reloadErr)
		}
		return
	}
	if *failErr != nil {
		// reload-after-rename failure: installed file kept, reported 'reload'.
		return
	}
	*succeeded = true
}

func (h *dumpHandler) runSidecarJob(ctx context.Context, staged, path string, failKind *string, failErr *error, succeeded *bool) {
	store, err := r18devdump.ValidateSidecar(ctx, staged)
	if err != nil {
		*failKind = kindValidation
		*failErr = err
		h.broadcastProgress(errorResponseKey, 0, 0, err.Error())
		return
	}
	// The validator store MUST be closed before the swap (Windows rename).
	// Validator handle MUST be closed before the swap (Windows rename); close
	// error semantics elsewhere in the codebase are best-effort too.
	_ = store.Close()

	h.dumpMu.Lock()
	unlockReload := h.rt.LockReload()
	old := h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(nil)
	if old != nil {
		_ = old.Close()
	}
	renameFn := h.renameFn
	if renameFn == nil {
		renameFn = os.Rename
	}
	renameErr := renameFn(staged, path)
	if renameErr != nil {
		// Original file intact; restore the previous handle.
		if reloadErr := h.reloadDumpLocked(path); reloadErr != nil {
			logging.Warnf("r18dev dump upload: failed to restore handle after failed rename: %v", reloadErr)
		}
		unlockReload()
		h.dumpMu.Unlock()
		*failKind = kindStaging
		*failErr = fmt.Errorf("install upload: %w", renameErr)
		h.broadcastProgress(errorResponseKey, 0, 0, (*failErr).Error())
		return
	}
	reloadErr := h.reloadDumpLocked(path)
	if unlockReload != nil {
		unlockReload()
	}
	h.dumpMu.Unlock()
	if reloadErr != nil {
		*failKind = kindReload
		*failErr = fmt.Errorf("dump installed but registry reload failed: %v", reloadErr)
		h.broadcastProgress(errorResponseKey, 0, 0, (*failErr).Error())
		return
	}
	*succeeded = true
}
