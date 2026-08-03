package poster

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// copyFixture builds a PosterManager over an in-memory fs with both assets of
// the OLD posterID present (the "refreshed" key) plus a STALE asset at the
// NEW key — the case-variant alias the rescrape mirror converges onto.
func copyFixture(t *testing.T) (*PosterManager, afero.Fs, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/temp", nil)
	dir := filepath.Join("/temp", "posters", "job-copy")
	require.NoError(t, afero.WriteFile(fs, dir+"/SRC-1-full.jpg", []byte("fresh-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dir+"/SRC-1.jpg", []byte("fresh-preview"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dir+"/ALIAS-1-full.jpg", []byte("stale-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dir+"/ALIAS-1.jpg", []byte("stale-preview"), 0o644))
	return pm, fs, dir
}

// TestCopyAssets_CopiesBothAssetsKeepingSource pins the alias refresh: the
// variant's raw key converges onto the refreshed bytes AND the source key is
// preserved (unlike MoveAssets, which re-keys). MemMapFs is case-SENSITIVE —
// the exact filesystem class the Codex P2 bug needs distinct files for.
func TestCopyAssets_CopiesBothAssetsKeepingSource(t *testing.T) {
	pm, fs, dir := copyFixture(t)

	require.NoError(t, pm.CopyAssets("job-copy", "SRC-1", "ALIAS-1"))

	for _, asset := range []string{"ALIAS-1-full.jpg", "ALIAS-1.jpg"} {
		got, err := afero.ReadFile(fs, dir+"/"+asset)
		require.NoError(t, err)
		assert.Contains(t, string(got), "fresh-", "the alias key must converge onto the refreshed bytes: %s", asset)
	}
	for _, asset := range []string{"SRC-1-full.jpg", "SRC-1.jpg"} {
		_, err := fs.Stat(dir + "/" + asset)
		assert.NoError(t, err, "the source key is preserved (this is a copy, not a move): %s", asset)
	}
}

// TestCopyAssets_AbsentSourceClearsStaleDestination pins the absent-source
// rule (parity with MoveAssets): nothing to copy, but a stale destination
// asset for that file is still removed so the variant key never carries an
// image no persisted state produced.
func TestCopyAssets_AbsentSourceClearsStaleDestination(t *testing.T) {
	pm, fs, dir := copyFixture(t)
	require.NoError(t, fs.Remove(dir+"/SRC-1-full.jpg"))

	require.NoError(t, pm.CopyAssets("job-copy", "SRC-1", "ALIAS-1"))

	_, err := fs.Stat(dir + "/ALIAS-1-full.jpg")
	assert.Error(t, err, "a stale destination full asset is removed when the source is absent")
	preview, err := afero.ReadFile(fs, dir+"/ALIAS-1.jpg")
	require.NoError(t, err)
	assert.Equal(t, "fresh-preview", string(preview), "the present asset still copies")
}

// TestCopyAssets_NoAssetsIsNoOp covers a poster-free variant pair: nothing
// exists at either key and the copy succeeds without creating anything.
func TestCopyAssets_NoAssetsIsNoOp(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/temp", nil)
	require.NoError(t, pm.CopyAssets("job-copy", "NOPE-1", "NOPE-2"))
	_, err := afero.ReadDir(fs, filepath.Join("/temp", "posters", "job-copy"))
	assert.Error(t, err, "no directory materialized for a fully-absent pair")
}

// TestCopyAssets_Validation pins the ID safety checks and the same-key no-op.
func TestCopyAssets_Validation(t *testing.T) {
	pm, fs, dir := copyFixture(t)

	require.NoError(t, pm.CopyAssets("job-copy", "SRC-1", "SRC-1"), "an unchanged pair is a no-op")
	stale, err := afero.ReadFile(fs, dir+"/ALIAS-1-full.jpg")
	require.NoError(t, err)
	assert.Equal(t, "stale-full", string(stale), "the no-op left the alias untouched")

	assert.Error(t, pm.CopyAssets("job-copy", "", "ALIAS-1"))
	assert.Error(t, pm.CopyAssets("job-copy", "SRC-1", "../evil"))
	assert.Error(t, pm.CopyAssets("../bad-job", "SRC-1", "ALIAS-1"))
}

// copyFailFs injects targeted failures into an underlying in-memory fs so
// CopyAssets' per-asset error joins become exercisable.
type copyFailFs struct {
	afero.Fs
	mkdirErr    map[string]error
	removeErr   map[string]error
	openErr     map[string]error
	openFileErr map[string]error
}

func (f *copyFailFs) MkdirAll(path string, perm os.FileMode) error {
	if err, ok := f.mkdirErr[path]; ok {
		return err
	}
	return f.Fs.MkdirAll(path, perm)
}

func (f *copyFailFs) Remove(name string) error {
	if err, ok := f.removeErr[name]; ok {
		return err
	}
	return f.Fs.Remove(name)
}

func (f *copyFailFs) Open(name string) (afero.File, error) {
	if err, ok := f.openErr[name]; ok {
		return nil, err
	}
	return f.Fs.Open(name)
}

func (f *copyFailFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if err, ok := f.openFileErr[name]; ok {
		return nil, err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func copyFailFixture(t *testing.T) (fs *copyFailFs, dir string) {
	t.Helper()
	base := afero.NewMemMapFs()
	dir = filepath.Join("/temp", "posters", "job-copy")
	require.NoError(t, afero.WriteFile(base, dir+"/SRC-1-full.jpg", []byte("fresh-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/SRC-1.jpg", []byte("fresh-preview"), 0o644))
	return &copyFailFs{Fs: base}, dir
}

// TestCopyAssets_ErrorBranches pins the per-asset join semantics: a failing
// source read, MkdirAll, destination write, or stale-destination removal is
// reported with its asset context and all failures join (never
// short-circuit).
func TestCopyAssets_ErrorBranches(t *testing.T) {
	t.Run("source read failure joins and continues", func(t *testing.T) {
		fs, dir := copyFailFixture(t)
		fs.openErr = map[string]error{filepath.Join(dir, "SRC-1-full.jpg"): os.ErrPermission}
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.CopyAssets("job-copy", "SRC-1", "ALIAS-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read poster asset SRC-1-full.jpg")
		// The preview asset still copied (no short-circuit).
		got, statErr := afero.ReadFile(fs, dir+"/ALIAS-1.jpg")
		require.NoError(t, statErr)
		assert.Equal(t, "fresh-preview", string(got))
	})

	t.Run("mkdir failure joins both assets", func(t *testing.T) {
		fs, dir := copyFailFixture(t)
		fs.mkdirErr = map[string]error{dir: os.ErrPermission}
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.CopyAssets("job-copy", "SRC-1", "ALIAS-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "copy poster asset directory")
	})

	t.Run("destination write failure joins both assets", func(t *testing.T) {
		fs, dir := copyFailFixture(t)
		// WriteFile OpenFile-creates the destination: fail the CREATE for
		// both destination names.
		fs.openFileErr = map[string]error{
			filepath.Join(dir, "ALIAS-1-full.jpg"): os.ErrPermission,
			filepath.Join(dir, "ALIAS-1.jpg"):      os.ErrPermission,
		}
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.CopyAssets("job-copy", "SRC-1", "ALIAS-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "copy poster asset SRC-1-full.jpg -> ALIAS-1-full.jpg")
		assert.Contains(t, err.Error(), "copy poster asset SRC-1.jpg -> ALIAS-1.jpg", "both asset failures join")
	})

	t.Run("stale destination removal failure", func(t *testing.T) {
		fs, dir := copyFailFixture(t)
		require.NoError(t, fs.Remove(dir+"/SRC-1-full.jpg")) // absent source
		require.NoError(t, afero.WriteFile(fs, dir+"/ALIAS-1-full.jpg", []byte("stale"), 0o644))
		fs.removeErr = map[string]error{filepath.Join(dir, "ALIAS-1-full.jpg"): errors.New("locked")}
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.CopyAssets("job-copy", "SRC-1", "ALIAS-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remove stale destination poster asset ALIAS-1-full.jpg")
	})
}

// TestCopyPosterAssets_Delegates pins the generator seam: with a manager the
// copy reaches the filesystem; without one it is a no-op.
func TestCopyPosterAssets_Delegates(t *testing.T) {
	pm, fs, dir := copyFixture(t)
	gen := NewScrapePosterGenerator(pm, "", "")
	require.NoError(t, gen.CopyPosterAssets("job-copy", "SRC-1", "ALIAS-1"))
	got, err := afero.ReadFile(fs, dir+"/ALIAS-1-full.jpg")
	require.NoError(t, err, "the delegate must reach the manager's fs")
	assert.Equal(t, "fresh-full", string(got))

	bare := NewScrapePosterGenerator(nil, "", "")
	require.NoError(t, bare.CopyPosterAssets("job-copy", "SRC-1", "ALIAS-1"),
		"a generator without a manager holds no assets to copy")
}
