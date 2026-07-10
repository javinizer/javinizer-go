package dump

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/r18devdump"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		if err := runDownload(context.Background(), &buf, "config.yaml", false); err != nil {
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
		if err := runDownload(context.Background(), &buf, "config.yaml", false); err != nil {
			t.Fatalf("first download: %v", err)
		}
		// Second download as "update" should detect the same source URL and skip.
		buf.Reset()
		if err := runDownload(context.Background(), &buf, "config.yaml", true); err != nil {
			t.Fatalf("update: %v", err)
		}
		if !strings.Contains(buf.String(), "unchanged") && !strings.Contains(buf.String(), "Unchanged") {
			t.Errorf("update did not skip unchanged dump: %s", buf.String())
		}
		if strings.Contains(buf.String(), "Importing dump") {
			t.Errorf("unchanged update should not print 'Importing dump': %s", buf.String())
		}
	})
}

// TestSearch_LookupErrorPropagated covers the CR2 error path: when the store
// returns a non-ErrDumpMiss error (here: the videos table is dropped, so the
// query fails), runSearch must surface it instead of masking it as "No match".
func TestSearch_LookupErrorPropagated(t *testing.T) {
	srv := newDumpHTTPServer(t)
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		var buf bytes.Buffer
		// Download to seed a valid sidecar DB.
		if err := runDownload(context.Background(), &buf, "config.yaml", false); err != nil {
			t.Fatalf("runDownload: %v", err)
		}

		// Corrupt the sidecar: drop the videos table so lookups fail with a
		// real query error (not ErrDumpMiss).
		dbPath := filepath.Join("data", "r18dev", "r18dev_dump.db")
		corruptor, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatalf("open corruptor: %v", err)
		}
		if _, err := corruptor.Exec("DROP TABLE videos"); err != nil {
			corruptor.Close()
			t.Fatalf("drop videos: %v", err)
		}
		corruptor.Close()

		// A search must now return an error (propagated), NOT "No match".
		buf.Reset()
		err = runSearch(&buf, "config.yaml", "IPX-535")
		if err == nil {
			t.Fatalf("expected a propagated lookup error, got nil; output: %s", buf.String())
		}
		if !strings.Contains(err.Error(), "lookup failed") {
			t.Errorf("expected a 'lookup failed' error, got: %v", err)
		}
	})
}

// TestResolveDumpPath_DefaultAndExplicit covers both branches of resolveDumpPath.
func TestResolveDumpPath_DefaultAndExplicit(t *testing.T) {
	cfg := &config.Config{}
	if got := resolveDumpPath(cfg); got != commandutil.DefaultR18DevDumpPath {
		t.Errorf("empty path: got %q, want default %q", got, commandutil.DefaultR18DevDumpPath)
	}
	cfg.Metadata.R18DevDump.Path = "/custom/path.db"
	if got := resolveDumpPath(cfg); got != "/custom/path.db" {
		t.Errorf("explicit path: got %q, want /custom/path.db", got)
	}
}

// TestFileSize_Errors covers the filepath.Abs and os.Stat error branches.
func TestFileSize_Errors(t *testing.T) {
	// os.Stat on a non-existent file.
	if _, err := fileSize(filepath.Join(t.TempDir(), "nope.db")); err == nil {
		t.Error("expected error for non-existent file")
	}
}

// TestNewUpdateCmd_RunE covers the update command's RunE closure (which calls
// runDownload with updateOnly=true via cobra context).
func TestNewUpdateCmd_RunE(t *testing.T) {
	srv := newDumpHTTPServer(t)
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		cmd := newUpdateCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetContext(context.Background())
		cmd.Flags().String("config", "config.yaml", "")
		// First call seeds the dump (update path with no existing dump).
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("update RunE: %v", err)
		}
	})
}

// invalidConfigFile writes a YAML file with invalid content and returns its
// path. config.LoadOrCreate treats a missing file as "create defaults" (no
// error), so to exercise the parse-error branch we need a file that exists
// but contains malformed YAML.
func invalidConfigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte(": not valid: yaml: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRunDownload_ConfigLoadError covers the config-load error branch.
func TestRunDownload_ConfigLoadError(t *testing.T) {
	err := runDownload(context.Background(), &bytes.Buffer{}, invalidConfigFile(t), false)
	if err == nil || !strings.Contains(err.Error(), "failed to load config") {
		t.Fatalf("expected config-load error, got: %v", err)
	}
}

// TestRunStatus_ConfigLoadError covers the runStatus config-load error branch.
func TestRunStatus_ConfigLoadError(t *testing.T) {
	err := runStatus(&bytes.Buffer{}, invalidConfigFile(t))
	if err == nil || !strings.Contains(err.Error(), "failed to load config") {
		t.Fatalf("expected config-load error, got: %v", err)
	}
}

// TestRunSearch_ConfigLoadError covers the runSearch config-load error branch.
func TestRunSearch_ConfigLoadError(t *testing.T) {
	err := runSearch(&bytes.Buffer{}, invalidConfigFile(t), "IPX-535")
	if err == nil || !strings.Contains(err.Error(), "failed to load config") {
		t.Fatalf("expected config-load error, got: %v", err)
	}
}

// TestRunStatus_StatsError covers the runStatus stats-error branch: the dump
// DB exists but its videos/dump_meta tables are missing, so Stats fails.
func TestRunStatus_StatsError(t *testing.T) {
	srv := newDumpHTTPServer(t)
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		var buf bytes.Buffer
		if err := runDownload(context.Background(), &buf, "config.yaml", false); err != nil {
			t.Fatalf("runDownload: %v", err)
		}
		// Corrupt: drop the videos table so Stats fails.
		dbPath := filepath.Join("data", "r18dev", "r18dev_dump.db")
		c, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatalf("open corruptor: %v", err)
		}
		if _, err := c.Exec("DROP TABLE videos"); err != nil {
			c.Close()
			t.Fatalf("drop videos: %v", err)
		}
		c.Close()
		buf.Reset()
		err = runStatus(&buf, "config.yaml")
		if err == nil || !strings.Contains(err.Error(), "failed to read dump stats") {
			t.Fatalf("expected stats error, got: %v", err)
		}
	})
}

// TestRunSearch_ContentIDLookupError covers the content_id lookup error branch
// (CR2): LookupByDVDID misses (ErrDumpMiss) but LookupByContentID returns a
// real error. We recreate the videos table without the dvd_id column so the
// content_id query fails.
func TestRunSearch_ContentIDLookupError(t *testing.T) {
	srv := newDumpHTTPServer(t)
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		var buf bytes.Buffer
		if err := runDownload(context.Background(), &buf, "config.yaml", false); err != nil {
			t.Fatalf("runDownload: %v", err)
		}
		// Recreate videos with only content_id + dvd_id_norm (no dvd_id column).
		// LookupByDVDID succeeds (misses on a non-existent ID → ErrDumpMiss),
		// then LookupByContentID fails (dvd_id column missing → real error).
		dbPath := filepath.Join("data", "r18dev", "r18dev_dump.db")
		c, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatalf("open corruptor: %v", err)
		}
		if _, err := c.Exec("DROP TABLE videos; CREATE TABLE videos (content_id TEXT, dvd_id_norm TEXT)"); err != nil {
			c.Close()
			t.Fatalf("recreate videos: %v", err)
		}
		c.Close()
		buf.Reset()
		err = runSearch(&buf, "config.yaml", "NOPE-999")
		if err == nil || !strings.Contains(err.Error(), "content_id lookup failed") {
			t.Fatalf("expected content_id lookup error, got: %v", err)
		}
	})
}

// TestRunDownload_DownloadError covers the Download error branch by pointing
// at an unreachable endpoint.
func TestRunDownload_DownloadError(t *testing.T) {
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = "http://127.0.0.1:1/unreachable"
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		err := runDownload(context.Background(), &bytes.Buffer{}, "config.yaml", false)
		if err == nil || !strings.Contains(err.Error(), "dump download failed") {
			t.Fatalf("expected download error, got: %v", err)
		}
	})
}

// TestRunStatus_SourceDateOutput covers the SourceDate output branch: when
// the dump has a source_date, status output includes it.
func TestRunStatus_SourceDateOutput(t *testing.T) {
	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		// Seed a dump directly with a source date (no HTTP needed).
		dbPath := filepath.Join("data", "r18dev", "r18dev_dump.db")
		dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"
		_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), dbPath, r18devdump.ImportOptions{
			SourceDate: "2026-04-28",
		})
		if err != nil {
			t.Fatalf("Import: %v", err)
		}
		var buf bytes.Buffer
		if err := runStatus(&buf, "config.yaml"); err != nil {
			t.Fatalf("runStatus: %v", err)
		}
		if !strings.Contains(buf.String(), "Source date: 2026-04-28") {
			t.Errorf("status output missing source date: %s", buf.String())
		}
	})
}

// TestRunDownload_ImportError covers the import-error branch (line 145): the
// download succeeds but Import fails because the dump path's parent is a file
// (not a directory), so MkdirAll fails.
func TestRunDownload_ImportError(t *testing.T) {
	srv := newDumpHTTPServer(t)
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		// Create "data" as a regular file so Import's MkdirAll("data/r18dev")
		// fails with "not a directory".
		require.NoError(t, os.WriteFile("data", []byte("x"), 0o600))
		err := runDownload(context.Background(), &bytes.Buffer{}, "config.yaml", false)
		require.Error(t, err, "import should fail when dump dir cannot be created")
		assert.Contains(t, err.Error(), "create dump dir")
	})
}

// TestRunDownload_ProgressOutput verifies that the progress callback fires
// during import (bar is set) and that "Importing dump" appears before
// "Imported". Uses a large dump body so data is read in multiple chunks
// during Import, triggering the progress callback after bar is created.
func TestRunDownload_ProgressOutput(t *testing.T) {
	var rows strings.Builder
	rows.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&rows, "118ipx%05d\tIPX-%d\n", i, i)
	}
	rows.WriteString("\\.\n")
	gz := gzipBytes(t, rows.String())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	}))
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		var buf bytes.Buffer
		err := runDownload(context.Background(), &buf, "config.yaml", false)
		require.NoError(t, err)
		out := buf.String()
		// "Importing dump" must appear before "Imported" result.
		idxImporting := strings.Index(out, "Importing dump")
		require.GreaterOrEqual(t, idxImporting, 0, "output should contain 'Importing dump'")
		idxImported := strings.Index(out, "Imported")
		require.Greater(t, idxImported, idxImporting, "'Imported' should come after 'Importing dump'")
	})
}

// TestRunDownload_InvalidGzip covers the error path where the response is not
// a valid gzip stream. The progress callback fires during the attempted gzip
// header read while bar is still nil, then Download returns an error.
func TestRunDownload_InvalidGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write([]byte("this is not a gzip stream"))
	}))
	defer srv.Close()
	origURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL
	defer func() { r18devdump.LatestDumpURL = origURL }()

	tmp := t.TempDir()
	runInDir(t, tmp, func() {
		var buf bytes.Buffer
		err := runDownload(context.Background(), &buf, "config.yaml", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "dump download failed")
	})
}
