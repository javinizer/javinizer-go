//go:build windows

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestVacateLinkObjectWindowsSharingRefusalPreservesLinkAndAllowsRetry(t *testing.T) {
	root := t.TempDir()
	target, name, terminal := filepath.Join(root, "target.mkv"), filepath.Join(root, "link.mkv"), filepath.Join(root, "vacated-link")
	require.NoError(t, os.WriteFile(target, []byte("target bytes must remain unchanged"), 0o600))
	if err := os.Symlink(target, name); errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
		t.Skipf("native symlink creation unavailable: %v", err)
	} else {
		require.NoError(t, err)
	}
	payload, err := os.Readlink(name)
	require.NoError(t, err)
	targetBefore, err := os.Stat(target)
	require.NoError(t, err)
	ptr, err := windows.UTF16PtrFromString(name)
	require.NoError(t, err)
	// Open the reparse point itself, not its target, and intentionally omit
	// FILE_SHARE_DELETE. The native kernel must refuse renaming this observed
	// link while the handle is alive; no syscall or filesystem seam is replaced.
	handle, err := windows.CreateFile(ptr, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	require.NoError(t, err)
	t.Cleanup(func() {
		if handle != windows.InvalidHandle {
			require.NoError(t, windows.CloseHandle(handle))
		}
	})
	var before windows.ByHandleFileInformation
	require.NoError(t, windows.GetFileInformationByHandle(handle, &before))

	err = vacateLinkObjectOsFs(name, terminal)
	require.ErrorIs(t, err, windows.ERROR_SHARING_VIOLATION)
	require.ErrorContains(t, err, "symlink vacate MoveFileEx "+name+" -> "+terminal+": ")
	require.NotErrorIs(t, err, ErrPublishCollision)
	require.NotErrorIs(t, err, os.ErrNotExist)
	info, err := os.Lstat(name)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
	gotPayload, err := os.Readlink(name)
	require.NoError(t, err)
	require.Equal(t, payload, gotPayload)
	_, err = os.Lstat(terminal)
	require.ErrorIs(t, err, os.ErrNotExist, "a refused move must not publish a destination")

	require.NoError(t, windows.CloseHandle(handle))
	handle = windows.InvalidHandle
	require.NoError(t, vacateLinkObjectOsFs(name, terminal), "closing the observer must allow the same move to succeed")
	_, err = os.Lstat(name)
	require.ErrorIs(t, err, os.ErrNotExist)
	gotPayload, err = os.Readlink(terminal)
	require.NoError(t, err)
	require.Equal(t, payload, gotPayload)
	ptr, err = windows.UTF16PtrFromString(terminal)
	require.NoError(t, err)
	handle, err = windows.CreateFile(ptr, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	require.NoError(t, err)
	var after windows.ByHandleFileInformation
	require.NoError(t, windows.GetFileInformationByHandle(handle, &after))
	require.Equal(t, before.VolumeSerialNumber, after.VolumeSerialNumber)
	require.Equal(t, before.FileIndexHigh, after.FileIndexHigh)
	require.Equal(t, before.FileIndexLow, after.FileIndexLow, "retry moves the original link object")
	targetAfter, err := os.Stat(target)
	require.NoError(t, err)
	require.True(t, os.SameFile(targetBefore, targetAfter), "link vacating must not replace its target")
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "target bytes must remain unchanged", string(data))
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 2, "only the original target and vacated link remain")
}
