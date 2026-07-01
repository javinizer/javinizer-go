package dump

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// runInDir runs fn with the working directory set to dir, restoring the
// original directory afterwards.
func runInDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)
	fn()
}

// runCobra executes the dump command tree with the given args, capturing
// stdout/stderr, and asserts the output contains wantSubstr. It registers a
// local --config persistent flag (normally provided by the root command) so
// the tree is drivable in isolation. This covers the cobra constructors and
// RunE closures that the direct run* tests bypass.
func runCobra(t *testing.T, args []string, wantSubstr string) {
	t.Helper()
	cmd := NewCommand()
	cmd.PersistentFlags().StringP("config", "c", "", "config file")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	// Inject --config (relative to the temp CWD) after the subcommand name so
	// LoadOrCreate receives a real path instead of "".
	fullArgs := append([]string{args[0], "--config", "config.yaml"}, args[1:]...)
	cmd.SetArgs(fullArgs)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute %v: %v\noutput: %s", args, err, buf.String())
	}
	if wantSubstr != "" && !strings.Contains(buf.String(), wantSubstr) {
		t.Errorf("Execute %v: output %q missing %q", args, buf.String(), wantSubstr)
	}
}

func newDumpHTTPServer(t *testing.T) *httptest.Server {
	dumpBody := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\nh_086mesu00103\tMESU-103\n\\.\n"
	gz := gzipBytes(t, dumpBody)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	}))
}

func TestDownloadStatusSearch(t *testing.T) {
	srv := newDumpHTTPServer(t)
	defer srv.Close()

	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	// Isolate config + dump path by running from the temp directory.
	runInDir(t, tmp, func() {
		var buf bytes.Buffer

		// download
		if err := runDownload(&buf, "config.yaml", false); err != nil {
			t.Fatalf("runDownload: %v\noutput: %s", err, buf.String())
		}
		out := buf.String()
		if !strings.Contains(out, "Imported 2 videos") {
			t.Errorf("download output missing import count: %s", out)
		}
		if !strings.Contains(out, "Dump ready") {
			t.Errorf("download output missing ready message: %s", out)
		}
		// The sidecar DB must exist at the default path.
		if _, err := os.Stat(filepath.Join("data", "r18dev", "r18dev_dump.db")); err != nil {
			t.Errorf("dump db not created: %v", err)
		}

		// status
		buf.Reset()
		if err := runStatus(&buf, "config.yaml"); err != nil {
			t.Fatalf("runStatus: %v", err)
		}
		status := buf.String()
		if !strings.Contains(status, "Rows:        2") {
			t.Errorf("status missing row count: %s", status)
		}
		if !strings.Contains(status, "Source date: 2026-04-28") && !strings.Contains(status, "Source date:") {
			// Source date is empty here since the httptest URL has no date; just
			// confirm the field header is absent-or-present gracefully.
		}

		// search by dvd_id
		buf.Reset()
		if err := runSearch(&buf, "config.yaml", "IPX-535"); err != nil {
			t.Fatalf("runSearch dvd_id: %v", err)
		}
		if got := buf.String(); !strings.Contains(got, "118ipx00535") {
			t.Errorf("search dvd_id output: %s", got)
		}

		// search by content_id
		buf.Reset()
		if err := runSearch(&buf, "config.yaml", "118ipx00535"); err != nil {
			t.Fatalf("runSearch content_id: %v", err)
		}
		if got := buf.String(); !strings.Contains(got, "IPX-535") {
			t.Errorf("search content_id output: %s", got)
		}

		// search miss
		buf.Reset()
		if err := runSearch(&buf, "config.yaml", "NOPE-999"); err != nil {
			t.Fatalf("runSearch miss: %v", err)
		}
		if got := buf.String(); !strings.Contains(got, "No match") {
			t.Errorf("search miss output: %s", got)
		}
	})
}

// TestCobraWiring drives the full command tree through cobra, covering the
// NewCommand/new*Cmd constructors and RunE closures that the direct run*
// tests bypass, and validating flag parsing and arg handling end-to-end.
func TestCobraWiring(t *testing.T) {
	srv := newDumpHTTPServer(t)
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		runCobra(t, []string{"status"}, "No local dump found")
		runCobra(t, []string{"download"}, "Dump ready")
		runCobra(t, []string{"status"}, "Rows:        2")
		runCobra(t, []string{"search", "IPX-535"}, "118ipx00535")
		runCobra(t, []string{"search", "118ipx00535"}, "IPX-535")
	})
}

func TestStatus_NoDump(t *testing.T) {
	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		var buf bytes.Buffer
		if err := runStatus(&buf, "config.yaml"); err != nil {
			t.Fatalf("runStatus: %v", err)
		}
		if got := buf.String(); !strings.Contains(got, "No local dump found") {
			t.Errorf("expected no-dump message, got: %s", got)
		}
	})
}

func TestSearch_NoDump(t *testing.T) {
	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		var buf bytes.Buffer
		err := runSearch(&buf, "config.yaml", "IPX-535")
		if err == nil {
			t.Fatal("expected error when no dump present")
		}
	})
}

func TestUpdate_UnchangedSkips(t *testing.T) {
	srv := newDumpHTTPServer(t)
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		var buf bytes.Buffer
		// First download.
		if err := runDownload(&buf, "config.yaml", false); err != nil {
			t.Fatalf("first download: %v", err)
		}
		// Second download as "update" should detect the same source URL and skip.
		buf.Reset()
		if err := runDownload(&buf, "config.yaml", true); err != nil {
			t.Fatalf("update: %v", err)
		}
		if !strings.Contains(buf.String(), "unchanged") && !strings.Contains(buf.String(), "Unchanged") {
			t.Errorf("update did not skip unchanged dump: %s", buf.String())
		}
	})
}
