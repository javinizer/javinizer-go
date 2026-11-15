package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersistWindowStateDirectoryChangedAfterResolution(t *testing.T) {
	dir := withTempUserDataDir(t)
	old := WindowState{Width: 1200, Height: 800}
	next := WindowState{Width: 1600, Height: 1000, Maximised: true}
	require.NoError(t, SaveWindowState(old))
	path, err := windowStatePath()
	require.NoError(t, err)
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	before := snapshotWindowStateFileInfo(t, path)
	data, err := json.MarshalIndent(next, "", "  ")
	require.NoError(t, err)

	// Resolution creates the directory. Model it being moved and replaced
	// before persistence, without racing another goroutine or changing hooks.
	movedDir := dir + "-moved"
	require.NoError(t, os.Rename(dir, movedDir))
	blocker := []byte("another writer replaced the directory")
	require.NoError(t, os.WriteFile(dir, blocker, 0o600))
	created := false
	err = persistWindowState(path, data, func(string, string) (windowStateTempFile, error) {
		created = true
		return nil, errors.New("unexpected staging creation")
	})
	require.ErrorIs(t, err, syscall.ENOTDIR)
	require.ErrorContains(t, err, "desktop: cannot create window state dir "+dir)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	require.Equal(t, "mkdir", pathErr.Op)
	require.Equal(t, dir, pathErr.Path)
	require.False(t, created, "directory preparation must precede staging creation")
	actual, err := os.ReadFile(dir)
	require.NoError(t, err)
	require.Equal(t, blocker, actual)
	assertWindowStateFileUnchanged(t, filepath.Join(movedDir, windowStateFile), original, before)
	assertOnlyWindowStateFile(t, movedDir)

	// Removing the obstruction must be enough to retry the public operation.
	require.NoError(t, os.Remove(dir))
	require.NoError(t, os.Rename(movedDir, dir))
	require.NoError(t, SaveWindowState(next))
	loaded, err := LoadWindowState()
	require.NoError(t, err)
	require.Equal(t, next, loaded)
	assertOnlyWindowStateFile(t, dir)
}

func TestPersistWindowStateStagingFailurePreservesExistingState(t *testing.T) {
	for _, failure := range []string{"close", "chmod_after_close"} {
		t.Run(failure, func(t *testing.T) {
			dir := withTempUserDataDir(t)
			old := WindowState{Width: 1200, Height: 800}
			next := WindowState{Width: 1600, Height: 1000, Maximised: true}
			require.NoError(t, SaveWindowState(old))
			path, err := windowStatePath()
			require.NoError(t, err)
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			before := snapshotWindowStateFileInfo(t, path)
			data, err := json.MarshalIndent(next, "", "  ")
			require.NoError(t, err)

			closeErr := errors.New("staging close failed")
			var stage string
			creates, closes := 0, 0
			err = persistWindowState(path, data, func(gotDir, pattern string) (windowStateTempFile, error) {
				creates++
				require.Equal(t, dir, gotDir, "stage must share the target directory")
				require.Equal(t, ".window-state-*.json", pattern)
				file, err := os.CreateTemp(gotDir, pattern)
				require.NoError(t, err)
				t.Cleanup(func() { _ = file.Close() })
				stage = file.Name()
				return &windowStateFileAfterClose{File: file, afterClose: func() error {
					closes++
					staged, err := os.ReadFile(stage)
					require.NoError(t, err)
					require.Equal(t, data, staged, "complete JSON must be written before closing")
					_, err = file.Write([]byte("must already be closed"))
					require.ErrorIs(t, err, os.ErrClosed)
					if failure == "close" {
						// A close can release its descriptor and still report an I/O
						// error. Keep the real staging file for deferred cleanup.
						return &os.PathError{Op: "close", Path: stage, Err: closeErr}
					}
					// An external removal after close makes the real Chmod fail.
					// No chmod/rename/remove function is replaced in production.
					require.NoError(t, os.Remove(stage))
					return nil
				}}, nil
			})
			require.Equal(t, 1, creates)
			require.Equal(t, 1, closes, "the staging descriptor must be closed exactly once")
			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr)
			require.Equal(t, stage, pathErr.Path)
			if failure == "close" {
				require.ErrorIs(t, err, closeErr)
				require.ErrorContains(t, err, "desktop: cannot close temp window state")
				require.Equal(t, "close", pathErr.Op)
			} else {
				require.ErrorIs(t, err, os.ErrNotExist)
				require.ErrorContains(t, err, "desktop: cannot chmod window state")
				require.Equal(t, "chmod", pathErr.Op)
			}
			_, err = os.Stat(stage)
			require.ErrorIs(t, err, os.ErrNotExist, "failed staging file must not remain")
			assertWindowStateFileUnchanged(t, path, original, before)
			loaded, err := LoadWindowState()
			require.NoError(t, err)
			require.Equal(t, old, loaded)
			assertOnlyWindowStateFile(t, dir)

			require.NoError(t, SaveWindowState(next))
			loaded, err = LoadWindowState()
			require.NoError(t, err)
			require.Equal(t, next, loaded)
			info, err := os.Stat(path)
			require.NoError(t, err)
			if runtime.GOOS != "windows" {
				require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			}
			assertOnlyWindowStateFile(t, dir)
		})
	}
}

// windowStateFileAfterClose delegates all I/O to its real file and alters only
// the selected post-close outcome for this call.
type windowStateFileAfterClose struct {
	*os.File
	afterClose func() error
}

func (f *windowStateFileAfterClose) Close() error {
	if err := f.File.Close(); err != nil {
		return err
	}
	return f.afterClose()
}

// File.Stat captures identity from the open handle. On Windows os.Stat may
// defer identity lookup until SameFile, after a rename or replacement occurred.
func snapshotWindowStateFileInfo(t *testing.T, path string) os.FileInfo {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	info, statErr := file.Stat()
	closeErr := file.Close()
	require.NoError(t, statErr)
	require.NoError(t, closeErr)
	return info
}

func assertWindowStateFileUnchanged(t *testing.T, path string, original []byte, before os.FileInfo) {
	t.Helper()
	actual, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, actual, "failed save must preserve published bytes")
	after := snapshotWindowStateFileInfo(t, path)
	require.True(t, os.SameFile(before, after), "failed save must preserve the published file")
	require.Equal(t, before.Mode().Perm(), after.Mode().Perm())
}

func assertOnlyWindowStateFile(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no staging files should remain")
	require.Equal(t, windowStateFile, entries[0].Name())
}
