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
// Re-arm callers reverse backup and dest to copy restored destination bytes
// back into a consumed backup using the same metadata-preserving semantics.
func copyBackupToDest(fsys afero.Fs, backup, dest string) error {
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
	if err := fsutil.ReplaceFile(fsys, staged, dest); err != nil {
		_ = fsys.Remove(staged)
		return fmt.Errorf("swap rollback: %w", err)
	}
	return nil
}

// rearmReplacementBackup recreates a backup consumed by a rollback restore.
// An existing backup wins: another rollback may already have re-armed it, and
// retention must never clobber those bytes. The reverse copy reuses
// copyBackupToDest's exclusive adjacent staging, atomic replace, source mode,
// and source ModTime preservation.
func rearmReplacementBackup(fsys afero.Fs, dest, backup string) error {
	if _, err := fsys.Stat(backup); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat backup for re-arm: %w", err)
	}
	return copyBackupToDest(fsys, dest, backup)
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
// the caller's dest→backup rename (and the Windows ReplaceFile swap) safely
// replaces the reservation, so every participant either wins a unique name
// or fails closed.
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
	info, statErr := lstatBackupCandidate(d.fs, destPath)
	switch {
	case statErr != nil && os.IsNotExist(statErr):
		return false, false, fsutil.ReplaceFile(d.fs, stagedPath, destPath)
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
	if err := d.fs.Rename(destPath, backupPath); err != nil {
		// The failed rename left our 0-byte reservation in place — release it
		// so a retry never has to climb past (or worse, journal) a placeholder.
		_ = d.fs.Remove(backupPath)
		return false, true, fmt.Errorf("failed to set aside existing bytes for %s: %w", destPath, err)
	}
	if err := ledger.recorder.RecordReplacement(ctx, ledger.opID, destPath, backupPath); err != nil {
		if rErr := d.fs.Rename(backupPath, destPath); rErr != nil {
			return false, true, fmt.Errorf("revert-ledger record failed: %w (AND backup restore failed: %v — bytes remain at %s)", err, rErr, backupPath)
		}
		return false, true, fmt.Errorf("revert-ledger record failed for %s: %w", destPath, err)
	}
	if err := fsutil.ReplaceFile(d.fs, stagedPath, destPath); err != nil {
		if rErr := d.fs.Rename(backupPath, destPath); rErr != nil {
			return false, true, fmt.Errorf("failed to replace %s: %w (AND backup restore failed: %v — bytes remain at %s)", destPath, err, rErr, backupPath)
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
