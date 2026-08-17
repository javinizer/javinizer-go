package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync/atomic"

	"github.com/javinizer/javinizer-go/internal/config"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
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

// rejectedRevert reports a failed revert whose cause is a refusal —
// the operation row's status stays Applied so a retry in the right order
// resolves the rejection. A `NewerAppliedDestError` marks it as an
// ordering-only rejection (R20-2); everything else (I/O, persistence)
// is reported as unexpected path state.
func rejectedRevert(op *models.BatchFileOperation, err error) *RevertFileResult {
	var chainErr *NewerAppliedDestError
	reason := models.RevertReasonUnexpectedPathState
	if errors.As(err, &chainErr) {
		reason = models.RevertReasonDestinationConflict
	}
	return &RevertFileResult{
		OperationID:  op.ID,
		MovieID:      op.MovieID,
		OriginalPath: op.OriginalPath,
		NewPath:      op.NewPath,
		Outcome:      models.RevertOutcomeFailed,
		Reason:       reason,
		Error:        err.Error(),
	}
}

// withRetryable tags the rejection as resolvable by sibling progress within
// the same run (codex P3 R6-1): a newer-APPLIED rejection is retried after
// its blocker is consumed. Hardware/restore failures are not retryable
// within the run.
func (r *RevertFileResult) withRetryable(cause error) *RevertFileResult {
	var nerr *NewerAppliedDestError
	if errors.As(cause, &nerr) {
		r.orderRetryable = true
	}
	return r
}

// restoreReplacementJournal replays an operation's replacement journal in
// true reverse destination-sequence order (per destination, highest DestSeq
// first — the sequence is assigned inside the downloader's destination lock,
// so it captures the actual replace order regardless of op begin order or row
// ids).
//
// The newer-applied rejection consults the LIVE journal only: an operation
// reverted earlier in this run has already consumed its entries (invisible
// here), while an operation whose revert failed or got skipped KEEPS its
// entries and correctly blocks older operations from climbing over it — the
// blanket "same run" exclusion could otherwise corrupt the replacement chain
// (codex P3 round 1).
//
// Returns the restored paths (destinations AND consumed backups) for the
// delete-list subtraction: restored content must never be swept by the
// generated-files cleanup that follows.
//
// Consumption: every successfully restored entry is removed from the row's
// journal immediately, so a partially-failed restore leaves an accurate
// ledger. Any restore failure aborts the revert leaving the operation
// Applied (full move-back success gate).
func (r *Reverter) restoreReplacementJournal(ctx context.Context, op *models.BatchFileOperation) (map[string]bool, error) {
	restored := map[string]bool{}
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return restored, fmt.Errorf("failed to parse replacement journal for op %d: %w", op.ID, err)
	}
	if len(gf.Replacements) == 0 {
		return restored, nil
	}

	// Group entries by destination — CANONICAL key, not the raw string
	// (codex P3 R16-1): one operation can journal the same physical path with
	// two spellings (`Poster.jpg`, `poster.jpg`, separators); raw grouping
	// would split one shared DestSeq chain into two groups and unwind them in
	// nondeterministic map order. The restore TARGET keeps the op's own
	// recorded spelling (both forms resolve to one file).
	byDest := make(map[string][]models.ReplacementEntry)
	destSpelling := make(map[string]string)
	for _, e := range gf.Replacements {
		key := fsutil.DestKey(e.Destination)
		byDest[key] = append(byDest[key], e)
		if destSpelling[key] == "" {
			destSpelling[key] = e.Destination
		}
	}
	for dest := range byDest {
		sort.SliceStable(byDest[dest], func(i, j int) bool { return byDest[dest][i].DestSeq > byDest[dest][j].DestSeq })
	}

	// codex P3 R20-1: phase-split preflight from restore. Preflight iterates
	// byDest WITHOUT locks so rejected ops halt BEFORE any bytes move — a
	// multi-destination op can then refuse while its secondary destinations
	// are still untouched (the interleaved minus hit). The per-destination
	// lock-gated phase still revisits the journal under the dest lock for
	// in-lock freshness.
	for key, entries := range byDest {
		dest := destSpelling[key]
		minOwn := entries[0].DestSeq
		for _, e := range entries {
			if e.DestSeq < minOwn {
				minOwn = e.DestSeq
			}
		}
		if rjErr := r.checkDestBlocking(ctx, op, dest, minOwn); rjErr != nil {
			return restored, rjErr
		}
	}

	// Phase 2 — per-destination restore under its lock. Same structure as
	// before: fresh journal query INSIDE the lock keeps serializable safety
	// against in-flight applies racing this revert.
	for key, entries := range byDest {
		dest := destSpelling[key]
		release := fsutil.SharedDestLocks().Acquire(dest)
		minOwn := entries[0].DestSeq
		for _, e := range entries {
			if e.DestSeq < minOwn {
				minOwn = e.DestSeq
			}
		}
		if rjErr := r.checkDestBlocking(ctx, op, dest, minOwn); rjErr != nil {
			release()
			return restored, rjErr
		}
		for _, e := range entries {
			if _, statErr := r.fs.Stat(e.Backup); statErr != nil {
				release()
				return restored, fmt.Errorf("journaled backup %s for destination %s is unreadable: %w", e.Backup, dest, statErr)
			}
			if repErr := copyRestoreBytes(r.fs, e.Backup, dest); repErr != nil {
				release()
				return restored, fmt.Errorf("failed to restore %s → %s: %w", e.Backup, dest, repErr)
			}
			restored[dest] = true
			if cErr := r.consumeReplacementEntry(ctx, op, e); cErr != nil {
				release()
				return restored, cErr
			}
			// Consumption persisted — the backup file is redundant now.
			_ = r.fs.Remove(e.Backup)
		}
		release()
	}
	return restored, nil
}

// consumeReplacementEntry removes one journaled entry from the operation
// row's ledger and persists it. The in-memory op is updated too so the
// cleanup that follows sees the post-restore journal.
func (r *Reverter) consumeReplacementEntry(ctx context.Context, op *models.BatchFileOperation, entry models.ReplacementEntry) error {
	// R15-1: serialize with every other row mutator and consume from the
	// FRESH row — never from a possibly-stale snapshot.
	release := fsutil.SharedJournalLocks().Acquire(fmt.Sprintf("%d", op.ID))
	defer release()
	fresh, frErr := r.batchFileOpRepo.FindByID(ctx, op.ID)
	if frErr != nil || fresh == nil {
		return fmt.Errorf("failed to re-read row for consumption on op %d: %v", op.ID, frErr)
	}
	gf, err := models.ParseGeneratedFiles(fresh.GeneratedFiles)
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
	fresh.GeneratedFiles = string(data)
	if err := r.batchFileOpRepo.Update(ctx, fresh); err != nil {
		return fmt.Errorf("backup %s restored to %s but journal consumption failed for op %d: %w", entry.Backup, entry.Destination, op.ID, err)
	}
	// Sync the caller's in-memory view — partial restores must be visible to
	// this op's later passes or the consumed entry is retried against a
	// vanished backup (count-N flake).
	op.GeneratedFiles = string(data)
	return nil
}

// NOTE: revert order drives safety, not exclusion: batch/scrape sorts ops
// by max journaled DestSeq descending, so a newer owner is always attempted
// first; if it succeeds its entries are consumed and the older op proceeds,
// and if it fails the entries remain visible and the older op is rejected.

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

// checkDestBlocking applies the newcomer/interleave rule against the LIVE
// journal for one destination. Caller holds the destination lock.
func (r *Reverter) checkDestBlocking(ctx context.Context, op *models.BatchFileOperation, dest string, minOwn int64) error {
	rows, qErr := r.batchFileOpRepo.FindOperationsByDestination(ctx, dest)
	if qErr != nil {
		return fmt.Errorf("failed to inspect destination journal for %s: %w", dest, qErr)
	}
	for i := range rows {
		row := &rows[i]
		if row.ID == op.ID || row.RevertStatus == models.RevertStatusReverted {
			continue
		}
		rowGf, pErr := models.ParseGeneratedFiles(row.GeneratedFiles)
		if pErr != nil {
			continue
		}
		for _, e := range rowGf.Replacements {
			if fsutil.DestKey(e.Destination) != fsutil.DestKey(dest) {
				continue
			}
			// Newer owner above the op's chain, or a foreign entry INTERLEAVED
			// strictly inside (minOwn, ...) — either way an op-granular restore
			// would cross that operation's still-applied bytes (R3-1).
			if e.DestSeq > minOwn {
				return &NewerAppliedDestError{Destination: dest, NewerOpID: row.ID, NewerMovieID: row.MovieID}
			}
		}
	}
	return nil
}

// restoreCopyNonce uniquifies the staged copy path for a destination restore.
var restoreCopyNonce atomic.Uint64

// copyRestoreBytes restores the backup bytes onto dest WITHOUT consuming the
// backup file: bytes are staged adjacent and swapped in with replace-aware
// rename; the caller removes the backup only after its journal entry is
// durably consumed.
func copyRestoreBytes(fs afero.Fs, backup, dest string) error {
	src, err := fs.Open(backup)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	defer func() { _ = src.Close() }()
	staged := dest + ".rstr." + strconv.FormatUint(restoreCopyNonce.Add(1), 16)
	// codex P3 R18h: stage with the backup's OWN permission bits so a revert
	// never widens restrictive media (0600 trailer) into world-readable.
	mode := os.FileMode(config.FilePerm)
	if info, serr := fs.Stat(backup); serr == nil {
		mode = info.Mode().Perm()
	}
	dstFile, err := fs.OpenFile(staged, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("stage restore open: %w", err)
	}
	// R5-3: stream with a bounded buffer — trailer-class backups can reach
	// gigabytes; a whole-file read would exhaust memory on revert/startup
	// recovery alike.
	buf := make([]byte, 256*1024)
	if _, cerr := io.CopyBuffer(dstFile, src, buf); cerr != nil {
		_ = dstFile.Close()
		_ = fs.Remove(staged)
		return fmt.Errorf("stage restore copy: %w", cerr)
	}
	if err := dstFile.Close(); err != nil {
		_ = fs.Remove(staged)
		return fmt.Errorf("stage restore close: %w", err)
	}
	if err := fsutil.ReplaceFile(fs, staged, dest); err != nil {
		_ = fs.Remove(staged)
		return fmt.Errorf("swap staged restore: %w", err)
	}
	return nil
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
