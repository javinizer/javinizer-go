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
