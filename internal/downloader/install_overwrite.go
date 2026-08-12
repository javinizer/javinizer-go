package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

const backupSuffixForDest = ".dlbak"

// overwriteBackupPath names the destination's backup for one operation.
// The opID is folded in as a hash (never a path component) so a stacked
// second overwrite on the same destination never clobbers the first
// operation's recoverable bytes.
func overwriteBackupPath(destPath, opID string) string {
	return destPath + backupSuffixForDest + "." + sha1hex8(opID)
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

	_, statErr := d.fs.Stat(destPath)
	switch {
	case statErr == nil:
		// replace flow below
	case os.IsNotExist(statErr):
		return false, false, fsutil.ReplaceFile(d.fs, stagedPath, destPath)
	default:
		return false, false, fmt.Errorf("failed to stat destination: %w", statErr)
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
		return false, true, fmt.Errorf("failed to replace file: %w", err)
		// the recorded entry stays: the failed op keeps its ledger row for
		// reconciliation (status failed rows are never auto-deleted).
	}
	return false, true, nil
}
