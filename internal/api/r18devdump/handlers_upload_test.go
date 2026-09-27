package r18devdump

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The upload receive path drives http.ResponseController deadline control,
// which httptest.ResponseRecorder cannot honor, so these tests always run
// startUpload through a real httptest server (HTTP/1; plus one HTTP/2 happy
// path).

const uploadOneRowDump = "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"

func gzBytes(t *testing.T, dump string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write([]byte(dump))
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

type uploadReq struct {
	body          []byte
	contentType   string
	contentLength int64
}

// buildUploadBody constructs a multipart body with the given RAW filename
// parameter — the raw Content-Disposition value the handler must inspect.
func buildUploadBody(t *testing.T, data []byte, rawFilename string) uploadReq {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, rawFilename))
	hdr.Set("Content-Type", "application/octet-stream")
	pw, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, err = pw.Write(data)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return uploadReq{body: buf.Bytes(), contentType: mw.FormDataContentType(), contentLength: int64(buf.Len())}
}

type uploadTestServer struct {
	srv    *httptest.Server
	client *http.Client
}

// newUploadTestServer serves startUpload/clearDump over a real listener so
// ResponseController deadline operations behave exactly as in production.
func newUploadTestServer(t *testing.T, h *dumpHandler) *uploadTestServer {
	t.Helper()
	router := gin.New()
	router.POST("/api/v1/r18dev/dump/upload", h.startUpload)
	router.DELETE("/api/v1/r18dev/dump", h.clearDump)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	return &uploadTestServer{srv: srv, client: srv.Client()}
}

// newUploadTestServerH2 serves the same routes over TLS HTTP/2.
func newUploadTestServerH2(t *testing.T, h *dumpHandler) *uploadTestServer {
	t.Helper()
	router := gin.New()
	router.POST("/api/v1/r18dev/dump/upload", h.startUpload)
	srv := httptest.NewUnstartedServer(router)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return &uploadTestServer{srv: srv, client: srv.Client()}
}

func (s *uploadTestServer) doUpload(t *testing.T, r uploadReq) (status int, body string, connHeader string) {
	t.Helper()
	var rdr io.Reader = bytes.NewReader(r.body)
	req, err := http.NewRequest(http.MethodPost, s.srv.URL+"/api/v1/r18dev/dump/upload", rdr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", r.contentType)
	req.ContentLength = r.contentLength
	if r.contentLength < 0 {
		req.TransferEncoding = []string{"chunked"}
	}
	resp, err := s.client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b), resp.Header.Get("Connection")
}

func (s *uploadTestServer) doClear(t *testing.T) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, s.srv.URL+"/api/v1/r18dev/dump", nil)
	require.NoError(t, err)
	resp, err := s.client.Do(req)
	require.NoError(t, err)
	return resp
}

func validSidecarFixture(t *testing.T, sourceURL string) []byte {
	t.Helper()
	dir := t.TempDir()
	src := dir + "/sidecar.db"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(uploadOneRowDump), src, r18devdump.ImportOptions{SourceURL: sourceURL, SourceDate: "2026-09-20"})
	require.NoError(t, err)
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	return data
}

func awaitDone(t *testing.T, h *dumpHandler, d time.Duration) {
	t.Helper()
	h.mu.Lock()
	done := h.done
	h.mu.Unlock()
	require.NotNil(t, done, "accepted operation should have a done channel")
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("operation did not finish in time")
	}
}

func handlerState(h *dumpHandler) (lastErr, kind string, running bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastError, h.lastErrorKind, h.running
}

func seedStatusError(h *dumpHandler, kind string) {
	h.mu.Lock()
	h.lastError = "previous failure"
	h.lastErrorKind = kind
	h.mu.Unlock()
}

func newUploadHandler(t *testing.T) (*dumpHandler, string, *uploadTestServer) {
	t.Helper()
	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	return h, dumpPath, newUploadTestServer(t, h)
}

// --- happy paths ---

func TestUpload_RawDump_HappyPath(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)

	lastErr, kind, running := handlerState(h)
	assert.Empty(t, lastErr)
	assert.Empty(t, kind)
	assert.False(t, running)

	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.RowCount)
	assert.Equal(t, "r18dotdev_dump_2026-09-20.sql.gz", stats.SourceURL)
	assert.Equal(t, "2026-09-20", stats.SourceDate)
}

func TestUpload_RawDump_HappyPath_HTTP2(t *testing.T) {
	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	srv := newUploadTestServerH2(t, h)

	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status, "h2 upload must not fail closed on deadline control: %s", body)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Empty(t, lastErr)
	assert.Empty(t, kind)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.Stats(context.Background())
	require.NoError(t, err)
}

func TestUpload_RawDump_ExtensionlessFilename(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "blob"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Empty(t, lastErr, "extension-less gzip must be detected by magic: %s", lastErr)
	assert.Empty(t, kind)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "blob", stats.SourceURL)
	assert.Empty(t, stats.SourceDate)
}

func TestUpload_Sidecar_HappyPath_PreservesEmbeddedProvenance(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	embedded := "https://example.invalid/dumps/r18dotdev_dump_2026-09-20.sql.gz"
	// URL-shaped multipart filename on a .db is harmless — sidecars ignore it.
	status, _, _ := srv.doUpload(t, buildUploadBody(t, validSidecarFixture(t, embedded), "https://x/output.db"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)

	lastErr, kind, _ := handlerState(h)
	assert.Empty(t, lastErr)
	assert.Empty(t, kind)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, stats.RowCount)
	assert.Equal(t, embedded, stats.SourceURL, "sidecar upload must preserve embedded dump_meta provenance, not the multipart filename")
}

// --- envelope failures ---

func TestUpload_EnvelopeFailures(t *testing.T) {
	mk := func(mods func(mw *multipart.Writer)) uploadReq {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		mods(mw)
		_ = mw.Close()
		return uploadReq{body: buf.Bytes(), contentType: mw.FormDataContentType(), contentLength: int64(buf.Len())}
	}

	cases := []struct {
		name string
		mk   func(mw *multipart.Writer)
	}{
		{"no file part", func(mw *multipart.Writer) {
			_ = mw.WriteField("other", "x")
		}},
		{"two file parts", func(mw *multipart.Writer) {
			for _, n := range []string{"a.gz", "b.gz"} {
				pw, _ := mw.CreateFormFile("file", n)
				_, _ = pw.Write([]byte("data"))
			}
		}},
		{"empty file part", func(mw *multipart.Writer) {
			pw, _ := mw.CreateFormFile("file", "empty.gz")
			_, _ = pw.Write([]byte{})
		}},
		{"wrong field name", func(mw *multipart.Writer) {
			pw, _ := mw.CreateFormFile("notfile", "dump.gz")
			_, _ = pw.Write([]byte("data"))
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, dumpPath, srv := newUploadHandler(t)
			seedStatusError(h, "import")
			status, _, _ := srv.doUpload(t, mk(tc.mk))
			assert.Equal(t, http.StatusBadRequest, status)
			lastErr, kind, running := handlerState(h)
			assert.Equal(t, "previous failure", lastErr, "sync failures must not touch status")
			assert.Equal(t, "import", kind)
			assert.False(t, running)
			_, err := os.Stat(dumpPath + ".upload")
			assert.True(t, os.IsNotExist(err), "nothing may be staged")
		})
	}

	t.Run("malformed boundary", func(t *testing.T) {
		_, _, srv := newUploadHandler(t)
		req := mk(func(mw *multipart.Writer) {
			pw, _ := mw.CreateFormFile("file", "x.gz")
			_, _ = pw.Write([]byte("data"))
		})
		req.contentType = strings.Replace(req.contentType, "boundary=", "boundary=broken-", 1)
		status, _, _ := srv.doUpload(t, req)
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("empty filename is envelope 400 not provenance", func(t *testing.T) {
		_, _, srv := newUploadHandler(t)
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", `form-data; name="file"`) // no filename ⇒ value part
		hdr.Set("Content-Type", "application/octet-stream")
		pw, _ := mw.CreatePart(hdr)
		_, _ = pw.Write([]byte("data"))
		_ = mw.Close()
		status, _, _ := srv.doUpload(t, uploadReq{body: buf.Bytes(), contentType: mw.FormDataContentType(), contentLength: int64(buf.Len())})
		assert.Equal(t, http.StatusBadRequest, status)
	})
}

// --- size cap ---

func TestUpload_DeclaredOversize_Preflight(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	h.uploadMaxBytes = 100
	seedStatusError(h, "import")

	status, _, _ := srv.doUpload(t, buildUploadBody(t, []byte(strings.Repeat("x", 5000)), "big.gz"))
	assert.Equal(t, http.StatusRequestEntityTooLarge, status)
	lastErr, _, running := handlerState(h)
	assert.Equal(t, "previous failure", lastErr)
	assert.False(t, running)
}

func TestUpload_ChunkedOverflow_MaxBytesReader(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	h.uploadMaxBytes = 100

	req := buildUploadBody(t, []byte(strings.Repeat("x", 5000)), "big.gz")
	req.contentLength = -1 // chunked / unknown length
	status, _, _ := srv.doUpload(t, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, status, "crossed cap must give 413 even with unknown length")
	_, _, running := handlerState(h)
	assert.False(t, running)
}

func TestUpload_ChunkedUnknownLength_Succeeds(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	req := buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz")
	req.contentLength = -1
	status, _, _ := srv.doUpload(t, req)
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.Stats(context.Background())
	require.NoError(t, err)
}

func TestUpload_SuppressedMaxBytesError_Still413(t *testing.T) {
	// Cap just under the exact body size: the read that completes the final
	// boundary also trips MaxBytesError — even if multipart would otherwise
	// return nil, the latched oversize flag must win with 413.
	h, _, srv := newUploadHandler(t)
	req := buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz")
	h.uploadMaxBytes = req.contentLength - 2
	status, _, _ := srv.doUpload(t, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, status)
}

// --- stall watchdog ---

func TestUpload_InboundStall_408ThenFreshRetrySucceeds(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	h.stallTimeout = 100 * time.Millisecond
	seedStatusError(h, "import")

	// Craft the multipart prefix manually and stall mid-part.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="slow.gz"`)
	hdr.Set("Content-Type", "application/octet-stream")
	pw, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, _ = pw.Write([]byte("partial"))
	prefix := buf.Bytes()

	pr, pipeW := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, srv.srv.URL+"/api/v1/r18dev/dump/upload", pr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}

	go func() {
		_, _ = pipeW.Write(prefix)
		time.Sleep(400 * time.Millisecond) // stall past the watchdog timeout, then resume
		_, _ = pipeW.Write([]byte("more"))
		_ = pipeW.Close()
	}()

	resp, err := srv.client.Do(req)
	require.NoError(t, err)
	bodyBytes, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusRequestTimeout, resp.StatusCode)
	assert.True(t, resp.Close, "the handler must dispose of the connection (Go parses Connection: close into resp.Close and strips the header)")
	assert.Contains(t, string(bodyBytes), ErrReceiveStalled.Error())

	lastErr, kind, running := handlerState(h)
	assert.Equal(t, "previous failure", lastErr, "408 receive failures must not touch status")
	assert.Equal(t, "import", kind)
	assert.False(t, running, "lock must be released after a 408")

	// Fresh retry on a NEW connection succeeds (fresh watchdog/lock).
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ = handlerState(h)
	assert.Empty(t, lastErr)
	assert.Empty(t, kind)
}

func TestUpload_SequentialUploadsAfterFailure(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, []byte("not a dump"), "junk.bin"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	_, kind1, _ := handlerState(h)
	assert.Equal(t, "validation", kind1)

	status, _, _ = srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status, "a failed upload must not wedge the guard")
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Empty(t, lastErr, "kind must be cleared by the following success")
	assert.Empty(t, kind)
}

// --- payload classification / failure kinds ---

func TestUpload_NeitherMagic_Validation(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, []byte("plain text payload that is neither gzip nor sqlite"), "mystery.bin"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind)
	assert.Contains(t, lastErr, "neither")
	_, err := os.Stat(dumpPath)
	assert.True(t, os.IsNotExist(err))
}

func TestUpload_OneByteFile_ValidationNotStaging(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, []byte("x"), "one.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	_, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind, "short format-detection reads are validation, never staging")
}

func TestUpload_GzipCarryingHTML_Validation(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, "<html><body>503</body></html>"), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind)
	assert.Contains(t, lastErr, "derived_video", lastErr)
}

func TestUpload_TruncatedGzip_Validation(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	full := gzBytes(t, uploadOneRowDump)
	trunc := full[:len(full)/2]
	status, _, _ := srv.doUpload(t, buildUploadBody(t, trunc, "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	_, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind, "gzip truncation classifies as validation, not import")
}

func TestUpload_InvalidSidecar_ValidationAndPreservesExisting(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")

	bad := validSidecarFixture(t, "https://example/x.sql.gz")
	bad = bad[:len(bad)/2]
	status, _, _ := srv.doUpload(t, buildUploadBody(t, bad, "dump.db"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	_, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind)
	_, statErr := os.Stat(dumpPath)
	require.NoError(t, statErr, "previous dump must remain installed")
}

func TestUpload_RawDump_URLFilenameRejectedValidation(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "https://x/dumps/r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind, "URL-shaped provenance rejected for gzip payloads")
	assert.Contains(t, lastErr, "URL")
}

func TestUpload_RawDump_TraversalFilenameReducedToBasename(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), `..\evil_r18dotdev_dump_2026-09-20.sql.gz`))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, _, _ := handlerState(h)
	assert.Empty(t, lastErr)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "evil_r18dotdev_dump_2026-09-20.sql.gz", stats.SourceURL)
	assert.Equal(t, "2026-09-20", stats.SourceDate)
}

func TestUpload_RawDump_MalformedDateInstallsWithEmptyDate(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-13-99.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Empty(t, lastErr, "malformed embedded date must not reject the upload")
	assert.Empty(t, kind)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.Empty(t, stats.SourceDate)
}

func TestUpload_SQLTruncatedCopy_Validation(t *testing.T) {
	// Codex: a dumped stream that ends before the COPY block's \. terminator
	// (gzip intact, SQL truncated) must be a validation failure — previously a
	// partial database could replace a complete one.
	h, _, srv := newUploadHandler(t)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n"), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind, "SQL-level truncation is a validation failure (content problem), not import")
	assert.Contains(t, lastErr, "truncated")
}

func TestUpload_TextRuntimeValue_Validation(t *testing.T) {
	// Codex: typed-value violations mid-stream are user-content problems
	// (validation), never import plumbing errors or a broken installed dump.
	h, dumpPath, srv := newUploadHandler(t)
	dump := "COPY public.derived_video (content_id, runtime_mins) FROM stdin;\n118ipx00535\tunknown\n\\.\n"
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, dump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "validation", kind)
	assert.Contains(t, lastErr, "runtime_mins")
	_, statErr := os.Stat(dumpPath)
	assert.True(t, os.IsNotExist(statErr))
}

// --- swap failure paths ---

func TestUpload_Sidecar_RenameFailure_RestoresPrevious(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")
	h.renameFn = func(_, _ string) error { return fmt.Errorf("simulated rename failure") }

	status, _, _ := srv.doUpload(t, buildUploadBody(t, validSidecarFixture(t, "https://example/new.sql.gz"), "new.db"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "staging", kind)
	assert.Contains(t, lastErr, "rename")
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.Stats(context.Background())
	require.NoError(t, err)
}

func TestUpload_Sidecar_ReloadFailureAfterRename_FileKept(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return fmt.Errorf("simulated reload failure") }

	status, _, _ := srv.doUpload(t, buildUploadBody(t, validSidecarFixture(t, "https://example/new.sql.gz"), "new.db"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "reload", kind)
	assert.Contains(t, lastErr, "reload")
	_, err := os.Stat(dumpPath)
	require.NoError(t, err, "a reload failure must never delete the installed (valid) dump")
}

// --- concurrency ---

func TestUpload_DuringOperation_Conflict409_AndStatusUntouched(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	seedStatusError(h, "import")
	require.True(t, h.tryAcquireDumpOp())
	defer h.releaseDumpOp()

	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusConflict, status)
	assert.Contains(t, body, "another dump operation")
	lastErr, kind, _ := handlerState(h)
	assert.Equal(t, "previous failure", lastErr)
	assert.Equal(t, "import", kind)
}

func TestClear_AcquiresGuard_UploadDuringClear409(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")

	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	h.reloadFn = func(_ *config.Config, _ bool) error {
		once.Do(func() { close(entered) })
		<-release
		return nil
	}

	clearDone := make(chan *http.Response, 1)
	go func() { clearDone <- srv.doClear(t) }()

	<-entered
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusConflict, status, "clear must hold the shared guard for its whole duration")

	close(release)
	resp := <-clearDone
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	status, _, _ = srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	assert.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
}

func TestClear_DuringUploadJob409(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")

	// Hold the job in progress via reloadFn gate.
	release := make(chan struct{})
	h.reloadFn = func(_ *config.Config, _ bool) error { <-release; return nil }
	status, _, _ := srv.doUpload(t, buildUploadBody(t, validSidecarFixture(t, "https://example/x.sql.gz"), "new.db"))
	require.Equal(t, http.StatusAccepted, status)

	// Poll until job is running (reloadFn may not have fired yet; the guard is
	// held by the job regardless).
	require.Eventually(t, func() bool {
		_, _, running := handlerState(h)
		return running
	}, 2*time.Second, 10*time.Millisecond)

	resp := srv.doClear(t)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusConflict, resp.StatusCode, "clear during an upload job must 409")

	close(release)
	awaitDone(t, h, 10*time.Second)
}

// --- status / progress semantics ---

func TestUpload_202ClearsStatusImmediately(t *testing.T) {
	h, _, srv := newUploadHandler(t)
	seedStatusError(h, "import")
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	lastErr, kind, _ := handlerState(h)
	assert.Empty(t, lastErr, "Transition 2 clears status immediately at 202")
	assert.Empty(t, kind)
	awaitDone(t, h, 10*time.Second)
}

func TestUpload_ProgressReflectsRequestBytesOnly(t *testing.T) {
	h, _, srv := newUploadHandler(t)

	var mu sync.Mutex
	type frame struct {
		phase string
		bytes int64
		total int64
	}
	var frames []frame
	h.broadcastProgressFn = func(phase string, b, total int64, _ string) {
		mu.Lock()
		frames = append(frames, frame{phase, b, total})
		mu.Unlock()
	}

	data := gzBytes(t, uploadOneRowDump)
	req := buildUploadBody(t, data, "r18dotdev_dump_2026-09-20.sql.gz")
	status, _, _ := srv.doUpload(t, req)
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, frames)
	var sawDownloading, sawImporting, sawDone bool
	for _, f := range frames {
		switch f.phase {
		case "downloading":
			sawDownloading = true
			assert.LessOrEqual(t, f.bytes, req.contentLength, "progress is request bytes only — staging reads must not inflate it")
			assert.Equal(t, req.contentLength, f.total)
		case "importing":
			sawImporting = true
		case "done":
			sawDone = true
		}
	}
	assert.True(t, sawDownloading)
	assert.True(t, sawImporting, "post-staging importing phase present")
	assert.True(t, sawDone, "terminal done frame present")
}

func TestUpload_UnsupportedDeadlineWriter_FailsClosed(t *testing.T) {
	// A bare gin test context wraps httptest.ResponseRecorder, which cannot
	// honor ResponseController deadlines — exactly the unsupported-writer case.
	h, _, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	seedStatusError(h, "import")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz")
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/upload", bytes.NewReader(req.body))
	c.Request.Header.Set("Content-Type", req.contentType)
	c.Request.ContentLength = req.contentLength

	h.startUpload(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code, "unsupported writer fails closed with 500 before receiving the body")
	assert.Contains(t, w.Body.String(), "deadline control")
	lastErr, kind, running := handlerState(h)
	assert.Equal(t, "previous failure", lastErr, "sync failures never touch status")
	assert.Equal(t, "import", kind)
	assert.False(t, running, "guard fully released")
}

// --- desktop deadline lift ---

func TestUpload_DesktopDeadlineServer_SlowBodySucceeds(t *testing.T) {
	// Mirrors the desktop server policy (30s read/write deadlines, scaled
	// down 100x): without the per-request lift this body would be cut off.
	h, dumpPath, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }

	router := gin.New()
	router.POST("/api/v1/r18dev/dump/upload", h.startUpload)
	srv := &http.Server{
		Handler:      router,
		ReadTimeout:  300 * time.Millisecond,
		WriteTimeout: 300 * time.Millisecond,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	data := buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz")
	pr, pw := io.Pipe()
	req, err := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+"/api/v1/r18dev/dump/upload", pr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", data.contentType)
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}

	// Trickle the body at 10% every 80ms => total ~800ms >> 300ms deadlines.
	go func() {
		chunk := len(data.body) / 10
		for i := 0; i < len(data.body); i += chunk {
			end := i + chunk
			if end > len(data.body) {
				end = len(data.body)
			}
			if _, err := pw.Write(data.body[i:end]); err != nil {
				return
			}
			time.Sleep(80 * time.Millisecond)
		}
		_ = pw.Close()
	}()

	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusAccepted, resp.StatusCode, "per-request deadline lift must let slow bodies past desktop server timeouts")
	awaitDone(t, h, 10*time.Second)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.Stats(context.Background())
	require.NoError(t, err)
}

func TestUpload_InboundStall_408_HTTP2(t *testing.T) {
	// Same stall semantics over HTTP/2: the expired deadline closes the body
	// pipe and the response is a GOAWAY'd-stall. Go strips Connection headers
	// on h2; assert the 408 + cleanup and a fresh-retry success.
	h, _, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	h.stallTimeout = 100 * time.Millisecond
	srv := newUploadTestServerH2(t, h)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="slow.gz"`)
	hdr.Set("Content-Type", "application/octet-stream")
	pw, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, _ = pw.Write([]byte("partial"))
	prefix := buf.Bytes()

	pr, pipeW := io.Pipe()
	defer func() { _ = pr.Close() }()
	req, err := http.NewRequest(http.MethodPost, srv.srv.URL+"/api/v1/r18dev/dump/upload", pr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.ContentLength = -1

	go func() {
		_, _ = pipeW.Write(prefix)
		time.Sleep(400 * time.Millisecond)
		_, _ = pipeW.Write([]byte("more"))
		_ = pipeW.Close()
	}()

	resp, err := srv.client.Do(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusRequestTimeout, resp.StatusCode)
	assert.Contains(t, string(body), ErrReceiveStalled.Error())
	_, _, running := handlerState(h)
	assert.False(t, running)
}

func TestUpload_UnobservedServerTimer_Completes202(t *testing.T) {
	// Buffered completion: body fully delivered in one shot with a minuscule
	// stall timeout still armed — if the watchdog ticks after the final
	// buffered read but before Stop, the CAS must prefer the completed parse.
	h, dumpPath, srv := newUploadHandler(t)
	h.stallTimeout = time.Hour // armed but never fires; guards against false stalls
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.Stats(context.Background())
	require.NoError(t, err)
}

func TestUpload_StagingCreatesMissingParentDir(t *testing.T) {
	// Fresh-install layout: data/r18dev/ may not exist when the first upload
	// arrives (nothing else creates it outside Import).
	h, _, _ := newTestHandlerWithHub(t)
	h.reloadFn = func(_ *config.Config, _ bool) error { return nil }
	nested := t.TempDir() + "/data/r18dev/r18dev_dump.db"
	cfg := &config.Config{}
	cfg.Metadata.R18DevDump.Path = nested
	cfg.Metadata.R18DevDump.Enabled = true
	h.rt.SetConfig(cfg)
	srv := newUploadTestServer(t, h)

	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status, body)
	awaitDone(t, h, 10*time.Second)
	lastErr, _, _ := handlerState(h)
	require.Empty(t, lastErr)
	store, err := r18devdump.Open(nested)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	_, err = store.Stats(context.Background())
	require.NoError(t, err)
}

func TestUpload_ProgressThrottledUnderFastLAN(t *testing.T) {
	// A fast body lands entirely inside one throttle window: without the
	// throttle this would emit one WebSocket frame per body read (~60+ for a
	// 2 MB part), overflowing the hub's 256-deep client queues and dropping
	// listeners mid-upload.
	h, _, srv := newUploadHandler(t)
	var mu sync.Mutex
	var frames []string
	h.broadcastProgressFn = func(phase string, _, _ int64, _ string) {
		mu.Lock()
		frames = append(frames, phase)
		mu.Unlock()
	}

	big := bytes.Repeat([]byte("row"), 700000) // ~2.1 MB part
	status, _, _ := srv.doUpload(t, buildUploadBody(t, big, "r18dotdev_dump_2026-09-20.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)

	mu.Lock()
	defer mu.Unlock()
	downloading := 0
	for _, f := range frames {
		if f == "downloading" {
			downloading++
		}
	}
	assert.LessOrEqual(t, downloading, 3, "downloading progress must be throttled, got %d frames for one fast body", downloading)
}

func TestReceiveWrapper_ClassificationTable(t *testing.T) {
	mkWrapper := func() *receiveWrapper {
		return newReceiveWrapper(io.NopCloser(strings.NewReader("xx")), nil, nil)
	}

	t.Run("oversize latched beats everything", func(t *testing.T) {
		w := mkWrapper()
		w.oversize.Store(true)
		assert.Equal(t, dispOversize, w.classify(nil))
	})

	t.Run("abort claim wins over parse-nil ⇒ stall", func(t *testing.T) {
		w := mkWrapper()
		w.onStall(func() {})
		assert.Equal(t, dispStalled, w.classify(nil))
	})

	t.Run("completed wins parse-nil ⇒ staging", func(t *testing.T) {
		w := mkWrapper()
		assert.Equal(t, dispStaging, w.classify(nil))
	})

	t.Run("completed wins parse error ⇒ envelope", func(t *testing.T) {
		w := mkWrapper()
		assert.Equal(t, dispEnvelope, w.classify(fmt.Errorf("multipart: NextPart: malformed")))
	})

	t.Run("server timer terminal", func(t *testing.T) {
		w := mkWrapper()
		w.state.Store(int32(recvServerTimedOut))
		assert.Equal(t, dispServerTimeout, w.classify(fmt.Errorf("unrelated parse error")))
	})
}

func TestProvenanceToken_Rules(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		err  bool
	}{
		{"canonical", "r18dotdev_dump_2026-09-20.sql.gz", "r18dotdev_dump_2026-09-20.sql.gz", false},
		{"traversal backslash", `..\evil.sql.gz`, "evil.sql.gz", false},
		{"traversal slash", "/abs/path.sql.gz", "path.sql.gz", false},
		{"url rejected", "https://x/dumps/y.sql.gz", "", true},
		{"separator only", `\\`, "", true},
		{"relative dotdot", "..", "", true},
		{"single slash", "/", "", true},
		{"empty", "", "", true},
		{"unicode ok", "データ.sql.gz", "データ.sql.gz", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := captureProvenance(tc.raw).token()
			if tc.err {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("tooLong rejected", func(t *testing.T) {
		_, err := captureProvenance(strings.Repeat("a", 300)).token()
		assert.Error(t, err)
	})

	t.Run("control chars replaced", func(t *testing.T) {
		tok, err := captureProvenance("ok\x00name.sql.gz").token()
		require.NoError(t, err)
		assert.Equal(t, "ok.name.sql.gz", tok)
	})
}

func TestUpload_DownloadBuiltSidecar_PreservedThroughUpload(t *testing.T) {
	h, dumpPath, srv := newUploadHandler(t)
	testDumpSrv := newDumpTestServer(t)
	defer testDumpSrv.Close()
	embedded := testDumpSrv.URL + "/dumps/r18dotdev_dump_2026-04-28.sql.gz"

	status, _, _ := srv.doUpload(t, buildUploadBody(t, validSidecarFixture(t, embedded), "copied.db"))
	require.Equal(t, http.StatusAccepted, status)
	awaitDone(t, h, 10*time.Second)
	lastErr, _, _ := handlerState(h)
	require.Empty(t, lastErr)

	store, err := r18devdump.Open(dumpPath)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	stats, err := store.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, embedded, stats.SourceURL)
}
