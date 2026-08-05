package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectSharedArtifactPaths(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache.json.gz")
	state := filepath.Join(dir, "state.jsonl")
	audit := filepath.Join(dir, "audit.json")

	require.NoError(t, rejectSharedArtifactPaths(options{output: cache, state: state, auditOutput: audit}))
	require.NoError(t, rejectSharedArtifactPaths(options{output: cache, state: state}))

	collisions := map[string]options{
		"state equals output": {output: cache, state: cache},
		"audit equals output": {output: cache, state: state, auditOutput: cache},
		"audit equals state":  {output: cache, state: state, auditOutput: state},
	}
	// Relative-spelling alias (skipped when the temp dir lives on another
	// volume than the test binary, e.g. CI runners with C: vs D:).
	if cwd, err := filepath.Abs("."); err == nil {
		if relCache, err := filepath.Rel(cwd, cache); err == nil && !filepath.IsAbs(relCache) {
			collisions["relative alias match"] = options{output: cache, state: relCache, auditOutput: ""}
		}
	}
	for name, opts := range collisions {
		err := rejectSharedArtifactPaths(opts)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "must name distinct paths", name)
	}
}

// run() enforces it before any build work begins.
func TestRunRejectsCollidingArtifactPaths(t *testing.T) {
	dir := t.TempDir()
	same := filepath.Join(dir, "one")
	out := &strings.Builder{}
	err := run(t.Context(), []string{"--output", same, "--state", same, "--sources", "test"}, out, out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must name distinct paths")
}

// When path absolutization is unavailable (dead working directory), the
// lexical-clean fallback must still catch identically-spelled collisions.
func TestRejectSharedArtifactPathsAbsFailureFallback(t *testing.T) {
	original := absPath
	t.Cleanup(func() { absPath = original })
	absPath = func(string) (string, error) { return "", assert.AnError }
	err := rejectSharedArtifactPaths(options{output: "same.bin", state: "same.bin"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must name distinct paths")
}

// Case-insensitive filesystems: differently-cased spellings of the same file
// must collide; with folding off (case-sensitive volumes) they stay distinct.
func TestRejectSharedArtifactPathsCaseFolding(t *testing.T) {
	original := artifactKeysFoldCase
	t.Cleanup(func() { artifactKeysFoldCase = original })
	dir := t.TempDir()

	artifactKeysFoldCase = true
	err := rejectSharedArtifactPaths(options{
		output: filepath.Join(dir, "cache.json.gz"),
		state:  filepath.Join(dir, "CACHE.JSON.GZ"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must name distinct paths")

	artifactKeysFoldCase = false
	assert.NoError(t, rejectSharedArtifactPaths(options{
		output: filepath.Join(dir, "cache.json.gz"),
		state:  filepath.Join(dir, "CACHE.JSON.GZ"),
	}))
}

// Symlinked spellings of an artifact must collide with the real path, and
// symlinked PARENTS must too (cache/state leaves usually do not exist yet).
func TestRejectSharedArtifactPathsResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	require.NoError(t, os.Mkdir(real, 0o755))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(real, link))

	// Existing leaf: state is the real journal file; output points at it
	// through the symlink.
	journal := filepath.Join(real, "state.jsonl")
	require.NoError(t, os.WriteFile(journal, []byte("{}"), 0o600))
	err := rejectSharedArtifactPaths(options{output: filepath.Join(link, "state.jsonl"), state: journal})
	require.Error(t, err)

	// Not-yet-created files inside a symlinked DIRECTORY collide as well.
	err = rejectSharedArtifactPaths(options{output: filepath.Join(link, "fresh.json"), state: filepath.Join(real, "fresh.json")})
	require.Error(t, err)

	// Legitimately distinct neighbors stay accepted.
	require.NoError(t, rejectSharedArtifactPaths(options{output: filepath.Join(link, "cache.json.gz"), state: filepath.Join(real, "state.jsonl")}))
}
