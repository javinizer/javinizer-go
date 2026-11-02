//go:build windows

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// This is the native primitive's argument-validation contract, not a claim
// that malformed paths pass UnlinkSymlinkVerified's earlier admission checks.
// No kernel or filesystem callbacks are replaced.
func TestVacateLinkObjectWindowsRejectsInvalidNULPaths(t *testing.T) {
	// OPEN_REPARSE_POINT observes the link object itself rather than its target.
	// Close every observation handle before invoking the primitive under test.
	identity := func(t *testing.T, path string) [3]uint32 {
		t.Helper()
		ptr, err := windows.UTF16PtrFromString(path)
		require.NoError(t, err)
		handle, err := windows.CreateFile(ptr, windows.FILE_READ_ATTRIBUTES,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		require.NoError(t, err)
		var info windows.ByHandleFileInformation
		statErr := windows.GetFileInformationByHandle(handle, &info)
		closeErr := windows.CloseHandle(handle)
		require.NoError(t, statErr)
		require.NoError(t, closeErr)
		return [3]uint32{info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow}
	}
	for _, invalidArgument := range []string{"source", "terminal"} {
		t.Run(invalidArgument, func(t *testing.T) {
			root, err := filepath.Abs(t.TempDir())
			require.NoError(t, err)
			target := filepath.Join(root, "target.mkv")
			name := filepath.Join(root, "link.mkv")
			terminal := filepath.Join(root, "occupied-terminal")
			unrelated := filepath.Join(root, "unrelated.keep")
			contents := map[string][]byte{
				target: []byte("target bytes"), terminal: []byte("terminal bytes"), unrelated: []byte("unrelated bytes"),
			}
			for path, data := range contents {
				require.NoError(t, os.WriteFile(path, data, 0o600))
			}
			if err := os.Symlink(target, name); errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
				// Match existing real-symlink fixture capability conventions.
				// A skipped case never counts as native coverage admission.
				t.Skipf("native symlink creation unavailable: %v", err)
			} else {
				require.NoError(t, err)
			}
			payload, err := os.Readlink(name)
			require.NoError(t, err)
			linkID := identity(t, name)
			ids := make(map[string][3]uint32)
			for path := range contents {
				ids[path] = identity(t, path)
			}
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			checkRegularFiles := func() {
				t.Helper()
				for path, data := range contents {
					got, err := os.ReadFile(path)
					require.NoError(t, err)
					require.Equal(t, data, got)
					require.Equal(t, ids[path], identity(t, path))
				}
			}
			checkLink := func(path string) {
				t.Helper()
				info, err := os.Lstat(path)
				require.NoError(t, err)
				require.NotZero(t, info.Mode()&os.ModeSymlink)
				got, err := os.Readlink(path)
				require.NoError(t, err)
				require.Equal(t, payload, got)
				require.Equal(t, linkID, identity(t, path))
			}

			badName, badTerminal := name, terminal
			offending := name + "\x00ignored-suffix"
			if invalidArgument == "source" {
				badName = offending
			} else {
				offending = terminal + "\x00ignored-suffix"
				badTerminal = offending
			}
			err = vacateLinkObjectOsFs(badName, badTerminal)
			require.ErrorIs(t, err, syscall.EINVAL)
			require.ErrorContains(t, err, "symlink vacate MoveFileEx "+offending+": ")
			require.NotErrorIs(t, err, ErrPublishCollision)
			require.NotErrorIs(t, err, os.ErrNotExist)
			checkLink(name)
			checkRegularFiles()
			after, err := os.ReadDir(root)
			require.NoError(t, err)
			require.Equal(t, len(entries), len(after), "invalid input left a residue")
			for i := range entries {
				require.Equal(t, entries[i].Name(), after[i].Name())
			}

			// Recovery moves the same link object, never the target or the
			// occupied terminal whose preservation was just established.
			retryTerminal := filepath.Join(root, "vacant-retry")
			require.NoError(t, vacateLinkObjectOsFs(name, retryTerminal))
			_, err = os.Lstat(name)
			require.ErrorIs(t, err, os.ErrNotExist)
			checkLink(retryTerminal)
			checkRegularFiles()
		})
	}
}
