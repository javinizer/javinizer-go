//go:build !windows && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package fsutil

import (
	"time"

	"golang.org/x/sys/unix"
)

// stagedHandleChtimes has no fd-scoped timestamp wrapper for this platform
// in x/sys (solaris/illumos, aix): it reports ENOSYS and ApplyStagingTimes
// takes the name-based Chtimes fallback, matching the pre-wave-29 behavior
// on these non-release platforms.
var stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error {
	return unix.ENOSYS
}
