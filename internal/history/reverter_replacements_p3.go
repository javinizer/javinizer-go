package history

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// POSTER-WRITE-HARDENING P3 — revert-ledger move-back for journalized media
// replacements. The downloader journals every destructive overwrite on the
// operation row (models.ReplacementEntry) BEFORE the new bytes land; reverting
// an operation restores the set-aside backups to their destinations with
// replace-aware rename semantics.

// NewerAppliedDestError rejects an operation revert whose destinations would
// climb above a newer journalized replacement owned by an operation that is
// not part of the current revert run and has not itself been reverted.
type NewerAppliedDestError struct {
	Destination  string
	NewerOpID    uint
	NewerMovieID string
}

func (e *NewerAppliedDestError) Error() string {
	return fmt.Sprintf("destination %s carries a newer journalized replacement from operation %d (movie %s); revert that operation first", e.Destination, e.NewerOpID, e.NewerMovieID)
}

// rejectedRevert reports a failed revert whose cause is a refusal — the
// operation row's status stays Applied so the newer owner can be reverted
// first and this operation retried.
func rejectedRevert(op *models.BatchFileOperation, errMsg string) *RevertFileResult {
	return &RevertFileResult{
		OperationID:  op.ID,
		MovieID:      op.MovieID,
		OriginalPath: op.OriginalPath,
		NewPath:      op.NewPath,
		Outcome:      models.RevertOutcomeFailed,
		Reason:       models.RevertReasonDestinationConflict,
		Error:        errMsg,
	}
}

// restoreReplacementJournal replays an operation's replacement journal in
// true reverse destination-sequence order (per destination, highest DestSeq
// first — the sequence is assigned inside the downloader's destination lock,
// so it captures the actual replace order regardless of op begin order or row
// ids). inFlight excludes the operations being reverted together in this run
// from the newer-applied rejection rule.
//
// Returns the restored paths (destinations AND consumed backups) for the
// delete-list subtraction: restored content must never be swept by the
// generated-files cleanup that follows.
//
// Consumption: every successfully restored entry is removed from the row's
// journal immediately, so a partially-failed restore leaves an accurate
// ledger. Any restore failure aborts the revert leaving the operation
// Applied (full move-back success gate).
func (r *Reverter) restoreReplacementJournal(ctx context.Context, op *models.BatchFileOperation, inFlight map[uint]bool) (map[string]bool, error) {
	restored := map[string]bool{}
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return restored, fmt.Errorf("failed to parse replacement journal for op %d: %w", op.ID, err)
	}
	if len(gf.Replacements) == 0 {
		return restored, nil
	}

	// Group entries by destination, highest sequence first per destination.
	byDest := make(map[string][]models.ReplacementEntry)
	for _, e := range gf.Replacements {
		byDest[e.Destination] = append(byDest[e.Destination], e)
	}
	for dest := range byDest {
		sort.SliceStable(byDest[dest], func(i, j int) bool { return byDest[dest][i].DestSeq > byDest[dest][j].DestSeq })
	}

	// Pre-flight the newer-applied rejection across ALL destinations before
	// any byte moves: refuse to climb above an unconsumed newer entry owned by
	// an operation outside this run.
	for dest, entries := range byDest {
		maxOwn := entries[0].DestSeq
		rows, qErr := r.batchFileOpRepo.FindOperationsByDestination(ctx, dest)
		if qErr != nil {
			return restored, fmt.Errorf("failed to inspect destination journal for %s: %w", dest, qErr)
		}
		for i := range rows {
			row := &rows[i]
			if row.ID == op.ID || inFlight[row.ID] || row.RevertStatus == models.RevertStatusReverted {
				continue
			}
			rowGf, pErr := models.ParseGeneratedFiles(row.GeneratedFiles)
			if pErr != nil {
				continue
			}
			for _, e := range rowGf.Replacements {
				if e.Destination == dest && e.DestSeq > maxOwn {
					return restored, &NewerAppliedDestError{Destination: dest, NewerOpID: row.ID, NewerMovieID: row.MovieID}
				}
			}
		}
	}

	// Execute restores. The shared per-destination lock registry serializes
	// restores against in-flight downloader overwrites of the same path.
	for dest, entries := range byDest {
		release := fsutil.SharedDestLocks().Acquire(dest)
		for _, e := range entries {
			if _, statErr := r.fs.Stat(e.Backup); statErr != nil {
				release()
				return restored, fmt.Errorf("journaled backup %s for destination %s is unreadable: %w", e.Backup, dest, statErr)
			}
			if repErr := fsutil.ReplaceFile(r.fs, e.Backup, dest); repErr != nil {
				release()
				return restored, fmt.Errorf("failed to restore %s → %s: %w", e.Backup, dest, repErr)
			}
			restored[dest] = true
			restored[e.Backup] = true
			if cErr := r.consumeReplacementEntry(ctx, op, e); cErr != nil {
				release()
				return restored, cErr
			}
		}
		release()
	}
	return restored, nil
}

// consumeReplacementEntry removes one journaled entry from the operation
// row's ledger and persists it. The in-memory op is updated too so the
// cleanup that follows sees the post-restore journal.
func (r *Reverter) consumeReplacementEntry(ctx context.Context, op *models.BatchFileOperation, entry models.ReplacementEntry) error {
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return fmt.Errorf("failed to re-parse journal for consumption on op %d: %w", op.ID, err)
	}
	kept := gf.Replacements[:0]
	removed := false
	for _, e := range gf.Replacements {
		if !removed && e.Destination == entry.Destination && e.Backup == entry.Backup && e.DestSeq == entry.DestSeq {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	gf.Replacements = kept
	data, err := json.Marshal(gf)
	if err != nil {
		return fmt.Errorf("failed to marshal consumed journal for op %d: %w", op.ID, err)
	}
	op.GeneratedFiles = string(data)
	if err := r.batchFileOpRepo.Update(ctx, op); err != nil {
		return fmt.Errorf("backup %s restored to %s but journal consumption failed for op %d: %w", entry.Backup, entry.Destination, op.ID, err)
	}
	return nil
}

// sweepJournaledDestinations runs the pre-revert targeted sweep over every
// destination journaled by the operations about to revert: crash-window
// restores land before the rejection/restore checks read destination state.
// Best-effort — a sweeper failure never blocks the revert path.
func (r *Reverter) sweepJournaledDestinations(ctx context.Context, ops []models.BatchFileOperation) {
	if r.sweeper == nil {
		return
	}
	dests := make([]string, 0, len(ops))
	for i := range ops {
		gf, err := models.ParseGeneratedFiles(ops[i].GeneratedFiles)
		if err != nil {
			continue
		}
		for _, rep := range gf.Replacements {
			dests = append(dests, rep.Destination)
		}
	}
	if len(dests) == 0 {
		return
	}
	if _, err := r.sweeper.SweepDestinations(ctx, dests); err != nil {
		logging.Warnf("pre-revert replacement sweep failed: %v (continuing with revert)", err)
	}
}

// maxJournalSeq reports the highest journaled destination sequence on an
// operation (0 when it carries no journal), used to order batch reverts in
// true reverse replace order.
func maxJournalSeq(op *models.BatchFileOperation) int64 {
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return 0
	}
	var max int64
	for _, e := range gf.Replacements {
		if e.DestSeq > max {
			max = e.DestSeq
		}
	}
	return max
}
