package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
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
