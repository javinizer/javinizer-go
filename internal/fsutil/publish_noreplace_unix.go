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
	// publishNoReplaceRollbackVerify re-proves — after a successful link(2)
	// and a failed staged-source unlink — that dst still names the
	// just-linked inode (dev/ino via Lstat(dst) vs Stat(src)) BEFORE the
	// rollback unlink touches the name (wave-32, codex local review round 2,
	// PR#215 finding R3). A foreign replacement claimed the destination in
	// the link→unlink window when this answers false, and the rollback must
	// never delete it. Same seam discipline as the link/remove pair: a
	// running host kernel cannot be coerced into the swap-inside-window
	// orderings, so tests replay them here.
	publishNoReplaceRollbackVerify = func(src, dst string) (bool, error) {
		srcInfo, serr := os.Stat(src)
		if serr != nil {
			return false, serr
		}
		dstInfo, derr := os.Lstat(dst)
		if derr != nil {
			return false, derr
		}
		return os.SameFile(srcInfo, dstInfo), nil
	}
	// publishNoReplaceRemoveBound is the staged-source cleanup counterpart of
	// publishNoReplaceRemove (codex P2, PR#215): the SameFile proof on src and
	// the unlink remain separate syscalls unless we re-verify at adjacency.
	// Two consecutive bound proofs pin the same inode; a swap between them or
	// at the unlink is preserved byte-intact and refused typed. Wherever the
	// filesystem refuses to expose identity (memfs) the seam's answer already
	// is the record.
	publishNoReplaceRemoveBound = func(src string, before os.FileInfo) error {
		ok, verr := publishNoReplaceStagedVerify(src, before)
		if verr != nil {
			return verr
		}
		if !ok {
			return fmt.Errorf("no-replace staged source %s no longer names the verified inode: %w", src, ErrTakeAsideForeign)
		}
		ok2, verr2 := publishNoReplaceStagedVerify(src, before)
		if verr2 != nil {
			return verr2
		}
		if !ok2 {
			return fmt.Errorf("no-replace staged source %s changed identity between the bound proofs: %w", src, ErrTakeAsideForeign)
		}
		return publishNoReplaceRemove(src)
	}
	// publishNoReplaceStagedVerify re-proves — after a successful link(2),
	// BEFORE the staged-source pathname unlink — that src still names the
	// object that was linked (wave-33, codex local review round 3, PR#215
	// finding R2): a foreign writer swapping the staged name inside the
	// link→unlink window otherwise gets ITS bytes removed by our staged
	// cleanup. The pre-link Lstat snapshot (no-follow) is compared against a
	// fresh no-follow Lstat by os.SameFile (dev/ino) plus size/mtime — the
	// same binding discipline as publishNoReplaceRollbackVerify — and only a
	// match may unlink. Same seam discipline as the link/remove pair: a
	// running host kernel cannot be coerced into the swap-inside-window
	// ordering, so tests replay it here.
	publishNoReplaceStagedVerify = func(src string, before os.FileInfo) (bool, error) {
		after, err := os.Lstat(src)
		if err != nil {
			return false, err
		}
		if !os.SameFile(before, after) {
			return false, nil
		}
		if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
			// Same inode but re-stamped mid-window: the object was mutated
			// after the link — the keep posture treats it as unproven rather
			// than unlinking something whose tail writes are no longer ours.
			return false, nil
		}
		return true, nil
	}
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
//
// wave-29 (codex P2, PR#215) closes the remaining degrade: ANY other link(2)
// failure (EMLINK, EACCES, EIO, a missing staged source, ...) used to fall
// into the same non-atomic virtual leg on an OsFs, re-introducing the very
// window the fallback exists to close. Every non-EEXIST, non-unsupported link
// failure now refuses TYPED (ErrPublishNoReplaceLinkFailed, original errno
// unwrap-reachable) with NOTHING published and the staged file intact; the
// non-atomic classify-then-rename leg is served exclusively to non-OsFs test
// doubles by PublishNoReplace's dispatch (publish_noreplace.go), never as an
// OsFs degrade.
func publishNoReplaceFallback(src, dst string) error {
	// Wave-33 (codex local review round 3, PR#215 finding R2): capture the
	// staged source's identity BEFORE the link so the staged cleanup below
	// unlinks only the object that was actually linked — never a foreign
	// occupant swapped onto the staged name inside the link→unlink window.
	// When the staged source cannot be identified at all the publish fails
	// closed exactly like a failed link(2) against a missing staged source
	// (nothing linked, nothing unlinked).
	srcIdentity, statErr := os.Lstat(src)
	if statErr != nil {
		return fmt.Errorf("no-replace publish %s -> %s: staged source unidentifiable: %w: %w", src, dst, ErrPublishNoReplaceLinkFailed, statErr)
	}
	if err := publishNoReplaceLink(src, dst); err != nil {
		if os.IsExist(err) {
			return publishCollision(dst)
		}
		if linkUnsupportedClass(err) {
			return fmt.Errorf("no-replace publish %s -> %s: %w: %w", src, dst, ErrPublishNoReplaceUnsupported, err)
		}
		return fmt.Errorf("no-replace publish %s -> %s: %w: %w", src, dst, ErrPublishNoReplaceLinkFailed, err)
	}
	// Wave-33 (finding R2): the destination link already carries the staged
	// bytes, so the publish STANDS regardless of what happens to the staged
	// name now — but the staged-source unlink below must stay bound to the
	// just-linked object. A staged name that no longer names it (foreign swap
	// or mutation in the link→cleanup window) is left BYTE-INTACT and the
	// refusal is typed ErrPublishNoReplaceStagedUnverified joined with
	// ErrPublishCompleted (the destination is provably ours — pending-kind
	// classifiers route the clean/owned-name kind). A staged name that
	// VANISHED on its own completes the cleanup by itself: plain success, no
	// foreign object was ever at risk. An indeterminate reverify proves
	// nothing, so it keeps the name and the same typed refusal.
	verified, verr := publishNoReplaceStagedVerify(src, srcIdentity)
	switch {
	case verr == nil && verified:
		// proven — the staged unlink below removes exactly the linked object
	case verr != nil && os.IsNotExist(verr):
		return nil
	case verr != nil:
		return fmt.Errorf("no-replace publish %s -> %s: staged source reverify indeterminate (%w) — staged name left untouched: %w: %w", src, dst, verr, ErrPublishNoReplaceStagedUnverified, ErrPublishCompleted)
	default:
		return fmt.Errorf("no-replace publish %s -> %s: staged source no longer names the just-linked object (foreign swap or mutation in the link→cleanup window) — staged name left untouched: %w: %w", src, dst, ErrPublishNoReplaceStagedUnverified, ErrPublishCompleted)
	}
	if err := publishNoReplaceRemoveBound(src, srcIdentity); err != nil {
		// The destination link already carries the staged bytes; only the
		// staged cleanup failed. Wave-32 (codex local review round 2, PR#215
		// finding R3): the rollback unlink is BOUND to the just-linked inode —
		// the destination is re-proven against the source's own identity
		// before ANY removal. A foreign replacement (or an unanswerable
		// reverify) in the link→unlink window previously had its bytes
		// deleted by the pathname rollback; the name is now left untouched
		// and the refusal goes typed (never ErrPublishCompleted — the name is
		// not provably ours).
		linked, verr := publishNoReplaceRollbackVerify(src, dst)
		switch {
		case verr != nil && os.IsNotExist(verr):
			// The destination vanished on its own: no foreign bytes are
			// endangered and no publish residue remains. The staged cleanup
			// failure stands alone (no completed marker — nothing is ours).
			return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %w", src, dst, err)
		case verr != nil:
			return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %w (AND the destination reverify was indeterminate: %w) — destination untouched: %w", src, dst, err, verr, ErrPublishNoReplaceRollbackUnverified)
		case !linked:
			return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %w (AND the destination no longer names the just-linked inode — foreign occupant preserved): %w", src, dst, err, ErrPublishNoReplaceRollbackUnverified)
		}
		// Undo the destination link so the publish fails closed with the
		// caller's pre-publish state (staged intact, destination absent)
		// instead of a duplicated inode pair — dst provably still names the
		// staged inode here. If the rollback ITSELF fails the destination
		// name keeps the staged bytes, so the error wraps ErrPublishCompleted
		// (wave-20): compensating callers must classify the name as OWNED,
		// not un-owned.
		if rbErr := publishNoReplaceRemove(dst); rbErr != nil {
			return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %v (AND publish rollback failed: %w): %w", src, dst, err, rbErr, ErrPublishCompleted)
		}
		return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed: %w", src, dst, err)
	}
	return nil
}
