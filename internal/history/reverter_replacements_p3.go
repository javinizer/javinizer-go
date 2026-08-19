package history

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
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
// and retryable; any later journal failure re-arms the backup before
// returning — and when that re-arm FAILS with ANY class (wave-20, codex P2,
// PR#215), the entry is instead marked RestorePending with the kind
// rearmPendingKind derives: the occupied-name refusal classes (fsutil
// .ErrPublishCollision / ErrPublishNoReplaceUnsupported, rounds 18+19) and
// every pre-publish failure leave the backup name foreign or absent and map
// to the rearm-refused kind — the pending retry consumes the entry WITHOUT
// any path operation against the unowned name — while a failure after the
// staged copy definitely published keeps this operation's bytes at the name
// and maps to the clean kind. An entry left armed against an unproven name
// must never be reachable from here: foreign occupants corrupt restores,
// and absent names wedge the retry's source stat forever.
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
				// The operation snapshot can predate another revert loop's
				// consumption. Re-read before touching the backup: a consumed entry
				// means the other loop already restored these bytes, so reopening
				// (and restoring) it would turn a harmless duplicate revert into a
				// missing-backup failure or a double restore.
				armed, pendingKind, freshErr := r.replacementEntryIsLive(ctx, op, e)
				if freshErr != nil {
					return freshErr
				}
				if !armed {
					continue
				}
				restorePending := pendingKind != ""

				// Capture the destination state before replacing it. A clean
				// missing result is the R9-2 crash-window state; an existing (or
				// indeterminate) destination must never be deleted as compensation.
				//
				// codex PR#215 w12: classify with Lstat, NOT Stat/afero.Exists —
				// exactly the wave-11 sweep classifier (restoreAndConsume,
				// sweepOne): Stat FOLLOWS a dangling symlink and reports it ENOENT,
				// so the retention rule read a pre-existing link object as
				// "absent"; when backup removal then failed AND RestorePending
				// persistence failed too, the compensation deleted the restored
				// destination even though a directory entry predated the restore,
				// vacuuming bytes that entry's presence must protect. Any
				// Lstat-success object — symlink included — is PRESENT; only a
				// genuine Lstat ENOENT is missing; every other Lstat error stays
				// conservatively present.
				_, destLstatErr := lstatRestoreSource(r.fs, dest)
				destMissingBeforeRestore := errors.Is(destLstatErr, afero.ErrFileNotFound)

				// Wave-19 (codex P2): a rearm-refused pending entry's backup name
				// is UNOWNED — a refused no-replace re-arm left it foreign-
				// occupied or absent. The retry therefore runs NO backup-path
				// operation at all: neither the source stat (an absent name would
				// fail every explicit revert forever) nor the removal below (a
				// foreign occupant must never be deleted). The destination
				// certified in place when the marker was written must still be
				// present before the only journal record of the restore is
				// consumed.
				if restorePending && pendingKind == models.RestorePendingKindRearmRefused {
					if destLstatErr != nil {
						return fmt.Errorf("journaled rearm-refused pending entry for destination %s cannot be consumed: destination unreadable: %w", dest, destLstatErr)
					}
					restored[dest] = true
					if cErr := r.consumeReplacementEntry(ctx, op, e); cErr != nil {
						absoluteBackup, _ := filepath.Abs(e.Backup)
						logging.Warnf("replacement restore %s: consumption of a rearm-refused pending entry failed: %v — unowned backup name untouched, durable marker retained for retry", absoluteBackup, cErr)
						return cErr
					}
					continue
				}

				// Capture the original backup metadata before removal so a failed
				// journal consumption can re-arm the same permission bits and mtime.
				backupInfo, statErr := lstatRestoreSource(r.fs, e.Backup)
				if statErr != nil {
					return fmt.Errorf("journaled backup %s for destination %s is unreadable: %w", e.Backup, dest, statErr)
				}
				// A live entry carrying the durable RestorePending marker has its
				// destination bytes certified in place already: only the backup
				// cleanup and the journal consumption remain. NEVER copy from the
				// backup path for a pending entry (codex P2, PR#215 round 18):
				// the marker certifies the destination carries the restored
				// bytes, and restoring anything onto it would destroy them. Skip
				// the copy and land dest in the delete-subtraction map; the
				// cleanup/consumption legs below run unchanged for the legacy
				// clean kind. (The rearm-refused kind never reaches this
				// copy-or-remove region: it consumed above without touching the
				// backup path.)
				if !restorePending {
					if repErr := copyRestoreBytes(r.fs, e.Backup, dest); repErr != nil {
						return fmt.Errorf("failed to restore %s → %s: %w", e.Backup, dest, repErr)
					}
				}
				restored[dest] = true
				if rmErr := removeReplacementBackup(r.fs, e.Backup, "replacement restore"); rmErr != nil {
					if markErr := markReplacementEntryRestorePending(ctx, r.batchFileOpRepo, op.ID, sweepSlash(e.Backup)); markErr != nil {
						absoluteBackup, _ := filepath.Abs(e.Backup)
						logging.Warnf("replacement restore failed to retain cleanup marker for backup %s: %v", absoluteBackup, markErr)
						// Without a durable marker, only undo a restore whose
						// destination was proven missing before the copy. That is the
						// armed-entry + intact-backup R9-2 compensation state.
						if destMissingBeforeRestore {
							if undoErr := r.fs.Remove(dest); undoErr != nil {
								logging.Warnf("replacement restore %s: cleanup marker persistence failed AND restore-undo failed (%v after %v)", absoluteBackup, undoErr, markErr)
							} else {
								logging.Warnf("replacement restore %s: cleanup marker persistence failed (%v) — restore undone, will retry", absoluteBackup, markErr)
							}
						} else {
							// The destination was not proven missing before this restore.
							// Keep the backup bytes in place and leave the armed entry
							// for an explicit retry; removing them would create a hole.
							logging.Warnf("replacement restore %s: cleanup marker persistence failed (%v) — restored destination retained for retry", absoluteBackup, markErr)
						}
					}
					return fmt.Errorf("restored %s → %s but backup cleanup failed: %w", e.Backup, dest, rmErr)
				}
				if cErr := r.consumeReplacementEntry(ctx, op, e); cErr != nil {
					if rearmErr := rearmReplacementBackup(r.fs, dest, e.Backup, backupInfo); rearmErr != nil {
						absoluteBackup, _ := filepath.Abs(e.Backup)
						// codex P2 (PR#215 rounds 18+19+20): the re-arm could not
						// re-establish the armed, backup-present retry posture, so
						// the entry must NOT stay ARMED regardless of the failure
						// class — a foreign occupant would be restored over the
						// destination and then deleted (rounds 18+19), and wave-20
						// closes the NON-refusal hole: a plain re-arm failure left
						// the entry armed against an ABSENT backup, so every later
						// explicit revert failed at the backup source stat forever
						// while sweeps saw an ordinary armed row with a present
						// destination and found nothing to repair. Persist the
						// durable RestorePending marker for EVERY re-arm failure
						// class; only the KIND routes on name ownership
						// (rearmPendingKind): refusal classes and pre-publish
						// failures leave the name foreign or absent (rearm-refused —
						// every retry runs only the consumption leg, journal-only),
						// while a failure after the staged copy definitely published
						// (fsutil's ErrPublishCompleted hard-link fallback leg —
						// wave-21 applies metadata pre-publish, so no other
						// post-publish class remains) leaves THIS operation's bytes at the
						// name (clean — the retry removes it). The marker certifies
						// the destination already carries the restored bytes; the
						// original consumption failure surfaces through cErr exactly
						// like the neighboring consumption legs, and the marker
						// outcome is logged via the logger seam only.
						pendingKind := rearmPendingKind(rearmErr)
						if markErr := markReplacementEntryRestorePendingKind(ctx, r.batchFileOpRepo, op.ID, sweepSlash(e.Backup), pendingKind); markErr != nil {
							if rearmOccupiedClass(rearmErr) {
								logging.Warnf("replacement restore %s: consumption failed (%v), re-arm refused (%v), and restore-pending persistence failed (%v) — restored destination retained, backup occupant untouched", absoluteBackup, cErr, rearmErr, markErr)
							} else {
								logging.Warnf("replacement restore %s: consumption failed (%v), re-arm failed (%v), and restore-pending persistence failed (%v) — restored destination retained, entry stays armed (intended pending kind %s)", absoluteBackup, cErr, rearmErr, markErr, pendingKind)
							}
						} else if rearmOccupiedClass(rearmErr) {
							logging.Warnf("replacement restore %s: consumption failed (%v) and re-arm refused (%v) — restored destination retained, entry marked restore-pending", absoluteBackup, cErr, rearmErr)
						} else {
							logging.Warnf("replacement restore %s: consumption failed (%v) and re-arm failed (%v) — restored destination retained, entry marked restore-pending (%s)", absoluteBackup, cErr, rearmErr, pendingKind)
						}
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

// replacementEntryIsLive re-reads the operation row before a restore leg
// opens its backup. A false first result is a successful no-op: another revert
// or sweep already restored and consumed this exact entry, so the caller must
// not touch the destination again. The second result is the FRESH entry's
// normalized restore-pending kind ("" when not pending): the destination
// bytes are certified in place and only cleanup/consumption pend, so the
// caller must never use the backup path as a restore source for that entry —
// the occupant can be a foreign file after a collided consumption-failure
// re-arm (codex P2, PR#215 round 18). Wave-19: the kind tells the caller
// whether the cleanup retry may touch the backup path at all — the
// rearm-refused kind forbids every path operation against the unowned name.
func (r *Reverter) replacementEntryIsLive(ctx context.Context, op *models.BatchFileOperation, entry models.ReplacementEntry) (bool, string, error) {
	release := fsutil.SharedJournalLocks().Acquire(fmt.Sprintf("%d", op.ID))
	defer release()

	fresh, frErr := r.batchFileOpRepo.FindByID(ctx, op.ID)
	if frErr != nil {
		return false, "", fmt.Errorf("failed to re-read row before restoring on op %d: %w", op.ID, frErr)
	}
	if fresh == nil {
		return false, "", fmt.Errorf("failed to re-read row before restoring on op %d: row not found", op.ID)
	}
	gf, parseErr := models.ParseGeneratedFiles(fresh.GeneratedFiles)
	if parseErr != nil {
		return false, "", fmt.Errorf("failed to re-parse journal before restoring on op %d: %w", op.ID, parseErr)
	}

	// Keep all later cleanup/consumption decisions on the same fresh ledger
	// even when this particular snapshot entry has already disappeared. The
	// status matters too: the primary revert must not follow a stale snapshot
	// after another request has already completed the operation.
	op.GeneratedFiles = fresh.GeneratedFiles
	op.RevertStatus = fresh.RevertStatus
	for _, cur := range gf.Replacements {
		if cur.Destination == entry.Destination && cur.Backup == entry.Backup && cur.DestSeq == entry.DestSeq {
			return true, cur.PendingKind(), nil
		}
	}
	return false, "", nil
}

// consumeReplacementEntry removes one journaled entry from the operation
// row's ledger and persists it. The in-memory op is updated too so the
// cleanup that follows sees the post-restore journal.
func (r *Reverter) consumeReplacementEntry(ctx context.Context, op *models.BatchFileOperation, entry models.ReplacementEntry) error {
	// R15-1: serialize with every other row mutator and consume from the
	// FRESH row — never from a possibly-stale snapshot. Review 4960250562: the
	// merge additionally runs inside a BEGIN IMMEDIATE transaction so an apply
	// in ANOTHER process (which neither this process lock nor the distinct
	// per-destination .dlbusy marker orders against) records or retracts its
	// entries against the same committed ledger instead of a divergent Save.
	release := fsutil.SharedJournalLocks().Acquire(fmt.Sprintf("%d", op.ID))
	defer release()
	var finalBlob string
	var fnErr error
	txErr := r.batchFileOpRepo.UpdateJournalInTx(ctx, op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		gf, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		if err != nil {
			fnErr = fmt.Errorf("failed to re-parse journal for consumption on op %d: %w", op.ID, err)
			return models.GeneratedFilesJSON{}, false, fnErr
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
		if !removed {
			// Another consumer committed first. Its restore leg already installed
			// the bytes; persist nothing and adopt its ledger as the fresh view.
			finalBlob = current.GeneratedFiles
			return gf, false, nil
		}
		gf.Replacements = kept
		finalBlob = models.MarshalLedgerJSON(gf)
		return gf, true, nil
	})
	switch {
	case fnErr != nil:
		return fnErr
	case errors.Is(txErr, database.ErrNotFound):
		// Row vanished after the restore leg re-read it: the restore is retried
		// through the caller's re-arm path as before.
		return fmt.Errorf("failed to re-read row for consumption on op %d: row not found", op.ID)
	case txErr != nil:
		return fmt.Errorf("backup %s restored to %s but journal consumption failed for op %d: %w", entry.Backup, entry.Destination, op.ID, txErr)
	}
	// Sync the caller's in-memory view — partial restores must be visible to
	// this op's later passes or the consumed entry is retried against a
	// vanished backup (count-N flake).
	op.GeneratedFiles = finalBlob
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

// rearmCopyNonce uniquifies the exclusively-staged copy path for the backup
// re-arm (journal-consumption compensation).
var rearmCopyNonce atomic.Uint64

// rearmStagingSuffix names the transient staged re-arm copy next to its
// backup — destination-adjacent like the `.rstr` restore staging, but a
// distinct marker: it must never match the sweeper's `.dlbak.<16hex>`
// ownership grammar (the suffix's non-hex letters keep it out).
const rearmStagingSuffix = ".dlrarm"

// rearmPublishFn is the publish seam behind copyRearmSourceBytes (same
// discipline as restoreStagingOwnershipFn): production publishes through
// fsutil.PublishNoReplace; tests record invocation order against the
// pre-publish metadata seams and replay the typed publish error classes
// (the ErrPublishCompleted compensation leg included).
var rearmPublishFn = fsutil.PublishNoReplace

// restoreOpenReplacementSource opens a journaled restore backup for reading
// with each platform's strongest protection against a final-component
// symlink swap between the Lstat gate and the open. The default passes the
// platform's no-follow flag through afero.OsFs to os.OpenFile (see
// restore_source_nofollow_unix.go / restore_source_nofollow_other.go); the
// Windows build replaces this at init with a reparse-point handle open (see
// restore_source_nofollow_windows.go), mirroring the downloader's wave-7
// rollback restore open.
var restoreOpenReplacementSource = openRestoreSourceNoFollow

// openRestoreSourceNoFollow is the POSIX/default restore source open: OsFs
// forwards restoreSourceNoFollow to os.OpenFile, while MemMapFs ignores the
// unknown flag bit and relies on the caller's Lstat gate.
func openRestoreSourceNoFollow(fsys afero.Fs, backup string) (afero.File, error) {
	return fsys.OpenFile(backup, os.O_RDONLY|restoreSourceNoFollow, 0)
}

// restoreStagingOwnershipFn forwards the history restore path to the wave-7
// fsutil ownership helper behind a package seam (same discipline as the
// downloader's rollback restore): POSIX tests record the requested uid/gid
// hand-off without kernel privileges. wave-29 (codex P1, PR#215): the helper
// and this seam take the OPEN STAGING HANDLE — the ownership hand-off is
// fchown-scoped, never a path operation on the staged name. The helper itself
// is NOT duplicated here — this is only the observation point.
var restoreStagingOwnershipFn = fsutil.RestoreStagingOwnership

// lstatRestoreSource describes a restore source without following its final
// path component when the injected filesystem supports afero.Lstater. Afero's
// MemMapFs has no symlink model; its regularity check still runs before
// opening, while production OsFs delegates to os.Lstat. The sweeper also uses
// it to classify a DESTINATION without following: a dangling symlink is an
// existing directory entry, never the restore-able absent state Stat reports.
func lstatRestoreSource(fs afero.Fs, backup string) (info os.FileInfo, err error) {
	if ls, ok := fs.(afero.Lstater); ok {
		info, _, err := ls.LstatIfPossible(backup)
		return info, err
	}
	info, err = fs.Stat(backup)
	return info, err
}

// restoreOSPath converts a possibly slash-normalized absolute path (journal
// spellings, sweepSlash/DestKey-derived keys, filepath.ToSlash'd sweep
// enumeration paths) into OS-native separator form BEFORE it reaches an OS
// call. Windows legacy journals retain either separator spelling, and the
// separator forms diverge downstream: afero's MemMapFs indexes filepath.Clean'd
// names while its Chmod performs a RAW map lookup, so a slash-form path misses
// the just-installed entry on the Windows runner ("chmod <path>: file does not
// exist"), and fsutil.ReplaceFile's native MoveFileEx receives the path
// verbatim. The leg follows the same fsutil.PathBackslashesAreSeparators seam
// DestKey/foldKeyedLock use, instead of a build tag, so the Windows posture is
// decidable in tests on any host; on POSIX the default posture is a no-op so
// literal-backslash filenames stay byte-exact. The conversion leg is
// filepath.FromSlash's Windows expansion spelled as strings.ReplaceAll —
// FromSlash itself is a compile-time no-op on POSIX builds and could never
// expose the Windows branch to a cross-host unit test.
func restoreOSPath(p string) string {
	if fsutil.PathBackslashesAreSeparators {
		return strings.ReplaceAll(p, "/", "\\")
	}
	return p
}

// copyRestoreBytes restores the backup bytes onto dest WITHOUT consuming the
// backup file: bytes are staged adjacent and swapped in with replace-aware
// rename; the caller removes the backup before consuming its journal entry.
// Replace semantics are the reverter's: undo puts old bytes over whatever the
// destination currently holds (a chained restore replaces the newer link).
func copyRestoreBytes(fs afero.Fs, backup, dest string) error {
	return copyRestoreBytesPublish(fs, backup, dest, fsutil.ReplaceFile, false)
}

// copyRestoreBytesNoReplace is copyRestoreBytes whose staged publish NEVER
// replaces an occupied destination — the sweep's MISSING-destination restore
// (its Lstat-ENOENT classification proved dest absent). Wave-16 (codex P2):
// the pre-wave-16 plain replace destroyed a foreign writer's bytes when it
// claimed the destination in the classify→publish window, then the sweep
// removed the backup and consumed the journal entry — the racer's bytes
// ended up backed up NOWHERE and unrecoverable. A collision drops the staged
// copy and returns the typed fsutil.ErrPublishCollision through the "swap
// staged restore" wrap; every sweep caller lands errors on its kept/warn leg
// (backup retained, journal entry unconsumed), exactly the conservative
// posture the window demands.
//
// Wave-17 (codex P2): a no-replace-UNSUPPORTED volume (typed
// fsutil.ErrPublishNoReplaceUnsupported — FAT/exFAT, where neither
// renameat2 nor hard links exist) maps onto the SAME kept/warn leg: the
// restore refuses rather than falling back to replacing semantics, the
// backup stays retained, and the journal entry stays unconsumed.
func copyRestoreBytesNoReplace(fs afero.Fs, backup, dest string) error {
	return copyRestoreBytesPublish(fs, backup, dest, fsutil.PublishNoReplace, true)
}

func copyRestoreBytesPublish(fs afero.Fs, backup, dest string, publish func(afero.Fs, string, string) error, noReplace bool) error {
	// Journal spellings may carry the legacy `/` form on Windows: every OS call
	// built on dest below (the .rstr staging name -> mode fix-up Chmod,
	// Chtimes, and ReplaceFile's native MoveFileEx on the swap) sees the
	// OS-native spelling. See restoreOSPath for the MemMapFs-raw-Chmod miss.
	dest = restoreOSPath(dest)
	// Lstat is deliberately before OpenFile: Stat/Open would follow a hostile
	// backup symlink and copy its target into the media directory.
	sourceInfo, err := lstatRestoreSource(fs, backup)
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

	// The open itself is platform-specific (see restoreOpenReplacementSource):
	// POSIX passes O_NOFOLLOW through OsFs to os.OpenFile; Windows opens with
	// FILE_FLAG_OPEN_REPARSE_POINT and refuses a reparse point on the returned
	// handle. MemMapFs ignores unknown read flags and has no symlink
	// representation; the Lstat+regularity gate above is therefore its
	// available protection, with the documented residual Lstat/OpenFile TOCTOU
	// for non-OsFs implementations.
	src, err := restoreOpenReplacementSource(fs, backup)
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
	// Wave-8 codex P2 follow-up: hand the staged inode back to the backup's
	// owner BEFORE the swap, mirroring the downloader's rollback restore
	// (install_overwrite.go). A privileged history restore (or startup sweep —
	// both funnels share this staging path) of a backup owned by another
	// account otherwise loses the original uid/gid the moment the backup is
	// deleted after the swap. Best-effort semantics ride the helper: EPERM
	// escalations are swallowed there and restore resilience is unchanged.
	// wave-29 (codex P1, PR#215): the hand-off is fchown THROUGH THE OPEN
	// HANDLE — the mode re-assert at create, the ownership here, and the times
	// inside CloseStaged all ride the descriptor, so a directory writer
	// renaming the staged name away and planting a symlink inside the window
	// can no longer redirect chmod/times/chown onto an arbitrary target (the
	// pre-wave-29 path-based calls on the staged name would have followed the
	// planted link). Virtual filesystems keep name-based fallback legs against
	// the stored spelling inside the fsutil helpers (they have no symlink
	// model).
	restoreStagingOwnershipFn(fs, dstFile, openedInfo)
	// wave-30 (codex P1, PR#215): the whole staging tail — identity proof,
	// times, close, publish — runs through fsutil.PublishStagedBound, which
	// binds the staged identity ACROSS the publish (hold-through-publish +
	// post-publish reverify + bounded re-stage-from-handle loop on POSIX;
	// prove-close-publish-reverify on Windows). The wave-29 proof alone left
	// a verify→publish window in which a directory writer could rename the
	// staged name away and plant a substitute that the path publish then
	// installed at dest — genuine backup bytes were consumed afterwards. The
	// caller-facing phase split below preserves every pre-wave-30 wrap text
	// and cleanup posture: an unproven staged name is never removed; a
	// proven-ours staged name is removed exactly where wave-29 removed it.
	pubErr := fsutil.PublishStagedBound(fsutil.StagedPublish{
		FS:          fs,
		Publish:     publish,
		NoReplace:   noReplace,
		Staged:      staged,
		Handle:      dstFile,
		Dest:        dest,
		Atime:       openedInfo.ModTime(),
		Mtime:       openedInfo.ModTime(),
		ApplyTimes:  true,
		Suffix:      ".rstr",
		NextOrdinal: func() uint64 { return restoreCopyNonce.Add(1) },
	})
	if pubErr != nil {
		var timesErr *fsutil.StagingTimesError
		switch {
		case errors.Is(pubErr, fsutil.ErrPublishStagedVerify):
			return fmt.Errorf("stage restore identity: %w", pubErr)
		case errors.As(pubErr, &timesErr):
			_ = fs.Remove(staged)
			return fmt.Errorf("stage restore times: %w", pubErr)
		case errors.Is(pubErr, fsutil.ErrPublishStagedClose):
			_ = fs.Remove(staged)
			return fmt.Errorf("stage restore close: %w", pubErr)
		default:
			_ = fs.Remove(staged)
			return fmt.Errorf("swap staged restore: %w", pubErr)
		}
	}
	return nil
}

// openRearmSource opens the destination whose bytes must rebuild a removed
// backup (journal-consumption compensation, rearmReplacementBackup) under the
// SAME discipline copyRestoreBytes applies to the backup open: Lstat first
// (never following the final component), require a regular non-symlink file,
// open through the platform no-follow seam, then verify the opened object is
// still that same regular file (dev+inode identity when the filesystem
// exposes it). Wave-10 codex follow-up: the compensation path used to copy
// dest→backup via a plain path open, so an attacker swapping the destination
// for a symlink between the backup removal and the re-arm got a PROTECTED
// file copied into the media-dir backup, armed for a later restore. The
// caller MUST stream from the returned handle — reopening the path would
// re-widen exactly this Lstat→open window.
func openRearmSource(fs afero.Fs, dest string) (afero.File, error) {
	sourceInfo, err := lstatRestoreSource(fs, dest)
	if err != nil {
		return nil, fmt.Errorf("re-arm source %s: %w", dest, err)
	}
	if sourceInfo == nil {
		return nil, refuseRestoreSource(dest, "filesystem returned no file information")
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return nil, refuseRestoreSource(dest, "destination is a symlink")
	}
	if !sourceInfo.Mode().IsRegular() {
		return nil, refuseRestoreSource(dest, fmt.Sprintf("destination is not a regular file (mode %s)", sourceInfo.Mode()))
	}

	// Waves 7–9 discipline mirrored from copyRestoreBytes: POSIX passes
	// O_NOFOLLOW through OsFs to os.OpenFile (a symlink swapped in after the
	// Lstat gate fails here); Windows opens with FILE_FLAG_OPEN_REPARSE_POINT
	// and refuses a reparse point on the returned handle. MemMapFs ignores the
	// unknown read flag and has no symlink model; the Lstat+regularity gate
	// above is its available protection.
	src, err := restoreOpenReplacementSource(fs, dest)
	if err != nil {
		return nil, fmt.Errorf("re-arm source %s: %w", dest, err)
	}
	openedInfo, err := src.Stat()
	if err != nil {
		_ = src.Close()
		return nil, fmt.Errorf("stat opened re-arm source %s: %w", dest, err)
	}
	if openedInfo == nil || openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() {
		_ = src.Close()
		return nil, refuseRestoreSource(dest, "opened object is not a regular file")
	}
	if sourceDev, sourceIno, sourceOK := restoreSourceIdentity(sourceInfo); sourceOK {
		if openedDev, openedIno, openedOK := restoreSourceIdentity(openedInfo); openedOK && (sourceDev != openedDev || sourceIno != openedIno) {
			_ = src.Close()
			return nil, refuseRestoreSource(dest, "opened object differs from the Lstat object")
		}
	}
	return src, nil
}

// copyRearmSourceBytes streams an already-open, identity-verified re-arm
// source into the backup path through an EXCLUSIVELY OWNED same-directory
// staging file + a no-replace publish — fsutil.CopyFileFs's write side
// WITHOUT its path re-open, which would drop the no-follow handle
// openRearmSource established.
//
// Wave-15: the publish is fsutil.PublishNoReplace, never a bare rename — an
// occupied backup name yields fsutil.ErrPublishCollision (staged copy dropped,
// foreign bytes intact) instead of a silent clobber. Wave-17: a volume that
// cannot express no-replace at all yields fsutil.ErrPublishNoReplaceUnsupported
// through the same publish error — the caller's re-arm failure stays the
// kept+warn posture (the conservative leg), never a replacing rename.
//
// Wave-21 (codex P1, PR#215): the stage is fsutil.CreateExclusiveStagingFile
// (O_EXCL — the staged inode is PROVABLY ours) with the caller's requested
// mode applied at create, and ALL remaining metadata (times, ownership) is
// applied BEFORE the publish. The pre-wave-21 flow ran
// RestoreStagingOwnership/Chmod/Chtimes on the PUBLISHED backup path: in a
// directory writable by another user the name could be swapped for a
// symlink inside the publish→metadata window, and the path-based
// chown/chmod/chtimes would follow the link to an arbitrary target. Path
// operations on the O_EXCL-created staged name are safe, and the published
// backup lands with the requested mode+times+ownership complete — NO
// post-publish metadata calls remain in this flow.
//
// wave-29 (codex P1, PR#215) hardens the pre-publish operations themselves:
// O_EXCL pins the INODE, not the NAME — a directory writer can still rename
// the staged name away and plant a symlink on it inside the copy/metadata
// window, so times+ownership+the publish-time identity proof now ride the
// OPEN HANDLE (fsutil.CloseStaged applies fd-scoped times before closing;
// fsutil.RestoreStagingOwnership takes the handle for fchown;
// fsutil.VerifyStagedIdentity must pass before the path-based publish may
// run). On identity failure the staged name is foreign: the handle is
// closed, NOTHING is removed at the name (removing it would delete the
// plant), and the failure classifies like every other pre-publish failure
// (rearmPendingKind → rearm-refused).
func copyRearmSourceBytes(fs afero.Fs, src io.Reader, backup string, info os.FileInfo) error {
	if err := fs.MkdirAll(filepath.Dir(backup), config.DirPerm); err != nil {
		return fmt.Errorf("re-arm create backup directory: %w", err)
	}
	// info == nil keeps wave-9's copy-only posture with the default mode;
	// otherwise the requested permission bits land on the staged inode at
	// create (CreateExclusiveStagingFile re-asserts them past the umask).
	mode := os.FileMode(config.FilePerm)
	if info != nil {
		mode = info.Mode().Perm()
	}
	staged, dstFile, err := fsutil.CreateExclusiveStagingFile(fs, backup, rearmStagingSuffix, rearmCopyNonce.Add(1), mode)
	if err != nil {
		return fmt.Errorf("re-arm staging backup %s: %w", backup, err)
	}
	if _, cerr := io.Copy(dstFile, src); cerr != nil {
		_ = dstFile.Close()
		_ = fs.Remove(staged)
		return fmt.Errorf("re-arm copy bytes for %s: %w", backup, cerr)
	}
	if info != nil {
		// Wave-14's ownership hand-off, pre-publish and now THROUGH THE HANDLE
		// (wave-29): the staged inode receives the original backup's uid/gid via
		// fchown before it becomes the backup. Best-effort semantics ride the
		// helper (EPERM swallowed; windows no-op), same as the restore path.
		restoreStagingOwnershipFn(fs, dstFile, info)
	}
	// wave-30 (codex P1, PR#215): the staging tail runs through
	// fsutil.PublishStagedBound (same bind as the restore path): the wave-29
	// pre-publish proof alone left a verify→publish window where the staged
	// name could be swapped for a plant that the no-replace publish then
	// installed at the ABSENT backup name, while the caller consumed the
	// journal as if the re-arm landed. The bound helper holds the handle
	// through the publish and re-verifies afterwards (POSIX recovery loop).
	// info == nil skips the times leg entirely (pre-wave-30 parity).
	var atime, mtime time.Time
	if info != nil {
		atime, mtime = info.ModTime(), info.ModTime()
	}
	pubErr := fsutil.PublishStagedBound(fsutil.StagedPublish{
		FS:          fs,
		Publish:     func(fsys afero.Fs, src, dst string) error { return rearmPublishFn(fsys, src, dst) },
		NoReplace:   true,
		Staged:      staged,
		Handle:      dstFile,
		Dest:        backup,
		Atime:       atime,
		Mtime:       mtime,
		ApplyTimes:  info != nil,
		Suffix:      rearmStagingSuffix,
		NextOrdinal: func() uint64 { return rearmCopyNonce.Add(1) },
	})
	if pubErr != nil {
		// Wave-15 (codex P2): publish the staged re-arm with NO-REPLACE
		// semantics. The original backup was REMOVED by the caller before
		// this compensation ran, so the window between staging the copy and
		// the publish is wide: a foreign writer claiming the backup name
		// mid-window would be destroyed by a plain rename, destroying
		// unrelated bytes with no ledger trace. The no-replace publish
		// refuses an occupied backup name instead; callers disarm any re-arm
		// failure into a restore-pending marker (kinds: rearm-refused unless
		// the publish definitely completed — fsutil.PublishCompleted — the
		// only post-publish signal left), so the collision surfaces through
		// the typed fsutil.ErrPublishCollision class with the foreign bytes
		// intact and the staged copy dropped. wave-30's identity break /
		// exhaustion classes take the same unproven-name classification.
		var timesErr *fsutil.StagingTimesError
		switch {
		case errors.Is(pubErr, fsutil.ErrPublishStagedVerify):
			// The staged name is unproven (foreign): left untouched, handle
			// already closed by the helper — wave-29 posture.
			return fmt.Errorf("re-arm stage backup identity %s: %w", backup, pubErr)
		case errors.As(pubErr, &timesErr):
			_ = fs.Remove(staged)
			return fmt.Errorf("re-arm stage backup times %s: %w", backup, pubErr)
		case errors.Is(pubErr, fsutil.ErrPublishStagedClose):
			_ = fs.Remove(staged)
			return fmt.Errorf("re-arm close backup %s: %w", backup, pubErr)
		default:
			_ = fs.Remove(staged)
			return fmt.Errorf("re-arm install backup %s: %w", backup, pubErr)
		}
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
