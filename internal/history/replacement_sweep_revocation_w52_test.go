// POSTER-WRITE-HARDENING wave-52 (codex local review round 7, PR#215 —
// findings F1+F2 against the wave-51 revocation design):
//
//   - F1: the wave-51 revocation gate fenced only the ENTRY of
//     restoreAndConsume's removal/consumption unit; a claim revoked past that
//     gate kept the unit running — its consume/persist legs failed under the
//     dead sweep ctx and its compensations (re-arm publishes, undo unlinks,
//     marker persists) moved or destroyed names against arbitration the
//     continued revert already owns, leaving rows armed against
//     interlude-shaped names. The unit is now fenced PER STAGE: every
//     quarantine claim/move/unlink arm, each pending persist, the consumption
//     update, and every name-moving compensation consults the claim's
//     revocation flag immediately before its first mutation. A revoked stage
//     never starts; a stage whose syscalls already began completes through
//     the audited identity vocabulary; and a revocation observed after the
//     publish + removal completed takes the wave-19 unlandable-consume
//     resting classification (rearm-refused restore-pending restage, never a
//     backward compensation). The reverter side is pinned end-to-end: a
//     worker consume committing between the reclaim and the per-entry fresh
//     read is skipped, never re-restored.
//
//   - F2: sweepOne/consumeRearmRefusedPending used to record their busy
//     claim only AFTER the blocking destination-lock wait — a ctx expiring
//     mid-wait owned the marker with NO ledger record (unhealable
//     ErrReplacementBusy stranding). The claim is now recorded at marker
//     acquire time, carrying a pending cell for the dest-lock release
//     (bindDestLock fills it when the wait completes; a reclaim mid-wait
//     hands the late-acquired lock back to the stranded goroutine as its own
//     release responsibility).
//
// These tests pin the per-stage gates (zero further mutation from every
// gated stage + the exact resting classification per leg), the
// register-before-wait ledger visibility (F2, e2e), the false-bind abandon,
// and the reverter's reclaim→fresh-read ordering.

package history

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w52StageRepo counts UpdateJournalInTx calls through the sweeper: onCall
// runs synchronously at a chosen call's entry (the deterministic point to
// cancel the sweep ctx and reclaim the claim), and fail answers chosen calls
// with an injected error instead of running the transaction.
type w52StageRepo struct {
	*p3OpRepo
	onCall func(n int)
	fail   map[int]error
	mu     sync.Mutex
	calls  int
}

func (r *w52StageRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	r.mu.Lock()
	r.calls++
	n := r.calls
	r.mu.Unlock()
	if r.onCall != nil {
		r.onCall(n)
	}
	if err, ok := r.fail[n]; ok {
		return err
	}
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

// w52LstatStageFs runs stage() synchronously inside the FIRST
// LstatIfPossible of one slash-form path (the deterministic revocation
// landing point inside a pending-cleanup leg), then delegates.
type w52LstatStageFs struct {
	afero.Fs
	match string
	stage func()
	once  sync.Once
}

func (f *w52LstatStageFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.ToSlash(name) == f.match {
		f.once.Do(f.stage)
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(name)
}

// w52LiveClaim registers a wave-52 claim for dest (fake marker release — the
// staged legs are direct-call, markerless) and returns the claim plus the
// synchronization-free reclaim closure that models the wave-8 deadline: the
// sweep ctx dies, the continued revert reclaims (revoke → releases), and the
// record leaves the ledger.
func w52LiveClaim(t *testing.T, dest string) (*sweepBusyMarkerClaim, context.CancelFunc, func()) {
	t.Helper()
	sweepCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	claim, untrack := recordSweepBusyClaim(sweepCtx, nil, dest, "", func() {})
	t.Cleanup(untrack)
	reclaim := func() {
		cancel()
		require.True(t, reclaimAbandonedSweepBusyMarker(dest), "the abandoned claim reclaims")
		require.True(t, claim.isRevoked(), "reclaim revokes before the releases")
	}
	return claim, cancel, reclaim
}

// w52Row hands back the indexer-style row copy restoreAndConsume expects.
func w52Row(t *testing.T, repo *p3OpRepo, id uint) *models.BatchFileOperation {
	t.Helper()
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	return row
}

// Ledger mechanics (F2): the pending dest-lock cell — bind ordering against
// the reclaim in both interleavings, empty-cell no-ops, release order
// (dest lock first, marker second), and once-only firings.
func TestSweepBusyClaimW52_PendingDestLockCell(t *testing.T) {
	dest := "/w52-unit/poster.jpg"

	t.Run("bind before reclaim: the reclaim fires both, dest lock first", func(t *testing.T) {
		ledger := newSweepBusyClaimLedger()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var order []string
		var mu sync.Mutex
		fire := func(what string) func() { return func() { mu.Lock(); order = append(order, what); mu.Unlock() } }
		rec, untrack := ledger.record(ctx, nil, dest, "", fire("marker"))
		require.True(t, rec.bindDestLock(fire("lock")), "an unreclaimed cell accepts the bind")
		require.False(t, ledger.reclaim(dest), "a live claim never reclaims")
		cancel()
		require.True(t, ledger.reclaim(dest))
		mu.Lock()
		require.Equal(t, []string{"lock", "marker"}, order, "dest-lock release precedes the marker release")
		mu.Unlock()
		rec.releaseDestLock()
		require.False(t, ledger.reclaim(dest))
		mu.Lock()
		require.Equal(t, []string{"lock", "marker"}, order, "post-reclaim defers are once-guard no-ops")
		mu.Unlock()
		untrack()
	})

	t.Run("reclaim during the wait: the late bind keeps sole ownership", func(t *testing.T) {
		ledger := newSweepBusyClaimLedger()
		ctx, cancel := context.WithCancel(context.Background())
		markerFired, lockFired := 0, 0
		rec, untrack := ledger.record(ctx, nil, dest, "", func() { markerFired++ })
		rec.releaseDestLock() // empty cell — a no-op while the wait is in flight
		cancel()
		require.True(t, ledger.reclaim(dest), "the mid-wait claim reclaims against the empty cell")
		require.Equal(t, 1, markerFired)
		require.False(t, rec.bindDestLock(func() { lockFired++ }),
			"the reclaimed claim hands the late-acquired lock back to its acquirer")
		require.Equal(t, 0, lockFired, "the guarded release never became visible to the reclaim")
		rec.releaseDestLock() // still an empty cell — the late bind was refused
		require.Equal(t, 0, lockFired)
		rec.release() // the stranded goroutine's own marker defer would double-call a raw closure...
		require.Equal(t, 2, markerFired, "...so only once-guarded (fsutil) marker releases are ever recorded")
		untrack()
		require.False(t, ledger.reclaim(dest))
	})

	t.Run("untrack before any reclaim keeps the wait private", func(t *testing.T) {
		ledger := newSweepBusyClaimLedger()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rec, untrack := ledger.record(ctx, nil, dest, "", func() {})
		require.True(t, rec.bindDestLock(func() {}))
		untrack()
		cancel()
		require.False(t, ledger.reclaim(dest), "an untracked record is never reclaimed")
		require.False(t, rec.isRevoked())
	})
}

// F2 e2e — RegisterBeforeWait: the wave-49 ledger sees the incoming claim
// DURING, not after, the destination-lock wait. A sweep whose ctx dies while
// parked on the held dest lock is reclaimable immediately (pre-wave-52 the
// record landed only after the wait, so the consult found nothing); when the
// wait finally releases, the stranded goroutine self-releases the
// just-acquired lock and abandons without a single classification read or
// mutation.
func TestSweepOneW52_LedgerSeesClaimDuringDestLockWait(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w52-reg", "REG-001", "/w52-reg", p3HexA)

	holdRelease := fsutil.SharedDestLocks().Acquire(dest)
	sweeper := NewReplacementSweeper(base, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	info, err := base.Stat(backup)
	require.NoError(t, err)

	log := swapRevokeLog(t)
	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	healed := make(chan int, 1)
	go func() { healed <- sweeper.sweepOne(sweepCtx, idx, "/w52-reg", info) }()

	// Wait for the marker — the worker is past the wave-46 entry gate and
	// inside the claim; the record lands instructions after the O_EXCL marker
	// create, the only remaining gap the reclaim retries below close.
	require.Eventually(t, func() bool {
		exists, err := afero.Exists(base, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
		return err == nil && exists
	}, 5*time.Second, 5*time.Millisecond, "the worker claimed the busy marker (it now waits on the held dest lock)")
	cancelSweep() // the wave-8 deadline fires while the worker waits on the dest lock
	require.Eventually(t, func() bool { return reclaimAbandonedSweepBusyMarker(dest) },
		5*time.Second, 5*time.Millisecond,
		"the ledger consult sees the claim DURING the dest-lock wait — register-before-wait")
	markerExists, err := afero.Exists(base, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.NoError(t, err)
	require.False(t, markerExists, "wave-55: the reclaim took the marker aside — the reverter re-acquires it under its own token")

	holdRelease() // the dest-lock wait finally completes — stranded side
	require.Equal(t, 0, <-healed, "a claim reclaimed during the wait abandons immediately")
	require.Equal(t, []string{"destination lock acquisition"}, log.phases)
	require.Equal(t, "original-REG-001", string(mustRead2(t, base, backup)), "byte-intact — no stage ever ran")
	destExists, err := afero.Exists(base, dest)
	require.NoError(t, err)
	require.False(t, destExists, "zero mutations: nothing published")
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "the entry keeps its armed classification")
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)))
	freeRelease := fsutil.SharedDestLocks().Acquire(dest) // the false-bind self-release kept the registry healthy
	freeRelease()

	// The next LIVE sweep heals the untouched crash window exactly as before.
	liveIdx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sweeper.sweepOne(context.Background(), liveIdx, "/w52-reg", info))
	require.Equal(t, "original-REG-001", string(mustRead2(t, base, dest)))
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

// F2 e2e, ledger-leg variant: consumeRearmRefusedPending shares the
// register-before-wait discipline — the claim is reclaimable during its
// dest-lock wait and the false-bind abandon leaves the pending entry, the
// certified destination, and every classification untouched.
func TestSweepW52_RearmRefusedPendingRegistersBeforeDestLockWait(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := w46SeedCrashWindow(t, base, repo, "job-w52-regp", "RGP-001", "/w52-regp", "poster.jpg", p3HexA)
	writeSweepFile(t, base, dest, "new", time.Hour)
	require.NoError(t, base.Remove(backup))
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), repo, op.ID, sweepSlash(backup), models.RestorePendingKindRearmRefused))

	sweeper := NewReplacementSweeper(base, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	require.Len(t, idx.refusedPendings, 1)
	entry := idx.refusedPendings[0]

	holdRelease := fsutil.SharedDestLocks().Acquire(dest)
	log := swapRevokeLog(t)
	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- sweeper.consumeRearmRefusedPending(sweepCtx, idx, entry) }()

	require.Eventually(t, func() bool {
		exists, err := afero.Exists(base, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
		return err == nil && exists
	}, 5*time.Second, 5*time.Millisecond, "the ledger-leg worker claimed the marker and waits on the dest lock")
	cancelSweep()
	require.Eventually(t, func() bool { return reclaimAbandonedSweepBusyMarker(dest) },
		5*time.Second, 5*time.Millisecond, "the ledger-leg claim is visible during its dest-lock wait")
	holdRelease()
	require.Equal(t, 0, <-done)
	require.Equal(t, []string{"destination lock acquisition"}, log.phases)
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(), "classification preserved")
	require.Equal(t, "new", string(mustRead2(t, base, dest)))

	// The next LIVE ledger leg consumes it journal-only.
	liveIdx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sweeper.consumeRearmRefusedPending(context.Background(), liveIdx, entry))
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

// Stage e — the journal-read-failure restore undo: a claim revoked inside
// the post-publish presence transaction skips the undo entirely (the
// completed publish stands; entry armed; backup at its journaled name), and
// the reclaiming revert converges the armed crash window from scratch.
func TestSweepW52_JournalTxFailureGateSkipsRestoreUndo(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, inner, "job-w52-e1", "EJ1-001", "/w52-e1", p3HexA)
	repo := &w52StageRepo{p3OpRepo: inner, fail: map[int]error{1: errors.New("w52 journal read wedged")}}
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	repo.onCall = func(n int) {
		if n == 1 {
			reclaim()
		}
	}

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, inner, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"journal-read failure compensation (restore undo)"}, log.phases)
	require.Equal(t, "original-EJ1-001", string(mustRead2(t, base, dest)),
		"the completed publish stands — the revoked claim never undid it")
	require.Equal(t, "original-EJ1-001", string(mustRead2(t, base, backup)), "the backup was never touched")
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "armed classification preserved")

	restored, err := NewReverter(base, inner).restoreReplacementJournal(context.Background(), w52Row(t, inner, op.ID))
	require.NoError(t, err, "the reclaiming arbitration converges the armed shape")
	require.True(t, restored[dest])
	require.Empty(t, requireLedgerReplacements(t, inner, op.ID))
}

// Stage b — the consumed-entry quarantine arms: a claim revoked inside the
// presence read (the entry was consumed by a racing consumer) stops before
// the verified object moves.
func TestSweepW52_ConsumedEntryQuarantineGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, inner, "job-w52-b1", "BQ1-001", "/w52-b1", p3HexA)
	// A racing consumer already consumed the entry (the !entryPresent leg).
	require.NoError(t, inner.UpdateJournalInTx(context.Background(), op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
		return consumeSweepJournalEntry(current, sweepSlash(backup))
	}))
	repo := &w52StageRepo{p3OpRepo: inner}
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	repo.onCall = func(n int) {
		if n == 1 {
			reclaim()
		}
	}

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, inner, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"consumed-entry backup quarantine removal"}, log.phases)
	require.Equal(t, "original-BQ1-001", string(mustRead2(t, base, dest)), "the completed publish stands")
	require.Equal(t, "original-BQ1-001", string(mustRead2(t, base, backup)), "no quarantine arm ever moved the backup")
	require.Empty(t, requireLedgerReplacements(t, inner, op.ID), "the consumed journal stays consumed")
	require.Empty(t, w26DirQuarNames(t, base, "/w52-b1"), "no quarantine sibling was ever claimed")
}

// Stage b — the armed-entry quarantine arms: revocation inside the presence
// read stops the unit before the backup moves; the entry stays armed and the
// reclaiming revert re-runs the whole unit.
func TestSweepW52_ArmedEntryQuarantineGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, inner, "job-w52-b2", "BQ2-001", "/w52-b2", p3HexA)
	repo := &w52StageRepo{p3OpRepo: inner}
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	repo.onCall = func(n int) {
		if n == 1 {
			reclaim()
		}
	}

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, inner, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"armed-entry backup quarantine removal"}, log.phases)
	require.Equal(t, "original-BQ2-001", string(mustRead2(t, base, dest)), "publish completed pre-revocation")
	require.Equal(t, "original-BQ2-001", string(mustRead2(t, base, backup)), "the backup keeps its journaled name")
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending)

	restored, err := NewReverter(base, inner).restoreReplacementJournal(context.Background(), w52Row(t, inner, op.ID))
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Empty(t, requireLedgerReplacements(t, inner, op.ID))
}

// Stage c — the divergence move-back-failure persist: a claim revoked while
// the armed re-verify runs (destination diverged, then the move-back
// collides with a foreign claimant at the journaled name) stops bare: NO
// rearm-refused persist, no in-process fallback — the entry keeps its armed
// classification, the foreign occupant stays byte-intact, and the verified
// bytes stay recoverable at the quarantine name.
func TestSweepW52_DivergencePersistGate(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w52-c1", "CVR-001", "/w52-c1", p3HexA)
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))

	prev := restoredDestStillOurs
	calls := 0
	restoredDestStillOurs = func(fsys afero.Fs, d string, id restoredDestIdentity) bool {
		calls++
		if calls == 2 { // the armed-leg destination re-gate, quarantine already moved aside
			require.NoError(t, afero.WriteFile(base, backup, []byte("foreign claimant at the journaled name"), 0o644))
			reclaim()
			return false // destination diverged
		}
		return prev(fsys, d, id)
	}
	t.Cleanup(func() { restoredDestStillOurs = prev })

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, repo, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"divergence recovery restore-pending persist"}, log.phases)
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "armed classification preserved — the persist never ran")
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)), "the in-process fallback was never set")
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)),
		"the failed move-back leaves the foreign occupant byte-intact")
	quar := w26DirQuarNames(t, base, "/w52-c1")
	require.Len(t, quar, 1, "the verified bytes stay recoverable at the quarantine name")
	require.Equal(t, "original-CVR-001", string(mustRead2(t, base, "/w52-c1/"+quar[0])))
}

// Stage c — the removal-failure persist: the quarantined unlink's
// unlink-time re-verify meets a substituted occupant (scripted Lstat answer —
// the wave-32 harness; a real MemMapFs overwrite cannot stage it because its
// FileInfo reads live), the removal REFUSES, the occupancy compensation
// moves the verified object back — and the reclaim landed inside exactly
// that re-verify, so the revoked claim skips the whole marker-persist + undo
// block.
func TestSweepW52_RemovalFailurePersistGate(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w52-c2", "RMF-001", "/w52-c2", p3HexA)
	require.NoError(t, base.MkdirAll("/w52-c2f", 0o755))
	writeSweepFile(t, base, "/w52-c2f/foreign.bin", "a-much-longer-foreign-answer-object", time.Hour)
	foreignInfo, err := base.Stat("/w52-c2f/foreign.bin")
	require.NoError(t, err)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	fs := &w32QuarFs{Fs: base}
	fs.lstat = func(call int, name string) (os.FileInfo, error) {
		if call == 2 { // removeVerified's unlink-time quarantine re-verify
			reclaim()
			return foreignInfo, nil
		}
		return w32RestoreReadsReal(fs)(call, name)
	}
	sweeper := NewReplacementSweeper(fs, repo)

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, repo, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"removal-failure pending persist"}, log.phases)
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "armed classification preserved — no marker persists")
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)))
	require.Equal(t, "original-RMF-001", string(mustRead2(t, base, backup)),
		"the verified object moved back onto the journaled name — the scripted refusal never unlinked it")
	require.Equal(t, "original-RMF-001", string(mustRead2(t, base, dest)), "the publish stands untouched")
	require.Empty(t, w26DirQuarNames(t, base, "/w52-c2"))
}

// Stage d — the consumption gate: a claim revoked at the armed-leg
// destination re-verify (the quarantined unlink then completes the live
// stage) takes the wave-19 unlandable-consume resting classification — the
// rearm-refused restore-pending restage (durable + in-process), destination
// retained, backup name untouched — and the reclaiming revert consumes the
// entry JOURNAL-ONLY.
func TestSweepW52_ConsumptionGateRestagesRearmRefused(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w52-dg", "CSG-001", "/w52-dg", p3HexA)
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))

	prev := restoredDestStillOurs
	calls := 0
	restoredDestStillOurs = func(fsys afero.Fs, d string, id restoredDestIdentity) bool {
		calls++
		if calls == 2 {
			reclaim()
		}
		return prev(fsys, d, id)
	}
	t.Cleanup(func() { restoredDestStillOurs = prev })

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, repo, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"journal consumption"}, log.phases)
	backupExists, err := afero.Exists(base, backup)
	require.NoError(t, err)
	require.False(t, backupExists, "the live quarantine stage completed the unlink (never compensated backward)")
	require.Empty(t, w26DirQuarNames(t, base, "/w52-dg"))
	require.Equal(t, "original-CSG-001", string(mustRead2(t, base, dest)), "the restored destination is retained")
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.True(t, entries[0].RestorePending, "the wave-19 restage replaced the armed classification")
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind())
	kind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok, "the in-process fallback restage landed too")
	require.Equal(t, models.RestorePendingKindRearmRefused, kind)

	// The reclaiming revert consumes journal-only: no path operations against
	// the absent name, destination certified present.
	restored, err := NewReverter(base, repo).restoreReplacementJournal(context.Background(), w52Row(t, repo, op.ID))
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "original-CSG-001", string(mustRead2(t, base, dest)))
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

// Stage d, persist-failure leg of the restage: a doomed durable persist
// leaves the armed entry standing, but the in-process fallback still records
// the rearm-refused resting classification.
func TestSweepW52_ConsumptionGateRestagePersistFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, inner, "job-w52-df", "CSF-001", "/w52-df", p3HexA)
	repo := &w52StageRepo{p3OpRepo: inner, fail: map[int]error{2: errors.New("w52 persist wedged")}}
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))

	prev := restoredDestStillOurs
	calls := 0
	restoredDestStillOurs = func(fsys afero.Fs, d string, id restoredDestIdentity) bool {
		calls++
		if calls == 2 {
			reclaim()
		}
		return prev(fsys, d, id)
	}
	t.Cleanup(func() { restoredDestStillOurs = prev })

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, inner, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"journal consumption"}, log.phases)
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "the doomed durable persist leaves the armed entry standing")
	kind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, kind, "the in-process restage still landed")
	require.Equal(t, "original-CSF-001", string(mustRead2(t, base, dest)))
}

// Stage e — the consumption-failure compensation re-arm: a claim revoked
// INSIDE the failing consumption transaction never runs the re-arm publish —
// the same wave-19 restage stands in for it (the absent backup name is
// unowned by construction).
func TestSweepW52_ConsumptionFailureSkipsRearm(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, inner, "job-w52-e2", "CNC-001", "/w52-e2", p3HexA)
	innerNoFail := &w52StageRepo{p3OpRepo: inner, fail: map[int]error{2: errors.New("w52 consumption wedged")}}
	sweeper := NewReplacementSweeper(base, innerNoFail)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	innerNoFail.onCall = func(n int) {
		if n == 2 { // the consumption transaction itself
			reclaim()
		}
	}

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, inner, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"consumption failure compensation (re-arm)"}, log.phases)
	backupExists, err := afero.Exists(base, backup)
	require.NoError(t, err)
	require.False(t, backupExists, "NO re-arm publish ran — the absent name stays absent")
	require.Equal(t, "original-CNC-001", string(mustRead2(t, base, dest)), "restored destination retained")
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.True(t, entries[0].RestorePending)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind())

	restored, err := NewReverter(base, inner).restoreReplacementJournal(context.Background(), w52Row(t, inner, op.ID))
	require.NoError(t, err, "the reclaiming revert consumes the rearm-refused entry journal-only")
	require.True(t, restored[dest])
	require.Empty(t, requireLedgerReplacements(t, inner, op.ID))
}

// Stage e — the persist-failure leg of the compensation restage.
func TestSweepW52_ConsumptionFailureRestagePersistFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, inner, "job-w52-e2f", "CNF-001", "/w52-e2f", p3HexA)
	repo := &w52StageRepo{p3OpRepo: inner, fail: map[int]error{
		2: errors.New("w52 consumption wedged"),
		3: errors.New("w52 persist wedged"),
	}}
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	repo.onCall = func(n int) {
		if n == 2 {
			reclaim()
		}
	}

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, inner, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"consumption failure compensation (re-arm)"}, log.phases)
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "double failure: armed entry stands (the accepted wave-19 resting shape)")
	kind, ok := sweeper.pendingRemovalKind(sweepSlash(backup))
	require.True(t, ok)
	require.Equal(t, models.RestorePendingKindRearmRefused, kind)
	backupExists, err := afero.Exists(base, backup)
	require.NoError(t, err)
	require.False(t, backupExists, "no re-arm publish even when the restage persist fails")
	require.Equal(t, "original-CNF-001", string(mustRead2(t, base, dest)))
}

// Stage e — the post-rearm restore undo: a claim revoked AFTER the re-arm
// landed leaves the fully repaired armed retry shape (re-armed backup,
// published destination, armed entry) and never unlinks.
func TestSweepW52_PostRearmUndoGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, inner, "job-w52-e3", "CNU-001", "/w52-e3", p3HexA)
	repo := &w52StageRepo{p3OpRepo: inner, fail: map[int]error{2: errors.New("w52 consumption wedged")}}
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))

	prev := restoredDestStillOurs
	calls := 0
	restoredDestStillOurs = func(fsys afero.Fs, d string, id restoredDestIdentity) bool {
		calls++
		if calls == 3 { // the uErr leg's post-rearm identity check
			reclaim()
			return true
		}
		return prev(fsys, d, id)
	}
	t.Cleanup(func() { restoredDestStillOurs = prev })

	log := swapRevokeLog(t)
	require.False(t, sweeper.restoreAndConsume(context.Background(), w52Row(t, inner, op.ID), backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"consumption failure compensation (restore undo)"}, log.phases)
	require.Equal(t, "original-CNU-001", string(mustRead2(t, base, backup)), "the live re-arm landed before the revocation")
	require.Equal(t, "original-CNU-001", string(mustRead2(t, base, dest)), "the undo unlink never ran")
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "armed retry shape — classification unchanged")
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)))

	restored, err := NewReverter(base, inner).restoreReplacementJournal(context.Background(), w52Row(t, inner, op.ID))
	require.NoError(t, err, "the reclaiming arbitration re-runs the armed unit and converges")
	require.True(t, restored[dest])
	require.Empty(t, requireLedgerReplacements(t, inner, op.ID))
}

// Pending leg, stage c — the rearm-refused consumption-failure re-persist:
// the durable rearm-refused marker stands unchanged under a revoked claim.
func TestSweepW52_RearmRefusedRepersistGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := w46SeedCrashWindow(t, base, inner, "job-w52-pv", "PVT-001", "/w52-pv", "poster.jpg", p3HexA)
	writeSweepFile(t, base, dest, "new", time.Hour)
	require.NoError(t, base.Remove(backup))
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), inner, op.ID, sweepSlash(backup), models.RestorePendingKindRearmRefused))
	repo := &w52StageRepo{p3OpRepo: inner, fail: map[int]error{2: errors.New("w52 consumption wedged")}}
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	repo.onCall = func(n int) {
		if n == 2 { // the rearm-refused journal-only consumption
			reclaim()
		}
	}

	log := swapRevokeLog(t)
	require.False(t, sweeper.retryPendingRemovalClaimed(context.Background(), op.ID, backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"rearm-refused pending re-persist"}, log.phases)
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(), "the durable marker stands unchanged")
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)), "the in-process re-persist never ran")
	require.Equal(t, "new", string(mustRead2(t, base, dest)))
}

// Pending leg, stage b — the clean-kind quarantine arms: the revocation
// lands inside the backup-metadata Lstat (after the unit-entry gate passed)
// and stops the unit before the backup moves.
func TestSweepW52_CleanPendingQuarantineGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := w46SeedCrashWindow(t, base, inner, "job-w52-pq", "PQG-001", "/w52-pq", "poster.jpg", p3HexA)
	writeSweepFile(t, base, dest, "new", time.Hour)
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), inner, op.ID, sweepSlash(backup), models.RestorePendingKindClean))
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	fs := &w52LstatStageFs{Fs: base, match: filepath.ToSlash(backup), stage: reclaim}
	sweeper := NewReplacementSweeper(fs, inner)

	log := swapRevokeLog(t)
	require.False(t, sweeper.retryPendingRemovalClaimed(context.Background(), op.ID, backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"clean pending backup quarantine removal"}, log.phases)
	require.Equal(t, "original-PQG-001", string(mustRead2(t, base, backup)), "the quarantine arms never moved the backup")
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindClean, entries[0].PendingKind(), "the clean marker stands unchanged")
	require.Empty(t, w26DirQuarNames(t, base, "/w52-pq"))
}

// Pending leg, stage d — the clean-kind consumption: the revocation lands at
// the quarantined unlink (the live removal stage completes), leaving absent
// name + durable clean marker — the exact shape a live clean retry consumes.
func TestSweepW52_CleanPendingConsumptionGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := w46SeedCrashWindow(t, base, inner, "job-w52-pd", "PDG-001", "/w52-pd", "poster.jpg", p3HexA)
	writeSweepFile(t, base, dest, "new", time.Hour)
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), inner, op.ID, sweepSlash(backup), models.RestorePendingKindClean))
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	// Revoke at the destination-presence re-gate — after the quarantine move,
	// before the unlink; the unlink then completes the live stage and the
	// consumption gate fires.
	fs := &w52LstatStageFs{Fs: base, match: filepath.ToSlash(dest), stage: reclaim}
	sweeper := NewReplacementSweeper(fs, inner)

	log := swapRevokeLog(t)
	require.False(t, sweeper.retryPendingRemovalClaimed(context.Background(), op.ID, backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"clean pending journal consumption"}, log.phases)
	backupExists, err := afero.Exists(base, backup)
	require.NoError(t, err)
	require.False(t, backupExists, "the live removal stage completed — never compensated")
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindClean, entries[0].PendingKind(), "the durable clean marker remains the retry authorization")

	// A live clean retry tolerates the absent name and consumes journal-confirmed.
	require.True(t, sweeper.retryPendingRemovalClaimed(context.Background(), op.ID, backup, dest, sweepSlash(backup), nil),
		"the resting shape converges (nil-claim legacy posture)")
	require.Empty(t, requireLedgerReplacements(t, inner, op.ID))
}

// Pending leg, stage e — the clean consumption-failure re-arm compensation:
// never publishes the backup name under a revoked claim.
func TestSweepW52_CleanPendingRearmGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := w46SeedCrashWindow(t, base, inner, "job-w52-pe", "PEG-001", "/w52-pe", "poster.jpg", p3HexA)
	writeSweepFile(t, base, dest, "new", time.Hour)
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), inner, op.ID, sweepSlash(backup), models.RestorePendingKindClean))
	repo := &w52StageRepo{p3OpRepo: inner, fail: map[int]error{2: errors.New("w52 consumption wedged")}}
	sweeper := NewReplacementSweeper(base, repo)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	repo.onCall = func(n int) {
		if n == 2 {
			reclaim()
		}
	}

	log := swapRevokeLog(t)
	require.False(t, sweeper.retryPendingRemovalClaimed(context.Background(), op.ID, backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"clean pending consumption compensation (re-arm)"}, log.phases)
	backupExists, err := afero.Exists(base, backup)
	require.NoError(t, err)
	require.False(t, backupExists, "no re-arm publish — the absent name stays absent")
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindClean, entries[0].PendingKind())
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)))
}

// Pending leg, stage c — the clean divergence marker persist: destination
// missing, the move-back collides with a foreign claimant at the journaled
// name, and the revoked claim skips the rearm-refused upgrade persist.
func TestSweepW52_CleanPendingDivergencePersistGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := w46SeedCrashWindow(t, base, inner, "job-w52-pc", "PCG-001", "/w52-pc", "poster.jpg", p3HexA)
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), inner, op.ID, sweepSlash(backup), models.RestorePendingKindClean))
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	fs := &w52LstatStageFs{Fs: base, match: filepath.ToSlash(dest), stage: func() {
		require.NoError(t, afero.WriteFile(base, backup, []byte("foreign claimant at the journaled name"), 0o644))
		reclaim()
	}}
	sweeper := NewReplacementSweeper(fs, inner)

	log := swapRevokeLog(t)
	require.False(t, sweeper.retryPendingRemovalClaimed(context.Background(), op.ID, backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"clean pending divergence marker persist"}, log.phases)
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindClean, entries[0].PendingKind(), "the clean marker stands unchanged")
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)))
	require.Equal(t, "foreign claimant at the journaled name", string(mustRead2(t, base, backup)),
		"the no-replace move-back refusal left the occupant byte-intact")
	quar := w26DirQuarNames(t, base, "/w52-pc")
	require.Len(t, quar, 1)
	require.Equal(t, "original-PCG-001", string(mustRead2(t, base, "/w52-pc/"+quar[0])),
		"the verified bytes stay recoverable at the quarantine name")
}

// Pending leg, stage c — the clean removal-failure marker persist: the
// quarantined unlink refuses on a scripted substitution; the revoked claim
// leaves the clean marker and the in-process fallback untouched.
func TestSweepW52_CleanPendingRemovalFailurePersistGate(t *testing.T) {
	base := afero.NewMemMapFs()
	inner := newP3OpRepo()
	op, dest, backup := w46SeedCrashWindow(t, base, inner, "job-w52-pr", "PRG-001", "/w52-pr", "poster.jpg", p3HexA)
	writeSweepFile(t, base, dest, "new", time.Hour)
	require.NoError(t, markReplacementEntryRestorePendingKind(context.Background(), inner, op.ID, sweepSlash(backup), models.RestorePendingKindClean))
	require.NoError(t, base.MkdirAll("/w52-prf", 0o755))
	writeSweepFile(t, base, "/w52-prf/foreign.bin", "a-much-longer-foreign-answer-object", time.Hour)
	foreignInfo, err := base.Stat("/w52-prf/foreign.bin")
	require.NoError(t, err)
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	fs := &w32QuarFs{Fs: base}
	fs.lstat = func(call int, name string) (os.FileInfo, error) {
		if call == 2 { // removeVerified's unlink-time quarantine re-verify
			reclaim()
			return foreignInfo, nil
		}
		return w32RestoreReadsReal(fs)(call, name)
	}
	sweeper := NewReplacementSweeper(fs, inner)

	log := swapRevokeLog(t)
	require.False(t, sweeper.retryPendingRemovalClaimed(context.Background(), op.ID, backup, dest, sweepSlash(backup), claim))
	require.Equal(t, []string{"clean pending removal-failure marker persist"}, log.phases)
	entries := requireLedgerReplacements(t, inner, op.ID)
	require.Len(t, entries, 1)
	require.Equal(t, models.RestorePendingKindClean, entries[0].PendingKind())
	require.False(t, sweeper.hasPendingRemoval(sweepSlash(backup)))
	require.Equal(t, "original-PRG-001", string(mustRead2(t, base, backup)),
		"the verified object moved back onto the journaled name — the scripted refusal never unlinked it")
	require.Empty(t, w26DirQuarNames(t, base, "/w52-pr"))
}

// F1, reverter side — the reclaim→arbitrate→fresh-read ordering contract: a
// stranded worker that commits its consumption between the reclaim's return
// and the revert's per-entry fresh read is observed not-live at the read and
// SKIPPED — never re-restored, never wiped. The hook fires exactly in that
// window (after the phase-2 reclaim consult, before the dest-lock
// acquisition and every entry's fresh read).
func TestReverterW52_ConsumeBetweenReclaimAndFreshReadSkips(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w52-ord", "W52-ORD", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	backup := dest + ".dlbak.a"

	// The stranded sweep shape: marker + dest lock both held, ctx dead.
	sweepRelease, sweepToken, err := fsutil.AcquireReplacementBusyEx(fixture.fs, dest)
	require.NoError(t, err)
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	sweepCancel() // the wave-8 deadline already fired
	claim, untrack := recordSweepBusyClaim(sweepCtx, fixture.fs, dest, sweepToken, sweepRelease)
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)))
	defer claim.releaseDestLock()
	defer sweepRelease()
	defer untrack()

	hooked := false
	prevHook := preDestLockConsultHook
	t.Cleanup(func() { preDestLockConsultHook = prevHook })
	preDestLockConsultHook = func(d string) {
		if d != dest || hooked {
			return
		}
		hooked = true
		require.True(t, claim.isRevoked(),
			"the phase-2 reclaim consult revoked the abandoned claim BEFORE this window")
		// The stranded worker's wedged syscalls answer NOW — it publishes,
		// removes its backup, and commits the consumption between the reclaim
		// and the revert's fresh read.
		require.NoError(t, afero.WriteFile(fixture.fs, dest, []byte("old"), 0o644))
		require.NoError(t, fixture.fs.Remove(backup))
		require.NoError(t, fixture.repo.UpdateJournalInTx(context.Background(), op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			return consumeSweepJournalEntry(current, sweepSlash(backup))
		}))
	}

	restored, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err, "the consumed entry is skipped — the completed-consume flop never re-restores")
	require.True(t, hooked, "the reclaim→fresh-read window fired")
	require.False(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest),
		"the worker's just-restored bytes stand — nothing re-published, nothing wiped")
	require.Empty(t, requireLedgerReplacements(t, fixture.repo, op.ID), "the committed consumption stands")
	busyExists, err := afero.Exists(fixture.fs, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.NoError(t, err)
	require.False(t, busyExists, "the revert's marker lifecycle is clean")
}
