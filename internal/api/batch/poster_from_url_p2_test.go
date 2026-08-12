package batch

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING P2 regression lock-ins (landed shape verified):
// canonical names are touched ONLY by the in-lock staged promote; the state
// commit never overlaps the download.

// Success: committed row references the canonical preview; canonical bytes
// are the downloaded image; no staged or backup residue remains.
func TestPosterFromURL_StateCommitSeesOnlyStagedPromotedAssets(t *testing.T) {
	deps, job, router, ts := fromURLFixtureRealJob(t, "P2URL-1")
	w := postFromURLRequest(t, router, job, "P2URL-1", ts.URL+"/pic.jpg")
	require.Equal(t, 200, w.Code, "%s", w.Body.String())

	dir := filepath.Join("data/temp", "posters", job.GetID())
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		data, err := afero.ReadFile(deps.GetFs(), filepath.Join(dir, "P2URL-1"+suffix))
		require.NoError(t, err)
		require.True(t, len(data) > 100, "canonical %s carries downloaded bytes", suffix)
	}
	entries, err := afero.ReadDir(deps.GetFs(), dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".stage-", "no staged residue: %s", e.Name())
		assert.NotContains(t, e.Name(), ".bak", "no backup residue: %s", e.Name())
		assert.NotContains(t, e.Name(), ".promote-", "witness swept: %s", e.Name())
	}
	stored := storedMovie(t, deps, job, "/path/to/P2URL-1.mp4")
	require.NotNil(t, stored)
	assert.Contains(t, stored.Poster.PosterURL, ts.URL)
	assert.Contains(t, stored.Poster.CroppedPosterURL, "P2URL-1.jpg", "committed state references the canonical preview, never a staged name")
	assert.NotContains(t, stored.Poster.CroppedPosterURL, ".stage-")
}

// Re-from-URL onto an installed pair: the newer generation promotes in place
// — post-commit canonical bytes are the NEW download (prior-generation
// eviction semantics never touch the just-installed pair).
func TestPosterFromURL_NoSelfEviction(t *testing.T) {
	deps, job, router, ts := fromURLFixtureRealJob(t, "P2URL-2")
	w1 := postFromURLRequest(t, router, job, "P2URL-2", ts.URL+"/pic.jpg")
	require.Equal(t, 200, w1.Code, "%s", w1.Body.String())

	dir := filepath.Join("data/temp", "posters", job.GetID())
	first, err := afero.ReadFile(deps.GetFs(), filepath.Join(dir, "P2URL-2-full.jpg"))
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(deps.GetFs(), filepath.Join(dir, "P2URL-2-full.jpg"), []byte("SENTINEL-OLD"), 0o644))

	w2 := postFromURLRequest(t, router, job, "P2URL-2", ts.URL+"/pic.jpg?v=2")
	require.Equal(t, 200, w2.Code, "%s", w2.Body.String())
	second, err2 := afero.ReadFile(deps.GetFs(), filepath.Join(dir, "P2URL-2-full.jpg"))
	require.NoError(t, err2)
	require.NotEqual(t, "SENTINEL-OLD", string(second), "old sentinel evicted; new generation installed")
	require.Equal(t, len(first), len(second), "both downloads are the same fixture image")

	// Again: nothing litters the poster dir after the replacement promote.
	entries, derr := afero.ReadDir(deps.GetFs(), dir)
	require.NoError(t, derr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".stage-", e.Name())
		assert.NotContains(t, e.Name(), ".bak", e.Name())
		assert.NotContains(t, e.Name(), ".promote-", e.Name())
		assert.NotContains(t, e.Name(), ".evict-", e.Name())
	}
}
