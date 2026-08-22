package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	byDest, destSpelling, destOrder := groupReplacementEntries(gf.Replacements)

	// codex P3 R20-1: phase-split preflight from restore. Preflight iterates
	// byDest WITHOUT locks so rejected ops halt BEFORE any bytes move — a
	// multi-destination op can then refuse while its secondary destinations
	// are still untouched (the interleaved minus hit). The per-destination
	// lock-gated phase still revisits the journal under the dest lock for
	// in-lock freshness. Wave-45: both phases walk destOrder — the
	// deterministic group order — never Go map iteration.
	for _, key := range destOrder {
		dest := destSpelling[key]
		entries := byDest[key]
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
	// Wave-45: deterministic destOrder walk (same order as the preflight).
	// Wave-57: a wedged sweep claim skips its dest (busy-class) without
	// overhanging the loop — other dests still process; the op fails busy at
	// loop end so a later retry restores the wedged dest once it self-releases.
	var wedgedDest string
	for _, key := range destOrder {
		dest := destSpelling[key]
		entries := byDest[key]
		restoreErr := func() error {
			// Wave-50 (codex P2, PR#215 finding F1): consult the abandoned-sweep
			// claim ledger BEFORE the blocking dest-lock acquisition. A sweep
			// stranded past the wave-8 deadline parks on its wedged fs call while
			// holding this dest lock (bound to its claim at record time), so
			// blocking first would hang this revert behind the stranding forever
			// — ahead of the marker-reclaim consult below. A ctx-done record
			// reclaims BOTH holds through the claim's once-guarded releases (dest
			// lock first — pure in-process work that cannot wedge on the stranded
			// filesystem — then the marker) and the acquisition retried below
			// proceeds against the freed arbitration. A live record (a sweep
			// someone still waits on) refuses, and the acquire keeps its ordinary
			// blocking posture.
			// Wave-51 (codex P1, PR#215) — ORDERING CONTRACT between the stranded
			// worker's journal commit and this restore loop's mutations. The
			// reclaim below flips the abandoned claim's revocation flag BEFORE
			// releasing its arbitration holds, and every sweep mutation surface
			// then refuses to start (the gates in restoreAndConsume/
			// retryPendingRemovalClaimed/sweepOne). But a worker whose wedged fs
			// call resumed BEFORE the flag was set may still COMMIT its journal
			// consumption afterwards — the "last recorded mutation already
			// succeeded" case — and a revert that mutated on a stale journal
			// would fork a second restore of the same backup. This path therefore
			// holds a fixed order: (1) consult + revoke abandoned claims BEFORE
			// the dest-lock acquisition, (2) arbitrate (dest lock + busy marker),
			// (3) fresh journal re-read per entry under that row's journal lock
			// (replacementEntryIsLive — serialized against the worker's
			// consumption transaction through SharedJournalLocks), and only then
			// (4) any restore mutation. A worker commit landing anywhere in
			// (1)–(3) is observed by (3), the entry is SKIPPED, and the reclaim
			// flops to the completed-consume posture: nothing re-publishes onto
			// the destination the worker just restored and nothing wipes or
			// re-removes its backup-side state. No restore leg below may touch
			// the destination, the backup, or the journal ahead of its (3).
			// Wave-52 (codex local review round 7, PR#215 finding F1): the order
			// is pinned end-to-end by
			// TestReverterW52_ConsumeBetweenReclaimAndFreshReadSkips — a worker
			// consume committing between the reclaim's return and the fresh read
			// at (3) reads back as not-live there and is skipped, never
			// re-restored.
			reclaimAbandonedSweepBusyMarker(dest)
			if sweepClaimIsWedged(dest) {
				logging.Warnf("replacement restore %s: sweep claim wedged (in-flight mutation, holds retained) — skipping destination for retry", dest)
				wedgedDest = dest
				return nil
			}
			preDestLockConsultHook(dest)
			release := fsutil.SharedDestLocks().Acquire(dest)
			defer release()

			// SharedDestLocks only arbitrates goroutines in this process. Claim
			// the durable marker before checking/restoring so a downloader in
			// another process cannot be between rename-to-backup and journaling
			// while this revert consumes an older backup. Acquire is deliberately
			// non-blocking for a live marker, including one owned by this process;
			// that preserves W14a's same-process liveness contract.
			busyRelease, busyErr := fsutil.AcquireReplacementBusy(r.fs, dest)
			if errors.Is(busyErr, fsutil.ErrReplacementBusy) && reclaimAbandonedSweepBusyMarker(dest) {
				// Wave-49 (codex P2): the blocker is a marker this process's own
				// abandoned pre-revert sweep claimed before its ctx died (the
				// wave-8 deadline proceed). The reclaim revoked it through the
				// claim's own once-guarded, token-bound release — retry the
				// acquisition once against the freed name. A marker owned by
				// anything still waited on (or another process entirely) is
				// never touched and keeps the ordinary busy refusal below. Wave-50
				// (finding F1): a claim recorded AFTER the consult above still
				// reclaims here — both consults share the frozen-key ledger.
				// Wave-55 (finding 1): the reclaim takes the marker aside directly
				// (no tombstone/grace) — the worker's mutations are ownership-
				// attested at every stage gate, so the reverter re-acquires the
				// freed marker under its own token and never bypasses a
				// still-owned one.
				busyRelease, busyErr = fsutil.AcquireReplacementBusy(r.fs, dest)
			}
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
				//
				// Wave-32 (codex local review round 2, PR#215 finding R1 — pending
				// legs): destination PRESENCE is deliberately the whole gate here.
				// The wave-25 backup-facts binding cannot apply: this kind exists
				// precisely because the backup name is UNOWNED — the recorded facts
				// describe the operation's own set-aside, which no longer owns the
				// name (a refusal left it foreign or absent), so any facts verdict
				// against that name measures foreign bytes. Legacy-unstamped
				// entries keep the same presence posture (documented residual).
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
				// Wave-62 (codex P2, PR#215 finding F1): the entry's arm-time facts
				// bind the copy's opened backup to the OWNED set-aside BEFORE any
				// bytes reach dest. copyRestoreBytesIdentityFacts authenticates the
				// opened backup against the wave-25-stamped facts (dev/ino + size/
				// mtime) and refuses up front on a mismatch — dest untouched, entry
				// live, foreign bytes preserved — instead of landing a substituted
				// backup's bytes at dest and stopping only the cleanup. The same
				// entryFacts pointer feeds the removal-side quarantine below.
				entryFacts := e
				// Wave-31: the identity the wave-31 recheck binds to is declared
				// here so the armed leg's copy can fill it; the pending legs copy
				// nothing (their dest was certified when the marker was written)
				// and skip the recheck entirely.
				var restoredID restoredDestIdentity
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
					var repErr error
					restoredID, repErr = copyRestoreBytesIdentityFacts(r.fs, e.Backup, dest, &entryFacts)
					if repErr != nil {
						return fmt.Errorf("failed to restore %s → %s: %w", e.Backup, dest, repErr)
					}
				}
				restored[dest] = true
				// Wave-31 (codex local round 1, PR#215 finding L1): an armed leg just
				// PUBLISHED the restored bytes at dest — before the backup removal +
				// journal consumption below, dest must STILL name that exact object.
				// A foreign writer swapping or deleting dest in the publish→remove
				// window no longer gets the backup (the sole remaining copy of the
				// pre-replacement bytes) unlinked or the only journal record of the
				// restore consumed: the entry stays ARMED (never marked pending —
				// that marker certifies dest carries restored bytes, unproven now),
				// the backup and destination stay untouched, and the op surfaces
				// unexpected-path-state for an explicit retry.
				if !restorePending && !restoredDestStillOurs(r.fs, dest, restoredID) {
					return fmt.Errorf("restored %s → %s but the destination no longer names the restored object (foreign swap or deletion in the restore window) — backup retained, journal entry left armed, destination untouched", e.Backup, dest)
				}
				// Wave-25 (codex P3 PR#215 finding 2): the removal must bind to the
				// OWNED object — the entry's arm-time facts (downloader-stamped
				// size/mtime) plus the identity of the object this leg just classed
				// (backupInfo; for the armed leg, the exact object the restore
				// streamed) — never to the backup pathname alone: a foreign file
				// swapped onto the name must be retained, never deleted-then-
				// consumed.
				// Wave-32 (codex local review round 2, PR#215 finding R1): the
				// wave-31 destination identity check above ran BEFORE the removal;
				// a foreign swap or deletion landing between the two used to get
				// the backup — the sole remaining copy of the pre-replacement
				// bytes — unlinked with consumption going through. The removal
				// therefore runs through the split quarantine: the verified backup
				// object moves aside first, then the destination is re-gated, and
				// only then is the QUARANTINED object unlinked. The re-gate is the
				// finding's pending-retry requirement made explicit — never mere
				// presence alone on the armed leg:
				//   - the armed leg re-proves that dest still names the object
				//     THIS pass just published (the wave-31 identity);
				//   - the pending-clean leg re-proves destination PRESENCE (the
				//     durable marker certified the restored bytes there; any
				//     Lstat-success object is present, wave-12 posture), with the
				//     removal side bound by the recorded backup-facts identity
				//     (wave-25) applied inside the quarantine above.
				//
				// On divergence the verified object moves back onto the journaled
				// name (NO-REPLACE — never clobbering a racer), the entry keeps its
				// armed/pending posture (never newly marked restore-pending against
				// an unproven destination), and the op surfaces unexpected-path-
				// state for an explicit retry. Any wedge step removes nothing and
				// leaves the entry live.
				hold, rmErr := quarantineReplacementBackupForRemoval(r.fs, e.Backup, "replacement restore", &entryFacts, backupInfo)
				if rmErr == nil {
					diverged := false
					if restorePending {
						// Pending-clean retry: destination presence re-gate. The
						// rearm-refused kind never reaches this leg — its backup
						// name is unowned (wave-19) and its consumption above is
						// journal-only by design.
						if _, derr := lstatRestoreSource(r.fs, dest); derr != nil {
							diverged = true
						}
					} else if !restoredDestStillOurs(r.fs, dest, restoredID) {
						diverged = true
					}
					if diverged {
						if rerr := hold.restore(); rerr != nil {
							// Wave-36 (codex local review round 6, PR#215 finding F3):
							// the move-back failed — the journaled backup name is
							// unowned (a foreign claimant holds it or the publish
							// wedged) while the verified bytes sit at the quarantine
							// name. Propagate to the caller through the error AND disarm
							// the entry: left armed (or clean-pending) it would later
							// stat/copy/remove the foreign occupant at that name, so the
							// rearm-refused pending kind persists instead (journal-only
							// retry; the quarantined bytes stay recoverable manually).
							absoluteBackupPath, _ := filepath.Abs(e.Backup)
							if markErr := markReplacementEntryRestorePendingKind(ctx, r.batchFileOpRepo, op.ID, sweepSlash(e.Backup), models.RestorePendingKindRearmRefused); markErr != nil {
								logging.Warnf("replacement restore %s: destination diverged after the backup was quarantined, the verified move-back failed (%v), AND the rearm-refused marker could not be persisted (%v) — entry left as-is, destination untouched, verified bytes recoverable at the quarantine name %s", absoluteBackupPath, rerr, markErr, hold.quarantine)
							}
							return fmt.Errorf("restored %s → %s but the destination diverged after the backup was quarantined and the verified move-back failed: %v — the journaled name is unowned; entry marked restore-pending (rearm-refused), verified bytes recoverable at the quarantine name %s", e.Backup, dest, rerr, hold.quarantine)
						}
						return fmt.Errorf("restored %s → %s but the destination diverged after the backup was quarantined (foreign swap or deletion inside the check-to-delete window); backup restored to its journaled name, journal entry left live, destination untouched", e.Backup, dest)
					}
					rmErr = hold.removeVerified()
				}
				if rmErr != nil {
					// Wave-32 (finding R4): the vanished class marked the
					// rearm-refused (journal-only) kind — the journaled name is
					// absent by construction, so no retry may touch it — while
					// every other class keeps the clean marker for the
					// file-driven retry.
					if markErr := markReplacementEntryRestorePendingKind(ctx, r.batchFileOpRepo, op.ID, sweepSlash(e.Backup), pendingKindForRemovalError(rmErr)); markErr != nil {
						absoluteBackup, _ := filepath.Abs(e.Backup)
						logging.Warnf("replacement restore failed to retain cleanup marker for backup %s: %v", absoluteBackup, markErr)
						// Without a durable marker, only undo a restore whose
						// destination was proven missing before the copy. That is the
						// armed-entry + intact-backup R9-2 compensation state.
						// Wave-34 (codex local review round 4, PR#215 finding F2):
						// destMissingBeforeRestore is the PRE-COPY Lstat — it says
						// nothing about the object answering at dest NOW. The undo
						// unlink is bound to the identity THIS pass published
						// (restoredID, the wave-31 publish identity): the seam
						// re-derives the no-follow identity + SameFile, and a foreign
						// create/swap landing inside the classify→undo window is
						// RETAINED byte-intact (never deleted), the entry left in its
						// armed live state like the retained neighbor legs. A PENDING
						// leg published nothing on this pass (restoredID is empty by
						// construction), so an occupant at a destination classified
						// missing pre-copy is necessarily foreign — retain it without
						// an unlink. An indeterminate verdict fails closed exactly
						// like the wave-31 publish-time recheck.
						// Wave-35 (codex local review round 5, PR#215): the undo unlink
						// runs through the destination quarantine
						// (restore_dest_quarantine_w35) — the seam verdict's
						// check→Remove window closes (a foreign substitute inside it
						// is refused byte-intact), and a wedge puts the verified
						// object back NO-REPLACE.
						if destMissingBeforeRestore {
							switch {
							case restorePending:
								logging.Warnf("replacement restore %s: cleanup marker persistence failed (%v) — restored destination retained for retry: this pending leg published nothing, so the occupant at a destination classified missing pre-copy is foreign and never deleted", absoluteBackup, markErr)
							case !restoredDestStillOurs(r.fs, dest, restoredID):
								logging.Warnf("replacement restore %s: cleanup marker persistence failed (%v) — restored destination retained for retry: it no longer names the published restore object (foreign swap or creation in the undo window)", absoluteBackup, markErr)
							default:
								if undoErr := removeRestoredDestQuarantined(r.fs, dest, "replacement restore", restoredID); undoErr != nil {
									logging.Warnf("replacement restore %s: cleanup marker persistence failed AND restore-undo failed (%v after %v)", absoluteBackup, undoErr, markErr)
								} else {
									logging.Warnf("replacement restore %s: cleanup marker persistence failed (%v) — restore undone, will retry", absoluteBackup, markErr)
								}
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
	if wedgedDest != "" {
		// Wave-57: a wedged dest was skipped — the op fails busy (stays Applied) so
		// a later sweep/RevertBatch retries it once the wedge self-releases.
		return restored, fmt.Errorf("replacement destination %s is wedged by an in-flight sweep mutation: %w", wedgedDest, fsutil.ErrReplacementBusy)
	}
	return restored, nil
}

// groupReplacementEntries buckets one invocation's replacement journal by
// destination key and hands back the DETERMINISTIC group iteration order
// (wave-45, codex P2, PR#215 finding F2):
//
//   - Posture freeze: fsutil.DestKey resolves the case/normalization probe
//     postures PER CALL, and a transient probe error is deliberately never
//     cached (wave-25) — so entries of ONE journal used to derive under
//     successively different postures (first entry: probe error → conserved
//     case; second entry: probe success → folded). One file's case-variant
//     cousins then sorted into SEPARATE destination groups, and the restore
//     loops' Go map iteration interleaved the stacked chains in an arbitrary
//     order, leaving intermediate bytes on the last restore. The
//     fsutil.DestKeyResolver resolves each present root exactly once per
//     invocation; every key of the invocation derives from that frozen
//     posture set (one probe per root per restoreReplacementJournal call on
//     an uncached, erroring root at most).
//   - Order: entries within a group sort by DestSeq descending as before
//     (true reverse replace order per destination); the GROUPS walk highest
//     journaled DestSeq first (reverse replace order across destinations),
//     destination key ascending on ties — Go map iteration order can never
//     pick the interleave.
func groupReplacementEntries(reps []models.ReplacementEntry) (map[string][]models.ReplacementEntry, map[string]string, []string) {
	resolver := fsutil.NewDestKeyResolver()
	byDest := make(map[string][]models.ReplacementEntry)
	destSpelling := make(map[string]string)
	for _, e := range reps {
		key := resolver.Key(e.Destination)
		byDest[key] = append(byDest[key], e)
		if destSpelling[key] == "" {
			destSpelling[key] = e.Destination
		}
	}
	for key := range byDest {
		sort.SliceStable(byDest[key], func(i, j int) bool { return byDest[key][i].DestSeq > byDest[key][j].DestSeq })
	}
	maxSeq := make(map[string]int64, len(byDest))
	destOrder := make([]string, 0, len(byDest))
	for key, entries := range byDest {
		destOrder = append(destOrder, key)
		for _, e := range entries {
			if e.DestSeq > maxSeq[key] {
				maxSeq[key] = e.DestSeq
			}
		}
	}
	sort.Slice(destOrder, func(i, j int) bool {
		if maxSeq[destOrder[i]] != maxSeq[destOrder[j]] {
			return maxSeq[destOrder[i]] > maxSeq[destOrder[j]]
		}
		return destOrder[i] < destOrder[j]
	})
	return byDest, destSpelling, destOrder
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

// reverterSweepTimeout bounds the reverter's pre-revert targeted sweep
// (codex P2, PR#215 #discussion_r3808360868): the CLI's wave-8 pre-sweep
// already ran under its own 30s cap, but the IMMEDIATELY following
// RevertBatch/RevertScrape drove this sweep with the caller's UNBOUNDED ctx,
// so one stalled network filesystem hung the whole revert right after the
// bounded pre-sweep passed. Package-level (not const) so tests can shrink it.
var reverterSweepTimeout = 30 * time.Second

// reverterSweepDestinations is the targeted-sweep invocation seam:
// production wires the concrete sweeper method; tests substitute
// capture/blocking doubles to force the deadline and error legs
// deterministically (a wedged afero.ReadDir that never observes its context
// cannot be staged otherwise).
var reverterSweepDestinations = func(ctx context.Context, sweeper *ReplacementSweeper, dests []string) (int, error) {
	return sweeper.SweepDestinations(ctx, dests)
}

// preDestLockConsultHook is a coverage/test seam fired in the window
// BETWEEN the wave-50 pre-acquisition claim-ledger consult and the blocking
// dest-lock acquisition (wave-50, codex P2, PR#215 finding F1): with the
// consult running first, the wave-49 busy-retry leg below is reachable in
// tests only by recording an abandoned claim inside exactly that window.
// Production wires a no-op.
var preDestLockConsultHook = func(string) {}

// reverterSweepRoots is the root-directory invocation seam (wave-34, codex
// local review round 4, PR#215 finding F4): the direct-revert pre-sweep also
// scans the operations' Begin-persisted roots, exactly where a process dying
// between the destination move-aside and RecordReplacement strands the
// destination-adjacent backup its never-written journal entry cannot name
// for the destination-only sweep set. Production wires the concrete sweeper
// method; SwapReverterSweepForTest substitutes it together with the
// destinations seam so the whole pre-sweep surface shares one wedge/timeout
// discipline.
var reverterSweepRoots = func(ctx context.Context, sweeper *ReplacementSweeper, dirs []string) (int, error) {
	return sweeper.SweepDirs(ctx, dirs)
}

// opSweepRoots computes the pre-sweep DIRECTORY scope of one operation
// (wave-34, codex local review round 4, PR#215 finding F4): the
// Begin-persisted destination roots (gf.Roots) ∪ the journaled replacement
// destination directories. The destination-only sweep set (gf.Replacements
// dests) misses the crash window between the destination move-aside and
// RecordReplacement entirely: that window leaves NO journaled entry, so the
// stranded backup sits under a Begin root its sweep never scanned and the
// original bytes were never recovered, even though gf.Roots covers the
// window. An unparseable ledger contributes nothing — the pre-sweep's
// destination collection skips the op the same way.
func opSweepRoots(op *models.BatchFileOperation) []string {
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var dirs []string
	add := func(d string) {
		if strings.TrimSpace(d) == "" {
			return
		}
		cleaned := filepath.ToSlash(filepath.Clean(d))
		if seen[cleaned] {
			return
		}
		seen[cleaned] = true
		dirs = append(dirs, cleaned)
	}
	for _, root := range gf.Roots {
		add(root)
	}
	for _, rep := range gf.Replacements {
		add(filepath.Dir(rep.Destination))
	}
	return dirs
}

// sweepJournaledDestinations runs the pre-revert targeted sweep over every
// destination journaled by the operations about to revert: crash-window
// restores land before the rejection/restore checks read destination state.
// Wave-34 (codex local review round 4, PR#215 finding F4): the sweep scope
// unions the operations' Begin-persisted roots (+ replacement destination
// dirs, opSweepRoots) through SweepDirs AFTER the unchanged destination set,
// so a stranded backup left by a process dying between the move-aside and
// RecordReplacement (no journaled entry at all) is seen by the orphan
// arbitration legs and healed instead of leaking past the revert.
// Best-effort — a sweeper failure never blocks the revert path.
//
// The sweep runs under the wave-8 bounded discipline (the same shape the
// CLI's runPreRevertReplacementSweep uses): SweepDestinations observes its
// context only BETWEEN directory scans — a stalled network filesystem blocks
// forever INSIDE afero.ReadDir where no context check can reach it — so the
// sweep runs in a goroutine behind a derived deadline and the caller selects
// on it. A sweep that outlives the budget is abandoned mid-flight: its
// goroutine stays parked on the wedged ReadDir until the filesystem answers
// (one goroutine per stuck revert — the accepted, bounded-leak tradeoff so
// the revert itself never wedges). The overrun is logged and the revert
// proceeds either way. On fast filesystems the sweep still completes fully
// before the revert reads destination state: external behavior is unchanged.
func (r *Reverter) sweepJournaledDestinations(ctx context.Context, ops []models.BatchFileOperation) {
	if r.sweeper == nil {
		return
	}
	dests := make([]string, 0, len(ops))
	var roots []string
	rootsSeen := map[string]bool{}
	for i := range ops {
		gf, err := models.ParseGeneratedFiles(ops[i].GeneratedFiles)
		if err != nil {
			continue
		}
		for _, rep := range gf.Replacements {
			dests = append(dests, rep.Destination)
		}
		// Wave-34 (finding F4): the operations' roots (Begin-persisted,
		// covering the pre-RecordReplacement crash window) join the sweep
		// scope alongside the journaled destinations.
		for _, dir := range opSweepRoots(&ops[i]) {
			if rootsSeen[dir] {
				continue
			}
			rootsSeen[dir] = true
			roots = append(roots, dir)
		}
	}
	if len(dests) == 0 && len(roots) == 0 {
		return
	}

	sweepCtx, cancel := context.WithTimeout(ctx, reverterSweepTimeout)
	// Buffered so an abandoned sweep (deadline leg below) never parks on the
	// send after the caller has moved on.
	sweepDests := reverterSweepDestinations
	sweepRoots := reverterSweepRoots
	done := make(chan error, 1)
	go func() {
		// The destination sweep runs FIRST (its arbitration set and ordering
		// are the pre-wave-34 contract — unchanged); the roots sweep then
		// heals the roots-only leftovers (finding F4). Each leg is
		// best-effort; the FIRST error surfacing keeps the caller's failure
		// log exactly as before.
		var firstErr error
		if len(dests) > 0 {
			if _, err := sweepDests(sweepCtx, r.sweeper, dests); err != nil {
				firstErr = err
			}
		}
		if len(roots) > 0 {
			if _, err := sweepRoots(sweepCtx, r.sweeper, roots); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		done <- firstErr
	}()
	select {
	case err := <-done:
		// Happy path: the sweep answered inside its budget.
		if err != nil {
			logging.Warnf("pre-revert replacement sweep failed: %v (continuing with revert)", err)
		}
	case <-sweepCtx.Done():
		logging.Warnf("pre-revert replacement sweep over %d destination(s) exceeded %s budget; continuing with revert", len(dests), reverterSweepTimeout)
	}
	// Called on EVERY path: the happy path releases the timer immediately; on
	// the deadline leg the context is already spent and cancel is a no-op.
	cancel()
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

// rearmNextOrdinal supplies strictly-increasing ordinals to restaged re-arm
// names (a named func so the coverage gate sees the pin test).
var rearmNextOrdinal = func() uint64 { return rearmCopyNonce.Add(1) }

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

// publishStagedBoundInfoFn is the bound-publish seam behind
// copyRestoreBytesPublish (same discipline as rearmPublishFn): production
// publishes through fsutil.PublishStagedBoundInfo; tests replay wave-61's
// completed-with-identity outcome (the ENOSYS-times-skipped leg on
// AIX/Solaris/illumos, where stagedHandleChtimes answers ENOSYS and r12
// refuses the name-based fallback) without the cross-package fd-times
// plumbing.
var publishStagedBoundInfoFn = fsutil.PublishStagedBoundInfo

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
//
//nolint:unused // test-facing error-only shape pinned across waves 7–30; production restore legs ride the wave-31 identity variant.
func copyRestoreBytes(fs afero.Fs, backup, dest string) error {
	_, err := copyRestoreBytesIdentity(fs, backup, dest)
	return err
}

// copyRestoreBytesIdentity is copyRestoreBytes with the restored object's
// publish-time identity handed back (wave-31, codex local round 1, PR#215
// finding L1): the caller revalidates that dest still names THAT object
// before any backup deletion or journal consumption runs.
//
//nolint:unused // test-facing identity shape; production restore legs ride the wave-62 facts variant (copyRestoreBytesIdentityFacts) which authenticates the opened backup against the journaled arm-time facts before any bytes reach dest.
func copyRestoreBytesIdentity(fs afero.Fs, backup, dest string) (restoredDestIdentity, error) {
	return copyRestoreBytesPublish(fs, backup, dest, fsutil.ReplaceFile, false, nil)
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
	_, err := copyRestoreBytesNoReplaceIdentity(fs, backup, dest)
	return err
}

// copyRestoreBytesNoReplaceIdentity is copyRestoreBytesNoReplace with the
// restored object's publish-time identity handed back (wave-31): the sweep's
// restore-and-consume leg revalidates dest against it before the backup
// removals and journal consumptions below ever run.
// copyRestoreBytesNoReplaceIdentityFacts is copyRestoreBytesNoReplaceIdentity
// with the journaled entry's arm-time facts threaded in (wave-62, finding F1):
// the sweep's armed restore-and-consume leg authenticates the opened backup
// against the wave-25-stamped facts BEFORE any bytes reach dest. See
// copyRestoreBytesIdentityFacts for the refusal posture.
func copyRestoreBytesNoReplaceIdentityFacts(fs afero.Fs, backup, dest string, entry *models.ReplacementEntry) (restoredDestIdentity, error) {
	return copyRestoreBytesPublish(fs, backup, dest, fsutil.PublishNoReplace, true, entry)
}

func copyRestoreBytesNoReplaceIdentity(fs afero.Fs, backup, dest string) (restoredDestIdentity, error) {
	return copyRestoreBytesPublish(fs, backup, dest, fsutil.PublishNoReplace, true, nil)
}

// copyRestoreBytesIdentityFacts is copyRestoreBytesIdentity with the
// journaled entry's arm-time facts threaded in (wave-62, codex P2, PR#215
// finding F1): the opened backup is authenticated AGAINST the wave-25-
// stamped facts (dev/ino consistency + size/mtime) BEFORE any bytes reach
// dest — a foreign file swapped onto the backup name after the journal
// write used to land at dest (the removal-side quarantine refused only the
// unlink). Mismatch refuses up front: dest untouched, entry live, foreign
// bytes preserved. A nil entry keeps the legacy pathname posture.
func copyRestoreBytesIdentityFacts(fs afero.Fs, backup, dest string, entry *models.ReplacementEntry) (restoredDestIdentity, error) {
	return copyRestoreBytesPublish(fs, backup, dest, fsutil.ReplaceFile, false, entry)
}

func copyRestoreBytesPublish(fs afero.Fs, backup, dest string, publish func(afero.Fs, string, string) error, noReplace bool, entry *models.ReplacementEntry) (restoredDestIdentity, error) {
	// Journal spellings may carry the legacy `/` form on Windows: every OS call
	// built on dest below (the .rstr staging name -> mode fix-up Chmod,
	// Chtimes, and ReplaceFile's native MoveFileEx on the swap) sees the
	// OS-native spelling. See restoreOSPath for the MemMapFs-raw-Chmod miss.
	dest = restoreOSPath(dest)
	// Lstat is deliberately before OpenFile: Stat/Open would follow a hostile
	// backup symlink and copy its target into the media directory.
	sourceInfo, err := lstatRestoreSource(fs, backup)
	if err != nil {
		return restoredDestIdentity{}, fmt.Errorf("read backup: %w", err)
	}
	if sourceInfo == nil {
		return restoredDestIdentity{}, refuseRestoreSource(backup, "filesystem returned no file information")
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return restoredDestIdentity{}, refuseRestoreSource(backup, "backup is a symlink")
	}
	if !sourceInfo.Mode().IsRegular() {
		return restoredDestIdentity{}, refuseRestoreSource(backup, fmt.Sprintf("backup is not a regular file (mode %s)", sourceInfo.Mode()))
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
		return restoredDestIdentity{}, fmt.Errorf("read backup: %w", err)
	}
	defer func() { _ = src.Close() }()

	// File.Stat is fstat for afero OsFs. Verify the object actually opened is
	// still regular, and compare identity when the OsFs Stat_t is available.
	openedInfo, err := src.Stat()
	if err != nil {
		return restoredDestIdentity{}, fmt.Errorf("stat opened backup: %w", err)
	}
	if openedInfo == nil || openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.Mode().IsRegular() {
		return restoredDestIdentity{}, refuseRestoreSource(backup, "opened object is not a regular file")
	}
	if sourceDev, sourceIno, sourceOK := restoreSourceIdentity(sourceInfo); sourceOK {
		if openedDev, openedIno, openedOK := restoreSourceIdentity(openedInfo); openedOK && (sourceDev != openedDev || sourceIno != openedIno) {
			return restoredDestIdentity{}, refuseRestoreSource(backup, "opened object differs from the Lstat object")
		}
	}
	// Wave-62 (codex P2, PR#215 finding F1): authenticate the OPENED backup
	// AGAINST the journaled entry's arm-time facts (the dev/ino consistency
	// above + the wave-25-stamped size/mtime) BEFORE any bytes reach dest.
	// A foreign file swapped onto the backup name after the journal write used
	// to land at dest — the removal-side quarantine refused only the unlink,
	// so the substituted bytes already stood at dest and only cleanup was
	// stopped. The copy now refuses up front on a stamped-facts mismatch: dest
	// stays untouched, the entry stays live, and the foreign occupant at the
	// backup name is preserved byte-intact. A nil or unstamped entry keeps the
	// legacy pathname posture (the documented size/mtime residual remains).
	if entry != nil && entry.BackupFactsStamped() {
		if openedInfo.Size() != entry.BackupSize || openedInfo.ModTime().Unix() != entry.BackupModUnix {
			return restoredDestIdentity{}, refuseRestoreSource(backup, fmt.Sprintf("occupant identity mismatch — journaled %d bytes @ %d, found %d bytes @ %d", entry.BackupSize, entry.BackupModUnix, openedInfo.Size(), openedInfo.ModTime().Unix()))
		}
	}

	stagedOrdinal := restoreCopyNonce.Add(1)
	// codex P3 R18h: stage with the backup's OWN permission bits so a revert
	// never widens restrictive media (0600 trailer) into world-readable.
	mode := openedInfo.Mode().Perm()
	staged, dstFile, err := fsutil.CreateExclusiveStagingFile(fs, dest, ".rstr", stagedOrdinal, mode)
	if err != nil {
		return restoredDestIdentity{}, fmt.Errorf("stage restore open: %w", err)
	}
	// R5-3: stream with a bounded buffer — trailer-class backups can reach
	// gigabytes; a whole-file read would exhaust memory on revert/startup
	// recovery alike. Wave-36 (codex local review round 6, PR#215 finding F2):
	// tee the stream through SHA-256 as it lands — the digest of the bytes this
	// pass PUBLISHES rides the returned identity, so the rechecks and the
	// wave-35 undo-quarantine can reject an inode-reused substitute whose
	// content was swapped after the identity metadata still matched.
	buf := make([]byte, 256*1024)
	digest := sha256.New()
	if _, cerr := io.CopyBuffer(dstFile, io.TeeReader(src, digest), buf); cerr != nil {
		// The staged name (dest-adjacent .rstr.<ordinal>) is
		// near-predictable: discard ONLY while it provably names the handle's
		// inode — a substitute planted in the copy→remove window is preserved
		// byte-intact (the wave-45 bound cleanup; it closes the handle).
		fsutil.DiscardFailedExclusiveStaging(fs, staged, dstFile)
		return restoredDestIdentity{}, fmt.Errorf("stage restore copy: %w", cerr)
	}
	var publishedSum [32]byte
	copy(publishedSum[:], digest.Sum(nil))
	// Wave-63 (codex P2): the opened backup's sha256 must equal the journaled
	// BackupSHA256 — size+mtime are forgeable. Mismatch refuses before the
	// publish: staged copy discarded, dest untouched, entry live, foreign
	// preserved. An unstamped entry keeps the wave-25 posture.
	if entry != nil && entry.BackupSHA256 != "" {
		if hex.EncodeToString(publishedSum[:]) != entry.BackupSHA256 {
			fsutil.DiscardFailedExclusiveStaging(fs, staged, dstFile)
			return restoredDestIdentity{}, refuseRestoreSource(backup, fmt.Sprintf("backup sha256 mismatch — journaled %s, streamed %s", entry.BackupSHA256, hex.EncodeToString(publishedSum[:])))
		}
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
	// wave-31 (codex local round 1, PR#215 finding L1): the Info variant hands
	// back the post-publish-VERIFIED destination object — the staged inode as
	// it landed — so the caller can revalidate dest against exactly what this
	// restore published before deleting the backup or consuming the journal.
	published, pubErr := publishStagedBoundInfoFn(fsutil.StagedPublish{
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
			return restoredDestIdentity{}, fmt.Errorf("stage restore identity: %w", pubErr)
		case errors.As(pubErr, &timesErr):
			// Codex P2 (r26): the handle was released before the name was
			// cleaned — a directory writer can replace .rstr.* afterwards, so
			// removing it by pathname could delete foreign bytes. ONLY a
			// rename-based vacate is safe; the timeline's windows have been
			// salted (<suffix>.<ordinal>), so the retained residue is inert.
			// Never Remove it blind; document and warn per the retain posture
			// already used by the claim/release helpers (wave-57/62).
			// Codex P2 (r26): the staged name was closed BEFORE this cleanup;
			// a directory writer can replace the name afterwards, so a
			// pathname Remove could delete foreign bytes. The stage names are
			// O_EXCL + ordinal-salted (.rstr.<n>) so their residue is inert;
			// retain with a warn — never unlink the unproven name.
			logging.Warnf("replacement restore: staged name %s left in place after the times failure (closed handle, unverifiable post-close identity) — residue is inert, manual cleanup advised", staged)
			return restoredDestIdentity{}, fmt.Errorf("stage restore times: %w", pubErr)
		case errors.Is(pubErr, fsutil.ErrPublishStagedClose):
			logging.Warnf("replacement restore: staged name %s left in place after the close failure (closed handle, unverifiable post-close identity) — residue is inert, manual cleanup advised", staged)
			return restoredDestIdentity{}, fmt.Errorf("stage restore close: %w", pubErr)
		default:
			// Wave-34 (codex local review round 4, PR#215 finding F3): a
			// publish failure carrying fsutil.ErrPublishCompleted proves the
			// DESTINATION already carries the staged bytes while fsutil
			// DELIBERATELY left the staged name in place — the POSIX hard-link
			// fallback's staged cleanup could not re-prove it
			// (fsutil.ErrPublishNoReplaceStagedUnverified: the name may now
			// address a foreign object swapped on mid-window) or its unlink
			// failed with the destination rollback failing too (wave-20). An
			// unlink here could destroy those possibly-foreign bytes, so the
			// staged cleanup runs ONLY for error classes that prove nothing
			// was published (our own staged copy, safe to drop); the retained
			// litter is a transient staging name, never a backup-grammar file.
			// r12: the ENOSYS leg (a platform with no fd-scoped times primitive)
			// joins the completed class too — the times are SKIPPED after an
			// identity-verified publish (the name-based fallback is refused),
			// and the successful publish itself consumed the staged name, so
			// there the skipped Remove has nothing to do; the posture is shared.
			// Wave-61 (codex P2, PR#215): when that completed leg carries a
			// VERIFIED non-nil identity (the ENOSYS-times-skipped publish —
			// fsutil.PublishStagedBoundInfo hands back the post-publish-verified
			// destination stat), the publish SUCCEEDED: dest provably carries
			// the restored bytes. Treat it as a successful restore — hand the
			// identity back so the caller's wave-31 revalidation + backup
			// removal + journal consumption run exactly like the plain-success
			// leg; on drift mid-revalidation the caller's wave-31 refusal
			// fires (no consumption, no overwrite of a substitute). Pre-wave-61
			// this returned the error, so the backup + journal entry were never
			// consumed and every retry republished and failed again. A nil
			// identity (the hard-link fallback's staged-cleanup refusal /
			// rollback failure) keeps the legacy completed discipline below: no
			// verified identity to revalidate against.
			if fsutil.PublishCompleted(pubErr) && published != nil {
				return restoredDestIdentityFromContent(published, publishedSum), nil
			}
			if fsutil.PublishCompleted(pubErr) {
				logging.Warnf("staged restore copy %s left in place — publish completed but the staged name could not be re-proven (possibly foreign); manual cleanup advised: %v", staged, pubErr)
			} else {
				_ = fs.Remove(staged)
			}
			return restoredDestIdentity{}, fmt.Errorf("swap staged restore: %w", pubErr)
		}
	}
	return restoredDestIdentityFromContent(published, publishedSum), nil
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
		// Same wave-45 bound discard as the restore copy: the staged name
		// (backup-adjacent .dlrarm.<ordinal>) is near-predictable — unlink it
		// only while it provably names the handle's inode, preserving any
		// mid-window substitute byte-intact. The helper closes the handle.
		fsutil.DiscardFailedExclusiveStaging(fs, staged, dstFile)
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
		NextOrdinal: rearmNextOrdinal,
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
			// Wave-34 (codex local review round 4, PR#215 finding F3): the
			// staged-cleanup discipline of the restore publish applies here too
			// — an fsutil.ErrPublishCompleted-carrying publish failure (the
			// wave-33 staged-cleanup refusal, joined with the completed class,
			// or the wave-20 rollback-failure leg) left the staged name in place
			// DELIBERATELY because it may address a foreign object; the backup
			// name already carries this operation's bytes. Refuse the cleanup
			// for that class and drop only provably-unpublished staged copies.
			// r12: the ENOSYS leg (platform without an fd-scoped times primitive)
			// joins the completed class with the times SKIPPED after a verified
			// publish — its successful publish consumed the staged name, so the
			// skipped Remove below is a no-op there too (same posture, nothing
			// foreign ever stamped or removed by the times leg).
			if fsutil.PublishCompleted(pubErr) {
				logging.Warnf("staged re-arm copy %s left in place — publish completed but the staged name could not be re-proven (possibly foreign); manual cleanup advised: %v", staged, pubErr)
			} else {
				_ = fs.Remove(staged)
			}
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
