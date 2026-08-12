package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOptionsRejectsUnknownOptionKey(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parseOptions([]string{"--option", "r18dev-dump-typo=/x.db"}, &stderr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --option key")
	// The accepted set must be enumerated so typos are fixable from the error.
	for _, key := range []string{"jvthumbs.csv", "legacy.csv", "minnanoav.sitemap", "r18dev.dump", "sitemap"} {
		assert.Contains(t, err.Error(), key)
	}
}

func TestParseOptionsAcceptsDocumentedOptionKeys(t *testing.T) {
	var stderr bytes.Buffer
	opts, err := parseOptions([]string{"--option", "r18dev.dump=/x.db", "--option", "sitemap=https://x/sitemap.xml"}, &stderr)
	require.NoError(t, err)
	assert.Equal(t, "/x.db", opts.parameters["r18dev.dump"])
}

// The documented --option r18dev.dump=PATH form must register the r18dev
// source: with a missing dump file the failure is the open error, proving
// registration was attempted instead of "unknown actress cache source".
func TestRunHonorsR18DevDumpOption(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.db")
	state := filepath.Join(dir, "state.jsonl")
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{
		"--source", "r18dev", "--option", "r18dev.dump=" + missing, "--state", state,
	}, &stdout, &stderr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open r18.dev dump")
	assert.NotContains(t, err.Error(), "unknown actress cache source")
}

// A zero-record run must fail loudly instead of atomically replacing the
// published artifact with an empty cache.
func TestRunRefusesToPublishEmptyCache(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "legacy.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("FullName,LastName,FirstName,JapaneseName,ThumbUrl\n"), 0o600))
	state := filepath.Join(dir, "state.jsonl")
	output := filepath.Join(dir, "out.json.gz")
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{
		"--source", "legacy-jvthumbs", "--option", "legacy.csv=" + csvPath,
		"--output", output, "--state", state,
		"--min-dimension", "1", "--delay", "0", "--image-delay", "0", "--workers", "1",
	}, &stdout, &stderr)
	require.ErrorContains(t, err, "refusing to publish empty actress cache")
	_, statErr := os.Stat(output)
	assert.True(t, os.IsNotExist(statErr), "no empty artifact must be written")
}

// The runtime projection drops candidates with no DMM ID, no names, and no
// aliases (e.g. an r18.dev dump row carrying only a SourceID); the publish
// path must refuse instead of writing an empty runtime artifact.
func TestRunRefusesToPublishIdentitylessProjection(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		require.NoError(t, png.Encode(w, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	}))
	defer server.Close()
	dumpPath := filepath.Join(dir, "dump.db")
	db, err := sql.Open("sqlite3", dumpPath)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE actresses (id TEXT, name_romaji TEXT, image_url TEXT, name_kanji TEXT, name_kana TEXT)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO actresses VALUES ('k001', '', '" + server.URL + "/thumb.png', '', '')")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	state := filepath.Join(dir, "state.jsonl")
	output := filepath.Join(dir, "out.json.gz")
	var stdout, stderr bytes.Buffer
	err = run(t.Context(), []string{
		"--source", "r18dev", "--option", "r18dev.dump=" + dumpPath,
		"--output", output, "--state", state,
		"--min-dimension", "1", "--delay", "0", "--image-delay", "0", "--workers", "1", "--allow-private-hosts",
	}, &stdout, &stderr)
	require.ErrorContains(t, err, "zero projected records")
	_, statErr := os.Stat(output)
	assert.True(t, os.IsNotExist(statErr))
}

// The journal commit happens only after BOTH artifacts are written; a write
// failure there must propagate (and must NOT poison last-good state).
func TestRunPropagatesJournalStaleError(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		require.NoError(t, png.Encode(w, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	}))
	defer server.Close()
	csvPath := filepath.Join(dir, "legacy.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte("FullName,ThumbUrl\nA Name,"+server.URL+"/thumb.png\n"), 0o600))
	state := filepath.Join(dir, "state.jsonl")
	output := filepath.Join(dir, "out.json.gz")
	var stdout, stderr bytes.Buffer
	buildArgs := []string{
		"--source", "legacy-jvthumbs", "--legacy-csv", csvPath,
		"--output", output, "--state", state,
		"--min-dimension", "1", "--delay", "0", "--image-delay", "0", "--workers", "1",
		"--allow-private-hosts",
	}
	// Seed one build so a next one has prune-eligible entries.
	require.NoError(t, run(t.Context(), buildArgs, &stdout, &stderr))

	reg := actresscache.NewRegistry
	_ = reg
	original := journalStale
	t.Cleanup(func() { journalStale = original })
	journalStale = func(string, []string) error { return errors.New("journal down") }
	err := run(t.Context(), buildArgs, &stdout, &stderr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "journal stale entries")
}
func TestRunPropagatesFetcherConstructionError(t *testing.T) {
	original := newFetcherWithOptions
	t.Cleanup(func() { newFetcherWithOptions = original })
	newFetcherWithOptions = func(*http.Client, time.Duration, string, map[string]time.Duration, bool) (*actresscache.Fetcher, error) {
		return nil, errors.New("unpinnable transport")
	}
	dir := t.TempDir()
	state := filepath.Join(dir, "state.jsonl")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"--source", "legacy-jvthumbs", "--option", "legacy.csv=/nonexistent.csv", "--state", state,
	}, &stdout, &stderr)
	require.ErrorContains(t, err, "unpinnable transport")
}
