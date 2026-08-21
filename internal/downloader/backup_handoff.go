package downloader

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// POSTER-WRITE-HARDENING wave-37/wave-38 (codex P2, PR#215) — the
// reserved-backup handoff must be ATOMIC where the platform offers the
// primitive and CONDITIONAL everywhere else: no leg ever separates "verify
// the reservation still ours" from "rename dest onto the reservation name"
// by two pathname syscalls a foreign writer could interpose (wave-38 finding
// F2).
//
// The wave-36/37 verify-then-rename left windows open around the
// dest→backup move (install_overwrite.go:772):
//
//   - OVERWRITE window: after overwriteBackupReservationStillOurs proved the
//     reservation still ours, the replacing rename ran as a SEPARATE
//     pathname op; a foreign writer replacing the reservation in between had
//     its bytes silently overwritten by the rename (POSIX) or the replacing
//     ReplaceFile (Windows).
//   - CLEANUP-UNLINK window: when the move failed, the caller's removal of
//     the backup name unlinked whatever occupied it by then — including the
//     foreign occupant planted in that window.
//
// The closed variants, by platform:
//
//   - Linux/OsFs (backup_handoff_linux.go): renameat2(RENAME_EXCHANGE) swaps
//     the dest and reservation dentries atomically — there is NO
//     verify→rename interposition at all. The placeholder the exchange parks
//     at dest is then taken aside + unlinked through the wave-38 bound
//     take-aside (dest→scratch no-replace take, identity re-proof at the
//     scratch name, unlink bound to the claim at unlink time).
//   - Everywhere else (non-Linux POSIX incl. Darwin, Windows, every virtual
//     filesystem) — the wave-38 CONDITIONAL take-aside order below: the
//     reservation placeholder is taken ASIDE first (backupPath→scratch onto
//     a fresh O_EXCL-reserved quarantine name, replace-aware only against
//     OUR OWN placeholder; proof afterwards: Lstat(backupPath) must be
//     ENOENT AND the object at the scratch name must still be the claim's
//     identity); then dest→backupPath moves NO-REPLACE (fsutil.PublishNoReplace
//     — a racer reclaiming the freed backup name mid-window is the typed
//     collision class, its plant preserved, the scratch restored back);
//     then ONLY the scratch name is unlinked, re-bound against the claim
//     identity at unlink time. Every wedge leg's compensation restores
//     original names with no-replace moves.

// handoffViaVerifiedRename is the conditional handoff for platforms without
// an atomic exchange primitive (wave-38 finding F2): the move of dest onto
// the reserved backup name can never overwrite a foreign occupant because
// the reservation placeholder is PROVABLY absent from that name first
// (taken aside onto the caller-provable scratch), and the dest move itself
// runs NO-REPLACE.
func handoffViaVerifiedRename(fsys afero.Fs, destPath, backupPath string, claim os.FileInfo) error {
	// Syscall-adjacency re-derivation — the take-aside is attempted ONLY
	// while the backup name still addresses THE claimed placeholder. Any
	// divergence is the typed collision class and the foreign occupant is
	// never moved/renamed over.
	if verErr := overwriteBackupReservationStillOurs(fsys, backupPath, claim); verErr != nil {
		return verErr
	}
	// Take the placeholder ASIDE onto a fresh reserved quarantine sibling
	// (the downloader quarantine claim discipline — O_EXCL reservation plus
	// captured identity). The take re-proves BOTH the scratch reservation
	// (still our claim, pre-move) and the landed object (still our claim
	// placeholder, post-move); every wedge compensation inside the
	// no-replace take keeps foreign bytes intact.
	scratch, scratchClaim, cerr := claimRollbackQuarantineName(fsys, backupPath)
	if cerr != nil {
		releaseClaimedReservation(fsys, backupPath, claim)
		return fmt.Errorf("reserved backup handoff (take-aside scratch claim): %w", cerr)
	}
	hold, terr := fsutil.TakeAside(fsutil.TakeAsideSpec{
		FS:      fsys,
		Src:     backupPath,
		Scratch: scratch,
		Claim:   scratchClaim,
		Prove: func(moved os.FileInfo) error {
			if !destPlaceholderMatchesClaim(moved, claim) {
				return fmt.Errorf("object taken aside from %s is not the claimed reservation placeholder — foreign bytes preserved: %w", backupPath, fsutil.ErrPublishCollision)
			}
			return nil
		},
	})
	if terr != nil {
		releaseClaimedReservation(fsys, backupPath, claim)
		return fmt.Errorf("reserved backup handoff (take-aside of the reservation): %w", terr)
	}
	// The codex-specified proof after the take: Lstat(backupPath) must be
	// ENOENT (the take freed the name) — a racer reclaiming it mid-window is
	// the typed collision class, its plant preserved, the placeholder
	// restored back NO-REPLACE.
	if _, lerr := lstatBackupCandidate(fsys, backupPath); lerr == nil {
		// A racer reclaimed the freed reservation name between the take and
		// the proof: typed collision, plant preserved, placeholder restored
		// no-replace where the name is still free (a collision there keeps the
		// foreign claimant byte-intact and strands only our own placeholder).
		rerr := hold.Restore()
		if rerr == nil {
			releaseClaimedReservation(fsys, backupPath, claim)
		}
		return errors.Join(
			fmt.Errorf("reserved backup handoff: %s re-occupied between the take-aside and the source-freedom proof (plant preserved): %w", backupPath, fsutil.ErrPublishCollision),
			rerr,
		)
	} else if !os.IsNotExist(lerr) {
		rerr := hold.Restore()
		if rerr == nil {
			releaseClaimedReservation(fsys, backupPath, claim)
		}
		return errors.Join(
			fmt.Errorf("reserved backup handoff: %s indeterminate after the take-aside: %w", backupPath, lerr),
			rerr,
		)
	}
	// The reservation name is provably FREE: move dest onto it NO-REPLACE.
	// On any failure (collision → the plant is preserved; kernel/IO →
	// nothing moved), restore the placeholder from the scratch, and release
	// it by the claimed-placeholder binding exactly as the wave-37 cleanup
	// did (foreign-swapped answers are preserved byte-intact there).
	if moveErr := fsutil.PublishNoReplace(fsys, destPath, backupPath); moveErr != nil {
		rerr := hold.Restore()
		if rerr == nil {
			releaseClaimedReservation(fsys, backupPath, claim)
		}
		return errors.Join(fmt.Errorf("reserved backup handoff (no-replace move of the destination): %w", moveErr), rerr)
	}
	// Handoff achieved: only the scratch name is unlinked, re-bound against
	// the taken-aside placeholder identity at unlink time. A wedged unlink
	// leaves the inert 0-byte quarantine sibling (sweeps never arbitrate
	// .dlq. names as set-asides) — the handoff stands.
	if uerr := hold.Unlink(); uerr != nil {
		logging.Warnf("downloader: take-aside release of the backup reservation placeholder at %s failed: %v — inert scratch retained for manual cleanup", hold.Scratch(), uerr)
	}
	return nil
}

// releaseClaimedReservation unlinks a still-claimed backup reservation after
// a failed handoff — bound to the claim's identity (wave-37 (ii)): only the
// verified 0-byte placeholder this operation reserved may be removed. A
// reservation that VANISHED on its own completed the cleanup by itself (no
// foreign bytes were ever at risk). Any other answer — foreign occupant,
// identity mismatch, indeterminate lookup — is a REFUSAL: the name is left
// byte-intact and the install surfaces the handoff failure with the occupant
// preserved.
func releaseClaimedReservation(fsys afero.Fs, backupPath string, claim os.FileInfo) {
	err := overwriteBackupReservationStillOurs(fsys, backupPath, claim)
	if err == nil {
		// Codex P2 (wave-59): the check itself is a snapshot — re-prove the
		// reservation identity at adjacency to the unlink (vacate-verify
		// class: SameFile against the claim AND against the first proof).
		// Any doubt preserves the occupant, so nothing foreign is ever
		// deleted even under a wedged FS or a racing replacement.
		reproof, rerr := lstatBackupCandidate(fsys, backupPath)
		if rerr != nil || !destPlaceholderMatchesClaim(reproof, claim) {
			logging.Warnf("downloader: failed set-aside cleanup of %s refused — the reservation no longer provably names our claimed placeholder between proof and unlink (%v); the occupant is left byte-intact", backupPath, rerr)
			return
		}
		reproof2, rerr2 := lstatBackupCandidate(fsys, backupPath)
		if rerr2 != nil || !destPlaceholderMatchesClaim(reproof2, claim) || reproof2.ModTime() != reproof.ModTime() || reproof2.Size() != reproof.Size() {
			logging.Warnf("downloader: failed set-aside cleanup of %s refused at the second adjacency proof (%v) — nothing unlinked", backupPath, rerr2)
			return
		}
		_ = fsys.Remove(backupPath)
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		// The reservation vanished on its own — nothing foreign to protect,
		// nothing left to remove.
		return
	}
	logging.Warnf("downloader: failed set-aside cleanup of %s refused — the reservation no longer provably names our claimed placeholder (%v); the occupant is left byte-intact", backupPath, err)
}

// destPlaceholderMatchesClaim reports whether cur — the object at a take
// binding instant — is still THE claimed reservation placeholder: regular,
// non-symlink, size 0, same mtime on every platform, and same dev/inode
// where the filesystem exposes them. The shape mirrors
// overwriteBackupReservationStillOurs (wave-36); wave-38 shares it between
// the exchange leg's dest side and the take-aside proof.
func destPlaceholderMatchesClaim(cur, claim os.FileInfo) bool {
	if cur == nil || cur.Mode()&os.ModeSymlink != 0 || !cur.Mode().IsRegular() || cur.Size() != 0 || !cur.ModTime().Equal(claim.ModTime()) {
		return false
	}
	if claimDev, claimIno, claimOK := restoreSourceIdentity(claim); claimOK {
		if curDev, curIno, curOK := restoreSourceIdentity(cur); curOK && (claimDev != curDev || claimIno != curIno) {
			return false
		}
	}
	return true
}

// releaseExchangedPlaceholder — built untagged although only the Linux
// exchange leg (backup_handoff_linux.go) calls it, so its new take-aside
// legs stay host-testable on every CI depot — removes the reservation
// placeholder a successful exchange parked at destPath, leaving the
// destination absent for the staged publish. Wave-38 take-aside binding
// (finding F3, the exchange-leg mirror of the fallback handoff shape): take
// dest→scratch (no-replace onto OUR OWN fresh reserved quarantine name, so
// the take can never displace foreign bytes), re-prove the object at the
// scratch name against the claim identity (dev/inode where the filesystem
// exposes them, plus size 0, mtime, and a regular non-symlink shape), then
// unlink ONLY the scratch, re-bound at unlink time. A placeholder that
// VANISHED on its own completed the cleanup by itself. Anything else — a
// foreign occupant that rode the swap or landed mid-take, an indeterminate
// lookup, a failed unlink — is a refusal: the install fails closed, no
// foreign byte is ever removed, and the set-aside backup (which holds the
// original bytes by then) is retained for the orphan sweep. Wedge
// compensations restore the original names with no-replace moves where the
// name is still free; otherwise the object stays recoverable at the scratch
// name.
//
//nolint:unused // production callers sit on the Linux-tagged exchange leg (backup_handoff_linux.go); host builds exercise this through tests.
func releaseExchangedPlaceholder(fsys afero.Fs, destPath string, claim os.FileInfo) error {
	cur, err := lstatBackupCandidate(fsys, destPath)
	switch {
	case err != nil && os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("inspect exchanged placeholder %s before its removal: %w", destPath, err)
	case !destPlaceholderMatchesClaim(cur, claim):
		logging.Warnf("downloader: exchange-parked placeholder at %s no longer names the claimed reservation — a foreign occupant rode the swap; destination left byte-intact, set-aside backup retained", destPath)
		return fmt.Errorf("placeholder the exchange parked at %s no longer names the claimed reservation (foreign occupant preserved): %w", destPath, fsutil.ErrPublishCollision)
	}
	scratch, scratchClaim, cerr := claimRollbackQuarantineName(fsys, destPath)
	if cerr != nil {
		return fmt.Errorf("reserve take-aside scratch for exchanged placeholder %s: %w", destPath, cerr)
	}
	hold, terr := fsutil.TakeAside(fsutil.TakeAsideSpec{
		FS:      fsys,
		Src:     destPath,
		Scratch: scratch,
		Claim:   scratchClaim,
		Prove: func(moved os.FileInfo) error {
			if !destPlaceholderMatchesClaim(moved, claim) {
				return fmt.Errorf("object taken aside from %s is not the claim's exchange-parked placeholder — foreign bytes preserved: %w", destPath, fsutil.ErrPublishCollision)
			}
			return nil
		},
	})
	if terr != nil {
		return fmt.Errorf("take-aside of exchanged placeholder %s: %w", destPath, terr)
	}
	if uerr := hold.Unlink(); uerr != nil {
		// The unlink wedge restores the placeholder onto destPath no-replace
		// where the name is still free (recovering the pre-take shape); a
		// failed restore leaves the object recoverable at the scratch name.
		return errors.Join(
			fmt.Errorf("remove take-aside exchanged placeholder %s: %w", hold.Scratch(), uerr),
			hold.Restore(),
		)
	}
	return nil
}
