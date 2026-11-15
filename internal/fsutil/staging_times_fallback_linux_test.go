//go:build linux

package fsutil

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestStagedHandleTimesLegacyKernelFallbackKeepsInodeBoundary(t *testing.T) {
	for _, unsupported := range []error{unix.EINVAL, unix.ENOENT} {
		t.Run(unsupported.Error(), func(t *testing.T) {
			dir := t.TempDir()
			original, moved := filepath.Join(dir, "stage"), filepath.Join(dir, "moved")
			file, err := os.Create(original)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, file.Close()) })
			_, err = file.WriteString("owned bytes")
			require.NoError(t, err)
			require.NoError(t, os.Rename(original, moved))
			require.NoError(t, os.WriteFile(original, []byte("replacement bytes"), 0o600))
			replacementTime := time.Unix(1_600_000_000, 123456789)
			require.NoError(t, os.Chtimes(original, replacementTime, replacementTime))
			atime, mtime := time.Unix(1_650_000_000, 234567891), time.Unix(1_650_000_100, 345678912)
			calls := 0
			err = setStagedHandleTimes(file.Fd(), atime, mtime, func(fd int, path string, times []unix.Timespec, flags int) error {
				calls++
				require.Equal(t, []unix.Timespec{unix.NsecToTimespec(atime.UnixNano()), unix.NsecToTimespec(mtime.UnixNano())}, times)
				if calls == 1 {
					require.Equal(t, int(file.Fd()), fd)
					require.Empty(t, path)
					require.Equal(t, unix.AT_EMPTY_PATH, flags)
					return unsupported
				}
				require.Equal(t, 2, calls)
				require.Equal(t, unix.AT_FDCWD, fd)
				require.Equal(t, "/proc/self/fd/"+strconv.Itoa(int(file.Fd())), path)
				require.Zero(t, flags)
				return unix.UtimesNanoAt(fd, path, times, flags)
			})
			require.NoError(t, err)
			require.Equal(t, 2, calls)
			var ownedStat, replacementStat unix.Stat_t
			require.NoError(t, unix.Stat(moved, &ownedStat))
			require.NoError(t, unix.Stat(original, &replacementStat))
			require.Equal(t, atime.UnixNano(), ownedStat.Atim.Nano())
			require.Equal(t, mtime.UnixNano(), ownedStat.Mtim.Nano())
			require.Equal(t, replacementTime.UnixNano(), replacementStat.Atim.Nano())
			require.Equal(t, replacementTime.UnixNano(), replacementStat.Mtim.Nano())
			owned, err := os.ReadFile(moved)
			require.NoError(t, err)
			require.Equal(t, "owned bytes", string(owned))
			replacement, err := os.ReadFile(original)
			require.NoError(t, err)
			require.Equal(t, "replacement bytes", string(replacement))
		})
	}
}

func TestStagedHandleTimesPreservesSyscallErrors(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second error
		calls         int
	}{
		{"invalid descriptor", unix.EBADF, nil, 1},
		{"permission denied", unix.EPERM, nil, 1},
		{"fallback unavailable", unix.EINVAL, unix.ENOENT, 2},
		{"fallback permission denied", unix.ENOENT, unix.EACCES, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			timestamp := time.Unix(1_650_000_000, 0)
			err := setStagedHandleTimes(42, timestamp, timestamp, func(int, string, []unix.Timespec, int) error {
				calls++
				if calls == 1 {
					return tc.first
				}
				return tc.second
			})
			expected := tc.first
			if tc.calls == 2 {
				expected = tc.second
			}
			require.ErrorIs(t, err, expected)
			require.Equal(t, tc.calls, calls)
		})
	}
}
