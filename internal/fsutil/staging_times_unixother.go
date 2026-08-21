//go:build !windows && !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package fsutil

import (
	"time"

	"golang.org/x/sys/unix"
)

// stagedHandleChtimes has no fd-scoped timestamp wrapper for this platform
// in x/sys (solaris/illumos, aix): it reports ENOSYS. The CloseStaged
// staging tail then falls back to the name-based Chtimes on the STAGED
// name (pre-publish, matching the pre-wave-29 posture); the bound publish
// (r12) instead completes the identity-verified publish with the times
// SKIPPED — the delayed name-based fallback onto the PUBLISHED name kept
// a re-proof→utimens window a directory writer could chase onto a
// substitute, so ErrPublishCompleted is surfaced with the times unapplied
// (destination bytes proven, foreign bytes and metadata untouched).
var stagedHandleChtimes = func(fd uintptr, atime, mtime time.Time) error {
	return unix.ENOSYS
}
