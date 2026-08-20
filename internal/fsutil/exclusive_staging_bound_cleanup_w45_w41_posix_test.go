//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING wave-45 (codex P2, PR#215 finding F3), POSIX legs —
// the failed staging cleanup binds its unlink to the OPENED INODE
// (discardFailedExclusiveStaging): the stagedHandleChmod seam plants the
// window deterministically (rename-away + substitute) right as the strict
// mode re-assert "fails".

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

var errW45Chmod = errors.New("injected w45 handle chmod failure")

func w45SwapChmodSeam(t *testing.T, plant func(name string)) {
	t.Helper()
	prev := stagedHandleChmod
	stagedHandleChmod = func(f *os.File, _ os.FileMode) error {
		if plant != nil {
			plant(f.Name())
		}
		return errW45Chmod
	}
	t.Cleanup(func() { stagedHandleChmod = prev })
}

// Unmodified staged object: the bound cleanup proves the staged name still
// addresses the handle's inode and unlinks exactly that object.
func TestCreateExclusiveStagingFileW45_UnsubstitutedStagedIsRemoved(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	w45SwapChmodSeam(t, nil)

	staged, file, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "poster.jpg"), ".rstr", 1, 0o666)
	require.Error(t, err)
	require.ErrorIs(t, err, errW45Chmod)
	require.Empty(t, staged)
	require.Nil(t, file)

	entries, rerr := os.ReadDir(dir)
	require.NoError(t, rerr)
	require.Empty(t, entries, "the verified-own staging name is removed")
}

// Substitute planted at the staged name: the handle fstat vs Lstat verdict
// diverges, so the foreign object is preserved byte-intact and the renamed
// original stays recoverable.
func TestCreateExclusiveStagingFileW45_SubstitutePreserved(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	staged := dest + ".rstr.1"
	w45SwapChmodSeam(t, func(name string) {
		require.Equal(t, staged, name)
		require.NoError(t, os.Rename(name, name+".hidden"))
		require.NoError(t, os.WriteFile(name, []byte("foreign planted substitute"), 0o644))
	})

	_, file, err := CreateExclusiveStagingFile(fs, dest, ".rstr", 1, 0o666)
	require.Error(t, err)
	require.ErrorIs(t, err, errW45Chmod)
	require.Nil(t, file)

	kept, rerr := os.ReadFile(staged)
	require.NoError(t, rerr)
	require.Equal(t, "foreign planted substitute", string(kept),
		"a substitute rotated onto the staged name is never unlinked")
	hidden, herr := os.ReadFile(staged + ".hidden")
	require.NoError(t, herr)
	require.Empty(t, hidden, "the renamed-aside original is left recoverable")
}

// Staged name vacated (renamed away, nothing replanted): the lookup is
// indeterminate — the leg refuses to touch anything and still surfaces the
// typed mode error.
func TestCreateExclusiveStagingFileW45_VacatedNameTouchesNothing(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	w45SwapChmodSeam(t, func(name string) {
		require.NoError(t, os.Rename(name, name+".hidden"))
	})

	_, _, err := CreateExclusiveStagingFile(fs, dest, ".rstr", 1, 0o666)
	require.Error(t, err)
	require.ErrorIs(t, err, errW45Chmod)

	_, lerr := os.Lstat(dest + ".rstr.1")
	require.True(t, os.IsNotExist(lerr), "the vacated name never materializes")
	entries, rerr := os.ReadDir(dir)
	require.NoError(t, rerr)
	require.Len(t, entries, 1, "only the renamed-aside original remains")
	require.Equal(t, "poster.jpg.rstr.1.hidden", entries[0].Name())
}
