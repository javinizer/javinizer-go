//go:build windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestShortPathRetryPreservesInputWhenTargetDisappears(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.Len(t, filepath.VolumeName(root), 2)
	subtree := `\\?\` + filepath.Join(root, "short-path-retry")
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(subtree)) })
	segment := strings.Repeat("owned-segment-", 6)
	path := filepath.Join(subtree, segment, segment, segment, "marker.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte("original"), 0600))
	calls := 0
	var retryErr error
	got := resolveShortPathNameWith(path, func(input, output *uint16, size uint32) (uint32, error) {
		calls++
		require.Equal(t, path, windows.UTF16PtrToString(input))
		n, callErr := windows.GetLongPathName(input, output, size)
		if calls == 1 {
			require.NoError(t, callErr)
			require.Equal(t, uint32(windows.MAX_PATH), size)
			require.Greater(t, n, size, "real native call must require a larger buffer")
			require.NoError(t, os.Remove(path), "model an external removal before the native retry")
		} else {
			require.Equal(t, 2, calls, "failed resolution must not retry indefinitely")
			require.Greater(t, size, uint32(windows.MAX_PATH))
			require.Error(t, callErr, "retry must observe the real missing file")
			retryErr = callErr
		}
		return n, callErr
	})
	require.Equal(t, 2, calls)
	require.Error(t, retryErr)
	require.Equal(t, path, got, "a partial native buffer must never replace the original path")
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err), "resolution must not recreate the removed target")
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Empty(t, entries)
	// A later independent call can resolve the replacement normally.
	require.NoError(t, os.WriteFile(path, []byte("replacement"), 0600))
	resolved := resolveShortPathName(path)
	contents, err := os.ReadFile(resolved)
	require.NoError(t, err)
	require.Equal(t, "replacement", string(contents))
}
