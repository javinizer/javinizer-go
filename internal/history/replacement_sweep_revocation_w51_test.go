// POSTER-WRITE-HARDENING wave-51 (codex P1, PR#215 — "do not revoke the lock
// while the sweep is still running"): wave-49/50's
// reclaimAbandonedSweepBusyMarker fired the recorded releases even though the
// abandoned goroutine's wedged fs call cannot observe cancellation — the
// continued revert then mutated the destination while the stranded goroutine,
// once its fs call answered, ran post-call verification, quarantine, removal,
// and journal consumption UNCHECKED (two restore/consume sequences racing one
// backup). The claim now carries a monotonic epoch + an in-process revocation
// flag the stranded goroutine itself checks at every mutation surface; the
// reclaimer flips the flag BEFORE firing the releases. These tests pin:
//
//   - the ledger mechanics (epoch monotonicity, revoke-before-release, the
//     wave-46 reclaim gates unchanged);
//   - both resume orderings around the reclaim: gate-stop (revert continues)
//     and completed-consume flop (nothing re-published, nothing wiped);
//   - every gated mutation surface: destination publish, the
//     backup-removal + journal-consumption unit, the pending-cleanup units
//     (orphan-shaped, clean kind, and rearm-refused kind), and the orphan
//     restore publish.

package history

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w51ParkFs wedges the n-th matched LstatIfPossible call like a stalled
// network filesystem: the caller releases it after staging the reclaim. Every
// later call flows through (the release channel stays closed).
//
// Wave-53 (codex P5, PR#215 — the windows CI hang): the path match is
// PLATFORM-AGNOSTIC — both sides normalize through filepath.ToSlash before
// the equality test. seedCrashWindow hands back a filepath.FromSlash'd dest
// (backslash separators on Windows), so the pre-shape comparison
// `filepath.ToSlash(name) == f.destSlash` matched a slash-form name against a
// backslash-form destSlash on Windows and NEVER fired: <-fs.entered blocked
// forever and timed out the whole history suite (10m). The fake models the
// fs-call-ordering choreography on every platform once the comparison is
// separator-agnostic; the revocation contract this test pins (gate-stop at
// the destination publish) is covered unchanged on Windows too — coverage is
// not weakened.
type w51ParkFs struct {
	afero.Fs
	destSlash string // slash-form path whose n-th LstatIfPossible wedges
	n         int
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
	mu        sync.Mutex
	count     int
}

func (f *w51ParkFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.destSlash) {
		f.mu.Lock()
		f.count++
		hit := f.count == f.n
		f.mu.Unlock()
		if hit {
			f.once.Do(func() { close(f.entered) })
			<-f.release
		}
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(name)
}

// w51RevokeLog captures the revocation-gate seam's calls (which surface
// refused a revoked claim, at which claim epoch).
type w51RevokeLog struct {
	mu     sync.Mutex
	phases []string
	epochs []uint64
}

func (l *w51RevokeLog) add(phase string, epoch uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.phases = append(l.phases, phase)
	l.epochs = append(l.epochs, epoch)
}

func swapRevokeLog(t *testing.T) *w51RevokeLog {
	t.Helper()
	rec := &w51RevokeLog{}
	prev := sweepClaimRevokedLogFn
	sweepClaimRevokedLogFn = func(phase string, epoch uint64, _, _ string) { rec.add(phase, epoch) }
	t.Cleanup(func() { sweepClaimRevokedLogFn = prev })
	return rec
}

// w51ReclaimedClaim records (and immediately abandons + reclaims) a claim for
// dest, handing back the revoked claim for direct retryPendingRemovalClaimed
// staging.
func w51ReclaimedClaim(t *testing.T, dest string) *sweepBusyMarkerClaim {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	claim, untrack := recordSweepBusyClaim(ctx, nil, dest, "", func() {})
	cancel() // the wave-8 deadline fired; the goroutine is stranded mid-op
	require.True(t, reclaimAbandonedSweepBusyMarker(dest), "the abandoned claim reclaims")
	require.True(t, claim.isRevoked(), "the reclaim flipped the revocation flag before the releases")
	untrack() // pointer-scoped no-op after the reclaim deleted the record
	return claim
}

// Ledger mechanics: epochs are monotonic, the revocation flag is set BEFORE
// the recorded releases fire, a live claim is never reclaimed nor revoked,
// and the gate helper reports through the seam with the claim's epoch.
func TestSweepBusyClaimW51_RevocationOrdering(t *testing.T) {
	ledger := newSweepBusyClaimLedger()
	dest := "/w51-unit/poster.jpg"

	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	rec1, untrack1 := ledger.record(ctxA, nil, dest, "", func() {})
	ctxB, cancelB := context.WithCancel(context.Background())
	var releaseObservedRevoked bool
	var rec2 *sweepBusyMarkerClaim
	rec2, untrack2 := ledger.record(ctxB, nil, dest, "", func() { releaseObservedRevoked = rec2.isRevoked() })

	require.Positive(t, rec1.epoch, "claims carry a ledger-issued epoch")
	require.Greater(t, rec2.epoch, rec1.epoch, "claim epochs are monotonic across records")

	require.False(t, ledger.reclaim(dest), "a live sweep's record is never reclaimed")
	require.False(t, rec2.isRevoked(), "a live claim is never revoked")

	cancelB() // the deadline fired; the goroutine is stranded mid-op
	require.True(t, ledger.reclaim(dest))
	require.True(t, rec2.isRevoked(), "the reclaim set the revocation flag")
	require.True(t, releaseObservedRevoked,
		"the recorded release observed the flag already set — revoke strictly precedes the releases")
	require.False(t, rec1.isRevoked(), "the superseded record's flag is untouched by the newer claim's reclaim")
	require.False(t, ledger.reclaim(dest), "a reclaimed record is gone — double reclaim refuses")

	// The gate reports through the seam with the claim's epoch and phase.
	log := swapRevokeLog(t)
	require.True(t, rec2.abandonIfRevoked("test surface", "backup", dest))
	require.Equal(t, []string{"test surface"}, log.phases)
	require.Equal(t, []uint64{rec2.epoch}, log.epochs)
	require.False(t, rec1.abandonIfRevoked("test surface", "backup", dest), "an unrevoked claim passes every gate")
	var nilClaim *sweepBusyMarkerClaim
	require.False(t, nilClaim.abandonIfRevoked("test surface", "backup", dest),
		"the nil-claim direct-caller posture is never revoked")

	untrack1()
	untrack2()
	require.False(t, ledger.reclaim(dest))
}

// Ordering A — reclaim-before-worker-wake, gate-stop: the worker parked
// INSIDE the destination classification Lstat resumes after the reclaim and
// performs ZERO further mutations (no publish, no removal, no journal op, no
// pending persist) — then the continued revert heals the crash window
// normally (the reverter continues leg).
func TestSweepW51_StrandedResumeAfterRevokeStopsAtPublishGate(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w51-resume", "RSM-001", "/w51-resume", p3HexA)

	log := swapRevokeLog(t)
	fs := &w51ParkFs{Fs: base, destSlash: dest, n: 1, entered: make(chan struct{}), release: make(chan struct{})}
	sweeper := NewReplacementSweeper(fs, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	info, err := base.Stat(backup)
	require.NoError(t, err)

	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	healed := make(chan int, 1)
	go func() { healed <- sweeper.sweepOne(sweepCtx, idx, "/w51-resume", info) }()
	<-fs.entered // the worker is parked inside the classify Lstat

	cancelSweep() // the wave-8 deadline proceeds with the revert
	require.True(t, reclaimAbandonedSweepBusyMarker(dest), "the continued revert reclaims the stranded claim")
	markerExists, err := afero.Exists(base, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.NoError(t, err)
	require.False(t, markerExists, "wave-55: the reclaim took the marker aside — the reverter re-acquires it under its own token")

	close(fs.release) // the wedged filesystem finally answers
	require.Equal(t, 0, <-healed, "a revoked claim heals nothing")

	require.Equal(t, []string{"destination publish"}, log.phases, "the publish gate caught the revoked claim")
	require.Len(t, log.epochs, 1)
	require.Positive(t, log.epochs[0], "the gate reported the claim's ledger epoch")
	destExists, err := afero.Exists(base, dest)
	require.NoError(t, err)
	require.False(t, destExists, "zero further mutations: the revoked worker never published")
	require.Equal(t, "original-RSM-001", string(mustRead2(t, base, backup)), "the backup stays byte-intact")
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1, "the journal stays — no consumption, no pending persist")
	require.False(t, entries[0].RestorePending, "the entry keeps its armed pre-mutation classification")
	markerExists, err = afero.Exists(base, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.NoError(t, err)
	require.False(t, markerExists, "the stranded goroutine's deferred releases were once-guard no-ops")

	// The continued revert heals the crash window exactly as if the stranded
	// sweep never ran: restore, consume, release.
	restored, err := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err, "the reverter continues after the gate-stop")
	require.True(t, restored[dest])
	require.Equal(t, "original-RSM-001", string(mustRead2(t, base, dest)))
	backupExists, err := afero.Exists(base, backup)
	require.NoError(t, err)
	require.False(t, backupExists)
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

// Ordering B — worker resumes and commits BEFORE the reclaim: the heal runs
// to completion (journal committed) and the reclaim flops to the
// completed-consume posture — the revert re-publishes NOTHING onto the
// destination the worker just restored and wipes NOTHING of its state (it
// skips the consumed entry and continues with the remaining live ones). The
// revocation seam never fires: the worker's claim was never revoked.
func TestSweepW51_CommitBeforeReclaimFlopsToCompletedConsume(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest1 := "/w51-flop/a/poster.jpg"
	backup1 := dest1 + ".dlbak." + p3HexA
	dest2 := "/w51-flop/b/poster.jpg"
	backup2 := dest2 + ".dlbak." + p3HexB
	require.NoError(t, base.MkdirAll("/w51-flop/a", 0o755))
	require.NoError(t, base.MkdirAll("/w51-flop/b", 0o755))
	writeSweepFile(t, base, backup1, "old-one", time.Hour) // crash window: dest1 absent
	writeSweepFile(t, base, dest2, "new-two", time.Hour)
	writeSweepFile(t, base, backup2, "old-two", time.Hour)

	raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest1, Backup: backup1, DestSeq: 1},
		{Destination: dest2, Backup: backup2, DestSeq: 2},
	}})
	require.NoError(t, err)
	op := &models.BatchFileOperation{
		BatchJobID: "job-w51-flop", MovieID: "FLP-001", OriginalPath: "/src/FLP-001.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))

	log := swapRevokeLog(t)
	fs := &w51ParkFs{Fs: base, destSlash: dest1, n: 1, entered: make(chan struct{}), release: make(chan struct{})}
	sweeper := NewReplacementSweeper(fs, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	info, err := base.Stat(backup1)
	require.NoError(t, err)

	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	healed := make(chan int, 1)
	go func() { healed <- sweeper.sweepOne(sweepCtx, idx, "/w51-flop/a", info) }()
	<-fs.entered // parked inside the destination classification Lstat

	cancelSweep()     // the deadline fires…
	close(fs.release) // …but the fs answers BEFORE anyone reclaims: the
	require.Equal(t, 1, <-healed, "worker resumes with its claim never revoked and completes the heal")

	require.Empty(t, log.phases, "no gate ever fired — the commit landed before any revocation")
	require.Equal(t, "old-one", string(mustRead2(t, base, dest1)), "the crash window healed")
	backup1Exists, err := afero.Exists(base, backup1)
	require.NoError(t, err)
	require.False(t, backup1Exists, "the worker consumed its backup")
	require.False(t, reclaimAbandonedSweepBusyMarker(dest1),
		"the completed worker untracked its claim — nothing is reclaimable")

	// The revert's journal-commit gate (the fresh re-read serialized through
	// the row's journal lock) observes the worker's committed consumption and
	// SKIPS the entry: any restore attempt against dest1 would fail at its
	// absent backup, so the successful run proves nothing re-published.
	restored, err := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.False(t, restored[dest1], "the consumed entry is skipped — the completed-consume flop re-publishes nothing")
	require.True(t, restored[dest2], "the remaining live entry still reverts")
	require.Equal(t, "old-one", string(mustRead2(t, base, dest1)), "the worker's restored bytes stand untouched — nothing wiped or re-published")
	require.Equal(t, "old-two", string(mustRead2(t, base, dest2)))
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

// The removal-unit gate: a revocation landing while the worker is parked in
// the post-publish destination re-verification stops the quarantine move,
// the verified unlink, the consumption, and every pending persist from even
// starting. The publish itself completed pre-revocation (allowed), the
// backup stays at its journaled name, the entry stays armed, and the
// continued revert converges the state afterwards.
func TestSweepW51_StrandedResumeAfterRevokeStopsAtRemovalUnit(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w51-rmunit", "RMU-001", "/w51-rmunit", p3HexA)

	log := swapRevokeLog(t)
	sweeper := NewReplacementSweeper(base, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	info, err := base.Stat(backup)
	require.NoError(t, err)

	// Park inside the wave-31 publish-time destination re-verification seam —
	// the exact "post-call verification" instant from the finding — then
	// reclaim while the worker is parked there.
	entered := make(chan struct{})
	release := make(chan struct{})
	prev := restoredDestStillOurs
	var once sync.Once
	restoredDestStillOurs = func(fsys afero.Fs, d string, id restoredDestIdentity) bool {
		once.Do(func() { close(entered) })
		<-release // the first call parks; after close every later call flows through
		return prev(fsys, d, id)
	}
	t.Cleanup(func() { restoredDestStillOurs = prev })

	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	healed := make(chan int, 1)
	go func() { healed <- sweeper.sweepOne(sweepCtx, idx, "/w51-rmunit", info) }()
	<-entered // dest published; the worker is parked at the re-verification

	cancelSweep()
	require.True(t, reclaimAbandonedSweepBusyMarker(dest))
	close(release) // the verification answers true; the next mutation surface gates
	require.Equal(t, 0, <-healed, "a revoked claim heals nothing")

	require.Equal(t, []string{"backup removal and journal consumption"}, log.phases,
		"the removal-unit entry gate caught the revoked claim")
	require.Equal(t, "original-RMU-001", string(mustRead2(t, base, dest)),
		"the publish completed BEFORE revocation and is left in place")
	require.Equal(t, "original-RMU-001", string(mustRead2(t, base, backup)), "the backup was never quarantined or unlinked")
	entries := requireLedgerReplacements(t, repo, op.ID)
	require.Len(t, entries, 1)
	require.False(t, entries[0].RestorePending, "armed classification preserved — journal never clobbered half-mutated")

	// The continued revert converges the post-gate state: re-restore over the
	// published bytes, remove the backup, consume the entry.
	restored, err := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.Equal(t, "original-RMU-001", string(mustRead2(t, base, dest)))
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

// Pending-cleanup gates: the orphan-shaped (no durable entry) cleanup, the
// clean-kind pending cleanup, and the journal-only rearm-refused consumption
// all abandon without further mutation once their claim was reclaimed —
// durable markers, kinds, and bytes all stay exactly as classified.
func TestSweepW51_RevokedPendingCleanupAbandons(t *testing.T) {
	ctx := context.Background()

	t.Run("orphan-shaped cleanup keeps bytes and fallback memory", func(t *testing.T) {
		base := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest := "/w51-pend-a/poster.jpg"
		backup := dest + ".dlbak." + p3HexA
		require.NoError(t, base.MkdirAll("/w51-pend-a", 0o755))
		writeSweepFile(t, base, dest, "new", time.Hour)
		writeSweepFile(t, base, backup, "old", time.Hour)
		raw, err := json.Marshal(models.GeneratedFilesJSON{})
		require.NoError(t, err)
		row := &models.BatchFileOperation{
			BatchJobID: "job-w51-pend-a", MovieID: "PND-001", OriginalPath: "/src/PND-001.mkv",
			OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
			RevertStatus: models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(ctx, row))

		sweeper := NewReplacementSweeper(base, repo)
		sweeper.rememberPendingRemoval(sweepSlash(backup))
		claim := w51ReclaimedClaim(t, dest)

		log := swapRevokeLog(t)
		require.False(t, sweeper.retryPendingRemovalClaimed(ctx, row.ID, backup, dest, sweepSlash(backup), claim),
			"the orphan-shaped pending cleanup abandons once its claim was reclaimed")
		require.Equal(t, []string{"unowned pending-entry backup cleanup"}, log.phases)
		require.True(t, sweeper.hasPendingRemoval(sweepSlash(backup)), "the fallback memory is never dropped by the abandon")
		require.Equal(t, "old", string(mustRead2(t, base, backup)), "the backup stays byte-intact")
		require.Empty(t, requireLedgerReplacements(t, repo, row.ID), "no journal record was touched")
	})

	t.Run("clean pending entry keeps marker and bytes", func(t *testing.T) {
		base := afero.NewMemMapFs()
		repo := newP3OpRepo()
		op, dest, backup := w46SeedCrashWindow(t, base, repo, "job-w51-pend-c", "PND-002", "/w51-pend-c", "poster.jpg", p3HexA)
		writeSweepFile(t, base, dest, "new", time.Hour) // present destination is the pending-cleanup shape
		require.NoError(t, markReplacementEntryRestorePendingKind(ctx, repo, op.ID, sweepSlash(backup), models.RestorePendingKindClean))

		sweeper := NewReplacementSweeper(base, repo)
		claim := w51ReclaimedClaim(t, dest) // fresh dest ⇒ no ledger cross-talk with the sibling subtests

		// The default log seam stays wired (its production body is exercised).
		require.False(t, sweeper.retryPendingRemovalClaimed(ctx, op.ID, backup, dest, sweepSlash(backup), claim))
		entries := requireLedgerReplacements(t, repo, op.ID)
		require.Len(t, entries, 1, "the entry is never consumed under revocation")
		require.True(t, entries[0].RestorePending)
		require.Equal(t, models.RestorePendingKindClean, entries[0].PendingKind(), "the durable clean kind is untouched")
		require.Equal(t, "original-PND-002", string(mustRead2(t, base, backup)), "the backup removal never started")
		require.Equal(t, "new", string(mustRead2(t, base, dest)))
	})

	t.Run("rearm-refused pending entry keeps marker and touches no path", func(t *testing.T) {
		base := afero.NewMemMapFs()
		repo := newP3OpRepo()
		op, dest, backup := w46SeedCrashWindow(t, base, repo, "job-w51-pend-r", "PND-003", "/w51-pend-r", "poster.jpg", p3HexA)
		writeSweepFile(t, base, dest, "new", time.Hour)
		require.NoError(t, base.Remove(backup)) // the rearm-refused name is unowned/absent by construction
		require.NoError(t, markReplacementEntryRestorePendingKind(ctx, repo, op.ID, sweepSlash(backup), models.RestorePendingKindRearmRefused))

		sweeper := NewReplacementSweeper(base, repo)
		claim := w51ReclaimedClaim(t, dest)

		require.False(t, sweeper.retryPendingRemovalClaimed(ctx, op.ID, backup, dest, sweepSlash(backup), claim),
			"the journal-only consumption abandons under revocation")
		entries := requireLedgerReplacements(t, repo, op.ID)
		require.Len(t, entries, 1, "the entry is never consumed under revocation")
		require.True(t, entries[0].RestorePending)
		require.Equal(t, models.RestorePendingKindRearmRefused, entries[0].PendingKind(), "the rearm-refused classification is preserved")
		require.Equal(t, "new", string(mustRead2(t, base, dest)))
	})
}

// The orphan restore publish gate: an unjournaled marker-shaped backup whose
// stranded worker resumes after the reclaim is never published onto the
// destination; the byte-intact backup re-arbitrates on the next LIVE sweep.
func TestSweepW51_RevokedOrphanPublishAbandons(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	require.NoError(t, base.MkdirAll("/w51-orph", 0o755))
	dest := "/w51-orph/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	writeSweepFile(t, base, backup, "orphan-bytes", time.Hour)

	log := swapRevokeLog(t)
	fs := &w51ParkFs{Fs: base, destSlash: dest, n: 1, entered: make(chan struct{}), release: make(chan struct{})}
	sweeper := NewReplacementSweeper(fs, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	info, err := base.Stat(backup)
	require.NoError(t, err)

	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	healed := make(chan int, 1)
	go func() { healed <- sweeper.sweepOne(sweepCtx, idx, "/w51-orph", info) }()
	<-fs.entered // parked inside the orphan destination classification

	cancelSweep()
	require.True(t, reclaimAbandonedSweepBusyMarker(dest))
	close(fs.release)
	require.Equal(t, 0, <-healed, "the revoked orphan publish heals nothing")

	require.Equal(t, []string{"orphan destination publish"}, log.phases)
	destExists, err := afero.Exists(base, dest)
	require.NoError(t, err)
	require.False(t, destExists, "no restore published after revocation")
	require.Equal(t, "orphan-bytes", string(mustRead2(t, base, backup)), "the unjournaled marker file stays byte-intact")

	// The next LIVE sweep re-arbitrates the untouched state exactly as before.
	liveIdx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, sweeper.sweepOne(context.Background(), liveIdx, "/w51-orph", info),
		"a live sweep restores the orphan crash window")
	require.Equal(t, "orphan-bytes", string(mustRead2(t, base, dest)))
}
