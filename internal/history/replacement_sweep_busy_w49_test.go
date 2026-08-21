package history

// POSTER-WRITE-HARDENING wave-49 (codex P2, PR#215 — "do not abandon a sweep
// while it owns a busy marker"): the wave-8 deadline proceeds with the revert
// while the abandoned sweep goroutine still holds a .dlbusy marker it claimed
// inside sweepOne (fs calls uninterruptible, the wave-46 entry gate already
// passed). The in-process, ctx-scoped claim ledger lets the continued revert
// reclaim exactly those stranded claims — through the claim's own
// once-guarded, token-bound release, never a pathname delete — while a LIVE
// sweep's marker keeps the ordinary busy refusal.

import (
	"context"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Ledger gating: no record, a live sweep's record, and a re-recorded claim
// are never reclaimed; only a record whose sweep context is DONE revokes.
func TestSweepBusyClaimW49_ReclaimGatesOnAbandonment(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w49", 0o755))
	dest := "/w49/poster.jpg"

	require.False(t, reclaimAbandonedSweepBusyMarker(dest), "no record — nothing to reclaim")

	release, token, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	liveCtx, liveCancel := context.WithCancel(context.Background())
	t.Cleanup(liveCancel)
	_, untrack := recordSweepBusyClaim(liveCtx, fs, dest, token, release)

	require.False(t, reclaimAbandonedSweepBusyMarker(dest),
		"a live sweep's marker is never reclaimed — someone still waits on it")

	liveCancel() // the deadline fired; the goroutine is stranded mid-op
	require.True(t, reclaimAbandonedSweepBusyMarker(dest), "the abandoned claim reclaims and frees the marker")
	busyGone, err := afero.Exists(fs, fsutil.ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.False(t, busyGone, "the reclaim freed the marker name")

	// The stranded goroutine's deferred release finally fires: the sync.Once
	// was consumed by the reclaim, so a FRESH claimant's marker is untouched
	// (no double-free), and its release proceeds normally.
	fresh, err := fsutil.AcquireReplacementBusy(fs, dest)
	require.NoError(t, err)
	release()
	stillHeld, err := afero.Exists(fs, fsutil.ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.True(t, stillHeld, "the stale goroutine release must not free the successor's marker")
	fresh()

	// The stranded goroutine's untrack is pointer-scoped: the record the
	// reclaim already removed is unaffected, and the ledger is empty.
	untrack()
	require.False(t, reclaimAbandonedSweepBusyMarker(dest))
}

// A re-recorded claim for the same destination (a later sweep invocation)
// survives a stale holder's untrack.
func TestSweepBusyClaimW49_UntrackIsPointerScoped(t *testing.T) {
	ctxA, cancelA := context.WithCancel(context.Background())
	t.Cleanup(cancelA)
	ctxB, cancelB := context.WithCancel(context.Background())
	t.Cleanup(cancelB)

	_, recA := recordSweepBusyClaim(ctxA, nil, "/w49/dup/poster.jpg", "", func() {})
	_, _ = recordSweepBusyClaim(ctxB, nil, "/w49/dup/poster.jpg", "", func() {})
	recA() // stale holder must not retract the live record
	require.False(t, reclaimAbandonedSweepBusyMarker("/w49/dup/poster.jpg"),
		"the live re-recorded claim survived the stale untrack")
	cancelB()
	require.True(t, reclaimAbandonedSweepBusyMarker("/w49/dup/poster.jpg"))
}

// End to end: the marker an abandoned sweep owns no longer blocks the
// continued revert — the revert's busy leg reclaims it and completes the
// restore; the stranded goroutine's cleanup afterwards is a no-op.
func TestRestoreReplacementJournalW49_ReclaimsAbandonedSweepMarker(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w49", "W49-RECLAIM", false, "new", p3Replacement{seq: 1, backupBytes: "old"})

	// The abandoned sweep, sweepOne-shaped: the marker claim landed before
	// its ctx died mid-op (the wedged fs the goroutine parks on).
	sweepRelease, sweepToken, err := fsutil.AcquireReplacementBusyEx(fixture.fs, dest)
	require.NoError(t, err)
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	_, untrack := recordSweepBusyClaim(sweepCtx, fixture.fs, dest, sweepToken, sweepRelease)

	// While the sweep is LIVE the revert keeps the ordinary busy refusal.
	restored, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, fsutil.ErrReplacementBusy)
	require.Empty(t, restored)
	require.Equal(t, "new", p3ReadFile(t, fixture.fs, dest), "a live sweep's marker is never reclaimed")
	require.Len(t, requireLedgerReplacements(t, fixture.repo, op.ID), 1, "nothing consumed under a live claim")

	// The deadline proceeds with the revert: the sweep's ctx is done while it
	// still owns the marker — the revert reclaims it and completes.
	sweepCancel()
	restored, err = NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err, "the abandoned sweep's marker no longer blocks the revert")
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
	_, err = fixture.fs.Stat(dest + ".dlbak.a")
	require.ErrorIs(t, err, os.ErrNotExist, "the restore consumed the backup")
	_, err = fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist, "the reverter released its own marker")
	require.Empty(t, requireLedgerReplacements(t, fixture.repo, op.ID), "the journal entry is consumed")

	// The stranded goroutine finally unblocks: deferred release + untrack are
	// no-ops versus the reclaim (no double-free, no stale record).
	sweepRelease()
	untrack()
	require.False(t, reclaimAbandonedSweepBusyMarker(dest))
	_, err = fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// sweepOne itself journals its claim: a sweep whose ctx dies mid-heal leaves
// a ledger record the revert can reclaim; a completed sweep leaves none.
func TestSweepOneW49_ClaimIsJournaledForTheDeadline(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	_, dest, backup := seedCrashWindow(t, fs, repo, "job-w49-rec", "REC-001", "/w49-rec", p3HexA)

	sweeper := NewReplacementSweeper(fs, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	info, err := fs.Stat(backup)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.Equal(t, 1, sweeper.sweepOne(ctx, idx, "/w49-rec", info),
		"a live sweep completes and releases its claim")
	require.False(t, reclaimAbandonedSweepBusyMarker(dest),
		"the completed sweep's record was untracked — nothing is reclaimable")

	cancel()
	require.False(t, reclaimAbandonedSweepBusyMarker(dest), "a finished sweep leaves no claim at all")
}
