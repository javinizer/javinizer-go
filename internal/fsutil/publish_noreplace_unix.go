//go:build !windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/afero"
)

// PublishNoReplace atomically (same-filesystem) publishes staged onto dst
// WITHOUT replacing an occupied destination: when dst is taken the publish
// fails with ErrPublishCollision and the existing bytes are never touched.
// OsFs routes to the kernel primitive (renameat2(RENAME_NOREPLACE) on Linux —
// publish_noreplace_linux.go; a hard-link publish elsewhere on POSIX —
// publish_noreplace_otherposix.go); virtual filesystems take the shared
// classify-then-rename leg (publish_noreplace.go).
//
// POSTER-WRITE-HARDENING wave-15 (codex P2): the downloader's create path and
// the history backup re-arm previously published with a bare rename, so a
// foreign writer occupying the destination inside the classify→rename window
// lost its bytes to the rename — no backup, no ledger, reported success.
func PublishNoReplace(fs afero.Fs, src, dst string) error {
	if _, ok := fs.(*afero.OsFs); ok {
		return publishNoReplaceOSFS(src, dst)
	}
	return publishNoReplaceVirtual(fs, src, dst)
}

// publishNoReplaceLink / publishNoReplaceRemove are the fallback's syscall
// pair, exposed as test seams (same discipline as probeRootStat /
// restoreChown): a running host kernel cannot be coerced into the
// link-succeeded-then-staged-unlink-failed orderings (EPERM mid-rollback on
// BOTH legs needs a mid-call permission change), so tests replay them here.
var (
	publishNoReplaceLink   = os.Link
	publishNoReplaceRemove = os.Remove
)

// linkUnsupportedClass reports whether a link(2) failure means the
// FILESYSTEM cannot express hard links at all (FAT/exFAT-class volumes
// answer EPERM on Linux, ENOTSUP/EPERM on Darwin), as opposed to a
// publish-specific refusal. ENOTSUP aliases EOPNOTSUPP on Linux, so both
// are listed for the Darwin spelling. Only these classes justify the
// wave-17 unsupported refusal: any other failure (EACCES, EIO, EMLINK,
// a missing staged source, ...) keeps the pre-existing degrade into the
// classified rename leg, refusing nothing the old behavior accepted.
func linkUnsupportedClass(err error) bool {
	return errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.ENOSYS) ||
		errors.Is(err, syscall.EOPNOTSUPP) ||
		errors.Is(err, syscall.ENOTSUP)
}

// publishNoReplaceFallback publishes via hard link: link(2) fails EEXIST
// atomically when dst is occupied, giving POSIX filesystems without a
// renameat2 wrapper the same no-replace semantics — the destination link and
// the staged source name the same inode, exactly as rename would leave them.
//
// Filesystems without hard-link support (FAT/exFAT media volumes) used to
// degrade to the classified rename leg (Stat-then-Rename), which is NOT
// atomic: a foreign writer occupying dst inside the classify→rename window
// was silently overwritten, collapsing every caller's no-replace guarantee to
// nothing on exactly the volumes that could express only the weak form
// (wave-17, codex P2). When the kernel no-replace primitive ALSO failed
// unsupported-class (or, on non-Linux POSIX, does not exist), the fallback
// now REFUSES with the typed ErrPublishNoReplaceUnsupported instead, and
// callers map it onto the same conservative leg as a collision: the
// operation fails cleanly, the armed/kept posture holds, and no foreign
// bytes are gambled away for a best-effort install.
func publishNoReplaceFallback(src, dst string) error {
	if err := publishNoReplaceLink(src, dst); err != nil {
		if os.IsExist(err) {
			return publishCollision(dst)
		}
		if linkUnsupportedClass(err) {
			return fmt.Errorf("no-replace publish %s -> %s: %w: %w", src, dst, ErrPublishNoReplaceUnsupported, err)
		}
		return publishNoReplaceVirtual(&afero.OsFs{}, src, dst)
	}
	if err := publishNoReplaceRemove(src); err != nil {
		// The destination link already carries the staged bytes; only the
		// staged cleanup failed. Undo the destination link so the publish
		// fails closed with the caller's pre-publish state (staged intact,
		// destination absent) instead of a duplicated inode pair. If the
		// rollback ITSELF fails the destination name keeps the staged bytes,
		// so the error wraps ErrPublishCompleted (wave-20): compensating
		// callers must classify the name as OWNED, not un-owned.
		if rbErr := publishNoReplaceRemove(dst); rbErr != nil {
			return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %v (AND publish rollback failed: %w): %w", src, dst, err, rbErr, ErrPublishCompleted)
		}
		return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %w", src, dst, err)
	}
	return nil
}
