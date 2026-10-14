//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestAtomicReplace_WindowsSharingViolationPreservesCommittedConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, atomicReplaceFile(path, []byte("committed"), FilePerm))
	before, err := publicationFileInfo(path)
	require.NoError(t, err)
	name, err := windows.UTF16PtrFromString(path)
	require.NoError(t, err)
	// Deliberately omit FILE_SHARE_DELETE: Windows must reject replacement while
	// this reader holds the destination. No injected filesystem errors are used.
	handle, err := windows.CreateFile(name, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL, 0)
	require.NoError(t, err)
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = windows.CloseHandle(handle)
		}
	})

	err = atomicReplaceFile(path, []byte("pending"), FilePerm)
	require.ErrorIs(t, err, windows.ERROR_SHARING_VIOLATION)
	assert.Contains(t, err.Error(), "failed to atomically replace config file")
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "committed", string(got))
	after, err := publicationFileInfo(path)
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed replacement must clean the temp without creating a backup")

	require.NoError(t, windows.CloseHandle(handle))
	closed = true
	require.NoError(t, atomicReplaceFile(path, []byte("published"), FilePerm))
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "published", string(got))
}
