//go:build !windows

package fsutil

import (
	"os"
	"syscall"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

// restoreFchown is the fd-scoped chown behind RestoreStagingOwnership,
// exposed as a test seam (same discipline as probeRootStat /
// replacementFindProcess): tests record the requested (fd, uid, gid) and
// exercise privilege failures without needing root. wave-29 (codex P1,
// PR#215) replaced the path-based os.Chown seam: a path-based chown on the
// staged name could follow a symlink planted by a directory writer inside
// the staging→publish window, changing an arbitrary target's ownership;
// fchown addresses only the inode the open handle refers to.
var restoreFchown = unix.Fchown

// restoreStagingMode applies a staging file's requested permission bits
// exactly. On the real OsFs the re-assert runs THROUGH THE OPEN HANDLE
// (stagedHandleChmod → fchmod): O_EXCL named the inode but nothing stops a
// directory writer from renaming that name away inside the window, and a
// path-based Chmod would then hit a planted symlink's target (wave-29,
// codex P1). Virtual filesystems (test doubles with no symlink model) take
// the name-based fallback against stagedVirtualModePath. POSIX Chmod is
// authoritative either way, so a failure here must fail the staging attempt
// (the caller closes and removes the staged inode) rather than silently
// restore media with narrowed permissions.
func restoreStagingMode(fs afero.Fs, staged string, fh afero.File, perm os.FileMode) error {
	if of, ok := osStagingHandle(fs, fh); ok {
		return stagedHandleChmod(of, perm)
	}
	return fs.Chmod(stagedVirtualModePath(staged), perm)
}

// RestoreStagingOwnership re-applies the backup's uid/gid to the staged
// inode THROUGH ITS OPEN HANDLE (fchown) before the publish replaces the
// destination. A privileged restore of a backup owned by another account
// must hand the restored bytes back to that owner; otherwise the staged
// inode stays owned by the Javinizer account and the original ownership is
// lost when the backup is deleted.
//
// wave-29 (codex P1, PR#215): the staged handle — never the staged path —
// carries the hand-off, closing the window where a swapped-out staged name
// (a directory writer planting a symlink) would have retargeted the chown
// onto an arbitrary file.
//
// This is strictly best-effort: unprivileged restores cannot chown at all
// (EPERM escalations are ignored), and any other unexpected error is ignored
// too — restore resilience must never be downgraded for metadata fidelity.
// Non-OsFs implementations (MemMapFs in tests) have no kernel ids and are
// skipped without touching their backing store.
func RestoreStagingOwnership(fs afero.Fs, staged afero.File, source os.FileInfo) {
	if source == nil {
		return
	}
	of, ok := osStagingHandle(fs, staged)
	if !ok {
		return
	}
	st, ok := source.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	_ = restoreFchown(int(of.Fd()), int(st.Uid), int(st.Gid))
}
