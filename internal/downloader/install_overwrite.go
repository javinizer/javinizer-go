package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const backupSuffixForDest = ".dlbak"

// backupOrdinal gives every backup leg a never-repeating tail. opID alone
// cannot disambiguate: ONE operation may overwrite the same destination
// twice (e.g. poster + cropped re-write), and without the ordinal the second
// rename would clobber the first backup while both journal entries point at
// it — revert could never recover the original bytes (codex P3 round 1).
var backupOrdinal atomic.Uint64
var restoreCopyOrdinal atomic.Uint64

// copyBackupToDest restores the backup bytes onto dest WITHOUT consuming the
// backup: staged adjacent write + replace-aware swap (Win-safe), streamed
// through a bounded buffer. Used by the confirm-failure rollback so the
// journal entry can never end up pointing at consumed bytes (codex P3 R9-1).
func copyBackupToDest(fsys afero.Fs, backup, dest string) error {
	src, err := fsys.Open(backup)
	if err != nil {
		return fmt.Errorf("open backup: %w", err)
	}
	defer func() { _ = src.Close() }()
	staged := dest + ".dlrstr." + strconv.FormatUint(restoreCopyOrdinal.Add(1), 16)
	// codex P3 R18h: keep the backup's permission bits through the swap too.
	mode := os.FileMode(0o644)
	if info, serr := fsys.Stat(backup); serr == nil {
		mode = info.Mode().Perm()
	}
	dstFile, err := fsys.OpenFile(staged, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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
	if err := fsutil.ReplaceFile(fsys, staged, dest); err != nil {
		_ = fsys.Remove(staged)
		return fmt.Errorf("swap rollback: %w", err)
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

// installOverwriting installs staged (already-downloaded) bytes onto
// destPath under the per-destination lock with the replace-ledger discipline
// (POSTER-WRITE-HARDENING P3):
//
//  1. Existence is classified INSIDE the lock (concurrent writers serialize;
//     the second operation measures the just-installed bytes correctly).
//  2. A create (nothing at destination) installs directly — no ledger arm.
//  3. A replace requires the revert ledger armed (operation ID recorded);
//     without it the overwrite is refused: destination preserved, skip+warn.
//  4. The pre-existing bytes are moved aside to a per-operation backup; the
//     record is journaled BEFORE the replace; a record-or-replace failure
//     restores the backup under the same lock.
//
// Returns (skipped, replaced, err): skipped reports a refused destructive
// overwrite (destination unchanged, no error); replaced reports the final
// in-lock classification the callers' results carry.
func (d *Downloader) installOverwriting(ctx context.Context, stagedPath, destPath string, ledger downloadLedger) (bool, bool, error) {
	release := d.destLocks.Acquire(destPath)
	defer release()

	info, statErr := d.fs.Stat(destPath)
	switch {
	case statErr != nil && os.IsNotExist(statErr):
		return false, false, fsutil.ReplaceFile(d.fs, stagedPath, destPath)
	case statErr != nil:
		return false, false, fmt.Errorf("failed to stat destination: %w", statErr)
	}

	// R20-1/R20-3 type-discipline: the ledger legs only model REGULAR files.
	// A directory destination would rename a whole tree into a .dlbak backup
	// that copyRestoreBytes then refuses; a symlinked destination whiffed by
	// Stat-follows-link semantics would come back a regular file.
	// Both are refused pre-journal (skip+warn — existing object untouched).
	if info.IsDir() {
		logging.Warnf("downloader: overwrite of %s refused — destination is a directory; keeping it intact", destPath)
		return true, true, nil
	}
	if Ls, ok := d.fs.(afero.Lstater); ok {
		if li, _, lerr := Ls.LstatIfPossible(destPath); lerr == nil && li.Mode()&os.ModeSymlink != 0 {
			logging.Warnf("downloader: overwrite of %s refused — destination is a symlink; keeping the link intact", destPath)
			return true, true, nil
		}
	}

	if !ledger.armed() {
		logging.Warnf("downloader: overwrite of %s refused — no revert-ledger operation recorded; keeping existing bytes", destPath)
		return true, true, nil
	}

	backupPath := overwriteBackupPath(destPath, ledger.opID)
	if err := d.fs.Rename(destPath, backupPath); err != nil {
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
			logging.Warnf("downloader: release of rolled-back journal entry failed for %s: %v (sweep retains it; destination is correct)", destPath, relErr)
		}
		return false, true, fmt.Errorf("failed to replace file: %w", err)
	}
	// R4-3/R5-2/R9-1: confirm the install so the sweeper can distinguish
	// "backup journaled but install never landed" from "installed media
	// deleted afterwards". An unconfirmed entry MUST NOT outlive a
	// successful return: a transient confirm failure rolls the install back
	// WITHOUT consuming the backup (staged copy + swap), so even a
	// simultaneous retract failure leaves entry + backup + restored
	// destination mutually consistent for sweep/retry.
	if cErr := ledger.recorder.ConfirmReplacement(ctx, ledger.opID, destPath, backupPath); cErr != nil {
		if rErr := copyBackupToDest(d.fs, backupPath, destPath); rErr != nil {
			return false, true, fmt.Errorf("install-confirm failed: %w (AND rollback restore failed: %v — bytes remain at %s)", cErr, rErr, backupPath)
		}
		if relErr := ledger.recorder.ReleaseReplacement(ctx, ledger.opID, destPath, backupPath); relErr != nil {
			return false, true, fmt.Errorf("install-confirm retract failed after rollback (%v): %w (backup %s intact — entry stays armed for the sweeper)", cErr, relErr, backupPath)
		}
		return false, true, fmt.Errorf("install-confirm failed, rolled back to pre-existing bytes: %w", cErr)
	}
	return false, true, nil
}
