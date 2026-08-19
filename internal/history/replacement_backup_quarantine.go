package history

// POSTER-WRITE-HARDENING wave-26 (codex P2, PR#215 finding 4) — keep the
// backup identity bound THROUGH the unlink.
//
// Wave-25 bound removeReplacementBackup's unlink to the OWNED object by
// Lstat + a no-follow open + handle stat — then CLOSED the handle and
// unlinked by pathname. A directory writer exploiting the close→Remove gap
// could plant a foreign file at the backup name and have THIS gate's own
// verification bless its deletion (foreign bytes destroyed, journal record
// consumed). The quarantine-then-reverify construction closes it:
//
//  1. reserve a hard-to-guess sibling quarantine name with O_EXCL
//     (claimBackupQuarantineName — the downloader's backup-name-claim
//     discipline with an unpredictable token, so a racer cannot pre-occupy
//     every draw);
//  2. move the VERIFIED object — the one the still-open no-follow handle
//     addresses — onto the reserved name with a replace-aware rename
//     (moveVerifiedBackupToQuarantine). The rename's own window now moves
//     whatever the name addresses at THAT instant, which is exactly what
//     step 3 re-proves;
//  3. Lstat the quarantine name and require the quarantined object to BE
//     the verified handle object (dev/inode when exposed, then size +
//     mtime). A plant that raced onto the ORIGINAL path meanwhile keeps
//     its bytes there, untouched by every leg below;
//  4. unlink the QUARANTINE name only — never the journaled pathname.
//
// Any wedge step — claim failure, rename failure, indeterminate re-verify,
// or a quarantined object that is not the verified one — removes NOTHING
// and leaves the journal entry live (the *BackupRemovalRefusedError class
// for proven-foreign objects, plain errors for indeterminate ones).
//
// Wave-32 (codex local review round 2, PR#215 findings R1+R4): the flow
// splits into quarantineVerifiedBackup → [caller's destination re-gate] →
// (*replacementBackupQuarantine).removeVerified so restore/rollback callers
// re-prove the destination between the verified move and the unlink (R1),
// and the unlink itself re-binds the name at Remove time with ENOENT no
// longer consumed (R4).

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

// backupQuarantineSuffix names the one-shot quarantine sibling of a journaled
// backup. The name deliberately does NOT match the `*.dlbak.<16hex>`
// ownership grammar (replacement_backup_name), so sweeps never arbitrate a
// transient (or wedged) quarantine file as a set-aside.
const backupQuarantineSuffix = ".dlq."

// backupQuarantineClaimTries bounds the unpredictable-name draw loop; every
// collision or racing claimant costs one draw.
const backupQuarantineClaimTries = 64

// backupQuarantineRandReader is the entropy source behind the quarantine
// token, exposed as a seam (same discipline as fsutil.caseProbeRandReader):
// the production source is cryptographically random and the token carries no
// path or user data, while tests wedge the failure leg deterministically.
var backupQuarantineRandReader io.Reader = cryptorand.Reader

// claimBackupQuarantineName atomically reserves a hard-to-guess quarantine
// sibling for backup: draw an unpredictable token, observe the name free,
// claim it O_CREATE|O_EXCL. The observation-to-claim race resolves in favor
// of the claim (os.IsExist → re-draw), so the returned name is provably
// owned by this process — a 0-byte placeholder the caller's replace-aware
// quarantine move then displaces (safe: it displaces OUR reservation, never
// a foreign file, because a foreign claimant would have failed OUR
// observation/O_EXCL step).
func claimBackupQuarantineName(fs afero.Fs, backup string) (string, error) {
	for attempt := 0; attempt < backupQuarantineClaimTries; attempt++ {
		var token [16]byte
		if _, err := io.ReadFull(backupQuarantineRandReader, token[:]); err != nil {
			return "", fmt.Errorf("quarantine token for %s: %w", backup, err)
		}
		candidate := backup + backupQuarantineSuffix + hex.EncodeToString(token[:])
		if _, err := lstatRestoreSource(fs, candidate); err == nil {
			continue // the draw is occupied (or a wedge tombstone) — draw again
		} else if !errors.Is(err, afero.ErrFileNotFound) {
			return "", fmt.Errorf("inspect quarantine candidate %s: %w", candidate, err)
		}
		reservation, rerr := fs.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case rerr == nil:
			if cerr := reservation.Close(); cerr != nil {
				// A reservation whose close failed is in an unknown on-disk
				// state — drop it rather than renaming over unverified bytes.
				_ = fs.Remove(candidate)
				return "", fmt.Errorf("close quarantine reservation %s: %w", candidate, cerr)
			}
			return candidate, nil
		case os.IsExist(rerr):
			continue // a racer claimed this draw first — draw again
		default:
			return "", fmt.Errorf("reserve quarantine candidate %s: %w", candidate, rerr)
		}
	}
	return "", fmt.Errorf("quarantine names exhausted for %s after %d attempts", backup, backupQuarantineClaimTries)
}

// moveVerifiedBackupToQuarantine renames the verified backup object onto its
// reserved quarantine name. The rename must be replace-aware on every
// platform: the reservation placeholder occupies the name by design, and a
// non-replacing rename (Windows MoveFileW semantics — or any wrapper whose
// rename refuses existing targets) would fail against OUR OWN placeholder.
// fsutil.ReplaceFile supplies the atomicity (POSIX rename replaces in place;
// Windows routes to MoveFileExW's replace form).
//
// Handle discipline: the open no-follow handle stays OPEN through the rename
// on POSIX (the descriptor pins the inode regardless of names, so the step-3
// re-verify compares against the object that was actually read). Windows
// cannot rename a file with an open Go handle (no FILE_SHARE_DELETE), so the
// Windows-posture seam closes it first; the re-verify still binds the moved
// object to the verified snapshot, narrowing that platform's close→rename
// gap to a refusable mismatch instead of a silent foreign-bytes unlink.
func moveVerifiedBackupToQuarantine(fs afero.Fs, backup, quarantine string, handle afero.File) error {
	if fsutil.PathBackslashesAreSeparators {
		_ = handle.Close()
	}
	return fsutil.ReplaceFile(fs, backup, quarantine)
}

// restoreQuarantinedBackup is the wedge compensation for the quarantine
// flow: once the verified object has moved to the quarantine name, every
// wedge leg (indeterminate re-verify, proven-foreign quarantined object, or
// a failed quarantine unlink) first restores the pre-call state — the
// quarantined object moves BACK onto the journaled name NO-REPLACE, so the
// retained journal entry keeps pointing at exactly the bytes it armed
// against and the pending retry re-derives this attempt's outcome instead of
// wedging on a vacant name. A racer's occupant at the journaled name is
// never clobbered (typed fsutil.ErrPublishCollision keeps the object at the
// quarantine name for manual recovery). The compensation is best-effort: its
// failure only warns and never reclassifies the wedge.
func restoreQuarantinedBackup(fs afero.Fs, phase, backup, quarantine string) {
	if back := fsutil.PublishNoReplace(fs, quarantine, backup); back != nil {
		logging.Warnf("%s failed to restore quarantined backup %s from %s after the removal wedge: %v — the original bytes stay recoverable at the quarantine name", phase, backup, quarantine, back)
	}
}

// errReplacementBackupQuarantineVanished classifies the quarantine name
// being empty AFTER the verified object provably moved onto it (wave-32,
// codex local review round 2, PR#215 finding R4): the pre-wave-32 legs
// answered that ENOENT as "removed" and let the journal entry CONSUME, even
// though the owned bytes vanished through a path this flow never unlinked —
// unownably destroyed, with the ledger record erased as if we had removed
// them safely. The vanished legs now refuse typed instead: nothing reports
// consumed, the journal entry stays live (the convergent pending/sweep retry
// legs still tolerate the absent journaled name on their regular gate), and
// no compensation move-back runs — there is nothing at the quarantine name
// to move.
var errReplacementBackupQuarantineVanished = errors.New("quarantined backup vanished before its verified unlink completed")

// replacementBackupQuarantine carries the wave-32 split between the verified
// quarantine MOVE and the only unlink (findings R1+R4): restore/rollback
// callers re-prove their DESTINATION between the two, so a foreign
// swap/deletion landing in the former check→delete window can no longer get
// the (quarantined) recoverable bytes unlinked or the journal consumed. When
// the destination gate diverges the caller runs (*...).restore() — the
// verified object moves back onto the journaled name NO-REPLACE — and leaves
// the entry live; otherwise removeVerified performs the unlink.
type replacementBackupQuarantine struct {
	fs         afero.Fs
	backup     string
	phase      string
	quarantine string
	quar       os.FileInfo
	moved      bool // the verified object currently sits at the quarantine name
	unlinked   bool // the verified unlink completed
}

// quarantineVerifiedBackup runs removeReplacementBackup's wave-26 final legs
// STOPPING before the unlink: the caller has ALREADY bound the backup name's
// occupant to the journal/restore facts AND re-opened it no-follow (verified
// is the open handle's own stat). The verified object moves aside under a
// hard-to-guess O_EXCL-reserved quarantine name (with the handle open where
// the platform allows) and is re-proven at the quarantine name against the
// verified snapshot. Every wedge step — claim failure, rename failure,
// indeterminate re-verify, a vanished quarantine name, or a quarantined
// object that is not the verified one — removes NOTHING and leaves the
// journal entry live exactly like removeReplacementBackup's earlier legs
// (the *BackupRemovalRefusedError class for proven-foreign objects, plain
// errors for indeterminate ones, the vanished sentinel for unownable loss).
// A successful hold names the quarantine and its re-verified object so the
// caller's destination re-gate and removeVerified can finish the wave-32
// sequence.
func quarantineVerifiedBackup(fs afero.Fs, backup, phase string, handle afero.File, verified os.FileInfo) (*replacementBackupQuarantine, error) {
	absoluteBackup, _ := filepath.Abs(backup)
	quarantine, cerr := claimBackupQuarantineName(fs, backup)
	if cerr != nil {
		logging.Warnf("%s could not reserve a quarantine name for backup %s: %v — journal entry retained live", phase, absoluteBackup, cerr)
		return nil, cerr
	}
	if renErr := moveVerifiedBackupToQuarantine(fs, backup, quarantine, handle); renErr != nil {
		// The rename is atomic: a failed move relocated NOTHING. Cleaning the
		// reservation drops OUR 0-byte claim file; the journaled name and the
		// entry stay untouched for the conservative retry legs.
		_ = fs.Remove(quarantine)
		logging.Warnf("%s failed to quarantine backup %s before removal: %v — journal entry retained live", phase, absoluteBackup, renErr)
		return nil, renErr
	}
	hold := &replacementBackupQuarantine{
		fs: fs, backup: backup, phase: phase, quarantine: quarantine, moved: true,
	}
	// The object the journaled name addressed at move time now sits at the
	// quarantine name. RE-PROVE it before returning the hold: a substitution
	// inside the open→rename window moved a FOREIGN plant instead, and that
	// plant — plus anything that raced onto the original path since — is
	// never removed by this gate.
	quarInfo, qerr := lstatRestoreSource(fs, quarantine)
	switch {
	case errors.Is(qerr, afero.ErrFileNotFound):
		// Wave-32 (finding R4): vanished-under-us is NOT "removed" — the
		// verified bytes disappeared unownably. Indeterminate retention:
		// nothing consumed, entry live, no move-back (nothing to move).
		hold.moved = false
		absoluteQuarantine, _ := filepath.Abs(quarantine)
		return nil, fmt.Errorf("%w: %s (quarantine %s empty at the post-move re-verify)", errReplacementBackupQuarantineVanished, absoluteBackup, absoluteQuarantine)
	case qerr != nil:
		hold.restore()
		logging.Warnf("%s failed to re-verify quarantined backup %s (quarantine %s) before removal: %v — journal entry retained live", phase, absoluteBackup, quarantine, qerr)
		return nil, qerr
	}
	if quarInfo == nil || quarInfo.Mode()&os.ModeSymlink != 0 || !quarInfo.Mode().IsRegular() {
		hold.restore()
		return nil, refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s is not the verified regular file", quarantine))
	}
	if verDev, verIno, verOK := restoreSourceIdentity(verified); verOK {
		if quarDev, quarIno, quarOK := restoreSourceIdentity(quarInfo); quarOK && (verDev != quarDev || verIno != quarIno) {
			hold.restore()
			return nil, refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s is not the verified object (dev/inode mismatch) — foreign bytes preserved", quarantine))
		}
	}
	if quarInfo.Size() != verified.Size() || !quarInfo.ModTime().Equal(verified.ModTime()) {
		hold.restore()
		return nil, refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s metadata differs from the verified object — foreign bytes preserved", quarantine))
	}
	hold.quar = quarInfo
	return hold, nil
}

// restore is the wave-32 wedge compensation exposure: with the verified
// object moved aside but NOT yet unlinked, a caller whose destination
// re-gate diverged (finding R1) moves it back onto the journaled name
// NO-REPLACE so the retained journal entry keeps pointing at exactly the
// bytes it armed against. Idempotent: only a live (moved, not yet unlinked)
// hold performs the move-back.
func (h *replacementBackupQuarantine) restore() {
	if !h.moved || h.unlinked {
		return
	}
	restoreQuarantinedBackup(h.fs, h.phase, h.backup, h.quarantine)
	h.moved = false
}

// removeVerified performs the one unlink of the quarantine flow: only THE
// QUARANTINE name is ever unlinked, never the journaled pathname.
//
// Wave-32 (finding R4): the fs.Remove is path-based, so the re-verify→Remove
// window is a watcher's. The quarantine name is re-derived no-follow AT
// UNLINK TIME and must STILL name the re-verified object (dev/inode when
// exposed, then size + mtime — the same binding the post-move re-verify
// applied) before the unlink runs; a substitution inside the window is
// restored back and refused, never deleted. And ENOENT at Remove time is no
// longer consumed (the owned bytes vanished unownably): it answers the typed
// vanished sentinel so the journal entry stays live.
func (h *replacementBackupQuarantine) removeVerified() error {
	if h.unlinked {
		return nil // absent-at-gate (or already completed) hold: nothing to do
	}
	absoluteBackup, _ := filepath.Abs(h.backup)
	cur, lerr := lstatRestoreSource(h.fs, h.quarantine)
	switch {
	case errors.Is(lerr, afero.ErrFileNotFound):
		h.moved = false
		absoluteQuarantine, _ := filepath.Abs(h.quarantine)
		return fmt.Errorf("%w: %s (quarantine %s empty at the unlink)", errReplacementBackupQuarantineVanished, absoluteBackup, absoluteQuarantine)
	case lerr != nil:
		h.restore()
		logging.Warnf("%s failed to re-verify quarantined backup %s (quarantine %s) at the unlink: %v — journal entry retained live", h.phase, absoluteBackup, h.quarantine, lerr)
		return lerr
	}
	if cur == nil || cur.Mode()&os.ModeSymlink != 0 || !cur.Mode().IsRegular() {
		h.restore()
		return refuseReplacementBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s no longer names the verified regular file at the unlink", h.quarantine))
	}
	if quarDev, quarIno, quarOK := restoreSourceIdentity(h.quar); quarOK {
		if curDev, curIno, curOK := restoreSourceIdentity(cur); curOK && (quarDev != curDev || quarIno != curIno) {
			h.restore()
			return refuseReplacementBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s names a different object than the re-verified one at the unlink (dev/inode mismatch) — foreign bytes preserved", h.quarantine))
		}
	}
	if cur.Size() != h.quar.Size() || !cur.ModTime().Equal(h.quar.ModTime()) {
		h.restore()
		return refuseReplacementBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s metadata changed between the re-verify and the unlink — foreign bytes preserved", h.quarantine))
	}
	if err := h.fs.Remove(h.quarantine); err != nil {
		if os.IsNotExist(err) {
			// The unlink-window vanish: indeterminate retention (finding R4),
			// never a consumed removal.
			h.moved = false
			absoluteQuarantine, _ := filepath.Abs(h.quarantine)
			return fmt.Errorf("%w: %s (quarantine %s vanished under the unlink)", errReplacementBackupQuarantineVanished, absoluteBackup, absoluteQuarantine)
		}
		h.restore()
		logging.Warnf("%s failed to remove quarantined backup %s (quarantine %s): %v", h.phase, absoluteBackup, h.quarantine, err)
		return err
	}
	h.moved = false
	h.unlinked = true
	return nil
}
