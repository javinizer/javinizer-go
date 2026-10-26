package history

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

type successCleanupContextKey struct{}

// Automatic cleanup is new-policy only: never inherit legacy name-only authority.
func successfulCleanupProof(entry models.ReplacementEntry) bool {
	digest, err := hex.DecodeString(entry.BackupSHA256)
	return err == nil && len(digest) == 32 && entry.BackupFactsStamped() && entry.BackupSize >= 0
}

func successfulCleanupEligible(op *models.BatchFileOperation, ledger models.GeneratedFilesJSON) bool {
	return op != nil && op.RevertStatus == models.RevertStatusApplied && ledger.SuccessfulUndoDisabled() && !ledger.PublicationPending && !ledger.UndoStarted && !models.PrimaryUndoNeedsRetention(*op)
}

// CleanupSuccessfulOperation consumes only positively owned replacement backups
// of this operation after success is durable. It never deletes output files,
// never changes legacy history, and never reads the current Undo setting.
func (s *ReplacementSweeper) CleanupSuccessfulOperation(ctx context.Context, id uint) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("successful backup cleanup: repository unavailable")
	}
	row, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("successful backup cleanup: operation %d missing", id)
	}
	ledger, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	if err != nil {
		return err
	}
	if !successfulCleanupEligible(row, ledger) || len(ledger.Replacements) == 0 {
		return nil
	}
	// Pending entries already own a two-phase cleanup intent. The ordinary sweep
	// retries these (including a vanished original or a quarantined object).
	// Never re-quarantine them based on an old pathname.
	ready := ledger.Replacements[:0:0]
	for _, entry := range ledger.Replacements {
		if !successfulCleanupProof(entry) {
			return fmt.Errorf("operation %d backup ownership proof unavailable", id)
		}
		if entry.RestorePending {
			continue
		}
		ready = append(ready, entry)
	}
	ledger.Replacements = ready
	row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
	if len(ready) > 0 {
		if err := s.PruneOperationBackups(context.WithValue(ctx, successCleanupContextKey{}, true), []models.BatchFileOperation{*row}); err != nil {
			return err
		}
	}
	// Shared-reference or other conservative refusals may return nil without
	// consuming an entry. Report the durable remainder rather than claiming clean.
	fresh, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if fresh == nil {
		return fmt.Errorf("operation %d disappeared during cleanup", id)
	}
	remaining, err := models.ParseGeneratedFiles(fresh.GeneratedFiles)
	if err != nil {
		return err
	}
	if len(remaining.Replacements) > 0 {
		return fmt.Errorf("operation %d retains backup cleanup intent for recovery", id)
	}
	return nil
}

// markSuccessfulCleanupPending proves eligibility and exact entry ownership in
// the same transaction that records cleanup intent. Unlike ordinary history
// pruning, this does not admit Undo or retire the operation's audit row.
func (s *ReplacementSweeper) markSuccessfulCleanupPending(ctx context.Context, id uint, entry models.ReplacementEntry) error {
	return s.repo.UpdateJournalInTx(ctx, id, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			return models.GeneratedFilesJSON{}, false, err
		}
		if !successfulCleanupEligible(current, ledger) {
			return ledger, false, fmt.Errorf("operation %d is not eligible for successful backup cleanup", id)
		}
		for i, live := range ledger.Replacements {
			if live.Backup != entry.Backup || live.Destination != entry.Destination {
				continue
			}
			if live != entry || !live.Installed || !successfulCleanupProof(live) {
				return ledger, false, fmt.Errorf("operation %d replacement changed before cleanup", id)
			}
			if live.RestorePending {
				if live.PendingKind() != models.RestorePendingKindPrune {
					return ledger, false, fmt.Errorf("operation %d has conflicting cleanup intent", id)
				}
				return ledger, false, nil // exact durable maintenance retry
			}
			ledger.Replacements[i].SetRestorePending(models.RestorePendingKindPrune)
			return ledger, true, nil
		}
		return ledger, false, fmt.Errorf("operation %d no longer owns backup %s", id, entry.Backup)
	})
}

// Run before index construction so newly committed cleanup and the established
// pending/quarantine recovery machinery see a fresh ledger after a restart.
func (s *ReplacementSweeper) cleanupSuccessfulOperations(ctx context.Context) error {
	rows, err := s.repo.FindOperationsWithLedger(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for i := range rows {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		ledger, err := models.ParseGeneratedFiles(rows[i].GeneratedFiles)
		if err != nil || !successfulCleanupEligible(&rows[i], ledger) || len(ledger.Replacements) == 0 {
			continue
		}
		if err := s.CleanupSuccessfulOperation(ctx, rows[i].ID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// findReplacementBackupCandidates keeps the authoritative shared-reference read
// inside the existing destination claim. Production repositories can project a
// conservative candidate set without hydrating historical NFO snapshots. Older
// repository implementations retain the fail-closed full-ledger behavior.
func (s *ReplacementSweeper) findReplacementBackupCandidates(ctx context.Context, backup string) ([]models.BatchFileOperation, error) {
	if projected, ok := s.repo.(interface {
		FindReplacementBackupCandidates(context.Context, string) ([]models.BatchFileOperation, error)
	}); ok {
		return projected.FindReplacementBackupCandidates(ctx, backup)
	}
	return s.repo.FindOperationsWithLedger(ctx)
}
