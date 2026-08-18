//go:build !windows

package fsutil

import (
	"os"
	"syscall"

	"github.com/spf13/afero"
)

// restoreChown is the seam behind RestoreStagingOwnership so tests can
// observe the requested uid/gid and exercise privilege failures without
// needing root (same style as probeRootStat / replacementFindProcess).
var restoreChown = os.Chown

// restoreStagingMode applies a staging file's requested permission bits
// exactly. POSIX Chmod is authoritative, so a failure here must fail the
// staging attempt (the caller closes and removes the staged inode) rather
// than silently restore media with narrowed permissions.
func restoreStagingMode(fs afero.Fs, path string, perm os.FileMode) error {
	return fs.Chmod(path, perm)
}

// RestoreStagingOwnership re-applies the backup's uid/gid to the staged
// inode before the swap replaces the destination. A privileged revert of a
// backup owned by another account must hand the restored bytes back to that
// owner; otherwise the staged inode stays owned by the Javinizer account and
// the original ownership is lost when the backup is deleted.
//
// This is strictly best-effort: unprivileged restores cannot chown at all
// (EPERM escalations are ignored), and any other unexpected error is ignored
// too — restore resilience must never be downgraded for metadata fidelity.
// Non-OsFs implementations (MemMapFs in tests) have no kernel ids and are
// skipped without touching their backing store.
func RestoreStagingOwnership(fs afero.Fs, staged string, source os.FileInfo) {
	if source == nil {
		return
	}
	if _, ok := fs.(*afero.OsFs); !ok {
		return
	}
	st, ok := source.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	_ = restoreChown(staged, int(st.Uid), int(st.Gid))
}
