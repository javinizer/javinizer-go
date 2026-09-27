package r18devdump

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func setLatestDumpURL(u string) {
	LatestDumpURL = u
}

// gzipped serves content as a gzip stream.
func gzipped(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(body)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func newDumpServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)

	mux := http.NewServeMux()
	mux.HandleFunc("/dumps/r18dotdev_dump_2026-04-28.sql.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	})
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dumps/r18dotdev_dump_2026-04-28.sql.gz", http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	return srv, srv.URL + "/latest"
}

func TestDownload_Import(t *testing.T) {
	srv, latest := newDumpServer(t)
	defer srv.Close()

	// Override the package endpoint to point at the test server.
	orig := LatestDumpURL
	setLatestDumpURL(latest)
	defer setLatestDumpURL(orig)

	var received strings.Builder
	gotURL := ""
	res, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		gotURL = d.FinalURL
		_, err := io.Copy(&received, r)
		return err
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.Unchanged {
		t.Error("expected a real download, not unchanged")
	}
	if res.SourceDate != "2026-04-28" {
		t.Errorf("SourceDate = %q, want 2026-04-28", res.SourceDate)
	}
	if !strings.Contains(gotURL, "2026-04-28") {
		t.Errorf("FinalURL = %q, want to contain date", gotURL)
	}
	if !strings.Contains(received.String(), "118ipx00535") {
		t.Errorf("importFn received decompressed body without expected row: %q", received.String())
	}
}

func TestDownload_UnchangedSkipsImport(t *testing.T) {
	srv, latest := newDumpServer(t)
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(latest)
	defer setLatestDumpURL(orig)

	// First download to discover the final (dated) URL.
	first, err := Download(context.Background(), srv.Client(), "", nil, func(io.Reader, DownloadResult) error { return nil })
	if err != nil {
		t.Fatalf("first Download: %v", err)
	}
	finalURL := first.FinalURL

	// Second download with currentSourceURL == finalURL must skip.
	importCalled := false
	res, err := Download(context.Background(), srv.Client(), finalURL, nil, func(io.Reader, DownloadResult) error {
		importCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("second Download: %v", err)
	}
	if !res.Unchanged {
		t.Error("expected Unchanged=true")
	}
	if importCalled {
		t.Error("importFn should not be called when unchanged")
	}
}

func TestDownload_ProgressReported(t *testing.T) {
	srv, latest := newDumpServer(t)
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(latest)
	defer setLatestDumpURL(orig)

	var lastProgress int64
	var lastTotal int64
	res, err := Download(context.Background(), srv.Client(), "", func(n, total int64) {
		lastProgress = n
		lastTotal = total
	}, func(r io.Reader, d DownloadResult) error {
		_, _ = io.Copy(io.Discard, r)
		return nil
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.Bytes <= 0 {
		t.Errorf("Bytes = %d, want > 0", res.Bytes)
	}
	if lastProgress <= 0 {
		t.Errorf("progress callback never reported bytes, last = %d", lastProgress)
	}
	if lastProgress != res.Bytes {
		t.Errorf("last progress %d != res.Bytes %d", lastProgress, res.Bytes)
	}
	if lastTotal != res.Bytes {
		t.Errorf("last total %d != res.Bytes %d (expected Content-Length to equal transferred bytes)", lastTotal, res.Bytes)
	}
}

func TestDownload_ProgressReported_ChunkedUnknownTotal(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)

	// Chunked transfer: flush before the body so Go omits Content-Length,
	// making resp.ContentLength == -1 (unknown). The download layer must
	// normalize this to 0 in the progress callback.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write(gz)
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)

	var lastTotal int64 = -1
	res, err := Download(context.Background(), srv.Client(), "", func(n, total int64) {
		lastTotal = total
	}, func(r io.Reader, d DownloadResult) error {
		_, _ = io.Copy(io.Discard, r)
		return nil
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.Bytes <= 0 {
		t.Errorf("Bytes = %d, want > 0", res.Bytes)
	}
	if lastTotal != 0 {
		t.Errorf("expected total=0 for chunked (unknown-length) response, got %d", lastTotal)
	}
}

func TestDownload_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)

	_, err := Download(context.Background(), srv.Client(), "", nil, func(io.Reader, DownloadResult) error { return nil })
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestDownload_InvalidGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not a gzip stream"))
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)

	_, err := Download(context.Background(), srv.Client(), "", nil, func(io.Reader, DownloadResult) error { return nil })
	if err == nil {
		t.Fatal("expected gunzip error for non-gzip body")
	}
}

// --- resumeReader coverage ---

// truncatingServer serves the gzip body with a declared Content-Length but
// writes only truncateAt bytes on the first (non-Range) request, then closes
// the connection — producing io.ErrUnexpectedEOF on the client. Range
// requests are honored with a proper 206 + Content-Range response.
func truncatingServer(t *testing.T, gz []byte, truncateAt int) (*httptest.Server, *atomic.Int64, *atomic.Bool, *atomic.Bool) {
	t.Helper()
	var rangeStart atomic.Int64
	var sawRange, sawIfRange atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"test-etag"`)
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			w.Header().Set("Content-Type", "application/sql")
			w.Header().Set("Content-Length", strconv.Itoa(len(gz)))
			_, _ = w.Write(gz[:truncateAt])
			return
		}
		sawRange.Store(true)
		if r.Header.Get("If-Range") == `"test-etag"` {
			sawIfRange.Store(true)
		}
		var off int64
		if _, err := fmt.Sscanf(rangeHdr, "bytes=%d-", &off); err != nil {
			t.Errorf("malformed Range header %q: %v", rangeHdr, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rangeStart.Store(off)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, len(gz)-1, len(gz)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(gz[off:])
	}))
	return srv, &rangeStart, &sawRange, &sawIfRange
}

func shrinkResumeBackoff(t *testing.T) {
	t.Helper()
	orig := resumeBackoffBase
	resumeBackoffBase = time.Millisecond
	t.Cleanup(func() { resumeBackoffBase = orig })
}

func TestDownload_ResumesAfterTruncation(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)
	truncateAt := len(gz) / 2

	srv, rangeStart, sawRange, sawIfRange := truncatingServer(t, gz, truncateAt)
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	var lastProgress, lastTotal int64
	var received bytes.Buffer
	_, err := Download(context.Background(), srv.Client(), "", func(n, total int64) {
		lastProgress, lastTotal = n, total
	}, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(&received, r)
		return err
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if received.String() != dumpBody {
		t.Errorf("received body mismatch:\ngot  %q\nwant %q", received.String(), dumpBody)
	}
	if !sawRange.Load() {
		t.Error("server never saw a Range request — truncation was not resumed")
	}
	if !sawIfRange.Load() {
		t.Error("resume request did not send If-Range with the original ETag")
	}
	if got := rangeStart.Load(); got != int64(truncateAt) {
		t.Errorf("Range resume started at byte %d, want %d", got, truncateAt)
	}
	if lastProgress != int64(len(gz)) || lastTotal != int64(len(gz)) {
		t.Errorf("final progress = %d/%d, want %d/%d", lastProgress, lastTotal, len(gz), len(gz))
	}
}

func TestDownload_ResumeFailsWhenRangeIgnored(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)
	truncateAt := len(gz) / 2

	// Truncate the first request; pretend to ignore Range afterwards (200
	// full-body), simulating a server/proxy that cannot resume.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"test-etag"`)
		w.Header().Set("Content-Type", "application/sql")
		w.Header().Set("Content-Length", strconv.Itoa(len(gz)))
		if r.Header.Get("Range") == "" {
			_, _ = w.Write(gz[:truncateAt])
			return
		}
		_, _ = w.Write(gz) // status 200 — Range ignored
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	_, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "dump stream interrupted") {
		t.Fatalf("expected a stream-interrupted error, got: %v", err)
	}
}

func TestDownload_ResumeFailsWhenObjectChanges(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)
	truncateAt := len(gz) / 2

	// Honor Range, but report a different total size on resume — the object
	// changed mid-download and the splice is unsafe.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"test-etag"`)
		w.Header().Set("Content-Type", "application/sql")
		rangeHdr := r.Header.Get("Range")
		if rangeHdr == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(gz)))
			_, _ = w.Write(gz[:truncateAt])
			return
		}
		var off int64
		_, _ = fmt.Sscanf(rangeHdr, "bytes=%d-", &off)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, len(gz), len(gz)+1))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(gz[off:])
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	_, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "dump object changed mid-download") {
		t.Fatalf("expected an object-changed error, got: %v", err)
	}
}

// errBody is an io.ReadCloser whose Read always fails with a truncation-class
// error, forcing resumeReader down the resume path on the first read.
type errBody struct{ err error }

func (b errBody) Read([]byte) (int, error) { return 0, b.err }
func (b errBody) Close() error             { return nil }

// rtFunc adapts a function to http.RoundTripper for resume tests.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newResumeReaderForTest(ctx context.Context, body io.ReadCloser, rt http.RoundTripper) *resumeReader {
	return &resumeReader{
		ctx:    ctx,
		client: &http.Client{Transport: rt},
		url:    "http://dump.invalid/dumps/x.sql.gz",
		etag:   `"test-etag"`, // tests override (including to "") as needed
		total:  1000,
		offset: 500,
		body:   body,
	}
}

// TestResumeReader_OpenRangeErrors drives every openRange validation failure:
// each truncating read ends in "dump stream interrupted" after the bounded
// resume retries, with the underlying cause preserved.
func TestResumeReader_OpenRangeErrors(t *testing.T) {
	shrinkResumeBackoff(t)
	cases := []struct {
		name    string
		rt      http.RoundTripper
		wantErr string
	}{
		{
			name: "request error",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial tcp: connection refused")
			}),
			wantErr: "connection refused",
		},
		{
			name: "server error status",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			}),
			wantErr: "resume request returned status 500",
		},
		{
			name: "malformed content-range",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				h := make(http.Header)
				return &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("")), Header: h}, nil
			}),
			wantErr: "malformed Content-Range",
		},
		{
			name: "offset mismatch",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				h := http.Header{"Content-Range": []string{"bytes 0-999/1000"}}
				return &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("")), Header: h}, nil
			}),
			wantErr: "Content-Range starts at byte 0, want 500",
		},
		{
			name: "total size changed",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				h := http.Header{"Content-Range": []string{"bytes 500-1000/1001"}}
				return &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("")), Header: h}, nil
			}),
			wantErr: "dump object changed mid-download",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newResumeReaderForTest(context.Background(), errBody{io.ErrUnexpectedEOF}, tc.rt)
			_, err := r.Read(make([]byte, 64))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), "dump stream interrupted at 500 of 1000 bytes") {
				t.Errorf("error missing progress label: %v", err)
			}
		})
	}
}

// TestResumeReader_BuildResumeRequestError covers the NewRequest failure path
// via a control character in the URL.
func TestResumeReader_BuildResumeRequestError(t *testing.T) {
	shrinkResumeBackoff(t)
	r := newResumeReaderForTest(context.Background(), errBody{io.ErrUnexpectedEOF}, rtFunc(func(*http.Request) (*http.Response, error) {
		t.Error("RoundTrip must not be called when the request cannot be built")
		return nil, errors.New("unexpected call")
	}))
	r.url = "http://example.com/\x7f"
	_, err := r.Read(make([]byte, 64))
	if err == nil || !strings.Contains(err.Error(), "build resume request") {
		t.Fatalf("got %v, want a build-resume-request error", err)
	}
}

// TestResumeReader_CancelBeforeResume covers fail-fast when the context is
// already canceled at the point truncation is detected.
func TestResumeReader_CancelBeforeResume(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := newResumeReaderForTest(ctx, errBody{io.ErrUnexpectedEOF}, rtFunc(func(*http.Request) (*http.Response, error) {
		t.Error("RoundTrip must not be called for a canceled context")
		return nil, errors.New("unexpected call")
	}))
	_, err := r.Read(make([]byte, 64))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v, want the original truncation error", err)
	}
}

// TestResumeReader_CancelDuringResumeBackoff cancels the context while the
// resume backoff sleep is in progress: resume aborts and the original
// truncation error is returned.
func TestResumeReader_CancelDuringResumeBackoff(t *testing.T) {
	orig := resumeBackoffBase
	resumeBackoffBase = 200 * time.Millisecond
	t.Cleanup(func() { resumeBackoffBase = orig })

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(10*time.Millisecond, cancel)
	r := newResumeReaderForTest(ctx, errBody{io.ErrUnexpectedEOF}, rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("should not reach RoundTrip once canceled")
	}))
	_, err := r.Read(make([]byte, 64))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v, want the original truncation error", err)
	}
}

// zeroThenDataReader returns a (0, nil) read once — which the consumer must
// retry, per the io.Reader contract — then data, then EOF.
type zeroThenDataReader struct {
	stage int
	data  []byte
}

func (z *zeroThenDataReader) Read(p []byte) (int, error) {
	switch z.stage {
	case 0:
		z.stage = 1
		return 0, nil
	case 1:
		z.stage = 2
		return copy(p, z.data), nil
	default:
		return 0, io.EOF
	}
}

func (z *zeroThenDataReader) Close() error { return nil }

func TestResumeReader_ZeroNilReadRetries(t *testing.T) {
	r := newResumeReaderForTest(context.Background(), &zeroThenDataReader{data: []byte("hello")}, nil)
	r.total = 5
	r.offset = 0
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil || n != 5 || string(buf[:n]) != "hello" {
		t.Fatalf("Read = (%d, %v, %q), want (5, nil, hello)", n, err, buf[:n])
	}
	if _, err := r.Read(buf); err != io.EOF {
		t.Fatalf("second Read = %v, want clean io.EOF at total", err)
	}
}

func TestResumeReader_Close(t *testing.T) {
	if err := (&resumeReader{}).Close(); err != nil {
		t.Errorf("Close with nil body: %v", err)
	}
	closed := false
	r := &resumeReader{body: closeTracker{closed: &closed}}
	if err := r.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if !closed {
		t.Error("Close did not close the underlying body")
	}
}

type closeTracker struct{ closed *bool }

func (c closeTracker) Read([]byte) (int, error) { return 0, io.EOF }
func (c closeTracker) Close() error             { *c.closed = true; return nil }

func TestResumeReader_ProgressLabel(t *testing.T) {
	known := &resumeReader{offset: 50, total: 100}
	if got := known.progressLabel(); got != "50 of 100 bytes" {
		t.Errorf("known-total label = %q", got)
	}
	unknown := &resumeReader{offset: 123, total: -1}
	if got := unknown.progressLabel(); got != "123 bytes" {
		t.Errorf("unknown-total label = %q", got)
	}
}

func TestResumeBackoffFor(t *testing.T) {
	origBase, origMax := resumeBackoffBase, resumeBackoffMax
	resumeBackoffBase, resumeBackoffMax = time.Second, 3*time.Second
	t.Cleanup(func() { resumeBackoffBase, resumeBackoffMax = origBase, origMax })
	cases := map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 3 * time.Second, 10: 3 * time.Second}
	for consecutive, want := range cases {
		if got := resumeBackoffFor(consecutive); got != want {
			t.Errorf("resumeBackoffFor(%d) = %v, want %v (capped)", consecutive, got, want)
		}
	}
}

func TestSleepContext(t *testing.T) {
	if !sleepContext(context.Background(), time.Millisecond) {
		t.Error("sleepContext returned false for a completed sleep")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepContext(ctx, time.Minute) {
		t.Error("sleepContext returned true for a canceled context")
	}
}

func TestDownload_ResumePrefersLastModifiedOverWeakETag(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)
	truncateAt := len(gz) / 2
	lastMod := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC).Format(http.TimeFormat)

	var gotIfRange atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sql")
		if r.Header.Get("Range") == "" {
			w.Header().Set("ETag", `W/"weak-v1"`)
			w.Header().Set("Last-Modified", lastMod)
			w.Header().Set("Content-Length", strconv.Itoa(len(gz)))
			_, _ = w.Write(gz[:truncateAt])
			return
		}
		gotIfRange.Store(r.Header.Get("If-Range"))
		var off int64
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &off)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, len(gz)-1, len(gz)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(gz[off:])
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	_, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	v, ok := gotIfRange.Load().(string)
	if !ok {
		t.Fatal("server never saw a Range request — truncation was not resumed")
	}
	if v != lastMod {
		t.Errorf("If-Range = %q, want Last-Modified %q (weak ETag must not be used)", v, lastMod)
	}
}

// --- unknown-length (chunked) EOF probing ---

// chunkedTruncatingServer streams gz[:writeUpTo] without a Content-Length
// (Flushing first forces chunked encoding, so the client sees length -1 and a
// premature connection close arrives as a CLEAN io.EOF), then serves Range
// requests via handleRange. Range requests and full-stream writes are
// coordinated per handler.
func TestDownload_UnknownLengthTruncationResumedByProbe(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)
	truncateAt := len(gz) / 2

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.Header().Set("ETag", `"test-etag"`)
			w.Header().Set("Content-Type", "application/sql")
			if f, ok := w.(http.Flusher); ok {
				f.Flush() // chunked: no Content-Length; clean EOF at connection end
			}
			_, _ = w.Write(gz[:truncateAt])
			return
		}
		// Probe reveals truncation: serve the remainder from the consumed offset.
		var off int64
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &off)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, len(gz)-1, len(gz)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(gz[off:])
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	var received bytes.Buffer
	_, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(&received, r)
		return err
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if received.String() != dumpBody {
		t.Errorf("received body mismatch:\ngot  %q\nwant %q", received.String(), dumpBody)
	}
}

func TestDownload_UnknownLengthGenuineEOFConfirmedBy416(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)

	var sawRange atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.Header().Set("ETag", `"test-etag"`)
			w.Header().Set("Content-Type", "application/sql")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = w.Write(gz) // full body, still unknown length on the client
			return
		}
		sawRange.Store(true)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(gz)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	var received bytes.Buffer
	_, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(&received, r)
		return err
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !sawRange.Load() {
		t.Error("expected an end-of-stream Range probe")
	}
	if received.String() != dumpBody {
		t.Errorf("received body mismatch:\ngot  %q\nwant %q", received.String(), dumpBody)
	}
}

func TestDownload_UnknownLengthTruncationVia416ThenResume(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)
	truncateAt := len(gz) / 2

	var rangeCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.Header().Set("ETag", `"test-etag"`)
			w.Header().Set("Content-Type", "application/sql")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = w.Write(gz[:truncateAt])
			return
		}
		call := rangeCalls.Add(1)
		if call == 1 {
			// EOF probe: reveal the true total, but return no body.
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(gz)))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		// Standard resume path: serve the remainder.
		var off int64
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &off)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", off, len(gz)-1, len(gz)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(gz[off:])
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	var received bytes.Buffer
	_, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(&received, r)
		return err
	})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if received.String() != dumpBody {
		t.Errorf("received body mismatch:\ngot  %q\nwant %q", received.String(), dumpBody)
	}
}

func TestDownload_UnknownLengthDeferToGzipWhenProbeInconclusive(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)
	truncateAt := len(gz) / 2

	// Chunked truncation, but Range is ignored (200 full body): the probe is
	// inconclusive, so EOF is handed to the gzip layer, which must fail LOUDLY
	// (unexpected EOF) instead of silently accepting truncated data.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"test-etag"`)
		w.Header().Set("Content-Type", "application/sql")
		if r.Header.Get("Range") == "" {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = w.Write(gz[:truncateAt])
			return
		}
		_, _ = w.Write(gz)
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	_, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if err == nil {
		t.Fatal("expected the gzip layer to reject the truncated unknown-length stream")
	}
}

func TestResumeReader_ClassifyUnknownEOF_DegradesToEOF(t *testing.T) {
	shrinkResumeBackoff(t)
	partial := http.Header{"Content-Range": []string{"bytes 1000-1999/2000"}} // wrong start: offset is 500
	cases := []struct {
		name    string
		url     string
		etag    string
		lastMod string
		rt      http.RoundTripper
	}{
		{
			name:    "weak etag falls back to last-modified on probe",
			etag:    `W/"weak"`,
			lastMod: "Wed, 01 Apr 2026 00:00:00 GMT",
			rt: rtFunc(func(req *http.Request) (*http.Response, error) {
				if got := req.Header.Get("If-Range"); got != "Wed, 01 Apr 2026 00:00:00 GMT" {
					t.Errorf("probe If-Range = %q, want Last-Modified (weak ETag must not be used)", got)
				}
				return nil, errors.New("probe transport error")
			}),
		},
		{
			name: "strong etag used on probe",
			etag: `"strong-v1"`,
			rt: rtFunc(func(req *http.Request) (*http.Response, error) {
				if got := req.Header.Get("If-Range"); got != `"strong-v1"` {
					t.Errorf("probe If-Range = %q, want strong ETag", got)
				}
				return nil, errors.New("probe transport error")
			}),
		},
		{
			name: "build probe request error",
			url:  "http://example.com/\x7f",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				t.Error("RoundTrip must not be called when the request cannot be built")
				return nil, errors.New("unexpected call")
			}),
		},
		{
			name: "probe request error",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("dial tcp: connection refused")
			}),
		},
		{
			name: "206 with malformed content-range",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			}),
		},
		{
			name: "206 with mismatched start",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("")), Header: partial}, nil
			}),
		},
		{
			name: "416 with malformed total",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				h := http.Header{"Content-Range": []string{"bytes */notanumber"}}
				return &http.Response{StatusCode: http.StatusRequestedRangeNotSatisfiable, Body: io.NopCloser(strings.NewReader("")), Header: h}, nil
			}),
		},
		{
			name: "unexpected status",
			rt: rtFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newResumeReaderForTest(context.Background(), errBody{io.EOF}, tc.rt)
			r.total = -1 // unknown length
			if tc.url != "" {
				r.url = tc.url
			}
			if tc.etag != "" {
				r.etag = tc.etag
			}
			if tc.lastMod != "" {
				r.lastMod = tc.lastMod
			}
			if _, err := r.Read(make([]byte, 64)); err != io.EOF {
				t.Fatalf("got %v, want io.EOF (inconclusive probes must degrade to EOF, letting gzip enforce integrity)", err)
			}
		})
	}
}

// TestResumeReader_ClassifyUnknownEOF_CappedProbes ensures repeated clean EOFs
// without progress stop probing once the consecutive-failure budget is spent.
func TestResumeReader_ClassifyUnknownEOF_CappedProbes(t *testing.T) {
	r := newResumeReaderForTest(context.Background(), errBody{io.EOF}, rtFunc(func(*http.Request) (*http.Response, error) {
		t.Error("RoundTrip must not be called once the probe budget is exhausted")
		return nil, errors.New("unexpected call")
	}))
	r.total = -1
	r.consecFailures = maxResumeAttempts
	if _, err := r.Read(make([]byte, 64)); err != io.EOF {
		t.Fatalf("got %v, want io.EOF", err)
	}
}

func TestDownload_ResumeRefusedWithoutValidator(t *testing.T) {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
	gz := gzipped(t, dumpBody)
	truncateAt := len(gz) / 2

	var sawRange atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			sawRange.Store(true)
		}
		w.Header().Set("Content-Type", "application/sql")
		w.Header().Set("Content-Length", strconv.Itoa(len(gz)))
		if r.Header.Get("Range") == "" {
			_, _ = w.Write(gz[:truncateAt]) // no ETag, no Last-Modified
			return
		}
		_, _ = w.Write(gz)
	}))
	defer srv.Close()
	orig := LatestDumpURL
	setLatestDumpURL(srv.URL)
	defer setLatestDumpURL(orig)
	shrinkResumeBackoff(t)

	_, err := Download(context.Background(), srv.Client(), "", nil, func(r io.Reader, d DownloadResult) error {
		_, err := io.Copy(io.Discard, r)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "cannot resume safely") {
		t.Fatalf("got %v, want a cannot-resume-safely error", err)
	}
	if sawRange.Load() {
		t.Error("a Range request was issued despite the missing validator — splicing unverifiable bytes is unsafe")
	}
}

func TestResumeReader_ClassifyUnknownEOF_NoValidatorDegrades(t *testing.T) {
	r := newResumeReaderForTest(context.Background(), errBody{io.EOF}, rtFunc(func(*http.Request) (*http.Response, error) {
		t.Error("Range probe must not be issued without a usable validator")
		return nil, errors.New("unexpected call")
	}))
	r.total = -1
	r.etag = `W/"weak"` // weak-only: unusable for identity, no Last-Modified fallback
	r.lastMod = ""
	if _, err := r.Read(make([]byte, 64)); err != io.EOF {
		t.Fatalf("got %v, want io.EOF (degrade and let gzip enforce integrity)", err)
	}
}

func TestResumeReader_IfRangeValidator(t *testing.T) {
	cases := []struct {
		name    string
		etag    string
		lastMod string
		want    string
	}{
		{"strong etag preferred", `"v1"`, "Wed, 01 Apr 2026 00:00:00 GMT", `"v1"`},
		{"weak etag falls back to last-modified", `W/"v1"`, "Wed, 01 Apr 2026 00:00:00 GMT", "Wed, 01 Apr 2026 00:00:00 GMT"},
		{"weak etag alone is unusable", `W/"v1"`, "", ""},
		{"last-modified only", "", "Wed, 01 Apr 2026 00:00:00 GMT", "Wed, 01 Apr 2026 00:00:00 GMT"},
		{"nothing usable", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &resumeReader{etag: tc.etag, lastMod: tc.lastMod}
			if got := r.ifRangeValidator(); got != tc.want {
				t.Errorf("ifRangeValidator() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseRangeNotSatisfiableTotal(t *testing.T) {
	cases := []struct {
		header string
		want   int64
		ok     bool
	}{
		{"bytes */1234", 1234, true},
		{"bytes 0-1/2", 0, false},
		{"bytes */abc", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseRangeNotSatisfiableTotal(tc.header)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseRangeNotSatisfiableTotal(%q) = (%d, %v), want (%d, %v)", tc.header, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseContentRange(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		wantStart int64
		wantTotal int64
		wantErr   bool
	}{
		{"standard", "bytes 100-199/1234", 100, 1234, false},
		{"unknown total", "bytes 1000-1999/*", 1000, -1, false},
		{"zero start", "bytes 0-99/100", 0, 100, false},
		{"empty", "", 0, 0, true},
		{"wrong unit", "items 0-1/2", 0, 0, true},
		{"missing slash", "bytes 0-1", 0, 0, true},
		{"missing dash", "bytes 0100/200", 0, 0, true},
		{"bad start", "bytes x-1/2", 0, 0, true},
		{"bad total", "bytes 0-1/zz", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, total, err := parseContentRange(tc.header)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseContentRange: %v", err)
			}
			if start != tc.wantStart || total != tc.wantTotal {
				t.Errorf("parseContentRange(%q) = (%d, %d), want (%d, %d)", tc.header, start, total, tc.wantStart, tc.wantTotal)
			}
		})
	}
}

// TestDumpURLOverride_EnvVar covers the JAVINIZER_R18DEV_DUMP_URL env-var
// branch of DumpURLOverride.
func TestDumpURLOverride_EnvVar(t *testing.T) {
	orig := os.Getenv("JAVINIZER_R18DEV_DUMP_URL")
	t.Setenv("JAVINIZER_R18DEV_DUMP_URL", "https://mirror.example.com/dump.sql.gz")
	defer os.Setenv("JAVINIZER_R18DEV_DUMP_URL", orig)

	if got := DumpURLOverride(); got != "https://mirror.example.com/dump.sql.gz" {
		t.Errorf("DumpURLOverride env: got %q, want mirror URL", got)
	}
}

// TestDownload_FetchError covers the client.Do error branch (e.g. a request to
// an unreachable endpoint).
func TestDownload_FetchError(t *testing.T) {
	orig := LatestDumpURL
	setLatestDumpURL("http://127.0.0.1:1/unreachable") // port 1: connection refused
	defer setLatestDumpURL(orig)

	_, err := Download(context.Background(), &http.Client{}, "", nil, func(io.Reader, DownloadResult) error { return nil })
	if err == nil {
		t.Fatal("expected a fetch error for an unreachable endpoint")
	}
}

// TestDownload_BuildRequestError covers the http.NewRequestWithContext error
// branch (line 54): an invalid URL (control character) causes request building
// to fail before any HTTP call.
func TestDownload_BuildRequestError(t *testing.T) {
	orig := LatestDumpURL
	// A URL with a control character is rejected by url.Parse → NewRequest fails.
	setLatestDumpURL("http://example.com/\x7f")
	defer setLatestDumpURL(orig)

	_, err := Download(context.Background(), &http.Client{}, "", nil, func(io.Reader, DownloadResult) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("expected build-request error, got: %v", err)
	}
}

// TestDownload_ImportFnError covers the importFn error branch (line 91): the
// download succeeds and gunzips, but importFn returns an error that Download
// propagates.
func TestDownload_ImportFnError(t *testing.T) {
	srv, latest := newDumpServer(t)
	defer srv.Close()

	orig := LatestDumpURL
	setLatestDumpURL(latest)
	defer setLatestDumpURL(orig)

	importErr := errors.New("import failed")
	_, err := Download(context.Background(), srv.Client(), "", nil, func(io.Reader, DownloadResult) error {
		return importErr
	})
	if err != importErr {
		t.Fatalf("expected importFn error, got: %v", err)
	}
}
