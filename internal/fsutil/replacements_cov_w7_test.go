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

// fchownCall records one restoreFchown invocation for seam assertions.
// wave-29 (codex P1, PR#215): the seam is FD-scoped — the ownership hand-off
// runs through the open staging handle, never a path.
type fchownCall struct {
	fd  int
	uid int
	gid int
}

// swapRestoreFchown installs a restoreFchown seam capturing every call and
// returning err, restoring the real seam in cleanup.
func swapRestoreFchown(t *testing.T, err error) *[]fchownCall {
	t.Helper()
	calls := new([]fchownCall)
	prev := restoreFchown
	restoreFchown = func(fd, uid, gid int) error {
		*calls = append(*calls, fchownCall{fd: fd, uid: uid, gid: gid})
		return err
	}
	t.Cleanup(func() { restoreFchown = prev })
	return calls
}

// statLessInfo carries a FileInfo whose Sys() is not a *syscall.Stat_t.
type statLessInfo struct {
	os.FileInfo
}

func (statLessInfo) Sys() any { return nil }

// w7OpenStaging opens path read/write as the staging handle the wave-29
// handle-based helpers consume on the real OsFs.
func w7OpenStaging(t *testing.T, path string) *os.File {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = fh.Close() })
	return fh
}

func TestRestoreStagingOwnershipW7_ChownSeamReceivesBackupOwnership(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	backup := filepath.Join(dir, "poster.jpg.dlbak")
	require.NoError(t, os.WriteFile(backup, []byte("original bytes"), 0o600))
	staged := filepath.Join(dir, "poster.jpg.rstr.1")
	require.NoError(t, os.WriteFile(staged, []byte("original bytes"), 0o600))
	stagedHandle := w7OpenStaging(t, staged)

	info, err := os.Stat(backup)
	require.NoError(t, err)
	st, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok, "OsFs FileInfo must expose *syscall.Stat_t on POSIX")

	calls := swapRestoreFchown(t, nil)
	RestoreStagingOwnership(fs, stagedHandle, info)

	require.Equal(t, []fchownCall{{fd: int(stagedHandle.Fd()), uid: int(st.Uid), gid: int(st.Gid)}}, *calls,
		"wave-29: ownership moves through the OPEN HANDLE's descriptor")
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
	stagedHandle := w7OpenStaging(t, staged)

	for _, chownErr := range []error{syscall.EPERM, syscall.EIO} {
		calls := swapRestoreFchown(t, chownErr)
		// Unprivileged restores surface EPERM; unexpected errors surface as
		// anything else. Neither may fail the restore.
		require.NotPanics(t, func() { RestoreStagingOwnership(fs, stagedHandle, info) })
		require.Len(t, *calls, 1, "chown must be attempted exactly once per restore")
		require.Equal(t, int(stagedHandle.Fd()), (*calls)[0].fd, "the attempt is fchown on the open staging handle")
	}

	// The restore flow completes unimpeded once ownership restore returned.
	require.NoError(t, stagedHandle.Close())
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
	stagedHandle, err := fs.OpenFile("/bk/poster.jpg.dlbak", os.O_RDWR, 0)
	require.NoError(t, err)
	defer func() { _ = stagedHandle.Close() }()

	calls := swapRestoreFchown(t, nil)
	RestoreStagingOwnership(fs, stagedHandle, info)
	require.Empty(t, *calls, "in-memory filesystems have no kernel ids; chown must not be attempted")
}

func TestRestoreStagingOwnershipW7_UnhandledSourceSkipsChown(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged := filepath.Join(dir, "poster.jpg.rstr.2")
	require.NoError(t, os.WriteFile(staged, []byte("x"), 0o600))
	stagedHandle := w7OpenStaging(t, staged)

	calls := swapRestoreFchown(t, nil)

	// nil source: defensive — callers always pass a checked openedInfo.
	RestoreStagingOwnership(fs, stagedHandle, nil)
	// FileInfo without a *syscall.Stat_t Sys (non-native sources).
	RestoreStagingOwnership(fs, stagedHandle, statLessInfo{})

	require.Empty(t, *calls)
}
