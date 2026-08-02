package poster

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// moveFixture builds a PosterManager over an in-memory fs with both assets of
// the OLD posterID present, returning the fs and the job's poster dir.
func moveFixture(t *testing.T) (*PosterManager, afero.Fs, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/temp", nil)
	dir := filepath.Join("/temp", "posters", "job-move")
	require.NoError(t, afero.WriteFile(fs, dir+"/OLD-1-full.jpg", []byte("full-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dir+"/OLD-1.jpg", []byte("preview-bytes"), 0o644))
	return pm, fs, dir
}

// TestMoveAssets_RenamesBothAssets pins the re-key: each present source asset
// replaces any stale destination file and lands under the new posterID; the
// old key is freed.
func TestMoveAssets_RenamesBothAssets(t *testing.T) {
	pm, fs, dir := moveFixture(t)
	// A stale destination asset must be REPLACED, not kept.
	require.NoError(t, afero.WriteFile(fs, dir+"/NEW-2.jpg", []byte("stale-preview"), 0o644))

	require.NoError(t, pm.MoveAssets("job-move", "OLD-1", "NEW-2"))

	full, err := afero.ReadFile(fs, dir+"/NEW-2-full.jpg")
	require.NoError(t, err)
	assert.Equal(t, "full-bytes", string(full))
	preview, err := afero.ReadFile(fs, dir+"/NEW-2.jpg")
	require.NoError(t, err)
	assert.Equal(t, "preview-bytes", string(preview), "the stale destination asset must be replaced")

	for _, gone := range []string{dir + "/OLD-1-full.jpg", dir + "/OLD-1.jpg"} {
		_, statErr := fs.Stat(gone)
		assert.Error(t, statErr, "the old key must be freed: %s", gone)
	}
}

// TestMoveAssets_AbsentSourceSkipsOrClearsDestination pins the absent-source
// rule: nothing to move, but a stale destination asset for that file is
// still removed so the new key never carries an image no state produced;
// other errors do not fire.
func TestMoveAssets_AbsentSourceSkipsOrClearsDestination(t *testing.T) {
	pm, fs, dir := moveFixture(t)
	// No FULL asset at the old key...
	require.NoError(t, fs.Remove(dir+"/OLD-1-full.jpg"))
	// ...but a stale one at the destination.
	require.NoError(t, afero.WriteFile(fs, dir+"/NEW-2-full.jpg", []byte("stale-full"), 0o644))

	require.NoError(t, pm.MoveAssets("job-move", "OLD-1", "NEW-2"))

	_, err := fs.Stat(dir + "/NEW-2-full.jpg")
	assert.Error(t, err, "a stale destination full asset is removed when the source is absent")
	preview, err := afero.ReadFile(fs, dir+"/NEW-2.jpg")
	require.NoError(t, err)
	assert.Equal(t, "preview-bytes", string(preview), "the present asset still moves")
}

// TestMoveAssets_NoAssetsIsNoOp covers a poster-free movie: nothing exists at
// either key and the move succeeds without creating anything.
func TestMoveAssets_NoAssetsIsNoOp(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "/temp", nil)
	require.NoError(t, pm.MoveAssets("job-move", "NOPE-1", "NOPE-2"))
	entries, err := afero.ReadDir(fs, filepath.Join("/temp", "posters", "job-move"))
	assert.Error(t, err, "no directory materialized for a fully-absent pair")
	_ = entries
}

// TestMoveAssets_Validation pins the ID safety checks and the
// same-key no-op.
func TestMoveAssets_Validation(t *testing.T) {
	pm, fs, dir := moveFixture(t)

	require.NoError(t, pm.MoveAssets("job-move", "OLD-1", "OLD-1"), "an unchanged pair is a no-op")
	full, err := afero.ReadFile(fs, dir+"/OLD-1-full.jpg")
	require.NoError(t, err)
	assert.Equal(t, "full-bytes", string(full), "the no-op left the assets untouched")

	assert.Error(t, pm.MoveAssets("job-move", "", "NEW-2"))
	assert.Error(t, pm.MoveAssets("job-move", "OLD-1", "../evil"))
	assert.Error(t, pm.MoveAssets("../bad-job", "OLD-1", "NEW-2"))
}

// TestMovePosterAssets_Delegates pins the generator seam: with a manager the
// move reaches the filesystem; without one it is a no-op.
func TestMovePosterAssets_Delegates(t *testing.T) {
	pm, fs, dir := moveFixture(t)
	gen := NewScrapePosterGenerator(pm, "", "")
	require.NoError(t, gen.MovePosterAssets("job-move", "OLD-1", "NEW-2"))
	_, err := afero.ReadFile(fs, dir+"/NEW-2-full.jpg")
	require.NoError(t, err, "the delegate must reach the manager's fs")

	bare := NewScrapePosterGenerator(nil, "", "")
	require.NoError(t, bare.MovePosterAssets("job-move", "OLD-1", "NEW-2"),
		"a generator without a manager holds no assets to move")
}

// moveFailFs injects targeted failures into an underlying in-memory fs so
// MoveAssets' per-asset error joins become exercisable. Modes fail on exact
// paths (Stat/MkdirAll/Remove) or on every Rename.
type moveFailFs struct {
	afero.Fs
	statErr   map[string]error
	mkdirErr  map[string]error
	removeErr map[string]error
	renameErr error
}

func (f *moveFailFs) Stat(name string) (os.FileInfo, error) {
	if err, ok := f.statErr[name]; ok {
		return nil, err
	}
	return f.Fs.Stat(name)
}

func (f *moveFailFs) MkdirAll(path string, perm os.FileMode) error {
	if err, ok := f.mkdirErr[path]; ok {
		return err
	}
	return f.Fs.MkdirAll(path, perm)
}

func (f *moveFailFs) Remove(name string) error {
	if err, ok := f.removeErr[name]; ok {
		return err
	}
	return f.Fs.Remove(name)
}

func (f *moveFailFs) Rename(oldname, newname string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	return f.Fs.Rename(oldname, newname)
}

func moveFailFixture(t *testing.T) (fs *moveFailFs, dir string) {
	t.Helper()
	base := afero.NewMemMapFs()
	dir = filepath.Join("/temp", "posters", "job-move")
	require.NoError(t, afero.WriteFile(base, dir+"/OLD-1-full.jpg", []byte("full-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/OLD-1.jpg", []byte("preview-bytes"), 0o644))
	return &moveFailFs{Fs: base}, dir
}

// TestMoveAssets_ErrorBranches pins the per-asset join semantics: a failing
// Stat, MkdirAll, destination-replace, rename, or stale-destination removal
// is reported with its asset context, all failures join (never
// short-circuit), and surviving assets stay untouched.
func TestMoveAssets_ErrorBranches(t *testing.T) {
	t.Run("stat failure", func(t *testing.T) {
		fs, dir := moveFailFixture(t)
		fs.statErr = map[string]error{filepath.Join(dir, "OLD-1-full.jpg"): os.ErrPermission}
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.MoveAssets("job-move", "OLD-1", "NEW-2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stat poster asset OLD-1-full.jpg")
		// The preview asset still moved (no short-circuit).
		_, statErr := afero.ReadFile(fs, dir+"/NEW-2.jpg")
		assert.NoError(t, statErr)
	})

	t.Run("mkdir failure joins both assets", func(t *testing.T) {
		fs, dir := moveFailFixture(t)
		fs.mkdirErr = map[string]error{dir: os.ErrPermission}
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.MoveAssets("job-move", "OLD-1", "NEW-2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "move poster asset directory")
		// nothing moved
		_, readErr := afero.ReadFile(fs, dir+"/OLD-1-full.jpg")
		assert.NoError(t, readErr)
	})

	t.Run("destination replace failure", func(t *testing.T) {
		fs, dir := moveFailFixture(t)
		require.NoError(t, afero.WriteFile(fs.Fs, dir+"/NEW-2-full.jpg", []byte("stale"), 0o644))
		fs.removeErr = map[string]error{filepath.Join(dir, "NEW-2-full.jpg"): os.ErrPermission}
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.MoveAssets("job-move", "OLD-1", "NEW-2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "replace destination poster asset NEW-2-full.jpg")
	})

	t.Run("rename failure joins both assets", func(t *testing.T) {
		fs, _ := moveFailFixture(t)
		fs.renameErr = os.ErrPermission
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.MoveAssets("job-move", "OLD-1", "NEW-2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "move poster asset OLD-1-full.jpg -> NEW-2-full.jpg")
		assert.Contains(t, err.Error(), "move poster asset OLD-1.jpg -> NEW-2.jpg")
	})

	t.Run("stale destination removal failure", func(t *testing.T) {
		fs, dir := moveFailFixture(t)
		// Source full asset absent; the stale destination removal fails.
		require.NoError(t, fs.Fs.Remove(dir+"/OLD-1-full.jpg"))
		fs.removeErr = map[string]error{filepath.Join(dir, "NEW-2-full.jpg"): os.ErrPermission}
		pm := NewPosterManager(fs, "/temp", nil)
		err := pm.MoveAssets("job-move", "OLD-1", "NEW-2")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "remove stale destination poster asset NEW-2-full.jpg")
	})
}
