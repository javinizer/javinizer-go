package downloader

// POSTER-WRITE-HARDENING wave-32 (codex local review round 2, PR#215 finding
// R1) — port history's wave-26 quarantine construction
// (internal/history/replacement_backup_quarantine.go) to the downloader's
// confirm-failure rollback so its "delete backup after verified restore"
// sequence can re-gate the destination BETWEEN the verified move and the
// only unlink.
//
// removeRollbackBackup's wave-31 posture bound the unlink to the object the
// rollback COPIED (Lstat + facts comparison), then deleted the journaled
// PATHNAME — and installOverwriting's rollback verified the restored
// DESTINATION before the removal, leaving a check→delete window a foreign
// writer could use: swap or delete the destination after the check and the
// backup (the only remaining copy of the pre-existing bytes) was unlinked
// while the journal release went through. The split quarantine closes it the
// same way history does:
//
//  1. bind the backup name's occupant to the copied-object identity,
//     re-opened no-follow (the wave-31 legs kept verbatim);
//  2. move the VERIFIED object onto a hard-to-guess O_EXCL-reserved
//     quarantine sibling and re-prove it there (dev/inode when exposed,
//     then size + mtime);
//  3. the caller re-gates the destination (rollbackRestoredDestStillOurs);
//     on a divergence the verified object moves back onto the journaled
//     name NO-REPLACE and the entry stays armed — nothing is removed;
//  4. only THE QUARANTINE name is unlinked — re-bound at unlink time so a
//     watcher swapping the quarantine name mid-window is caught, and an
//     ENOENT at Remove time is indeterminate retention (never consumed),
//     the same R4 posture history adopted this wave.
//
// Any wedge step removes NOTHING and leaves the journal entry live.
//
// Wave-30 follow-on (codex P2, PR#215 findings F1+F2), mirroring history's
// wave-36 construction (replacement_backup_quarantine.go) exactly:
//   - F1: the claim hands the caller the reservation's captured identity
//     (the open handle's pre-close Stat) and the move re-derives it first
//     (rollbackQuarantineReservationStillOurs) — a foreign writer renaming
//     the placeholder away and planting its own occupant at the reserved
//     name no longer gets its bytes silently displaced by the replace-aware
//     quarantine move; divergence is the typed collision class and behaves
//     exactly like the claim-failure leg (journal entry live).
//   - F2: the wedge move-back (restoreQuarantinedRollbackBackup, the hold's
//     restore) RETURNS its failure classified
//     (errRollbackQuarantineRestoreFailed): a failed move-back means the
//     journaled name is UNOWNED while the verified bytes sit at the .dlq.
//     name, so internal wedge legs JOIN the classification into the removal
//     error and the install_overwrite caller persists the rearm-refused
//     restore-pending kind instead of leaving the entry armed against a
//     foreign-claimed name.

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// rollbackQuarantineSuffix names the one-shot quarantine sibling of a
// rollback backup. The name deliberately does NOT match the
// `.dlbak.<16hex>` ownership grammar (overwriteBackupPath), so sweeps never
// arbitrate a transient (or wedged) quarantine file as a set-aside.
const rollbackQuarantineSuffix = ".dlq."

// rollbackQuarantineClaimTries bounds the unpredictable-name draw loop;
// every collision or racing claimant costs one draw.
const rollbackQuarantineClaimTries = 64

// errRollbackQuarantineVanished classifies the quarantine name being empty
// AFTER the verified object provably moved onto it (finding R4, shared with
// history): the owned bytes vanished unownably — through a path this flow
// never unlinked — so the honest answer is indeterminate retention, never a
// consumed removal.
var errRollbackQuarantineVanished = errors.New("quarantined rollback backup vanished before its verified unlink completed")

// errRollbackQuarantineRestoreFailed classifies the wedge-compensation
// failure (finding F2): the verified object moved to its quarantine name but
// could NOT be restored onto the journaled name, which is therefore unowned
// (foreign-occupied or wedged) while the owned bytes stay recoverable at the
// quarantine name. Callers with a live journal entry persist the
// rearm-refused restore-pending kind for it — no later retry may stat, copy
// from, or remove the journaled name — instead of leaving the entry armed
// against bytes nobody journals. errors.Is matches it through a joined
// wedge error chain.
var errRollbackQuarantineRestoreFailed = errors.New("quarantined rollback backup could not be restored onto its journaled name")

// rollbackQuarantineRandReader is the entropy source behind the quarantine
// token, exposed as a seam (same discipline as history's
// backupQuarantineRandReader): production is cryptographically random and
// the token carries no path or user data, while tests wedge the failure leg
// deterministically.
var rollbackQuarantineRandReader io.Reader = cryptorand.Reader

// claimRollbackQuarantineName atomically reserves a hard-to-guess quarantine
// sibling for the backup: draw an unpredictable token, observe the name
// free, claim it O_CREATE|O_EXCL. The observation-to-claim race resolves in
// favor of the claim (os.IsExist → re-draw), so the returned name is
// provably owned by this process — a 0-byte placeholder the caller's
// replace-aware quarantine move then displaces. Finding F1: the claim ALSO
// hands the caller the reservation's own captured identity (the open
// handle's pre-close Stat) so the reservation stays IDENTITY-BOUND through
// the claim→move handoff (rollbackQuarantineReservationStillOurs) — a
// foreign writer renaming the placeholder away and planting its own
// occupant at the reserved name no longer gets its bytes silently displaced
// by the replace-aware quarantine move.
func claimRollbackQuarantineName(fsys afero.Fs, backup string) (string, os.FileInfo, error) {
	for attempt := 0; attempt < rollbackQuarantineClaimTries; attempt++ {
		var token [16]byte
		if _, err := io.ReadFull(rollbackQuarantineRandReader, token[:]); err != nil {
			return "", nil, fmt.Errorf("quarantine token for %s: %w", backup, err)
		}
		candidate := backup + rollbackQuarantineSuffix + hex.EncodeToString(token[:])
		if _, err := lstatBackupCandidate(fsys, candidate); err == nil {
			continue // the draw is occupied (or a wedge tombstone) — draw again
		} else if !os.IsNotExist(err) {
			return "", nil, fmt.Errorf("inspect quarantine candidate %s: %w", candidate, err)
		}
		reservation, rerr := fsys.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case rerr == nil:
			info, serr := reservation.Stat()
			if serr != nil {
				// A reservation whose identity cannot even be read is in an
				// unknown on-disk state — drop it rather than renaming over
				// unverified bytes.
				_ = reservation.Close()
				_ = fsys.Remove(candidate)
				return "", nil, fmt.Errorf("stat quarantine reservation %s: %w", candidate, serr)
			}
			if cerr := reservation.Close(); cerr != nil {
				// A reservation whose close failed is in an unknown on-disk
				// state — drop it rather than renaming over unverified bytes.
				_ = fsys.Remove(candidate)
				return "", nil, fmt.Errorf("close quarantine reservation %s: %w", candidate, cerr)
			}
			return candidate, info, nil
		case os.IsExist(rerr):
			continue // a racer claimed this draw first — draw again
		default:
			return "", nil, fmt.Errorf("reserve quarantine candidate %s: %w", candidate, rerr)
		}
	}
	return "", nil, fmt.Errorf("quarantine names exhausted for %s after %d attempts", backup, rollbackQuarantineClaimTries)
}

// rollbackQuarantineReservationStillOurs re-derives the O_EXCL reservation's
// identity IMMEDIATELY BEFORE the quarantine move (finding F1): the reserved
// name must still address THE claimed placeholder — dev/inode where the
// filesystem exposes them, plus size 0, mtime, and a regular non-symlink
// shape on every platform. A foreign writer swapping its own object onto
// the name between claim and move is refused with the typed collision class
// (fsutil.ErrPublishCollision): the occupant keeps its bytes byte-intact,
// the journaled backup is never quarantined over it, and the caller's
// claim-failure leg leaves the journal entry live.
func rollbackQuarantineReservationStillOurs(fsys afero.Fs, quarantine string, claim os.FileInfo) error {
	absoluteQuarantine, _ := filepath.Abs(quarantine)
	cur, err := lstatBackupCandidate(fsys, quarantine)
	switch {
	case err != nil:
		return fmt.Errorf("inspect quarantine reservation %s before the move: %w", absoluteQuarantine, err)
	case cur == nil || cur.Mode()&os.ModeSymlink != 0 || !cur.Mode().IsRegular() || cur.Size() != 0 || !cur.ModTime().Equal(claim.ModTime()):
		return fmt.Errorf("quarantine reservation %s no longer names the claimed empty placeholder (foreign reservation swap) — foreign bytes preserved: %w", absoluteQuarantine, fsutil.ErrPublishCollision)
	}
	if claimDev, claimIno, claimOK := restoreSourceIdentity(claim); claimOK {
		if curDev, curIno, curOK := restoreSourceIdentity(cur); curOK && (claimDev != curDev || claimIno != curIno) {
			return fmt.Errorf("quarantine reservation %s no longer names the claimed placeholder (dev/inode mismatch) — foreign bytes preserved: %w", absoluteQuarantine, fsutil.ErrPublishCollision)
		}
	}
	return nil
}

// moveVerifiedRollbackBackupToQuarantine renames the verified backup object
// onto its reserved quarantine name. The rename must be replace-aware on
// every platform (the reservation placeholder occupies the name by design),
// so it rides fsutil.ReplaceFile exactly like moveIntoReservedBackup. The
// open no-follow handle stays OPEN through the rename on POSIX (the
// descriptor pins the inode regardless of names, so the re-verify compares
// against the object that was actually read); Windows cannot rename a file
// with an open Go handle, so the Windows-posture seam closes it first and
// the re-verify still binds the moved object to the verified snapshot.
func moveVerifiedRollbackBackupToQuarantine(fsys afero.Fs, backup, quarantine string, handle afero.File) error {
	if fsutil.PathBackslashesAreSeparators {
		_ = handle.Close()
	}
	return fsutil.ReplaceFile(fsys, backup, quarantine)
}

// restoreQuarantinedRollbackBackup is the wedge compensation for the
// rollback quarantine flow: once the verified object has moved to the
// quarantine name, every wedge leg (indeterminate re-verify, proven-foreign
// quarantined object, a failed quarantine unlink, or the caller's
// destination re-gate diverging) first restores the pre-call state — the
// quarantined object moves BACK onto the journaled name NO-REPLACE, so the
// retained journal entry keeps pointing at exactly the bytes it armed
// against. A racer's occupant at the journaled name is never clobbered
// (typed fsutil.ErrPublishCollision keeps the object at the quarantine name
// for manual recovery). Finding F2: the compensation result is RETURNED,
// not just logged — a failed move-back means the journaled name is UNOWNED
// (a foreign claimant holds it, or the publish failed outright) while the
// owned bytes sit at the quarantine name, and callers with a live journal
// entry must route it to the rearm-refused pending kind rather than leaving
// it armed against that name.
func restoreQuarantinedRollbackBackup(fsys afero.Fs, phase, backup, quarantine string) error {
	if back := fsutil.PublishNoReplace(fsys, quarantine, backup); back != nil {
		logging.Warnf("downloader: %s failed to restore quarantined backup %s from %s after the removal wedge: %v — the original bytes stay recoverable at the quarantine name, the journaled name is unowned", phase, backup, quarantine, back)
		return back
	}
	return nil
}

// rollbackBackupQuarantine carries the wave-32 split between the verified
// quarantine MOVE and the only unlink: installOverwriting's confirm-failure
// rollback re-gates the restored destination between the two, so a foreign
// swap or deletion landing in the former check→delete window can no longer
// get the (quarantined) recoverable bytes unlinked or the journal released.
type rollbackBackupQuarantine struct {
	fsys       afero.Fs
	backup     string
	phase      string
	quarantine string
	quar       os.FileInfo
	moved      bool // the verified object currently sits at the quarantine name
	unlinked   bool // the verified unlink completed
}

// restore moves the quarantined verified object back onto the journaled
// backup name NO-REPLACE (the caller's destination re-gate diverged).
// Idempotent: only a live (moved, not yet unlinked) hold acts.
// Finding F2: the move-back result is RETURNED to the caller. A failure
// means the journaled name is unowned (foreign-claimed or wedged) while the
// verified bytes sit at the quarantine name — the error wraps the typed
// errRollbackQuarantineRestoreFailed class so callers persist the
// rearm-refused pending kind for the entry rather than leaving it armed
// against that name. A failed restore leaves moved=true so a later caller
// retry can re-attempt the compensation against the same quarantine name.
func (h *rollbackBackupQuarantine) restore() error {
	if !h.moved || h.unlinked {
		return nil
	}
	if err := restoreQuarantinedRollbackBackup(h.fsys, h.phase, h.backup, h.quarantine); err != nil {
		return fmt.Errorf("%w: %s stays recoverable at quarantine %s: %v", errRollbackQuarantineRestoreFailed, h.backup, h.quarantine, err)
	}
	h.moved = false
	return nil
}

// restoreOrJoin runs the wedge move-back for internal quarantine legs and
// JOINS its failure into the wedge's own error (finding F2): the entry
// caller then sees the errRollbackQuarantineRestoreFailed class through
// errors.Is and persists the rearm-refused pending kind, since the journaled
// name is unowned once the compensation failed. A successful move-back
// leaves the wedge error untouched, byte-identical to the pre-F2 return.
func (h *rollbackBackupQuarantine) restoreOrJoin(err error) error {
	if rerr := h.restore(); rerr != nil {
		return errors.Join(err, rerr)
	}
	return err
}

// removeVerified performs the one unlink of the rollback quarantine flow:
// only THE QUARANTINE name is ever unlinked, never the journaled pathname.
// The fs.Remove is path-based, so the re-verify→Remove window is a
// watcher's: the quarantine name is re-derived no-follow AT UNLINK TIME and
// must STILL name the re-verified object (dev/inode when exposed, then size
// + mtime) before the unlink runs; a substitution inside the window is
// restored back and refused, never deleted. ENOENT at Remove time is
// indeterminate retention (the typed vanished sentinel) — the owned bytes
// vanished unownably, so nothing reports consumed.
func (h *rollbackBackupQuarantine) removeVerified() error {
	if h.unlinked {
		return nil // absent-at-gate (or already completed) hold: nothing to do
	}
	absoluteBackup, _ := filepath.Abs(h.backup)
	cur, lerr := lstatBackupCandidate(h.fsys, h.quarantine)
	switch {
	case os.IsNotExist(lerr):
		h.moved = false
		absoluteQuarantine, _ := filepath.Abs(h.quarantine)
		return fmt.Errorf("%w: %s (quarantine %s empty at the unlink)", errRollbackQuarantineVanished, absoluteBackup, absoluteQuarantine)
	case lerr != nil:
		logging.Warnf("downloader: %s failed to re-verify quarantined backup %s (quarantine %s) at the unlink: %v — journal entry remains armed", h.phase, absoluteBackup, h.quarantine, lerr)
		return h.restoreOrJoin(lerr)
	}
	if cur == nil || cur.Mode()&os.ModeSymlink != 0 || !cur.Mode().IsRegular() {
		return h.restoreOrJoin(refuseRollbackBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s no longer names the verified regular file at the unlink", h.quarantine)))
	}
	if quarDev, quarIno, quarOK := restoreSourceIdentity(h.quar); quarOK {
		if curDev, curIno, curOK := restoreSourceIdentity(cur); curOK && (quarDev != curDev || quarIno != curIno) {
			return h.restoreOrJoin(refuseRollbackBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s names a different object than the re-verified one at the unlink (dev/inode mismatch) — foreign bytes preserved", h.quarantine)))
		}
	}
	if cur.Size() != h.quar.Size() || !cur.ModTime().Equal(h.quar.ModTime()) {
		return h.restoreOrJoin(refuseRollbackBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s metadata changed between the re-verify and the unlink — foreign bytes preserved", h.quarantine)))
	}
	if err := h.fsys.Remove(h.quarantine); err != nil {
		if os.IsNotExist(err) {
			h.moved = false
			absoluteQuarantine, _ := filepath.Abs(h.quarantine)
			return fmt.Errorf("%w: %s (quarantine %s vanished under the unlink)", errRollbackQuarantineVanished, absoluteBackup, absoluteQuarantine)
		}
		logging.Warnf("downloader: %s failed to remove quarantined backup %s (quarantine %s): %v", h.phase, absoluteBackup, h.quarantine, err)
		return h.restoreOrJoin(err)
	}
	h.moved = false
	h.unlinked = true
	return nil
}

// quarantineRollbackBackupForRemoval runs removeRollbackBackup's wave-31
// ownership binding (Lstat no-follow + copied-object identity + no-follow
// reopen + handle SameFile) plus the verified quarantine move, then STOPS
// before the only unlink: the caller re-gates the restored destination
// against the hold and either finishes with the hold's removeVerified or
// puts the verified object back with the hold's restore. Errors keep the
// established ownership-cleanup rule: ENOENT classifies as "already gone ==
// removed" ONLY before the move (a nil hold answers success); afterwards a
// vanished quarantine is the typed indeterminate sentinel.
func quarantineRollbackBackupForRemoval(fsys afero.Fs, backup string, copiedFrom os.FileInfo, phase string) (*rollbackBackupQuarantine, error) {
	absoluteBackup, _ := filepath.Abs(backup)
	info, lerr := lstatBackupCandidate(fsys, backup)
	switch {
	case lerr == nil:
		// verified below
	case os.IsNotExist(lerr):
		// Already gone == removed (the established ownership rule).
		return &rollbackBackupQuarantine{fsys: fsys, backup: backup, phase: phase, unlinked: true}, nil
	default:
		logging.Warnf("downloader: %s failed to inspect backup %s before removal: %v; journal entry remains armed", phase, backup, lerr)
		return nil, lerr
	}
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, refuseRollbackBackupRemoval(backup, phase, "name no longer names the copied object (foreign occupant preserved)")
	}
	if copiedFrom != nil {
		if srcDev, srcIno, srcOK := restoreSourceIdentity(copiedFrom); srcOK {
			if curDev, curIno, curOK := restoreSourceIdentity(info); curOK && (srcDev != curDev || srcIno != curIno) {
				return nil, refuseRollbackBackupRemoval(backup, phase, "occupant is not the object the rollback copied (dev/inode mismatch) — foreign bytes preserved")
			}
		}
		if info.Size() != copiedFrom.Size() || !info.ModTime().Equal(copiedFrom.ModTime()) {
			return nil, refuseRollbackBackupRemoval(backup, phase, "occupant metadata differs from the object the rollback copied — foreign bytes preserved")
		}
	}
	handle, oerr := restoreOpenReplacementSource(fsys, backup)
	if oerr != nil {
		if os.IsNotExist(oerr) {
			return &rollbackBackupQuarantine{fsys: fsys, backup: backup, phase: phase, unlinked: true}, nil
		}
		logging.Warnf("downloader: %s failed to reopen backup %s before removal: %v; journal entry remains armed", phase, absoluteBackup, oerr)
		return nil, oerr
	}
	defer func() { _ = handle.Close() }()
	openedInfo, serr := handle.Stat()
	if serr != nil {
		logging.Warnf("downloader: %s failed to stat opened backup %s before removal: %v; journal entry remains armed", phase, absoluteBackup, serr)
		return nil, serr
	}
	if openedInfo == nil || openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() {
		return nil, refuseRollbackBackupRemoval(backup, phase, "opened object is not the regular file Lstat verified")
	}
	if curDev, curIno, curOK := restoreSourceIdentity(info); curOK {
		if opDev, opIno, opOK := restoreSourceIdentity(openedInfo); opOK && (curDev != opDev || curIno != opIno) {
			return nil, refuseRollbackBackupRemoval(backup, phase, "opened object differs from the Lstat object")
		}
	}

	quarantine, reservation, cerr := claimRollbackQuarantineName(fsys, backup)
	if cerr != nil {
		logging.Warnf("downloader: %s could not reserve a quarantine name for backup %s: %v — journal entry remains armed", phase, absoluteBackup, cerr)
		return nil, cerr
	}
	// Finding F1: keep the reservation IDENTITY bound through the handoff —
	// immediately before the move the reserved name must still address the
	// claimed 0-byte placeholder. A foreign writer renaming the reservation
	// away and planting its own occupant used to get its bytes silently
	// displaced by the replace-aware rename; the refusal keeps the occupant
	// intact and behaves exactly like the claim-failure leg above (journal
	// entry live).
	if rerr := rollbackQuarantineReservationStillOurs(fsys, quarantine, reservation); rerr != nil {
		logging.Warnf("downloader: %s refused the quarantine move for backup %s: %v — journal entry remains armed", phase, absoluteBackup, rerr)
		return nil, rerr
	}
	if renErr := moveVerifiedRollbackBackupToQuarantine(fsys, backup, quarantine, handle); renErr != nil {
		// The rename is atomic: a failed move relocated NOTHING. Cleaning
		// the reservation drops OUR 0-byte claim file; the journaled name
		// and the entry stay untouched.
		_ = fsys.Remove(quarantine)
		logging.Warnf("downloader: %s failed to quarantine backup %s before removal: %v — journal entry remains armed", phase, absoluteBackup, renErr)
		return nil, renErr
	}
	hold := &rollbackBackupQuarantine{
		fsys: fsys, backup: backup, phase: phase, quarantine: quarantine, moved: true,
	}
	// Re-prove the moved object at the quarantine name before returning the
	// hold: a substitution inside the open→rename window moved a FOREIGN
	// plant instead, and that plant is never removed by this gate.
	quarInfo, qerr := lstatBackupCandidate(fsys, quarantine)
	switch {
	case os.IsNotExist(qerr):
		// The verified bytes vanished unownably right after the move —
		// indeterminate retention (finding R4), never a consumed removal.
		hold.moved = false
		absoluteQuarantine, _ := filepath.Abs(quarantine)
		return nil, fmt.Errorf("%w: %s (quarantine %s empty at the post-move re-verify)", errRollbackQuarantineVanished, absoluteBackup, absoluteQuarantine)
	case qerr != nil:
		logging.Warnf("downloader: %s failed to re-verify quarantined backup %s (quarantine %s) before removal: %v — journal entry remains armed", phase, absoluteBackup, quarantine, qerr)
		return nil, hold.restoreOrJoin(qerr)
	}
	if quarInfo == nil || quarInfo.Mode()&os.ModeSymlink != 0 || !quarInfo.Mode().IsRegular() {
		return nil, hold.restoreOrJoin(refuseRollbackBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s is not the verified regular file", quarantine)))
	}
	if verDev, verIno, verOK := restoreSourceIdentity(openedInfo); verOK {
		if quarDev, quarIno, quarOK := restoreSourceIdentity(quarInfo); quarOK && (verDev != quarDev || verIno != quarIno) {
			return nil, hold.restoreOrJoin(refuseRollbackBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s is not the verified object (dev/inode mismatch) — foreign bytes preserved", quarantine)))
		}
	}
	if quarInfo.Size() != openedInfo.Size() || !quarInfo.ModTime().Equal(openedInfo.ModTime()) {
		return nil, hold.restoreOrJoin(refuseRollbackBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s metadata differs from the verified object — foreign bytes preserved", quarantine)))
	}
	hold.quar = quarInfo
	return hold, nil
}
