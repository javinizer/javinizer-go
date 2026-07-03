package dump

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// binaryOnce ensures the javinizer binary is built only once per test process.
// `go build` takes ~2s; caching the artifact across subtests keeps the suite fast.
var (
	binaryOnce sync.Once
	binaryPath string
	binaryErr  error
)

// buildJavinizerBinary builds the javinizer CLI binary to a temp path and
// returns it. The build is cached for the lifetime of the test process.
func buildJavinizerBinary(t *testing.T) string {
	t.Helper()
	binaryOnce.Do(func() {
		// Resolve the module root so `go build` works regardless of the test's
		// package-relative CWD.
		rootOut, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
		if err != nil {
			binaryErr = err
			return
		}
		moduleRoot := strings.TrimSpace(string(rootOut))

		f, err := os.CreateTemp("", "javinizer-e2e-*")
		if err != nil {
			binaryErr = err
			return
		}
		_ = f.Close()
		binaryPath = f.Name()

		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/javinizer")
		cmd.Dir = moduleRoot
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		binaryErr = cmd.Run()
		if binaryErr != nil {
			binaryErr = fmt.Errorf("go build: %w: %s", binaryErr, stderr.String())
		}
	})
	if binaryErr != nil {
		t.Fatalf("failed to build javinizer binary: %v", binaryErr)
	}
	return binaryPath
}

// gzippedDump serves a minimal r18.dev dump as a gzip stream.
func gzippedDump(t *testing.T) []byte {
	t.Helper()
	dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" +
		"118ipx00535\tIPX-535\n" +
		"h_086mesu00103\tMESU-103\n" +
		"\\.\n"
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(dump)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newE2EDumpServer returns an httptest server that serves a gzipped dump at
// /latest (redirecting to a dated path, mirroring r18.dev's real behavior).
func newE2EDumpServer(t *testing.T) *httptest.Server {
	t.Helper()
	gz := gzippedDump(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/dumps/r18dotdev_dump_2026-04-28.sql.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(gz)
	})
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dumps/r18dotdev_dump_2026-04-28.sql.gz", http.StatusFound)
	})
	return httptest.NewServer(mux)
}

// runBinary executes the javinizer binary with the given args and env,
// returning combined stdout+stderr. The working directory is set to workDir so
// config.yaml and the dump DB resolve under the temp dir.
func runBinary(t *testing.T, bin, workDir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("javinizer %v failed: %v\noutput: %s", args, err, out.String())
	}
	return out.String()
}

// TestE2E_Binary_DownloadStatusSearch is a true black-box end-to-end test: it
// builds the real javinizer binary, then drives the full `dump` command tree
// (download → status → search) through exec.Command against a local httptest
// server. This validates the entire stack — cobra registration, config
// loading, env-var URL override, HTTP fetch, gzip decompression, SQLite
// import, read-only store, and query — exactly as a user would invoke it,
// catching integration regressions that in-process tests miss (e.g. command
// not wired into root.go, flag parsing, embedded config drift).
func TestE2E_Binary_DownloadStatusSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("binary e2e test skipped in -short mode")
	}
	bin := buildJavinizerBinary(t)
	srv := newE2EDumpServer(t)
	defer srv.Close()

	workDir := t.TempDir()

	// Write a minimal config.yaml so LoadOrCreate doesn't try to save defaults
	// (which requires a writable parent and can race on atomic rename).
	configYAML := "config_version: 3\n"
	if err := os.WriteFile(filepath.Join(workDir, "config.yaml"), []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	env := []string{"JAVINIZER_R18DEV_DUMP_URL=" + srv.URL + "/latest"}

	// 1. download — fetches from the test server, gunzips, imports to SQLite.
	out := runBinary(t, bin, workDir, env, "--config", "config.yaml", "dump", "download")
	if !strings.Contains(out, "Imported 2 videos") {
		t.Errorf("download: expected 'Imported 2 videos' in output:\n%s", out)
	}
	if !strings.Contains(out, "Dump ready") {
		t.Errorf("download: expected 'Dump ready' in output:\n%s", out)
	}

	// The sidecar DB must exist at the default path.
	dbPath := filepath.Join(workDir, "data", "r18dev", "r18dev_dump.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("dump DB not created at %s: %v", dbPath, err)
	}

	// 2. status — reads the imported DB and reports row count + source date.
	out = runBinary(t, bin, workDir, env, "--config", "config.yaml", "dump", "status")
	if !strings.Contains(out, "Rows:        2") {
		t.Errorf("status: expected 'Rows:        2' in output:\n%s", out)
	}
	if !strings.Contains(out, "Source date: 2026-04-28") {
		t.Errorf("status: expected source date in output:\n%s", out)
	}

	// 3. search by dvd_id — resolves IPX-535 -> 118ipx00535.
	out = runBinary(t, bin, workDir, env, "--config", "config.yaml", "dump", "search", "IPX-535")
	if !strings.Contains(out, "118ipx00535") {
		t.Errorf("search dvd_id: expected content_id in output:\n%s", out)
	}

	// 4. search by content_id — reverse lookup.
	out = runBinary(t, bin, workDir, env, "--config", "config.yaml", "dump", "search", "118ipx00535")
	if !strings.Contains(out, "IPX-535") {
		t.Errorf("search content_id: expected dvd_id in output:\n%s", out)
	}

	// 5. search miss — graceful "No match" message.
	out = runBinary(t, bin, workDir, env, "--config", "config.yaml", "dump", "search", "NOPE-999")
	if !strings.Contains(out, "No match") {
		t.Errorf("search miss: expected 'No match' in output:\n%s", out)
	}
}

// TestE2E_Binary_StatusNoDump verifies the binary's `dump status` command
// gracefully reports a missing dump (the default state for new users) rather
// than erroring. This is the first command a user runs before downloading.
func TestE2E_Binary_StatusNoDump(t *testing.T) {
	if testing.Short() {
		t.Skip("binary e2e test skipped in -short mode")
	}
	bin := buildJavinizerBinary(t)
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "config.yaml"), []byte("config_version: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runBinary(t, bin, workDir, nil, "--config", "config.yaml", "dump", "status")
	if !strings.Contains(out, "No local dump found") {
		t.Errorf("expected 'No local dump found' in output:\n%s", out)
	}
	if !strings.Contains(out, "javinizer dump download") {
		t.Errorf("expected download hint in output:\n%s", out)
	}
}

// TestE2E_Binary_UpdateSkipsUnchanged verifies that `dump update` detects an
// unchanged dump (same redirect target) and skips re-downloading. This is the
// incremental-update path users run periodically.
func TestE2E_Binary_UpdateSkipsUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("binary e2e test skipped in -short mode")
	}
	bin := buildJavinizerBinary(t)
	srv := newE2EDumpServer(t)
	defer srv.Close()
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "config.yaml"), []byte("config_version: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"JAVINIZER_R18DEV_DUMP_URL=" + srv.URL + "/latest"}

	// First download to populate the dump + store the source URL.
	_ = runBinary(t, bin, workDir, env, "--config", "config.yaml", "dump", "download")

	// Second run as `update` — same server, same redirect target → skip.
	out := runBinary(t, bin, workDir, env, "--config", "config.yaml", "dump", "update")
	if !strings.Contains(strings.ToLower(out), "unchanged") {
		t.Errorf("update: expected 'unchanged' in output:\n%s", out)
	}
}
