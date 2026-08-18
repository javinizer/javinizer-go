//go:build !windows

package fsutil

import (
	"fmt"
	"os"

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

// publishNoReplaceFallback publishes via hard link: link(2) fails EEXIST
// atomically when dst is occupied, giving POSIX filesystems without a
// renameat2 wrapper the same no-replace semantics — the destination link and
// the staged source name the same inode, exactly as rename would leave them.
// Filesystems without hard-link support (FAT/exFAT media volumes) degrade to
// the classified rename leg: no stricter than the pre-hardening publish
// there, where their rename could never express no-replace anyway.
func publishNoReplaceFallback(src, dst string) error {
	if err := publishNoReplaceLink(src, dst); err != nil {
		if os.IsExist(err) {
			return publishCollision(dst)
		}
		return publishNoReplaceVirtual(&afero.OsFs{}, src, dst)
	}
	if err := publishNoReplaceRemove(src); err != nil {
		// The destination link already carries the staged bytes; only the
		// staged cleanup failed. Undo the destination link so the publish
		// fails closed with the caller's pre-publish state (staged intact,
		// destination absent) instead of a duplicated inode pair.
		if rbErr := publishNoReplaceRemove(dst); rbErr != nil {
			return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %v (AND publish rollback failed: %w)", src, dst, err, rbErr)
		}
		return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %w", src, dst, err)
	}
	return nil
}
