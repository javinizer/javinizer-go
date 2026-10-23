//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// This regression is deliberately expected to fail on the current implementation:
// the first acquisition attempt may encounter a pending deletion without having
// first observed an existing-file error. It needs native Windows execution.
func TestAcquireConfigFileLock_WindowsInitialDeletePending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte("config_version: 3\n")
	require.NoError(t, os.WriteFile(path, original, 0600))
	lockPath := path + ".lock"
	require.NoError(t, os.WriteFile(lockPath, []byte(makeConfigLockToken()), 0600))
	name, err := windows.UTF16PtrFromString(lockPath)
	require.NoError(t, err)
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	require.NoError(t, err)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = windows.CloseHandle(handle)
		}
	})
	// The sharing-compatible open handle retains the deleted file until closed.
	require.NoError(t, windows.DeleteFile(name))
	previousOpen := osOpenFileFunc
	t.Cleanup(func() { osOpenFileFunc = previousOpen })
	calls := 0
	var firstErr, closeErr error
	osOpenFileFunc = func(name string, flags int, mode os.FileMode) (*os.File, error) {
		calls++
		file, openErr := previousOpen(name, flags, mode)
		if calls == 1 {
			firstErr = openErr
			closeErr = windows.CloseHandle(handle)
			closed = closeErr == nil
		}
		return file, openErr
	}
	unlock, acquireErr := acquireConfigFileLock(path)
	osOpenFileFunc = previousOpen
	if unlock != nil {
		t.Cleanup(unlock)
	}
	require.NoError(t, closeErr)
	// Verify native evidence before asserting the desired acquisition outcome.
	require.ErrorIs(t, firstErr, windows.ERROR_ACCESS_DENIED)
	require.NoError(t, acquireErr, "a now-released delete-pending lock should permit bounded retry")
	require.GreaterOrEqual(t, calls, 2)
	require.NotNil(t, unlock)
	unlock()
	require.NoFileExists(t, lockPath)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, got)
}
