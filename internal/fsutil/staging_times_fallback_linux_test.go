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

func TestStagedHandleChtimesOldKernelFallbackKeepsOwnedInode(t *testing.T) {
	for _, oldKernelError := range []error{unix.ENOENT, unix.EINVAL} {
		t.Run(oldKernelError.Error(), func(t *testing.T) {
			dir := t.TempDir()
			stage := filepath.Join(dir, "stage")
			held := filepath.Join(dir, "held")
			file, err := os.Create(stage)
			require.NoError(t, err)
			defer file.Close()
			require.NoError(t, os.Rename(stage, held))
			require.NoError(t, os.WriteFile(stage, []byte("replacement"), 0600))
			untouched := time.Unix(1600000000, 0)
			require.NoError(t, os.Chtimes(stage, untouched, untouched))
			atime, mtime := time.Unix(1650000000, 123), time.Unix(1650000100, 456)
			calls := 0
			err = stagedHandleChtimesWithUtimes(file.Fd(), atime, mtime, func(fd int, path string, times []unix.Timespec, flags int) error {
				calls++
				require.Equal(t, []unix.Timespec{unix.NsecToTimespec(atime.UnixNano()), unix.NsecToTimespec(mtime.UnixNano())}, times)
				if calls == 1 {
					require.Equal(t, int(file.Fd()), fd)
					require.Empty(t, path)
					require.Equal(t, unix.AT_EMPTY_PATH, flags)
					return oldKernelError
				}
				require.Equal(t, unix.AT_FDCWD, fd)
				require.Equal(t, "/proc/self/fd/"+strconv.Itoa(int(file.Fd())), path)
				require.Zero(t, flags)
				// Only the unsupported first operation is simulated. The fallback runs
				// against the real kernel and must still address the held inode.
				return unix.UtimesNanoAt(fd, path, times, flags)
			})
			require.NoError(t, err)
			require.Equal(t, 2, calls)
			var heldStat, replacementStat unix.Stat_t
			require.NoError(t, unix.Stat(held, &heldStat))
			require.NoError(t, unix.Stat(stage, &replacementStat))
			require.Equal(t, atime.UnixNano(), heldStat.Atim.Nano())
			require.Equal(t, mtime.UnixNano(), heldStat.Mtim.Nano())
			require.Equal(t, untouched.UnixNano(), replacementStat.Atim.Nano())
			require.Equal(t, untouched.UnixNano(), replacementStat.Mtim.Nano())
			data, err := os.ReadFile(stage)
			require.NoError(t, err)
			require.Equal(t, "replacement", string(data))
		})
	}
}

func TestStagedHandleChtimesKernelErrors(t *testing.T) {
	for _, tc := range []struct {
		name            string
		first, fallback error
		wantCalls       int
	}{
		{"native success", nil, nil, 1},
		{"permission denied", unix.EPERM, nil, 1},
		{"bad descriptor", unix.EBADF, nil, 1},
		{"missing proc alias", unix.EINVAL, unix.ENOENT, 2},
		{"fallback denied", unix.ENOENT, unix.EACCES, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			timestamp := time.Unix(1650000000, 0)
			err := stagedHandleChtimesWithUtimes(123, timestamp, timestamp, func(int, string, []unix.Timespec, int) error {
				calls++
				if calls == 1 {
					return tc.first
				}
				return tc.fallback
			})
			require.Equal(t, tc.wantCalls, calls)
			want := tc.first
			if tc.wantCalls == 2 {
				want = tc.fallback
			}
			if want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, want)
			}
		})
	}
}
