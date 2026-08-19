//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package fsutil

import (
	"time"

	"golang.org/x/sys/unix"
)

// stagedHandleChtimes sets atime/mtime through the open staged file handle
// (wave-29, codex P1, PR#215). The BSD/Darwin utimensat has no empty-name
// AT_EMPTY_PATH form routed through x/sys, so the fd-scoped futimes carries
// the timestamps — microsecond granularity, which matches every consumer of
// this staging path (media backups restore at second/microsecond precision).
// Exposed as a test seam so tests can record the fd+times.
var stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error {
	tv := []unix.Timeval{unix.NsecToTimeval(atime.UnixNano()), unix.NsecToTimeval(mtime.UnixNano())}
	return unix.Futimes(int(fd), tv)
}
