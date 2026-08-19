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

// removeVerifiedBackupByQuarantine is removeReplacementBackup's final leg
// (wave-26): the caller has ALREADY bound the backup name's occupant to the
// journal/restore facts AND re-opened it no-follow (verified is the open
// handle's own stat). The verified object is moved aside under a
// hard-to-guess quarantine name (with the handle open where the platform
// allows), re-verified at the quarantine name, and only THE QUARANTINE is
// unlinked — a plant swapped onto the journaled pathname anywhere inside the
// sequence keeps its bytes (it either raced onto the original path after our
// move, untouched, or was moved to the quarantine itself, where the
// re-verify refuses the unlink). Every refusal/wedge leaves the journal
// entry live exactly like removeReplacementBackup's earlier legs.
func removeVerifiedBackupByQuarantine(fs afero.Fs, backup, phase string, handle afero.File, verified os.FileInfo) error {
	absoluteBackup, _ := filepath.Abs(backup)
	quarantine, cerr := claimBackupQuarantineName(fs, backup)
	if cerr != nil {
		logging.Warnf("%s could not reserve a quarantine name for backup %s: %v — journal entry retained live", phase, absoluteBackup, cerr)
		return cerr
	}
	if renErr := moveVerifiedBackupToQuarantine(fs, backup, quarantine, handle); renErr != nil {
		// The rename is atomic: a failed move relocated NOTHING. Cleaning the
		// reservation drops OUR 0-byte claim file; the journaled name and the
		// entry stay untouched for the conservative retry legs.
		_ = fs.Remove(quarantine)
		logging.Warnf("%s failed to quarantine backup %s before removal: %v — journal entry retained live", phase, absoluteBackup, renErr)
		return renErr
	}
	// The object the journaled name addressed at move time now sits at the
	// quarantine name. RE-PROVE it before any unlink: a substitution inside
	// the open→rename window moved a FOREIGN plant instead, and that plant —
	// plus anything that raced onto the original path since — is never
	// removed by this gate.
	quarInfo, qerr := lstatRestoreSource(fs, quarantine)
	switch {
	case errors.Is(qerr, afero.ErrFileNotFound):
		// vanished under us == removed (the established ownership rule).
		return nil
	case qerr != nil:
		restoreQuarantinedBackup(fs, phase, backup, quarantine)
		logging.Warnf("%s failed to re-verify quarantined backup %s (quarantine %s) before removal: %v — journal entry retained live", phase, absoluteBackup, quarantine, qerr)
		return qerr
	}
	if quarInfo == nil || quarInfo.Mode()&os.ModeSymlink != 0 || !quarInfo.Mode().IsRegular() {
		restoreQuarantinedBackup(fs, phase, backup, quarantine)
		return refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s is not the verified regular file", quarantine))
	}
	if verDev, verIno, verOK := restoreSourceIdentity(verified); verOK {
		if quarDev, quarIno, quarOK := restoreSourceIdentity(quarInfo); quarOK && (verDev != quarDev || verIno != quarIno) {
			restoreQuarantinedBackup(fs, phase, backup, quarantine)
			return refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s is not the verified object (dev/inode mismatch) — foreign bytes preserved", quarantine))
		}
	}
	if quarInfo.Size() != verified.Size() || !quarInfo.ModTime().Equal(verified.ModTime()) {
		restoreQuarantinedBackup(fs, phase, backup, quarantine)
		return refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s metadata differs from the verified object — foreign bytes preserved", quarantine))
	}
	// Bound and re-verified: unlink the QUARANTINE name, never the journaled
	// pathname — a plant that raced onto the original path keeps its bytes.
	if err := fs.Remove(quarantine); err != nil && !os.IsNotExist(err) {
		restoreQuarantinedBackup(fs, phase, backup, quarantine)
		logging.Warnf("%s failed to remove quarantined backup %s (quarantine %s): %v", phase, absoluteBackup, quarantine, err)
		return err
	}
	return nil
}
