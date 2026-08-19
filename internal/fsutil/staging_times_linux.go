//go:build linux

package fsutil

import (
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// stagedHandleChtimes sets atime/mtime through the open staged file handle
// (wave-29, codex P1, PR#215): utimensat with an empty pathname plus
// AT_EMPTY_PATH is the fd-scoped form (Linux ≥ 4.8, the same combination
// os.samefile-era code uses for Fstatat); kernels that reject the empty-name
// form fall back to the /proc/self/fd alias of the descriptor, exactly the
// route x/sys/unix's own Futimes takes on Linux. Exposed as a test seam so
// tests can record the fd+times and replay kernel failures.
var stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error {
	ts := []unix.Timespec{unix.NsecToTimespec(atime.UnixNano()), unix.NsecToTimespec(mtime.UnixNano())}
	if err := unix.UtimesNanoAt(int(fd), "", ts, unix.AT_EMPTY_PATH); err == nil {
		return nil
	} else if err == unix.ENOENT || err == unix.EINVAL {
		// Pre-AT_EMPTY_PATH kernels: address the inode through its
		// /proc/self/fd alias instead (glibc's futimens fallback does the
		// same). The alias names THIS process's descriptor, never the staged
		// directory name, so a swapped staged path still cannot redirect it.
		return unix.UtimesNanoAt(unix.AT_FDCWD, "/proc/self/fd/"+strconv.Itoa(int(fd)), ts, 0)
	} else {
		return err
	}
}
