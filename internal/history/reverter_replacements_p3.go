package history

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"

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

// ErrRestoreSourceRefused identifies a restore refusal caused by an unsafe
// backup object. Callers can classify this through errors.Is without losing
// the path-specific reason wrapped around it.
var ErrRestoreSourceRefused = errors.New("restore source refused")

// RestoreSourceRefusedError is returned when a journaled backup is not a
// regular, non-symlink file that can safely be used as a restore source.
type RestoreSourceRefusedError struct {
	Backup string
	Reason string
}

func (e *RestoreSourceRefusedError) Error() string {
	return fmt.Sprintf("restore source %s refused: %s", e.Backup, e.Reason)
}

func (e *RestoreSourceRefusedError) Unwrap() error { return ErrRestoreSourceRefused }

func refuseRestoreSource(backup, reason string) error {
	logging.Warnf("replacement restore refused for backup %s: %s; backup and journal retained", backup, reason)
	return &RestoreSourceRefusedError{Backup: backup, Reason: reason}
}

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
// Consumption: every successfully restored entry has its backup removed
// before the row is consumed. A failed backup removal leaves the entry armed
// and retryable; any later journal failure re-arms the backup before returning.
func (r *Reverter) restoreReplacementJournal(ctx context.Context, op *models.BatchFileOperation) (map[string]bool, error) {
	restored := map[string]bool{}
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return restored, fmt.Errorf("failed to parse replacement journal for op %d: %w", op.ID, err)
	}
	if len(gf.Replacements) == 0 {
		return restored, nil
	}

	// Group entries by destination — probe-aware key, not the raw string:
	// backslash separator variants match only under the Windows separator seam;
	// on POSIX a backslash remains part of the filename. Case variants match
	// only when the destination root is insensitive/tolerant. On a
	// case-sensitive root, `Poster.jpg` and `poster.jpg` remain independent
	// chains. The restore target keeps each group's recorded spelling.
	// Audit: POSIX journals carry `/` path spellings, so history correctness
	// never depended on translating a literal `\\`; Windows legacy journals
	// retain cross-form matching.
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

	// Phase 2 — per-destination restore under its process-local lock. The
	// durable marker is claimed before the fresh journal check or any backup /
	// destination read, then retained through every restore and consumption
	// leg. The closure keeps each destination's marker lifetime scoped to that
	// destination instead of deferring all releases until this method returns.
	for key, entries := range byDest {
		dest := destSpelling[key]
		restoreErr := func() error {
			release := fsutil.SharedDestLocks().Acquire(dest)
			defer release()

			// SharedDestLocks only arbitrates goroutines in this process. Claim
			// the durable marker before checking/restoring so a downloader in
			// another process cannot be between rename-to-backup and journaling
			// while this revert consumes an older backup. Acquire is deliberately
			// non-blocking for a live marker, including one owned by this process;
			// that preserves W14a's same-process liveness contract.
			busyRelease, busyErr := fsutil.AcquireReplacementBusy(r.fs, dest)
			if errors.Is(busyErr, fsutil.ErrReplacementBusy) {
				return fmt.Errorf("replacement destination %s is busy: %w", dest, busyErr)
			}
			if busyErr != nil {
				return fmt.Errorf("failed to arm replacement busy marker for %s: %w", dest, busyErr)
			}
			defer busyRelease()

			minOwn := entries[0].DestSeq
			for _, e := range entries {
				if e.DestSeq < minOwn {
					minOwn = e.DestSeq
				}
			}
			if rjErr := r.checkDestBlocking(ctx, op, dest, minOwn); rjErr != nil {
				return rjErr
			}
			for _, e := range entries {
				// Capture the original backup metadata before removal so a failed
				// journal consumption can re-arm the same permission bits and mtime.
				backupInfo, _, statErr := lstatRestoreSource(r.fs, e.Backup)
				if statErr != nil {
					return fmt.Errorf("journaled backup %s for destination %s is unreadable: %w", e.Backup, dest, statErr)
				}
				if repErr := copyRestoreBytes(r.fs, e.Backup, dest); repErr != nil {
					return fmt.Errorf("failed to restore %s → %s: %w", e.Backup, dest, repErr)
				}
				restored[dest] = true
				if rmErr := removeReplacementBackup(r.fs, e.Backup, "replacement restore"); rmErr != nil {
					if markErr := markReplacementEntryRestorePending(ctx, r.batchFileOpRepo, op.ID, sweepSlash(e.Backup)); markErr != nil {
						absoluteBackup, _ := filepath.Abs(e.Backup)
						logging.Warnf("replacement restore failed to retain cleanup marker for backup %s: %v", absoluteBackup, markErr)
					}
					return fmt.Errorf("restored %s → %s but backup cleanup failed: %w", e.Backup, dest, rmErr)
				}
				if cErr := r.consumeReplacementEntry(ctx, op, e); cErr != nil {
					if rearmErr := rearmReplacementBackup(r.fs, dest, e.Backup, backupInfo); rearmErr != nil {
						absoluteBackup, _ := filepath.Abs(e.Backup)
						logging.Warnf("replacement restore failed to re-arm backup %s after consumption failure: %v", absoluteBackup, rearmErr)
					}
					return cErr
				}
			}
			return nil
		}()
		if restoreErr != nil {
			return restored, restoreErr
		}
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
	fresh.GeneratedFiles = models.MarshalLedgerJSON(gf)
	if err := r.batchFileOpRepo.Update(ctx, fresh); err != nil {
		return fmt.Errorf("backup %s restored to %s but journal consumption failed for op %d: %w", entry.Backup, entry.Destination, op.ID, err)
	}
	// Sync the caller's in-memory view — partial restores must be visible to
	// this op's later passes or the consumed entry is retried against a
	// vanished backup (count-N flake).
	op.GeneratedFiles = fresh.GeneratedFiles
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
// journal for one destination. Caller holds the destination lock. Its
// DestKey comparisons inherit the same POSIX-literal/Windows-separator policy;
// POSIX journals use `/` spellings and do not rely on `\\` translation.
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

// lstatRestoreSource describes a restore source without following its final
// path component when the injected filesystem supports afero.Lstater. Afero's
// MemMapFs reports usedLstat=false because it has no symlink model; its
// regularity check still runs before opening, while production OsFs delegates
// to os.Lstat.
func lstatRestoreSource(fs afero.Fs, backup string) (info os.FileInfo, usedLstat bool, err error) {
	if ls, ok := fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(backup)
	}
	info, err = fs.Stat(backup)
	return info, false, err
}

// copyRestoreBytes restores the backup bytes onto dest WITHOUT consuming the
// backup file: bytes are staged adjacent and swapped in with replace-aware
// rename; the caller removes the backup before consuming its journal entry.
func copyRestoreBytes(fs afero.Fs, backup, dest string) error {
	// Lstat is deliberately before OpenFile: Stat/Open would follow a hostile
	// backup symlink and copy its target into the media directory.
	sourceInfo, _, err := lstatRestoreSource(fs, backup)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
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

	// OsFs passes this flag through to os.OpenFile. MemMapFs ignores unknown
	// read flags and has no symlink representation; the Lstat+regularity gate
	// above is therefore its available protection, with the documented residual
	// Lstat/OpenFile TOCTOU for non-OsFs implementations.
	src, err := fs.OpenFile(backup, os.O_RDONLY|restoreSourceNoFollow, 0)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	defer func() { _ = src.Close() }()

	// File.Stat is fstat for afero OsFs. Verify the object actually opened is
	// still regular, and compare identity when the OsFs Stat_t is available.
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

	stagedOrdinal := restoreCopyNonce.Add(1)
	// codex P3 R18h: stage with the backup's OWN permission bits so a revert
	// never widens restrictive media (0600 trailer) into world-readable.
	mode := openedInfo.Mode().Perm()
	staged, dstFile, err := fsutil.CreateExclusiveStagingFile(fs, dest, ".rstr", stagedOrdinal, mode)
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
	if err := fs.Chtimes(staged, openedInfo.ModTime(), openedInfo.ModTime()); err != nil {
		_ = fs.Remove(staged)
		return fmt.Errorf("stage restore times: %w", err)
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
