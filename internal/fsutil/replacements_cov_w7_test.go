//go:build !windows

package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// chownCall records one restoreChown invocation for seam assertions.
type chownCall struct {
	path string
	uid  int
	gid  int
}

// swapRestoreChown installs a restoreChown seam capturing every call and
// returning err, restoring the real seam in cleanup.
func swapRestoreChown(t *testing.T, err error) *[]chownCall {
	t.Helper()
	calls := new([]chownCall)
	prev := restoreChown
	restoreChown = func(path string, uid, gid int) error {
		*calls = append(*calls, chownCall{path: path, uid: uid, gid: gid})
		return err
	}
	t.Cleanup(func() { restoreChown = prev })
	return calls
}

// statLessInfo carries a FileInfo whose Sys() is not a *syscall.Stat_t.
type statLessInfo struct {
	os.FileInfo
}

func (statLessInfo) Sys() any { return nil }

func TestRestoreStagingOwnershipW7_ChownSeamReceivesBackupOwnership(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak")
	require.NoError(t, os.WriteFile(backup, []byte("original bytes"), 0o600))

	info, err := os.Stat(backup)
	require.NoError(t, err)
	st, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok, "OsFs FileInfo must expose *syscall.Stat_t on POSIX")

	staged := filepath.Join(dir, "poster.jpg.rstr.1")
	calls := swapRestoreChown(t, nil)

	RestoreStagingOwnership(fs, staged, info)

	require.Equal(t, []chownCall{{path: staged, uid: int(st.Uid), gid: int(st.Gid)}}, *calls)
}

func TestRestoreStagingOwnershipW7_ChownErrorsAreNonFatalAndRestoreProceeds(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak")
	require.NoError(t, os.WriteFile(backup, []byte("original bytes"), 0o600))
	info, err := os.Stat(backup)
	require.NoError(t, err)

	staged := filepath.Join(dir, "poster.jpg.rstr.1")
	dest := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(staged, []byte("original bytes"), 0o600))

	for _, chownErr := range []error{syscall.EPERM, syscall.EIO} {
		calls := swapRestoreChown(t, chownErr)
		// Unprivileged restores surface EPERM; unexpected errors surface as
		// anything else. Neither may fail the restore.
		require.NotPanics(t, func() { RestoreStagingOwnership(fs, staged, info) })
		require.Len(t, *calls, 1, "chown must be attempted exactly once per restore")
	}

	// The restore flow completes unimpeded once ownership restore returned.
	require.NoError(t, ReplaceFile(fs, staged, dest))
	content, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "original bytes", string(content))
}

func TestRestoreStagingOwnershipW7_NonOsFsSkipsChown(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/bk/poster.jpg.dlbak", []byte("x"), 0o600))
	info, err := fs.Stat("/bk/poster.jpg.dlbak")
	require.NoError(t, err)

	calls := swapRestoreChown(t, nil)
	RestoreStagingOwnership(fs, "/bk/poster.jpg.rstr.1", info)
	require.Empty(t, *calls, "in-memory filesystems have no kernel ids; chown must not be attempted")
}

func TestRestoreStagingOwnershipW7_UnhandledSourceSkipsChown(t *testing.T) {
	fs := afero.NewOsFs()
	calls := swapRestoreChown(t, nil)

	// nil source: defensive — callers always pass a checked openedInfo.
	RestoreStagingOwnership(fs, "/stage", nil)
	// FileInfo without a *syscall.Stat_t Sys (non-native sources).
	RestoreStagingOwnership(fs, "/stage", statLessInfo{})

	require.Empty(t, *calls)
}
