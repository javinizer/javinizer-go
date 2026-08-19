package fsutil

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/afero"
)

// ErrPublishCollision classifies a no-replace publish whose destination was
// occupied by a foreign writer between the caller's existence classification
// and the publish itself (POSTER-WRITE-HARDENING wave-15, codex P2): a plain
// ReplaceFile/rename would destroy the racer's bytes with no backup and no
// ledger entry. Callers reclassify on errors.Is(err, ErrPublishCollision)
// instead of retrying the publish against the same name.
var ErrPublishCollision = errors.New("no-replace publish destination already occupied")

// ErrPublishNoReplaceUnsupported means the destination filesystem cannot
// express an ATOMIC no-replace publish at all — the kernel no-replace
// primitive (renameat2 RENAME_NOREPLACE) is unimplemented/rejected AND
// hard links are unsupported (FAT/exFAT-class media volumes answer link(2)
// with EPERM/ENOSYS/EOPNOTSUPP/ENOTSUP). The pre-wave-17 chain degraded to
// classify-then-rename there, which is NOT atomic: a foreign file created at
// the destination inside the window was silently overwritten, weakening every
// caller's collision guarantee to nothing on exactly the volumes that need it
// (POSTER-WRITE-HARDENING wave-17, codex P2). Callers MUST treat
// errors.Is(err, ErrPublishNoReplaceUnsupported) as a REFUSAL — the same
// conservative fail/kept-with-warn leg they apply to ErrPublishCollision —
// never as license to fall back to replacing semantics.
var ErrPublishNoReplaceUnsupported = errors.New("filesystem cannot express an atomic no-replace publish")

// ErrPublishCompleted classifies a publish error returned AFTER the staged
// bytes were installed at the destination name (wave-20, codex P2, PR#215):
// the POSIX hard-link fallback's staged-source cleanup can fail AFTER
// link(2) created the destination link, and when the destination rollback
// ALSO fails the name stays occupied by the staged bytes even though an
// error returns. Callers compensating a failed publish classify on
// errors.Is(err, ErrPublishCompleted) BEFORE assuming an error means
// "nothing published" — history's backup re-arm treats this class as an
// OWNED name (the restore-pending CLEAN kind: the pending retry removes the
// name), never as an unowned one. The refusal classes never wrap it: a
// refusal installed nothing by definition, so PublishRefusal and
// ErrPublishCompleted are disjoint signals for one publish error.
var ErrPublishCompleted = errors.New("publish completed despite the returned error")

// PublishRefusal reports whether err carries one of the typed no-replace
// REFUSAL classes — ErrPublishCollision (a foreign writer owns the name now)
// or ErrPublishNoReplaceUnsupported (the volume cannot express an atomic
// no-replace publish at all). In both classes nothing was attempted that
// could have touched the occupied bytes, so the name is UNOWNED from the
// caller's perspective: history (re-arm compensation) and the downloader
// (rollback re-arm) share this classifier to decide that a journaled entry
// must never be retried through the occupied/absent path (wave-19, codex P2
// PR#215).
func PublishRefusal(err error) bool {
	return errors.Is(err, ErrPublishCollision) || errors.Is(err, ErrPublishNoReplaceUnsupported)
}

// PublishCompleted reports whether err carries ErrPublishCompleted — a
// publish error returned AFTER the staged bytes were installed at the
// destination name (the POSIX hard-link fallback's staged-source cleanup
// with a failed destination-rollback leg). It is the shared compensation
// classifier pairing with PublishRefusal (wave-21, codex P2, PR#215):
// history's backup re-arm pending-kind classifier and the downloader's
// rollback re-arm mark read one publish error through the same pair —
// PublishRefusal classes left the name FOREIGN/ABSENT (never touched again),
// PublishCompleted left it OWNED by this operation's own bytes (the pending
// retry reaps it), and every other publish error proves nothing (treated as
// unowned).
func PublishCompleted(err error) bool {
	return errors.Is(err, ErrPublishCompleted)
}

// ErrPublishNoReplaceLinkFailed classifies a POSIX hard-link fallback
// failure that is NEITHER an occupied destination (ErrPublishCollision), NOR
// an unsupported-volume refusal (ErrPublishNoReplaceUnsupported), NOR a
// completed publish (ErrPublishCompleted) — EMLINK, EACCES, EIO, a missing
// staged source, and every other unexpected link(2) failure (wave-29, codex
// P2, PR#215). Pre-wave-29 such failures degraded into the NON-ATOMIC
// classify-then-rename virtual leg even for OsFs, silently restoring
// replacing semantics on a kernel-capable volume: a foreign writer occupying
// the destination inside the window was overwritten. These failures now
// refuse with the original link error wrapped behind this sentinel; nothing
// is published, the staged file stays intact, and callers map the class onto
// their existing conservative legs (name unproven → rearm-refused pending /
// rollback-error surfaces) exactly like any other pre-publish failure.
var ErrPublishNoReplaceLinkFailed = errors.New("no-replace hard-link publish failed")

// ErrPublishNoReplaceRollbackUnverified classifies the POSIX hard-link
// fallback's rollback REFUSAL (wave-32, codex local review round 2, PR#215
// finding R3): link(2) landed the staged inode at the destination and the
// staged-source unlink then failed, so the fallback tried to undo the
// destination link by pathname — but the pathname could NO LONGER be
// re-proven to name the just-linked inode (a foreign replacement claimed it
// in the link→unlink window, it vanished, or the reverify answer was
// indeterminate). Pre-wave-32 the rollback deleted whatever the name held,
// destroying a racer's unjournaled bytes. The destination is now left
// BYTE-INTACT in every refusal class, and the error deliberately does NOT
// wrap ErrPublishCompleted — the name is NOT provably this operation's own
// object — so pending-kind classifiers (history's rearmPendingKind, the
// downloader's rollbackRearmPendingKind) route the failure to the
// rearm-refused kind like any other unowned-name failure.
var ErrPublishNoReplaceRollbackUnverified = errors.New("no-replace publish rollback could not re-prove the destination")

// ErrPublishNoReplaceStagedUnverified classifies the POSIX hard-link
// fallback's staged-cleanup REFUSAL (wave-33, codex local review round 3,
// PR#215 finding R2): link(2) already landed the staged inode at the
// destination, but the staged NAME could no longer be re-proven to address
// the just-linked object before its unlink — a foreign writer swapped the
// staged name inside the link→unlink window (or mutated/re-stamped the
// object), or the reverify lookup itself was indeterminate. Pre-wave-33 the
// pathname unlink removed whatever the staged name then held, destroying
// foreign bytes the operation never owned. The staged name is now left
// BYTE-INTACT on every unproven answer; only a staged name that VANISHED on
// its own needs no unlink and reports plain success (the cleanup completed
// itself, no foreign object was ever at risk). The PUBLISHED destination
// stands by construction — the link landed before any of this ran — so the
// error always wraps ErrPublishCompleted alongside this sentinel: the shared
// pending-kind classifiers (PublishCompleted) keep treating the DESTINATION
// name as OWNED (the pending retry reaps it), exactly like the wave-20
// completed-despite-error leg.
var ErrPublishNoReplaceStagedUnverified = errors.New("no-replace publish staged source could not be re-proven after linking")

// publishCollision wraps the destination name in the collision class.
func publishCollision(dst string) error {
	return fmt.Errorf("%w: %s", ErrPublishCollision, dst)
}

// publishNoReplaceVirtual is PublishNoReplace's non-OsFs leg: OsFs routes to
// the platform's kernel no-replace primitive (publish_noreplace_unix.go /
// publish_noreplace_windows.go), while a virtual filesystem can only offer
// classify-then-rename. The Lstat classification refuses an already-occupied
// destination, and — because the classify→rename window cannot be closed
// portably on virtual filesystems — a rename that REFUSES an occupied
// destination (Windows MoveFileW semantics, test seam filesystems playing the
// kernel's role) maps its os.IsExist-class refusal into the same collision
// signal. Callers therefore observe exactly one race classification no matter
// which leg detected it.
func publishNoReplaceVirtual(fs afero.Fs, src, dst string) error {
	var err error
	if ls, ok := fs.(afero.Lstater); ok {
		_, _, err = ls.LstatIfPossible(dst)
	} else {
		_, err = fs.Stat(dst)
	}
	switch {
	case err == nil:
		// Any successful classification — symlink included, Lstat never
		// follows — means the destination is occupied.
		return publishCollision(dst)
	case !os.IsNotExist(err):
		// An indeterminate destination (permissions/IO) fails closed: the
		// publish must not gamble with possibly-present foreign bytes.
		return fmt.Errorf("classify no-replace publish destination %s: %w", dst, err)
	}
	if err := fs.Rename(src, dst); err != nil {
		if os.IsExist(err) {
			return publishCollision(dst)
		}
		return err
	}
	return nil
}
