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
//
// Wave-42 (codex P2, PR#215): the quarantine handoff itself is now the
// CONDITIONAL take-aside (fsutil/bound_take.go's TakeAside shape, mirrored
// from the downloader fallback handoff in downloader/backup_handoff.go).
// The wave-36 verify-then-ReplaceFile construction re-proved the reservation
// and then REPLACED whatever occupied the name at rename time — a foreign
// plant landing between the two had its bytes silently destroyed before the
// post-move re-verify could reject. The handoff now takes the reservation
// ASIDE (no object is ever replaced), proves the reservation name free, and
// records the verified object onto it with a NO-REPLACE rename
// (moveVerifiedBackupToQuarantine carries the legs).

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
//
// Wave-36 (codex local review round 6, PR#215 finding F4): the claim ALSO
// hands the caller the reservation's own captured identity (the open
// handle's pre-close Stat). The reservation stays IDENTITY-BOUND through the
// claim→move handoff (backupQuarantineReservationStillOurs): a foreign
// writer renaming the placeholder away and planting its own occupant at the
// reserved name no longer gets its bytes silently displaced by the
// replace-aware quarantine move.
func claimBackupQuarantineName(fs afero.Fs, backup string) (string, os.FileInfo, error) {
	for attempt := 0; attempt < backupQuarantineClaimTries; attempt++ {
		var token [16]byte
		if _, err := io.ReadFull(backupQuarantineRandReader, token[:]); err != nil {
			return "", nil, fmt.Errorf("quarantine token for %s: %w", backup, err)
		}
		candidate := backup + backupQuarantineSuffix + hex.EncodeToString(token[:])
		if _, err := lstatRestoreSource(fs, candidate); err == nil {
			continue // the draw is occupied (or a wedge tombstone) — draw again
		} else if !errors.Is(err, afero.ErrFileNotFound) {
			return "", nil, fmt.Errorf("inspect quarantine candidate %s: %w", candidate, err)
		}
		reservation, rerr := fs.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case rerr == nil:
			info, serr := reservation.Stat()
			if serr != nil {
				// A reservation whose identity cannot even be read is in an
				// unknown on-disk state. Wave-r19 (codex P2, PR#215 finding
				// F3, mirroring claimOverwriteBackupPath's wave-62 fix): the
				// name's identity is UNPROVEN — between our O_EXCL create and
				// now another writer may have replaced it, so unlinking the
				// path could delete foreign bytes. Retain it for manual
				// cleanup (the name stays claimed and visible; nothing here
				// mutates on doubt).
				_ = reservation.Close()
				logging.Warnf("quarantine reservation %s left in place — its identity could not be proven (%v); manual cleanup advised", candidate, serr)
				return "", nil, fmt.Errorf("stat quarantine reservation %s: %w", candidate, serr)
			}
			if cerr := reservation.Close(); cerr != nil {
				// A reservation whose close failed is in an unknown on-disk
				// state, but its identity WAS captured. Wave-r19 (codex P2,
				// PR#215 finding F4 — the history twin): bind cleanup to the
				// captured info — re-prove the candidate still names our
				// claimed placeholder (SameFile) and unlink only when matching;
				// retain on doubt (never a pathname Remove of an unproven
				// object).
				releaseBackupQuarantineReservation(fs, candidate, info)
				return "", nil, fmt.Errorf("close quarantine reservation %s: %w", candidate, cerr)
			}
			return candidate, info, nil
		case os.IsExist(rerr):
			continue // a racer claimed this draw first — draw again
		default:
			return "", nil, fmt.Errorf("reserve quarantine candidate %s: %w", candidate, rerr)
		}
	}
	return "", nil, fmt.Errorf("quarantine names exhausted for %s after %d attempts", backup, backupQuarantineClaimTries)
}

// backupQuarantineReservationStillOurs re-derives the O_EXCL reservation's
// identity IMMEDIATELY BEFORE the quarantine move (wave-36, finding F4):
// the reserved name must still address THE claimed placeholder — dev/inode
// where the filesystem exposes them, plus size 0, mtime, and a regular
// non-symlink shape on every platform. A foreign writer swapping its own
// object onto the name between claim and move is refused with the typed
// collision class (fsutil.ErrPublishCollision): the occupant keeps its
// bytes byte-intact, the journaled backup is never quarantined over it, and
// the caller's claim-failure leg leaves the journal entry live.
func backupQuarantineReservationStillOurs(fs afero.Fs, quarantine string, claim os.FileInfo) error {
	absoluteQuarantine, _ := filepath.Abs(quarantine)
	cur, err := lstatRestoreSource(fs, quarantine)
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

// releaseBackupQuarantineReservation unlinks a still-claimed quarantine
// reservation after a failed handoff — bound to the claim's identity (same
// discipline as the downloader's releaseClaimedReservation and fsutil's
// releaseTakeAsideVacClaim): only the verified 0-byte placeholder this
// operation reserved may be removed. A reservation that VANISHED on its own
// needs no cleanup. Any other answer — foreign occupant, identity mismatch,
// indeterminate lookup — is a REFUSAL: the name is left byte-intact and the
// handoff failure surfaces with the occupant preserved. Wave-r19 (codex P2,
// PR#215 finding F3): the claim's identity is carried from claim time and
// re-proved twice at unlink adjacency (SameFile-lstat pair) before the
// Remove, so a plant swapped into the verify→Remove window keeps its bytes.
func releaseBackupQuarantineReservation(fs afero.Fs, quarantine string, claim os.FileInfo) {
	// F3 (codex P2, PR#215): the verify→Remove window —
	// backupQuarantineReservationStillOurs proved the reservation ours then
	// fs.Remove ran by pathname; a plant swapped in between had its foreign
	// bytes deleted. Carry the reservation's identity from claim time and
	// unlink ONLY when still equal at adjacency (SameFile-lstat pair: a
	// second no-follow Lstat must equal BOTH the claim record and the first
	// proof), retain with warn on doubt. Mirrors fsutil's
	// releaseTakeAsideVacClaim and the downloader's releaseClaimedReservation.
	cur, lerr := lstatRestoreSource(fs, quarantine)
	switch {
	case errors.Is(lerr, afero.ErrFileNotFound):
		return
	case lerr != nil:
		logging.Warnf("failed quarantine-reservation cleanup of %s refused — the reservation could not be inspected before its release (%v); the occupant is left byte-intact", quarantine, lerr)
		return
	case !backupQuarantinePlaceholderMatches(cur, claim):
		logging.Warnf("failed quarantine-reservation cleanup of %s refused — the reservation no longer names our claimed placeholder; the occupant is left byte-intact", quarantine)
		return
	}
	repro, rerr := lstatRestoreSource(fs, quarantine)
	if rerr != nil {
		if errors.Is(rerr, afero.ErrFileNotFound) {
			return
		}
		logging.Warnf("failed quarantine-reservation cleanup of %s refused at the adjacency re-proof (%v); the occupant is left byte-intact", quarantine, rerr)
		return
	}
	if !backupQuarantinePlaceholderMatches(repro, claim) || !backupQuarantinePlaceholderMatches(repro, cur) {
		logging.Warnf("failed quarantine-reservation cleanup of %s refused — the reservation changed identity between the proof and the unlink; the occupant is left byte-intact", quarantine)
		return
	}
	_ = fs.Remove(quarantine)
}

// backupQuarantinePlaceholderMatches reports whether cur — the object the
// take-aside landed at the taken-aside name — is still THE claimed
// reservation placeholder: regular, non-symlink, size 0, same mtime on every
// platform, and same dev/inode where the filesystem exposes them (the
// take-aside Prove binding; the shape mirrors
// backupQuarantineReservationStillOurs).
func backupQuarantinePlaceholderMatches(cur, claim os.FileInfo) bool {
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

// moveVerifiedBackupToQuarantine moves the verified backup object onto its
// reserved quarantine name — wave-42 (codex P2, PR#215): the move is the
// CONDITIONAL take-aside handoff (fsutil/bound_take.go's TakeAside shape,
// mirrored from the downloader's fallback handoff in
// downloader/backup_handoff.go). The pre-wave-42 verify-then-ReplaceFile
// construction re-proved the reservation and then replaced whatever occupied
// the name at rename time: a foreign plant landing between the re-proof and
// the rename had its bytes silently destroyed before the post-move re-verify
// could reject. ReplaceFile no longer moves src anywhere in this flow:
//
//  1. the reservation placeholder is taken ASIDE: a fresh O_EXCL-reserved
//     sibling — drawn through claimBackupQuarantineName, so the taken name
//     mixes in the crypto token with the same uniqueness discipline —
//     receives the placeholder through TakeAside's rename (replace-aware
//     ONLY against OUR OWN freshly claimed placeholder, re-proven at take
//     time), and the landed object is re-proven against the reservation's
//     claim identity at syscall adjacency. A plant swapped in after the
//     caller's re-proof is what the take moves; the proof refuses it (typed
//     fsutil.ErrPublishCollision) and it rides back onto the reservation
//     name no-replace, byte-intact;
//  2. a source-freedom proof pins the window the old check→replace left
//     open: immediately after the take the reservation name must Lstat
//     ENOENT — a racer reclaiming it mid-window is the typed collision
//     class, its plant preserved, the placeholder restored no-replace;
//  3. the verified object is recorded INTO the provably-free reservation
//     name by a NO-REPLACE rename (fsutil.PublishNoReplace). A plant winning
//     the freedom→publish gap is refused typed: BOTH the source object (it
//     was never moved — a failed publish relocates nothing) and the plant
//     survive; the compensation moves the placeholder back from the taken
//     name onto the reservation name ONLY when free (no-replace) — an
//     occupied name keeps BOTH foreign objects byte-intact and strands just
//     our own placeholder at the taken name;
//  4. only the taken name is unlinked afterwards, re-bound to the reservation
//     claim identity at unlink time (the claimed placeholder is the only
//     thing this flow ever deletes). A wedge there leaves the inert 0-byte
//     sibling for manual cleanup with a warn — sweeps never arbitrate .dlq.
//     names, and the handoff stands.
//
// Residue discipline: every failure leg above releases the still-claimed
// reservation placeholder ONLY through releaseBackupQuarantineReservation —
// proven-foreign occupants are never unlinked by this flow, and the caller's
// failure leg no longer touches the quarantine name at all (its pre-wave-42
// blind Remove could unlink a mid-window plant).
//
// Handle discipline: the open no-follow handle stays OPEN through the take
// and the publish on POSIX (the descriptor pins the inode regardless of
// names, so the post-move re-verify compares against the object that was
// actually read). Windows cannot rename a file with an open Go handle (no
// FILE_SHARE_DELETE), so the Windows-posture seam closes it immediately
// before the publish — the only rename of the handle-addressed object. A
// nil handle (the wave-35 destination undo flow —
// restore_dest_quarantine_w35) skips the close dance entirely.
func moveVerifiedBackupToQuarantine(fs afero.Fs, backup, quarantine string, reservation os.FileInfo, handle afero.File) error {
	taken, takenClaim, cerr := claimBackupQuarantineName(fs, backup)
	if cerr != nil {
		releaseBackupQuarantineReservation(fs, quarantine, reservation)
		return fmt.Errorf("reserve the take-aside name for quarantine %s: %w", quarantine, cerr)
	}
	hold, terr := fsutil.TakeAside(fsutil.TakeAsideSpec{
		FS:      fs,
		Src:     quarantine,
		Scratch: taken,
		Claim:   takenClaim,
		Prove: func(moved os.FileInfo) error {
			if !backupQuarantinePlaceholderMatches(moved, reservation) {
				return fmt.Errorf("object taken aside from %s is not the claimed reservation placeholder — foreign bytes preserved: %w", quarantine, fsutil.ErrPublishCollision)
			}
			return nil
		},
	})
	if terr != nil {
		releaseBackupQuarantineReservation(fs, quarantine, reservation)
		return fmt.Errorf("take-aside of the quarantine reservation %s: %w", quarantine, terr)
	}
	// The codex-specified proof after the take: the reservation name must
	// Lstat ENOENT (the take freed it) — a racer reclaiming it mid-window is
	// the typed collision class, its plant preserved, the placeholder
	// restored back no-replace where the name is still free (a collision
	// there keeps the foreign claimant byte-intact and strands only our own
	// placeholder).
	if _, lerr := lstatRestoreSource(fs, quarantine); lerr == nil {
		rerr := hold.Restore()
		if rerr == nil {
			releaseBackupQuarantineReservation(fs, quarantine, reservation)
		}
		return errors.Join(
			fmt.Errorf("quarantine reservation %s re-occupied between the take-aside and the source-freedom proof (plant preserved): %w", quarantine, fsutil.ErrPublishCollision),
			rerr,
		)
	} else if !errors.Is(lerr, afero.ErrFileNotFound) {
		rerr := hold.Restore()
		if rerr == nil {
			releaseBackupQuarantineReservation(fs, quarantine, reservation)
		}
		return errors.Join(
			fmt.Errorf("quarantine reservation %s indeterminate after the take-aside: %w", quarantine, lerr),
			rerr,
		)
	}
	// The reservation name is provably FREE: record the verified object onto
	// it NO-REPLACE. On any failure (collision → the plant is preserved, the
	// source never moved; kernel/IO → nothing moved) the placeholder rides
	// back no-replace and, when the restore lands, is released identity-bound.
	if handle != nil && fsutil.PathBackslashesAreSeparators {
		_ = handle.Close()
	}
	if moveErr := fsutil.PublishNoReplace(fs, backup, quarantine); moveErr != nil && !fsutil.PublishCompleted(moveErr) {
		rerr := hold.Restore()
		if rerr == nil {
			releaseBackupQuarantineReservation(fs, quarantine, reservation)
		}
		return errors.Join(
			fmt.Errorf("quarantine handoff (no-replace move of the verified object onto %s): %w", quarantine, moveErr),
			rerr,
		)
	} else if moveErr != nil {
		// Wave-44 (codex P2, PR#215 finding F1): an error carrying
		// fsutil.ErrPublishCompleted means the verified object LANDED at the
		// quarantine name (the hard-link fallback's staged-source
		// cleanup/reverify failed AFTER the destination stood and the
		// rollback also failed). The handoff is therefore INSTALLED, not
		// failed — falling into the caller's post-move verification + hold
		// construction exactly like the clean publish (the moved object was
		// verified pre-publish; the post-move reverify still re-binds the
		// quarantine name, so a vanish/substitution takes the existing
		// conservative legs). The placeholder must NOT ride back onto the
		// reservation name: the completed publish consumed the name with our
		// own bytes, so the restore would collide and join a spurious
		// restore-failure while the journal entry stayed armed against an
		// absent/foreign journaled name with the owned bytes stranded under
		// .dlq. — precisely the wave-20 OWNED-name class (the caller's entry
		// routing consumes the record only after the hold's verified
		// unlink). The staged (journaled) name may keep a residue link (the
		// staged-cleanup legs): the sweeps re-arbitrate marker-shaped
		// siblings conservatively, never unlinking unjournaled bytes.
		logging.Warnf("quarantine handoff of backup %s onto %s completed with a staged-residue error (%v) — the publish provably landed; treating the quarantine as INSTALLED like the wave-21 owned-name rule", backup, quarantine, moveErr)
	}
	// Handoff achieved: only the taken name is unlinked, re-bound against the
	// reservation claim identity at unlink time. A wedged unlink leaves the
	// inert 0-byte quarantine sibling (sweeps never arbitrate .dlq. names) —
	// the handoff stands.
	if uerr := hold.Unlink(); uerr != nil {
		logging.Warnf("take-aside release of the quarantine reservation placeholder at %s failed: %v — inert scratch retained for manual cleanup", hold.Scratch(), uerr)
	}
	return nil
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
// quarantine name for manual recovery).
// Wave-36 (codex local review round 6, PR#215 finding F3): the compensation
// result is RETURNED, not just logged — a failed move-back means the
// journaled name is UNOWNED (a foreign claimant holds it, or the publish
// failed outright) while the owned bytes sit at the quarantine name, and
// callers with a live journal entry must route it to the rearm-refused
// pending kind rather than leaving it armed or clean-pending against that
// name.
func restoreQuarantinedBackup(fs afero.Fs, phase, backup, quarantine string) error {
	if back := fsutil.PublishNoReplace(fs, quarantine, backup); back != nil {
		logging.Warnf("%s failed to restore quarantined backup %s from %s after the removal wedge: %v — the original bytes stay recoverable at the quarantine name, the journaled name is unowned", phase, backup, quarantine, back)
		return back
	}
	return nil
}

// errBackupQuarantineRestoreFailed classifies the wave-36 wedge-compensation
// failure (finding F3): the verified object moved to its quarantine name but
// could NOT be restored onto the journaled name, which is therefore unowned
// (foreign-occupied or wedged) while the owned bytes stay recoverable at the
// quarantine name. Callers with a live journal entry persist the
// rearm-refused restore-pending kind for it — no later retry may stat, copy
// from, or remove the journaled name — instead of leaving the entry armed
// (or clean-pending) against bytes nobody journals. errors.Is matches it
// through a joined wedge error chain.
var errBackupQuarantineRestoreFailed = errors.New("quarantined backup could not be restored onto its journaled name")

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
// is the open handle's own stat). Wave-35: a nil handle is the destination
// undo flow (restore_dest_quarantine_w35) — it binds the CURRENT dest
// occupant to the published restore identity itself and passes that
// snapshot as verified, so no no-follow descriptor ever exists.
// The verified object moves aside under a
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
	quarantine, reservation, cerr := claimBackupQuarantineName(fs, backup)
	if cerr != nil {
		logging.Warnf("%s could not reserve a quarantine name for backup %s: %v — journal entry retained live", phase, absoluteBackup, cerr)
		return nil, cerr
	}
	// Wave-36 (codex local review round 6, PR#215 finding F4): keep the
	// reservation IDENTITY bound through the handoff — immediately before the
	// move the reserved name must still address the claimed 0-byte
	// placeholder. A foreign writer renaming the reservation away and
	// planting its own occupant used to get its bytes silently displaced by
	// the replace-aware rename; the refusal keeps the occupant intact and
	// behaves exactly like the claim-failure leg above (journal entry live).
	if rerr := backupQuarantineReservationStillOurs(fs, quarantine, reservation); rerr != nil {
		logging.Warnf("%s refused the quarantine move for backup %s: %v — journal entry retained live", phase, absoluteBackup, rerr)
		return nil, rerr
	}
	if renErr := moveVerifiedBackupToQuarantine(fs, backup, quarantine, reservation, handle); renErr != nil {
		// Wave-42: the conditional handoff owns its residue — a failed move
		// relocated NOTHING foreign, the still-claimed reservation placeholder
		// was released identity-bound where provable, and proven-foreign
		// occupants keep their bytes. The pre-wave-42 blind Remove of the
		// quarantine name is gone: after a take-aside/publish wedge the name
		// may name a foreign plant that must never be unlinked.
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
		logging.Warnf("%s failed to re-verify quarantined backup %s (quarantine %s) before removal: %v — journal entry retained live", phase, absoluteBackup, quarantine, qerr)
		return nil, hold.restoreOrJoin(qerr)
	}
	if quarInfo == nil || quarInfo.Mode()&os.ModeSymlink != 0 || !quarInfo.Mode().IsRegular() {
		return nil, hold.restoreOrJoin(refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s is not the verified regular file", quarantine)))
	}
	if verDev, verIno, verOK := restoreSourceIdentity(verified); verOK {
		if quarDev, quarIno, quarOK := restoreSourceIdentity(quarInfo); quarOK && (verDev != quarDev || verIno != quarIno) {
			return nil, hold.restoreOrJoin(refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s is not the verified object (dev/inode mismatch) — foreign bytes preserved", quarantine)))
		}
	}
	if quarInfo.Size() != verified.Size() || !quarInfo.ModTime().Equal(verified.ModTime()) {
		return nil, hold.restoreOrJoin(refuseReplacementBackupRemoval(backup, phase, fmt.Sprintf("quarantined object at %s metadata differs from the verified object — foreign bytes preserved", quarantine)))
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
// Wave-36 (codex local review round 6, PR#215 finding F3): the move-back
// result is RETURNED to the caller. A failure means the journaled name is
// unowned (foreign-claimed or wedged) while the verified bytes sit at the
// quarantine name — the error wraps the typed errBackupQuarantineRestoreFailed
// class so callers persist the rearm-refused pending kind for the entry
// rather than leaving it armed or clean-pending against that name. A failed
// restore leaves moved=true so a later caller retry can re-attempt the
// compensation against the same quarantine name.
func (h *replacementBackupQuarantine) restore() error {
	if !h.moved || h.unlinked {
		return nil
	}
	if err := restoreQuarantinedBackup(h.fs, h.phase, h.backup, h.quarantine); err != nil {
		return fmt.Errorf("%w: %s stays recoverable at quarantine %s: %v", errBackupQuarantineRestoreFailed, h.backup, h.quarantine, err)
	}
	h.moved = false
	return nil
}

// restoreOrJoin runs the wedge move-back for internal quarantine legs and
// JOINS its failure into the wedge's own error (wave-36, finding F3): the
// entry-routing classifiers (pendingKindForRemovalError) then see the
// errBackupQuarantineRestoreFailed class through errors.Is and persist the
// rearm-refused pending kind, since the journaled name is unowned once the
// compensation failed. A successful move-back leaves the wedge error
// untouched, byte-identical to the pre-wave-36 return.
func (h *replacementBackupQuarantine) restoreOrJoin(err error) error {
	if rerr := h.restore(); rerr != nil {
		return errors.Join(err, rerr)
	}
	return err
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
		logging.Warnf("%s failed to re-verify quarantined backup %s (quarantine %s) at the unlink: %v — journal entry retained live", h.phase, absoluteBackup, h.quarantine, lerr)
		return h.restoreOrJoin(lerr)
	}
	if cur == nil || cur.Mode()&os.ModeSymlink != 0 || !cur.Mode().IsRegular() {
		return h.restoreOrJoin(refuseReplacementBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s no longer names the verified regular file at the unlink", h.quarantine)))
	}
	if quarDev, quarIno, quarOK := restoreSourceIdentity(h.quar); quarOK {
		if curDev, curIno, curOK := restoreSourceIdentity(cur); curOK && (quarDev != curDev || quarIno != curIno) {
			return h.restoreOrJoin(refuseReplacementBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s names a different object than the re-verified one at the unlink (dev/inode mismatch) — foreign bytes preserved", h.quarantine)))
		}
	}
	if cur.Size() != h.quar.Size() || !cur.ModTime().Equal(h.quar.ModTime()) {
		return h.restoreOrJoin(refuseReplacementBackupRemoval(h.backup, h.phase, fmt.Sprintf("quarantine %s metadata changed between the re-verify and the unlink — foreign bytes preserved", h.quarantine)))
	}
	// F1 (codex P2, PR#215): the final Remove ran by pathname after the
	// identity checks — a swap in the verify→Remove gap deleted the
	// replacement. The unlink now runs the file's own conditional take-aside
	// binding (vacate→rebind→unlink terminal): the proven object vacates onto
	// a fresh crypto-claimed terminal sibling, the terminal re-binds to the
	// verified identity at syscall adjacency, and only the terminal —
	// provably the verified object — is unlinked. Never a pathname Remove
	// of an unproven object; a plant swapped onto the quarantine name after
	// the re-verify rides the no-replace vacate onto the terminal, fails the
	// rebind, and rewinds byte-intact.
	if uerr := fsutil.UnlinkVerified(h.fs, h.quarantine, h.quar); uerr != nil {
		if errors.Is(uerr, fsutil.ErrTakeAsideVanished) {
			// The owned bytes vanished unownably — indeterminate retention
			// (finding R4), never a consumed removal.
			h.moved = false
			absoluteQuarantine, _ := filepath.Abs(h.quarantine)
			return fmt.Errorf("%w: %s (quarantine %s vanished under the unlink)", errReplacementBackupQuarantineVanished, absoluteBackup, absoluteQuarantine)
		}
		// Any other wedge — a foreign terminal (ErrTakeAsideForeign), a wedged
		// vacate/rebind/remove, or a wedged rewind — leaves the object
		// byte-intact: the reride inside UnlinkVerified already rewound it
		// onto the quarantine name NO-REPLACE, so restoreOrJoin moves it
		// back onto the journaled name. Never a pathname Remove of an
		// unproven object; the foreign bytes are preserved.
		logging.Warnf("%s failed to bound-unlink quarantined backup %s (quarantine %s): %v", h.phase, absoluteBackup, h.quarantine, uerr)
		return h.restoreOrJoin(uerr)
	}
	h.moved = false
	h.unlinked = true
	return nil
}
