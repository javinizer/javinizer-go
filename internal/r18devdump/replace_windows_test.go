//go:build windows

package r18devdump

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func replacementFixture(t *testing.T) (src, dst string) {
	t.Helper()
	dir := t.TempDir()
	src, dst = filepath.Join(dir, "new.db"), filepath.Join(dir, "old.db")
	require.NoError(t, os.WriteFile(src, []byte("new dump"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("working dump"), 0o600))
	return src, dst
}

func requireReplacementBytes(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want, string(got))
}

func TestReplaceFileWindowsInvalidPaths(t *testing.T) {
	for _, invalid := range []string{"source", "destination"} {
		t.Run(invalid, func(t *testing.T) {
			src, dst := replacementFixture(t)
			from, to := src, dst
			if invalid == "source" {
				from += "\x00"
			} else {
				to += "\x00"
			}
			require.ErrorIs(t, ReplaceFile(from, to), syscall.EINVAL)
			requireReplacementBytes(t, src, "new dump")
			requireReplacementBytes(t, dst, "working dump")
		})
	}
}

func TestReplaceFileWindowsMissingSourcePreservesDestination(t *testing.T) {
	src, dst := replacementFixture(t)
	require.NoError(t, os.Remove(src))
	err := ReplaceFile(src, dst)
	var linkErr *os.LinkError
	require.ErrorAs(t, err, &linkErr)
	require.Equal(t, "rename", linkErr.Op)
	require.Equal(t, src, linkErr.Old)
	require.Equal(t, dst, linkErr.New)
	require.ErrorIs(t, err, windows.ERROR_FILE_NOT_FOUND)
	require.NoFileExists(t, src)
	requireReplacementBytes(t, dst, "working dump")
}

func TestReplaceFileWindowsSharingFailurePreservesAndRetries(t *testing.T) {
	src, dst := replacementFixture(t)
	name, err := windows.UTF16PtrFromString(dst)
	require.NoError(t, err)
	// A reader without FILE_SHARE_DELETE must prevent replacement. This is a
	// real native handle, rather than a fake closer or injected rename error.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	require.NoError(t, err)
	defer func() {
		if handle != windows.InvalidHandle {
			_ = windows.CloseHandle(handle)
		}
	}()
	err = ReplaceFile(src, dst)
	var linkErr *os.LinkError
	require.ErrorAs(t, err, &linkErr)
	require.Equal(t, "rename", linkErr.Op)
	require.Equal(t, src, linkErr.Old)
	require.Equal(t, dst, linkErr.New)
	requireReplacementBytes(t, src, "new dump")
	requireReplacementBytes(t, dst, "working dump")

	require.NoError(t, windows.CloseHandle(handle))
	handle = windows.InvalidHandle
	require.NoError(t, ReplaceFile(src, dst))
	require.NoFileExists(t, src)
	requireReplacementBytes(t, dst, "new dump")
}
