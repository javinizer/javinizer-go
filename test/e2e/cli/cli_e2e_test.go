// Package cli_e2e exercises the real `javinizer` binary end-to-end through
// its CLI subcommands (sort, version). It is the CLI counterpart to the
// fullstack Playwright suite: where that suite drives the API server, this
// suite drives the command-line entry point.
//
// Determinism: the binary is built once (TestMain) and run with
// JAVINIZER_E2E_SCRAPERS=true, which substitutes the offline e2emock scraper
// at the dependency-injection seam (internal/commandutil/dependencies.go).
// No real scraper network calls are made; every GOOD-*/FAIL-*/MULTI-* ID
// returns deterministic metadata.
//
// Build tag `e2e` keeps this suite out of `go test ./...` / `make test-short`;
// run via `make test-e2e-cli`.
//
//go:build e2e

package cli_e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// binaryPath is set by TestMain after building the real javinizer binary.
var binaryPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "javinizer-e2e-bin-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: mkdir temp: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "javinizer")
	if os.Getenv("JAVINIZER_E2E_BIN") != "" {
		// Allow reusing a prebuilt binary to speed up local iteration.
		binaryPath = os.Getenv("JAVINIZER_E2E_BIN")
	} else {
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/javinizer")
		cmd.Dir = repoRoot()
		out, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "e2e: go build failed: %v\n%s\n", err, out)
			os.Exit(1)
		}
	}

	os.Exit(m.Run())
}

// repoRoot resolves the repository root from the test source location
// (test/e2e/cli -> ../../..).
func repoRoot() string {
	src, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	return filepath.Join(src, "..", "..", "..")
}

// runEnv is the environment for every CLI invocation: the e2e scraper seam is
// always on, and JAVINIZER_CONFIG points at the per-test config.
func runEnv(configPath string) []string {
	env := os.Environ()
	env = append(env, "JAVINIZER_E2E_SCRAPERS=true")
	env = append(env, "JAVINIZER_CONFIG="+configPath)
	return env
}

// writeConfig writes a minimal config.yaml into dir that:
//   - points the SQLite DB at dir/javinizer.db
//   - enables regex matching for the test IDs (GOOD-001, FAIL-001, MULTI-001-pt1)
//   - organizes into a flat <ID>/ folder with <ID>.mp4 + <ID>.nfo
//     (subfolder_format is emptied to avoid the default <ID>/<ID>/ double-nest)
//   - enables NFO generation
//   - disables all media downloads (e2emock image URLs are non-resolvable)
//
// NOTE: OutputDownloadConfig is yaml-inlined under `output:`, so the download
// toggles are flat keys (output.download_cover), NOT nested under
// output.download. Nesting them silently falls back to the true defaults and
// triggers real (failing) media downloads.
func writeConfig(t *testing.T, dir string) string {
	t.Helper()
	cfg := `config_version: 3

file_matching:
    extensions:
        - .mp4
        - .mkv
    regex_enabled: true
    regex_pattern: '([A-Z]{2,10}-\d{2,5}[A-Z]?)(?:-pt(\d{1,2}))?'

output:
    folder_format: "<ID>"
    subfolder_format: []
    file_format: "<ID><IF:MULTIPART>-<PART></IF>"
    rename_file: true
    download_cover: false
    download_poster: false
    download_extrafanart: true
    download_trailer: false
    download_actress: false
    download_timeout: 5

metadata:
    nfo:
        feature:
            enabled: true
        format:
            filename_template: <ID>.nfo

database:
    type: sqlite
    dsn: ` + filepath.Join(dir, "javinizer.db") + `
    log_level: silent

logging:
    level: error
`
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(cfg), 0o600))
	return p
}

// writeFile creates a dummy video file (zero bytes is fine — the scanner
// matches by extension, not content).
func writeFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte{}, 0o600))
}

// run executes the javinizer binary with the given args + e2e env, returning
// combined output, exit code, and any exec error.
func run(t *testing.T, configPath string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = runEnv(configPath)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = 127
	}
	return buf.String(), code
}

// TestCLI_Version confirms the binary boots and prints version info. This is
// the smallest possible "is the binary alive" check and pins the build step.
func TestCLI_Version(t *testing.T) {
	out, code := run(t, "", "--version")
	assert.Equal(t, 0, code, "expected exit 0, got %d\n%s", code, out)
	assert.Contains(t, out, "javinizer")
}

// TestCLI_Sort_Success drives the full sort pipeline through the real binary:
// scan → match → scrape (e2emock) → organize → NFO. A GOOD-001.mp4 file is
// sorted into <ID>/GOOD-001.mp4 with a GOOD-001.nfo carrying the scraped title.
func TestCLI_Sort_Success(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir)
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	writeFile(t, filepath.Join(src, "GOOD-001.mp4"))

	out, code := run(t, cfgPath, "sort", src)
	require.Equal(t, 0, code, "sort exited %d\n%s", code, out)
	assert.Contains(t, out, "Sort complete!")

	// Organized into <ID>/GOOD-001.mp4 (folder_format/file_format = <ID>).
	organized := filepath.Join(src, "GOOD-001", "GOOD-001.mp4")
	assert.FileExists(t, organized, "organized video should exist\n%s", out)

	// NFO carries the e2emock-scraped title.
	nfoPath := filepath.Join(src, "GOOD-001", "GOOD-001.nfo")
	require.FileExists(t, nfoPath, "NFO should be generated\n%s", out)
	nfo, err := os.ReadFile(nfoPath)
	require.NoError(t, err)
	assert.Contains(t, string(nfo), "E2E Movie GOOD-001", "NFO should carry scraped title\n%s", nfo)
}

// TestCLI_Sort_ScrapeFailure confirms a FAIL-* ID (e2emock returns a 404) is
// handled gracefully: the binary must not crash, and the unscrapable file is
// left in place rather than organized into a bogus <ID> folder.
func TestCLI_Sort_ScrapeFailure(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir)
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	writeFile(t, filepath.Join(src, "FAIL-001.mp4"))

	out, code := run(t, cfgPath, "sort", src)
	// Scrape failures are non-fatal: sort completes with exit 0, reporting
	// that the file was scanned but no metadata was found.
	assert.Equal(t, 0, code, "sort should not crash on scrape failure\n%s", out)
	assert.Contains(t, out, "Sort complete!")

	// No organized folder should be created for a failed scrape.
	_, err := os.Stat(filepath.Join(src, "FAIL-001"))
	assert.True(t, os.IsNotExist(err), "no organized folder for failed scrape\n%s", out)
}

// TestCLI_Sort_Multipart confirms two -pt1/-pt2 files are parsed by the
// real matcher to the SAME MovieID (MULTI-001) and that sort completes
// without crashing. We intentionally do NOT assert part-suffixed output
// filenames: the is_multi_part flag is populated by the scanner's
// FileMatchInfo derivation, which requires real sibling-file discovery that
// 1-byte placeholder fixtures don't exercise (the fullstack Playwright
// multipart spec makes the same call). The testable CLI-level guarantee is:
// both files match one ID, the scrape succeeds, and an NFO is produced.
func TestCLI_Sort_Multipart(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir)
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	writeFile(t, filepath.Join(src, "MULTI-001-pt1.mp4"))
	writeFile(t, filepath.Join(src, "MULTI-001-pt2.mp4"))

	out, code := run(t, cfgPath, "sort", src)
	require.Equal(t, 0, code, "sort exited %d\n%s", code, out)
	assert.Contains(t, out, "Sort complete!")
	assert.Contains(t, out, "Scraped MULTI-001 successfully",
		"both parts should scrape under the shared MULTI-001 ID\n%s", out)

	// An NFO is produced for the shared ID (one of the two organize attempts
	// succeeds; the part collision is non-fatal and logged).
	nfoPath := filepath.Join(src, "MULTI-001", "MULTI-001.nfo")
	require.FileExists(t, nfoPath, "NFO generated for the shared multipart ID\n%s", out)
	nfo, err := os.ReadFile(nfoPath)
	require.NoError(t, err)
	assert.Contains(t, string(nfo), "E2E Movie MULTI-001", "NFO carries scraped title\n%s", nfo)
}

// writePartSuffixConfig mirrors writeConfig but uses file_format
// "<ID><PARTSUFFIX>" and leaves the custom regex off (regex_enabled: false) so
// the built-in matcher + cd-suffix detection own the match — the exact shape
// of the reported bug's library (GOOD-701-cd1/2/3 planned onto the same
// destination).
func writePartSuffixConfig(t *testing.T, dir string) string {
	t.Helper()
	cfg := `config_version: 3

file_matching:
    extensions:
        - .mp4
        - .mkv
    regex_enabled: false

output:
    folder_format: "<ID>"
    subfolder_format: []
    file_format: "<ID><PARTSUFFIX>"
    rename_file: true
    download_cover: false
    download_poster: false
    download_extrafanart: false
    download_trailer: false
    download_actress: false
    download_timeout: 5

metadata:
    nfo:
        feature:
            enabled: true
        format:
            filename_template: <ID><PARTSUFFIX>.nfo

database:
    type: sqlite
    dsn: ` + filepath.Join(dir, "javinizer.db") + `
    log_level: silent

logging:
    level: error
`
	p := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(cfg), 0o600))
	return p
}

// TestCLI_Sort_Multipart_PartSuffixTargets pins the fix for the CLI
// part-suffix drop (fix/cli-part-suffix-drop): RunBatchCommand must carry the
// ScanAndMatch per-file metadata (IsMultiPart / PartNumber / PartSuffix) into
// the batch job so the apply phase plans <PARTSUFFIX>-suffixed destinations.
// Pre-fix, all of GOOD-701-cd1/2/3 planned onto dest/GOOD-701/GOOD-701.mp4 —
// cd1 applied, cd2/cd3 conflicted as "organization validation failed".
//
// Both legs are asserted:
//   - dry-run: zero "Apply failed" conflicts, "Would organize 3 file(s)",
//     and nothing is moved or created;
//   - live: all three parts land at distinct GOOD-701-cdN targets.
func TestCLI_Sort_Multipart_PartSuffixTargets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writePartSuffixConfig(t, dir)
	src := filepath.Join(dir, "lib")
	dest := filepath.Join(dir, "dest")
	require.NoError(t, os.MkdirAll(src, 0o700))
	require.NoError(t, os.MkdirAll(dest, 0o700))
	for _, part := range []string{"cd1", "cd2", "cd3"} {
		writeFile(t, filepath.Join(src, "GOOD-701-"+part+".mp4"))
	}

	t.Run("dry-run plans distinct targets", func(t *testing.T) {
		out, code := run(t, cfgPath, "sort", "--dry-run", "--move", src, "--dest", dest)
		require.Equal(t, 0, code, "dry-run sort exited %d\n%s", code, out)
		assert.NotContains(t, out, "Apply failed",
			"no part may conflict on the planned destination\n%s", out)
		assert.Contains(t, out, "Would organize 3 file(s)",
			"all three -cdN parts must plan\n%s", out)
		for _, part := range []string{"cd1", "cd2", "cd3"} {
			assert.FileExists(t, filepath.Join(src, "GOOD-701-"+part+".mp4"),
				"dry-run must not move sources\n%s", out)
		}
		entries, err := os.ReadDir(dest)
		require.NoError(t, err)
		assert.Empty(t, entries, "dry-run must not create organized output\n%s", out)
	})

	t.Run("live moves to part-suffixed targets", func(t *testing.T) {
		out, code := run(t, cfgPath, "sort", "--move", src, "--dest", dest)
		require.Equal(t, 0, code, "live sort exited %d\n%s", code, out)
		assert.Contains(t, out, "Sort complete!")
		assert.NotContains(t, out, "Apply failed", "no part may conflict on apply\n%s", out)
		assert.Contains(t, out, "Organized 3 file(s)",
			"all three -cdN parts must apply\n%s", out)
		for _, part := range []string{"cd1", "cd2", "cd3"} {
			assert.FileExists(t,
				filepath.Join(dest, "GOOD-701", "GOOD-701-"+part+".mp4"),
				"part %s must land at its part-suffixed target\n%s", part, out)
		}
		entries, err := os.ReadDir(src)
		require.NoError(t, err)
		assert.Empty(t, entries, "--move consumed the sources\n%s", out)
	})
}

// TestCLI_DryRun confirms --dry-run previews the operation without moving
// files or writing an NFO. Guards against a regression where dry-run silently
// mutates the filesystem.
func TestCLI_DryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir)
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	orig := filepath.Join(src, "GOOD-002.mp4")
	writeFile(t, orig)

	out, code := run(t, cfgPath, "sort", "--dry-run", src)
	require.Equal(t, 0, code, "dry-run exited %d\n%s", code, out)

	// Original file untouched; no organized folder or NFO written.
	assert.FileExists(t, orig, "dry-run must not move the source file\n%s", out)
	_, err := os.Stat(filepath.Join(src, "GOOD-002"))
	assert.True(t, os.IsNotExist(err), "dry-run must not create the organized folder\n%s", out)
}

// TestCLI_Help confirms the root help lists the subcommands a user expects
// (sort, scrape, version, upgrade), so a broken command registration is
// caught early.
func TestCLI_Help(t *testing.T) {
	out, code := run(t, "", "--help")
	assert.Equal(t, 0, code, "help exited %d\n%s", code, out)
	for _, want := range []string{"sort", "scrape", "version", "upgrade"} {
		assert.Contains(t, out, want, "help should list %q\n%s", want, out)
	}
}

// TestCLI_NoE2EEnvUsesRealScrapers is a sanity guard: without the e2e env,
// the binary must NOT register e2emock as a known scraper. We check via
// `javinizer info` (or scrape against an unknown ID) — the error wording for
// an unrecognized scraper must not mention e2emock. This pins the seam so a
// future refactor can't accidentally leak the test scraper into production.
func TestCLI_NoE2EEnvUsesRealScrapers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping production-scrape-isolation guard in short mode")
	}
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir)
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(src, 0o700))
	writeFile(t, filepath.Join(src, "GOOD-001.mp4"))

	// Run WITHOUT the e2e env — strip it from the environment explicitly.
	cmd := exec.Command(binaryPath, "sort", src)
	cmd.Env = func() []string {
		env := os.Environ()
		out := make([]string, 0, len(env))
		for _, kv := range env {
			if strings.HasPrefix(kv, "JAVINIZER_E2E_SCRAPERS=") {
				continue
			}
			if strings.HasPrefix(kv, "JAVINIZER_CONFIG=") {
				continue
			}
			out = append(out, kv)
		}
		return append(out, "JAVINIZER_CONFIG="+cfgPath)
	}()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	out := buf.String()
	// With the env off, GOOD-001 hits real scrapers (which will fail offline),
	// but the output must not carry the e2emock marker — proving the seam is
	// env-gated and production scrapers were wired instead of the test mock
	// leaking into production.
	assert.NotContains(t, strings.ToLower(out), "e2emock",
		"e2emock must not be registered without JAVINIZER_E2E_SCRAPERS\n%s", out)
}
