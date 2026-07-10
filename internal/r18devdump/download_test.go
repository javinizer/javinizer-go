package r18devdump

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
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
