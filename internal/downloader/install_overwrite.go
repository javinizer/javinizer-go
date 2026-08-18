package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const (
	backupSuffixForDest  = ".dlbak"
	backupNameClaimTries = 64
	// createPublishMaxAttempts bounds the create path's collision
	// reclassification (wave-15): every pass means a foreign writer won the
	// publish against the no-replace primitive and its bytes take the
	// armed-overwrite discipline. Churn beyond the bound fails the install
	// rather than spinning against an adversarial name.
	createPublishMaxAttempts = 8
)

// backupOrdinal gives every backup attempt a process-local tail. opID alone
// cannot disambiguate: ONE operation may overwrite the same destination
// twice (e.g. poster + cropped re-write), and without the ordinal the second
// rename would clobber the first backup while both journal entries point at
// it — revert could never recover the original bytes (codex P3 round 1).
// claimOverwriteBackupPath advances past occupied tails across restarts.
var backupOrdinal atomic.Uint64
var restoreCopyOrdinal atomic.Uint64

// copyBackupToDest restores the backup bytes onto dest WITHOUT consuming the
// backup: staged adjacent write + replace-aware swap (Win-safe), streamed
// through a bounded buffer. Used by the confirm-failure rollback so the
// journal entry can never end up pointing at consumed bytes (codex P3 R9-1).
func copyBackupToDest(fsys afero.Fs, backup, dest string) error {
	return copyBackupToDestPublish(fsys, backup, dest, fsutil.ReplaceFile)
}

// copyBackupToDestNoReplace is copyBackupToDest whose staged publish NEVER
// replaces an occupied destination: callers who copy onto a name their own
// rollback just vacated (the re-arm direction) must not clobber a foreign
// object that claimed the name mid-window. A collision drops the staged copy
// and returns the typed fsutil.ErrPublishCollision (see wave-15).
func copyBackupToDestNoReplace(fsys afero.Fs, backup, dest string) error {
	return copyBackupToDestPublish(fsys, backup, dest, fsutil.PublishNoReplace)
}

func copyBackupToDestPublish(fsys afero.Fs, backup, dest string, publish func(afero.Fs, string, string) error) error {
	// Validate the path before opening it: Stat/Open would follow a hostile
	// backup symlink and copy its target into the media directory.
	sourceInfo, err := lstatRestoreSource(fsys, backup)
	if err != nil {
		return fmt.Errorf("open backup: %w", err)
	}
	if sourceInfo == nil {
		return refuseRestoreSource(backup, "filesystem returned no file information")
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return refuseRestoreSource(backup, "backup is a symlink")
	}
	if !sourceInfo.Mode().IsRegular() {
		return refuseRestoreSource(backup, fmt.Sprintf("backup is not a regular file (mode %s)", sourceInfo.Mode()))
	}

	// The open itself is platform-specific (see downloader.go's
	// restoreOpenReplacementSource): POSIX passes O_NOFOLLOW through OsFs to
	// os.OpenFile; Windows opens with FILE_FLAG_OPEN_REPARSE_POINT and refuses
	// a reparse point on the returned handle. MemMapFs has no symlink
	// representation and safely ignores the protection; the Lstat gate above
	// remains its available protection.
	src, err := restoreOpenReplacementSource(fsys, backup)
	if err != nil {
		// A no-follow open can report the race before a handle exists. Recheck
		// with Lstat so a path that became a symlink is reported as the same
		// safe refusal posture as the pre-open gate; unrelated open errors keep
		// their original classification for callers and retries.
		if currentInfo, lerr := lstatRestoreSource(fsys, backup); lerr == nil && currentInfo != nil && currentInfo.Mode()&os.ModeSymlink != 0 {
			return refuseRestoreSource(backup, "backup became a symlink before open")
		}
		return fmt.Errorf("open backup: %w", err)
	}
	defer func() { _ = src.Close() }()

	// File.Stat is fstat for afero.OsFs. Verify the object actually opened is
	// still regular, and compare identity when the platform exposes Dev/Ino.
	openedInfo, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat opened backup: %w", err)
	}
	if openedInfo == nil || openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() {
		return refuseRestoreSource(backup, "opened object is not a regular file")
	}
	if sourceDev, sourceIno, sourceOK := restoreSourceIdentity(sourceInfo); sourceOK {
		if openedDev, openedIno, openedOK := restoreSourceIdentity(openedInfo); openedOK && (sourceDev != openedDev || sourceIno != openedIno) {
			return refuseRestoreSource(backup, "opened object differs from the Lstat object")
		}
	}

	stagedOrdinal := restoreCopyOrdinal.Add(1)
	// codex P3 R18h: keep the backup's permission bits through the swap too.
	mode := openedInfo.Mode().Perm()
	staged, dstFile, err := fsutil.CreateExclusiveStagingFile(fsys, dest, ".dlrstr", stagedOrdinal, mode)
	if err != nil {
		return fmt.Errorf("stage rollback: %w", err)
	}
	buf := make([]byte, 256*1024)
	if _, cerr := io.CopyBuffer(dstFile, src, buf); cerr != nil {
		_ = dstFile.Close()
		_ = fsys.Remove(staged)
		return fmt.Errorf("copy rollback: %w", cerr)
	}
	if err := dstFile.Close(); err != nil {
		_ = fsys.Remove(staged)
		return fmt.Errorf("close rollback: %w", err)
	}
	if err := fsys.Chtimes(staged, openedInfo.ModTime(), openedInfo.ModTime()); err != nil {
		_ = fsys.Remove(staged)
		return fmt.Errorf("stage rollback times: %w", err)
	}
	// Re-apply the backup's ownership before the swap: a privileged restore of
	// another account's backup must not leave the restored bytes owned by the
	// Javinizer account once the backup is deleted. Best-effort —
	// unprivileged restores cannot chown and must still succeed.
	fsutil.RestoreStagingOwnership(fsys, staged, openedInfo)
	if err := publish(fsys, staged, dest); err != nil {
		_ = fsys.Remove(staged)
		return fmt.Errorf("swap rollback: %w", err)
	}
	return nil
}

// rearmReplacementBackup recreates a backup consumed by a rollback restore,
// using the same metadata-preserving reverse copy as the confirm rollback.
//
// Wave-16 (codex P2) — an OCCUPIED backup name is a REFUSAL, never an
// acceptance: the rollback that prompts this re-arm removed the journal's
// verified backup first, so any object occupying the name afterwards is
// FOREIGN (a rename-over/impersonation — a conflicting rollback cannot exist:
// the destination lock and busy marker serialize the whole rollback window).
// The pre-wave-16 shortcut treated a Stat-success there as success and
// silently armed the journal entry against those unrelated bytes — a later
// revert/sweep would have restored them over the destination and then deleted
// them. The refusal leaves the journal entry in its prior armed/pending
// state, keeps the foreign object byte-intact, and surfaces through the
// typed fsutil.ErrPublishCollision class so the caller's kept+warn leg logs
// the refusal reason verbatim. The reverse copy's publish is itself
// no-replace (copyBackupToDestNoReplace), closing the window between this
// classification and the staged publish for a name claimed inside it.
//
// Wave-17 (codex P2): a no-replace-UNSUPPORTED volume (typed
// fsutil.ErrPublishNoReplaceUnsupported) takes the identical conservative
// leg — the publish error propagates to the caller's kept+warn handling, the
// journal entry stays armed/pending, and replacing semantics are never used
// to force the re-arm through.
func rearmReplacementBackup(fsys afero.Fs, dest, backup string) error {
	if _, err := fsys.Stat(backup); err == nil {
		logging.Warnf("downloader: re-arm of rolled-back backup %s refused — backup name occupied by a foreign object; journal entry remains armed/pending, foreign bytes kept", backup)
		return fmt.Errorf("re-arm backup %s refused: name occupied: %w", backup, fsutil.ErrPublishCollision)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat backup for re-arm: %w", err)
	}
	return copyBackupToDestNoReplace(fsys, dest, backup)
}

// restoreAsideBackup rolls a failed install back to its set-aside backup
// WITHOUT ever renaming over a foreign writer (wave-17, codex P2): the
// window between moving the destination aside and this rollback is wide (the
// busy marker deters Javinizer participants, not foreign processes), and the
// pre-wave-17 bare os.Rename silently REPLACED a foreign file created at
// destPath inside the window — no backup, no journal entry (POSIX rename
// overwrites in place). The restore therefore publishes NO-REPLACE:
//   - success CONSUMES the backup (restored=true) and callers keep their
//     prior bookkeeping (journal release / re-arm);
//   - an occupied destination (typed fsutil.ErrPublishCollision) or a volume
//     that cannot express no-replace (typed
//     fsutil.ErrPublishNoReplaceUnsupported) REFUSES the restore: the
//     foreign file keeps destPath byte-intact, the backup is RETAINED in
//     place at backupPath as the still-armed backup — the original bytes stay
//     recoverable through the existing sweep/revert flow (journaled entry) or
//     the orphan sweep's conservative retention (record-failure leg) — and
//     the refusal surfaces as a warn while the CALLER's primary error keeps
//     surfacing unchanged;
//   - any other publish failure is a genuine restore failure (restored=false,
//     err set — bytes remain at backupPath).
func restoreAsideBackup(fsys afero.Fs, destPath, backupPath string) (bool, error) {
	switch err := fsutil.PublishNoReplace(fsys, backupPath, destPath); {
	case err == nil:
		return true, nil
	case rollbackRestoreRefused(err):
		logging.Warnf("downloader: rollback restore of %s refused — %v; backup %s retained in place (foreign/unsupported destination never clobbered, original bytes stay recoverable)", destPath, err, backupPath)
		return false, nil
	default:
		return false, err
	}
}

// rollbackRestoreRefused reports whether a failed no-replace rollback restore
// is one of the REFUSAL classes (foreign-occupied destination,
// no-replace-unsupported volume) whose conservative posture retains the
// backup and warns rather than erroring: in both classes nothing was
// attempted that could have touched the destination's bytes.
func rollbackRestoreRefused(err error) bool {
	return errors.Is(err, fsutil.ErrPublishCollision) ||
		errors.Is(err, fsutil.ErrPublishNoReplaceUnsupported)
}

// removeRollbackBackup follows the established ownership-cleanup rule: a
// missing backup is already removed, while any other error retains durable
// journal ownership so a later sweep/retry can try the removal again.
func removeRollbackBackup(fsys afero.Fs, backup, phase string) error {
	if err := fsys.Remove(backup); err != nil && !os.IsNotExist(err) {
		logging.Warnf("downloader: %s failed to remove backup %s: %v; journal entry remains armed", phase, backup, err)
		return err
	}
	return nil
}

// overwriteBackupPath names the destination's backup for one replacement:
// opID folded as a hash (never a path component) plus a process-unique
// ordinal, so stacked same-op or cross-op overwrites never clobber a backup.
func overwriteBackupPath(destPath, opID string) string {
	return destPath + backupSuffixForDest + "." + sha1hex8(opID) + "." + strconv.FormatUint(backupOrdinal.Add(1), 16)
}

// sha1hex8 folds an op's identity to 16 lowercase hex chars for backup path
// naming — ledger identity must never inject path segments.
func sha1hex8(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

// lstatBackupCandidate treats every existing directory entry — including a
// symlink, directory, or older backup — as occupied. OsFs uses Lstat, while
// MemMapFs has no symlink model and safely falls back to Stat.
func lstatBackupCandidate(fsys afero.Fs, candidate string) (os.FileInfo, error) {
	if ls, ok := fsys.(afero.Lstater); ok {
		info, _, err := ls.LstatIfPossible(candidate)
		return info, err
	}
	return fsys.Stat(candidate)
}

// claimOverwriteBackupPath chooses a free destination-adjacent backup name
// and ATOMICALLY RESERVES it: after observing a free candidate the name is
// claimed with O_CREATE|O_EXCL, closing the Lstat-to-Rename window in which
// a foreign writer (one that does not honor the .dlbusy marker) could occupy
// the observed-free name — POSIX rename would then silently overwrite its
// bytes before the journal ever saw them (codex PR#215). An O_EXCL collision
// on an observed-free candidate means a racer reserved it first and the
// claim climbs to the next name. The returned placeholder is a 0-byte file;
// the caller's dest→backup handoff (moveIntoReservedBackup — replace-aware,
// ReplaceFile on Windows) safely replaces the reservation, so every
// participant either wins a unique name or fails closed.
// moveIntoReservedBackup hands the destination bytes to the atomically
// RESERVED backup name returned by claimOverwriteBackupPath: that name is
// occupied by the claim's 0-byte placeholder, so the handoff must REPLACE an
// existing target. POSIX rename does so atomically; Windows rename
// (OsFs.Rename → MoveFileW) REFUSES an existing destination, which turned
// every ledger-armed overwrite of an existing file into a set-aside failure
// on Windows (codex PR#215 w12). The Windows leg therefore routes through
// fsutil.ReplaceFile (OsFs → MoveFileExW with MOVEFILE_REPLACE_EXISTING).
// The leg is keyed on the same fsutil.PathBackslashesAreSeparators
// Windows-posture seam the history package's restoreOSPath/DestKey use —
// instead of a build tag — so the Windows branch is exercisable in host
// tests; both legs are behaviorally identical on a POSIX host because
// POSIX ReplaceFile is itself a rename. The set-aside is the only leg that
// renames into a reserved (pre-existing placeholder) target; the rollback
// restores below publish onto a path the set-aside just vacated with
// NO-REPLACE semantics (restoreAsideBackup, wave-17), so a foreign dest
// claimed mid-window is refused and kept instead of clobbered (Windows's
// MoveFileExW-without-replace refusal maps into the same retained class).
func moveIntoReservedBackup(fsys afero.Fs, src, dst string) error {
	if fsutil.PathBackslashesAreSeparators {
		return fsutil.ReplaceFile(fsys, src, dst)
	}
	return fsys.Rename(src, dst)
}

func claimOverwriteBackupPath(fsys afero.Fs, destPath, opID string) (string, error) {
	for attempt := 0; attempt < backupNameClaimTries; attempt++ {
		candidate := overwriteBackupPath(destPath, opID)
		if _, err := lstatBackupCandidate(fsys, candidate); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect backup candidate %s: %w", candidate, err)
		}
		reservation, reserveErr := fsys.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case reserveErr == nil:
			if err := reservation.Close(); err != nil {
				// A reservation whose close failed is in an unknown on-disk
				// state — drop it rather than renaming over unverified bytes.
				_ = fsys.Remove(candidate)
				return "", fmt.Errorf("close backup reservation %s: %w", candidate, err)
			}
			return candidate, nil
		case os.IsExist(reserveErr):
			// A foreign claimant won the Lstat-to-OpenFile window; climb.
			continue
		default:
			return "", fmt.Errorf("reserve backup candidate %s: %w", candidate, reserveErr)
		}
	}
	return "", fmt.Errorf("backup names exhausted for %s after %d attempts", destPath, backupNameClaimTries)
}

// installOverwriting installs staged (already-downloaded) bytes onto
// destPath under the per-destination lock with the replace-ledger discipline
// (POSTER-WRITE-HARDENING P3):
//
//  1. The durable .dlbusy marker is claimed BEFORE any existence
//     classification and held through BOTH the create and replace paths:
//     a foreign process may hold the marker while it has the destination
//     renamed aside, so an observed-absent destination must never take the
//     create path under a live owner (codex PR#215). The deferred release
//     runs on every exit, including the busy-refusal and error returns.
//  2. Existence is classified INSIDE the lock with Lstat semantics so a
//     dangling symlink is refused instead of falling into the create path
//     (os.Stat follows links and would report ENOENT, replacing the link
//     object with no ledger entry). Concurrent writers serialize; the
//     second operation measures the just-installed bytes correctly.
//  3. A create (nothing at destination) installs directly — no ledger arm.
//  4. A replace requires the revert ledger armed (operation ID recorded);
//     without it the overwrite is refused: destination preserved, skip+warn.
//  5. The pre-existing bytes are moved aside to an atomically reserved
//     per-operation backup; the record is journaled BEFORE the replace; a
//     record-or-replace failure restores the backup under the same lock.
//
// Returns (skipped, replaced, err): skipped reports a refused destructive
// overwrite (destination unchanged, no error); replaced reports the final
// in-lock classification the callers' results carry.
func (d *Downloader) installOverwriting(ctx context.Context, stagedPath, destPath string, ledger downloadLedger) (bool, bool, error) {
	release := d.destLocks.Acquire(destPath)
	// codex PR#215 R22-3: a caller canceled while queued on the destination
	// lock must not publish staged media after the lock is finally granted.
	if cerr := ctx.Err(); cerr != nil {
		release()
		return false, true, cerr
	}
	defer release()

	// codex PR#215: the busy marker is armed BEFORE the destination is
	// classified. Marked-absence is not proof of absence — the owner may be
	// mid-replacement — so the claim precedes both the create path and the
	// replacement path. AcquireReplacementBusy self-cleans on error, so only
	// a successful claim needs the deferred release below.
	busyRelease, busyErr := fsutil.AcquireReplacementBusy(d.fs, destPath)
	if errors.Is(busyErr, fsutil.ErrReplacementBusy) {
		logging.Warnf("downloader: overwrite of %s refused — another process owns the replacement", destPath)
		return true, true, nil
	}
	if busyErr != nil {
		return false, true, fmt.Errorf("failed to arm replacement busy marker for %s: %w", destPath, busyErr)
	}
	defer busyRelease()

	// codex PR#215: classify existence with Lstat, not Stat. os.Stat follows
	// symlinks, so a DANGLING symlink at the destination reports ENOENT and
	// the create path would overwrite the link object itself — no ledger
	// entry, no symlink refusal. Lstat sees the link object directly.
	//
	// codex PR#215 wave-15 (P2): the create path publishes through
	// fsutil.PublishNoReplace. Between the Lstat-ENOENT here and a plain
	// ReplaceFile a foreign writer can claim the destination, and the replace
	// would destroy the racer's bytes with no backup and no ledger entry
	// while the download reports success. An occupied destination is therefore
	// a RECLASSIFICATION, not a failure: the destination is now present, so
	// it falls into the armed-overwrite discipline below and the racer's
	// bytes are set aside + journaled exactly like any other pre-existing
	// destination. The bounded loop also tolerates a racer whose entry is
	// created and deleted again across repeated publishes.
	//
	// Wave-17 (codex P2): a volume that cannot express an atomic no-replace
	// publish at all (renameat2 AND hard links both unsupported — FAT/exFAT)
	// answers with the typed fsutil.ErrPublishNoReplaceUnsupported, which is
	// a REFUSAL, not a reclassification: the install fails cleanly through
	// the plain publish-error leg below (destination untouched, staged file
	// retained, nothing journaled) — replacing semantics are never used to
	// paper over the missing primitive.
	info, statErr := lstatBackupCandidate(d.fs, destPath)
	for createAttempts := 0; statErr != nil && os.IsNotExist(statErr); createAttempts++ {
		if createAttempts >= createPublishMaxAttempts {
			return false, true, fmt.Errorf("failed to install %s: foreign writers claimed the destination across %d no-replace publishes: %w", destPath, createAttempts, fsutil.ErrPublishCollision)
		}
		pubErr := fsutil.PublishNoReplace(d.fs, stagedPath, destPath)
		if !errors.Is(pubErr, fsutil.ErrPublishCollision) {
			return false, false, pubErr
		}
		logging.Warnf("downloader: create install of %s raced a foreign writer — no-replace publish preserved its bytes; reclassifying destination as present", destPath)
		info, statErr = lstatBackupCandidate(d.fs, destPath)
	}
	switch {
	case statErr != nil:
		return false, false, fmt.Errorf("failed to stat destination: %w", statErr)
	case info == nil:
		return false, false, fmt.Errorf("failed to stat destination %s: filesystem returned no file information", destPath)
	}

	// R20-1/R20-3 type-discipline: the ledger legs only model REGULAR files.
	// A non-regular destination would be moved into a .dlbak backup that the
	// restore path cannot safely consume; a symlink (Lstat never follows) is
	// refused with the link itself intact.
	// All such objects are refused pre-journal (skip+warn — existing object untouched).
	if info.Mode()&os.ModeSymlink != 0 {
		logging.Warnf("downloader: overwrite of %s refused — destination is a symlink; keeping the link intact", destPath)
		return true, true, nil
	}
	if info.IsDir() {
		logging.Warnf("downloader: overwrite of %s refused — destination is a directory; keeping it intact", destPath)
		return true, true, nil
	}
	if !info.Mode().IsRegular() {
		logging.Warnf("downloader: overwrite of %s refused — destination is not a regular file (mode %s); keeping it intact", destPath, info.Mode())
		return true, true, nil
	}

	if !ledger.armed() {
		logging.Warnf("downloader: overwrite of %s refused — no revert-ledger operation recorded; keeping existing bytes", destPath)
		return true, true, nil
	}

	// The marker armed above covers the entire claim-aside/journal/install/
	// confirm window below: it is visible to a CLI/startup sweep in another
	// process, and its PID makes dead owners reclaimable instead of retaining
	// crash leftovers forever.
	backupPath, claimErr := claimOverwriteBackupPath(d.fs, destPath, ledger.opID)
	if claimErr != nil {
		return false, true, fmt.Errorf("failed to claim backup path for %s: %w", destPath, claimErr)
	}
	if err := moveIntoReservedBackup(d.fs, destPath, backupPath); err != nil {
		// The failed handoff left our 0-byte reservation in place — release it
		// so a retry never has to climb past (or worse, journal) a placeholder.
		_ = d.fs.Remove(backupPath)
		return false, true, fmt.Errorf("failed to set aside existing bytes for %s: %w", destPath, err)
	}
	if err := ledger.recorder.RecordReplacement(ctx, ledger.opID, destPath, backupPath); err != nil {
		restored, rErr := restoreAsideBackup(d.fs, destPath, backupPath)
		if rErr != nil {
			return false, true, fmt.Errorf("revert-ledger record failed: %w (AND backup restore failed: %v — bytes remain at %s)", err, rErr, backupPath)
		}
		if !restored {
			// wave-17: the rollback was REFUSED (a foreign file claimed the
			// vacated destination, or the volume cannot express no-replace).
			// The foreign file is never replaced or removed; the retained
			// backup is UNJOURNALED here (the record failed) and stays
			// recoverable through the orphan sweep's conservative retention.
			return false, true, fmt.Errorf("revert-ledger record failed for %s: %w (rollback restore refused — destination occupied or no-replace unsupported; backup retained at %s with no journal entry)", destPath, err, backupPath)
		}
		return false, true, fmt.Errorf("revert-ledger record failed for %s: %w", destPath, err)
	}
	if err := fsutil.ReplaceFile(d.fs, stagedPath, destPath); err != nil {
		restored, rErr := restoreAsideBackup(d.fs, destPath, backupPath)
		if rErr != nil {
			return false, true, fmt.Errorf("failed to replace %s: %w (AND backup restore failed: %v — bytes remain at %s)", destPath, err, rErr, backupPath)
		}
		if !restored {
			// wave-17: rollback REFUSED — the foreign destination keeps its
			// bytes and the journal entry stays armed against the retained
			// backup (still at backupPath): a later sweep/revert recovers the
			// original bytes. Nothing to retract — the backup was NOT consumed.
			return false, true, fmt.Errorf("failed to replace %s: %w (rollback restore refused — destination occupied or no-replace unsupported; journal entry stays armed against the retained backup %s)", destPath, err, backupPath)
		}
		// The backup was consumed by the rollback restore — retract the journal
		// entry or the row permanently points at a vanished backup and every
		// later revert of this op fails stat-ing it (codex P3 round 1).
		if relErr := ledger.recorder.ReleaseReplacement(ctx, ledger.opID, destPath, backupPath); relErr != nil {
			logging.Warnf("downloader: release of rolled-back journal entry failed for %s: %v (destination is correct); re-arming backup", destPath, relErr)
			if rearmErr := rearmReplacementBackup(d.fs, destPath, backupPath); rearmErr != nil {
				logging.Warnf("downloader: re-arm of rolled-back backup failed for %s: %v (journal entry remains armed)", backupPath, rearmErr)
			}
		}
		return false, true, fmt.Errorf("failed to replace file: %w", err)
	}
	// R4-3/R5-2/R9-1: confirm the install so the sweeper can distinguish
	// "backup journaled but install never landed" from "installed media
	// deleted afterwards". An unconfirmed entry MUST NOT outlive a
	// successful return: a transient confirm failure rolls the install back
	// WITHOUT consuming the backup (staged copy + swap). The backup is then
	// removed while the journal still owns it, before the successful retract.
	// If retract itself fails, re-arm the backup so the still-armed journal
	// remains mutually consistent for sweep/retry.
	if cErr := ledger.recorder.ConfirmReplacement(ctx, ledger.opID, destPath, backupPath); cErr != nil {
		if rErr := copyBackupToDest(d.fs, backupPath, destPath); rErr != nil {
			return false, true, fmt.Errorf("install-confirm failed: %w (AND rollback restore failed: %v — bytes remain at %s)", cErr, rErr, backupPath)
		}
		if rmErr := removeRollbackBackup(d.fs, backupPath, "install-confirm rollback"); rmErr != nil {
			return false, true, fmt.Errorf("install-confirm failed, rolled back to pre-existing bytes, but backup cleanup failed: %w (confirmation error: %v; entry stays armed)", rmErr, cErr)
		}
		if relErr := ledger.recorder.ReleaseReplacement(ctx, ledger.opID, destPath, backupPath); relErr != nil {
			logging.Warnf("downloader: release of install-confirm rollback entry failed for %s: %v; re-arming backup", destPath, relErr)
			if rearmErr := rearmReplacementBackup(d.fs, destPath, backupPath); rearmErr != nil {
				logging.Warnf("downloader: re-arm of install-confirm rollback backup failed for %s: %v (journal entry remains armed)", backupPath, rearmErr)
			}
			return false, true, fmt.Errorf("install-confirm retract failed after rollback (%v): %w (backup %s re-arm attempted; entry stays armed for the sweeper)", cErr, relErr, backupPath)
		}
		return false, true, fmt.Errorf("install-confirm failed, rolled back to pre-existing bytes: %w", cErr)
	}
	return false, true, nil
}
