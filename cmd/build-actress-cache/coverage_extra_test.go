package main

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParameterMapStringIsSorted(t *testing.T) {
	values := parameterMap{"z": "last", "a": "first"}
	assert.Equal(t, "a=first,z=last", values.String())
}

func TestParseOptionsRejectsEveryInvalidBoundary(t *testing.T) {
	for _, args := range [][]string{
		{"--image-delay", "-1ns"},
		{"--timeout", "0s"},
		{"--limit", "-1"},
		{"--min-dimension", "-2"},
		{"--max-image-bytes", "0"},
	} {
		_, err := parseOptions(args, &bytes.Buffer{})
		require.Error(t, err, "%v", args)
	}
}

func TestRunBuildsLegacyCacheAndAudit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		require.NoError(t, png.Encode(w, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	}))
	defer server.Close()

	dir := t.TempDir()
	csvPath := filepath.Join(dir, "legacy.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("FullName,ThumbUrl\nTest Actress,"+server.URL+"/thumb.png\n"), 0o600))
	output, audit, state := filepath.Join(dir, "cache.json.gz"), filepath.Join(dir, "audit.json"), filepath.Join(dir, "state.jsonl")
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{
		"--source", "legacy-jvthumbs", "--legacy-csv", csvPath,
		"--output", output, "--audit-output", audit, "--state", state,
		"--min-dimension", "1", "--delay", "0", "--image-delay", "0",
		"--workers", "1", "--allow-private-hosts",
		// An accepted-but-unused option exercising the parameter copy path.
		"--option", "jvthumbs.csv=" + filepath.Join(dir, "absent.csv"),
	}, &stdout, &stderr)
	require.NoError(t, err, stderr.String())
	assert.FileExists(t, output)
	assert.FileExists(t, audit)
	assert.Contains(t, stdout.String(), "records=1")
}

func TestRunReportsBuildAndWriteErrors(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"--source", "legacy-jvthumbs", "--legacy-csv", filepath.Join(dir, "missing.csv"), "--state", filepath.Join(dir, "state")}, &stdout, &stderr)
	require.Error(t, err)

	// A non-empty build must exist so the write paths are reached (the empty
	// build fails earlier by design: empty-publish guard).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		require.NoError(t, png.Encode(w, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	}))
	defer server.Close()

	csvPath := filepath.Join(dir, "legacy.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("FullName,ThumbUrl\nA Name,"+server.URL+"/thumb.png\n"), 0o600))
	blocked := filepath.Join(dir, "blocked")
	require.NoError(t, os.WriteFile(blocked, []byte("file"), 0o600))
	buildArgs := func(state, out string) []string {
		return []string{"--source", "legacy-jvthumbs", "--legacy-csv", csvPath, "--state", state, "--output", out, "--min-dimension", "1", "--delay", "0", "--image-delay", "0", "--workers", "1", "--allow-private-hosts"}
	}
	err = run(t.Context(), buildArgs(filepath.Join(dir, "state2"), filepath.Join(blocked, "cache.gz")), &stdout, &stderr)
	require.ErrorContains(t, err, "write actress runtime cache")

	auditArgs := append(buildArgs(filepath.Join(dir, "state3"), filepath.Join(dir, "cache.gz")), "--audit-output", filepath.Join(blocked, "audit.json"))
	err = run(t.Context(), auditArgs, &stdout, &stderr)
	require.ErrorContains(t, err, "write actress audit cache")
}

// --min-dimension 0 must remain 0 (disable), while the omitted flag flows
// through as the -1 sentinel that maps to the 64 default.
func TestParseOptionsPreservesExplicitZeroMinDimension(t *testing.T) {
	opts, err := parseOptions([]string{}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, -1, opts.minDimension)

	opts, err = parseOptions([]string{"--min-dimension", "0"}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, 0, opts.minDimension, "explicit zero survives parsing")

	opts, err = parseOptions([]string{"--min-dimension", "128"}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, 128, opts.minDimension)
	_ = opts
}
