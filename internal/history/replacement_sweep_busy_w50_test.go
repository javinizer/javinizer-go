package history

// POSTER-WRITE-HARDENING wave-50 (codex P2, PR#215 findings F1+F2):
//
//  - F1: a sweep stranded past the wave-8 deadline parks on its wedged fs
//    call holding BOTH the destination's .dlbusy marker AND the
//    SharedDestLocks destination lock. The wave-49 marker-only reclaim sat
//    BEHIND the reverter's blocking dest-lock acquisition, so the continued
//    revert hung indefinitely on exactly the stranding the reclaim existed
//    to heal. The claim record is now born binding the dest lock, and the
//    reverter consults (and reclaims) BEFORE it blocks.
//  - F2: the claim ledger derives record and reclaim keys through ONE
//    ledger-lifetime fsutil.DestKeyResolver, so a probe drifting between a
//    transient failure (record) and a definitive recovery (reclaim) can no
//    longer split one claim across two keys.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// End to end (F1): the sweep holds the marker AND the dest lock when the
// deadline passes — the continued revert reclaims both through the claim's
// once-guarded releases and completes; the stranded goroutine's late defers
// are no-ops and the lock registry stays healthy.
func TestRestoreReplacementJournalW50_ReclaimsAbandonedSweepDestLock(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w50", "W50-LOCK", false, "new", p3Replacement{seq: 1, backupBytes: "old"})

	// The stranded sweep, sweepOne-shaped: marker claim and dest lock both
	// landed (and are both journaled) before its ctx died mid-op — the goroutine
	// is parked on the wedged fs and never reaches its defers.
	sweepRelease, sweepToken, err := fsutil.AcquireReplacementBusyEx(fixture.fs, dest)
	require.NoError(t, err)
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	claim, untrack := recordSweepBusyClaim(sweepCtx, fixture.fs, dest, sweepToken, sweepRelease)
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)),
		"the sweep's pending dest-lock cell takes the release the instant the wait completes (wave-52)")

	// The deadline passes with the sweep still stranded mid-op, holding BOTH
	// the marker and the dest lock — the wave-8 shape proceeds to the revert
	// only AFTER the budget is spent, so the record is already ctx-done when
	// the revert consults the ledger.
	sweepCancel()

	// The continued revert must proceed: its pre-acquisition reclaim consult
	// frees the bound dest lock FIRST and the marker second, and the restore
	// completes. Run under a bound so a regression reports instead of hanging.
	type outcome struct {
		restored map[string]bool
		err      error
	}
	resultCh := make(chan outcome, 1)
	go func() {
		restored, rerr := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
		resultCh <- outcome{restored, rerr}
	}()
	select {
	case res := <-resultCh:
		require.NoError(t, res.err, "the abandoned sweep's lock+marker no longer block the revert")
		require.True(t, res.restored[dest])
	case <-time.After(5 * time.Second):
		t.Fatal("the revert hung behind the abandoned sweep's dest lock — the reclaim must run before the blocking acquisition")
	}
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest), "the restore completed after the reclaim")
	_, err = fixture.fs.Stat(dest + ".dlbak.a")
	require.ErrorIs(t, err, os.ErrNotExist, "the restore consumed the backup")
	_, err = fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist, "the reverter released its own marker")
	require.Empty(t, requireLedgerReplacements(t, fixture.repo, op.ID), "the journal entry is consumed")

	// The stranded goroutine finally unblocks: deferred releases + untrack are
	// once-guard no-ops versus the reclaim (no double-free, no stale record).
	claim.releaseDestLock()
	sweepRelease()
	untrack()
	require.False(t, reclaimAbandonedSweepBusyMarker(dest))
	_, err = fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist)

	// The dest-lock registry survived the out-from-under release: the
	// destination locks and releases normally.
	freeRelease := fsutil.SharedDestLocks().Acquire(dest)
	freeRelease()
}

// Live-foreign posture (F1): a sweep someone still waits on is never
// reclaimed — the revert blocks behind its dest lock until the sweep finishes
// WELL; the normal (reclaim-free) takeover then proceeds.
func TestRestoreReplacementJournalW50_LiveSweepClaimKeepsTheBlockingPosture(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w50-live", "W50-LIVE", false, "new", p3Replacement{seq: 1, backupBytes: "old"})

	sweepRelease, sweepToken, err := fsutil.AcquireReplacementBusyEx(fixture.fs, dest)
	require.NoError(t, err)
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	t.Cleanup(sweepCancel)
	claim, untrack := recordSweepBusyClaim(sweepCtx, fixture.fs, dest, sweepToken, sweepRelease)
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)))

	blocked := make(chan error, 1)
	go func() {
		_, rerr := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
		blocked <- rerr
	}()
	select {
	case rerr := <-blocked:
		t.Fatalf("a live sweep's claim must keep the revert waiting, got %v", rerr)
	case <-time.After(200 * time.Millisecond):
	}

	// The sweep finishes WELL (its ctx never dies): untrack, then marker, then
	// lock — freeing in that order means the waiting revert can only observe
	// the lock freed AFTER the marker is already gone, so every interleaving
	// converges to the reclaim-free happy path.
	untrack()
	sweepRelease()
	claim.releaseDestLock()

	select {
	case rerr := <-blocked:
		require.NoError(t, rerr, "once the live sweep finishes, the revert completes without any reclaim")
	case <-time.After(5 * time.Second):
		t.Fatal("the revert must complete once the live sweep releases")
	}
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
	require.Empty(t, requireLedgerReplacements(t, fixture.repo, op.ID))
	require.False(t, reclaimAbandonedSweepBusyMarker(dest))
}

// Retry leg (F1): the abandoned sweep's claim lands in the window BETWEEN
// the revert's pre-acquisition consult and the busy-marker acquire (the
// deadline firing mid-window), so the wave-49 busy-leg consult is what
// reclaims it and the retried acquisition succeeds.
func TestRestoreReplacementJournalW50_LateRecordedClaimReclaimedByBusyLeg(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w50-retry", "W50-RETRY", false, "new", p3Replacement{seq: 1, backupBytes: "old"})

	// The abandoned sweep owns the marker already but, stranded mid-op, has not
	// journaled its claim yet: the pre-acquisition consult finds nothing.
	sweepRelease, sweepToken, err := fsutil.AcquireReplacementBusyEx(fixture.fs, dest)
	require.NoError(t, err)
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	sweepCancel() // its ctx died first (the wave-8 deadline)

	prevHook := preDestLockConsultHook
	t.Cleanup(func() { preDestLockConsultHook = prevHook })
	hooked := false
	var untrack func()
	preDestLockConsultHook = func(d string) {
		if d != dest || hooked {
			return
		}
		hooked = true
		_, untrack = recordSweepBusyClaim(sweepCtx, fixture.fs, dest, sweepToken, sweepRelease)
	}

	restored, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err, "the busy-leg consult reclaims the claim recorded after the pre-acquisition consult")
	require.True(t, hooked, "the staged window fired")
	require.True(t, restored[dest])
	require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
	require.Empty(t, requireLedgerReplacements(t, fixture.repo, op.ID), "the journal entry is consumed")
	_, err = fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist, "the reverter released its own marker")

	sweepRelease() // the stranded goroutine's late release is a once-guard no-op
	untrack()
	require.False(t, reclaimAbandonedSweepBusyMarker(dest))
}

// Ledger mechanics (F1): the dest-lock release bound at record time is what
// the reclaim fires; a marker-only claim (nil dest release) reclaims exactly
// like before.
func TestSweepBusyClaimW50_RecordBindsBothArbitrationHolds(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w50-unit", 0o755))
	dest := "/w50-unit/poster.jpg"

	sweepRelease, sweepToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	claim, untrack := recordSweepBusyClaim(ctx, fs, dest, sweepToken, sweepRelease)
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)))

	require.False(t, reclaimAbandonedSweepBusyMarker(dest), "a live claim is never reclaimed")

	cancel() // the deadline fired; the goroutine is stranded mid-op
	require.True(t, reclaimAbandonedSweepBusyMarker(dest),
		"the abandoned claim is reclaimed through its bound releases")
	busyGone, err := afero.Exists(fs, fsutil.ReplacementBusyPath(dest))
	require.NoError(t, err)
	require.False(t, busyGone, "the reclaim freed the marker name")

	// The reclaim released the bound dest lock out from under the stranded
	// holder — the destination is lockable again within a bounded wait.
	freed := make(chan func(), 1)
	go func() { freed <- fsutil.SharedDestLocks().Acquire(dest) }()
	select {
	case rel := <-freed:
		rel()
	case <-time.After(5 * time.Second):
		t.Fatal("the abandoned claim's dest lock was NOT released by the reclaim")
	}

	// Once-guards: the stranded goroutine's own deferred releases are no-ops,
	// and the pointer-scoped untrack leaves nothing behind.
	claim.releaseDestLock()
	sweepRelease()
	untrack()
	require.False(t, reclaimAbandonedSweepBusyMarker(dest))

	// A marker-only claim (no bound dest lock) keeps the wave-49 reclaim leg.
	markerOnly, markerToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	ctx2, cancel2 := context.WithCancel(context.Background())
	_, untrack2 := recordSweepBusyClaim(ctx2, fs, dest, markerToken, markerOnly)
	cancel2()
	require.True(t, reclaimAbandonedSweepBusyMarker(dest))
	untrack2()
}

// Frozen key (F2): the ledger's DestKeyResolver freezes the root posture at
// record time, so a probe that FAILS for the record and SUCCEEDS for the
// reclaim can never split the claim across two keys.
//
// Probe-seam mutation is process-global — never parallel.
func TestSweepBusyClaimW50_FrozenKeySurvivesProbeDrift(t *testing.T) {
	prevProbe := fsutil.CaseSensitiveProbe
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = prevProbe
		fsutil.ResetCaseSensitivityCache()
	})
	fsutil.ResetCaseSensitivityCache()

	probeCalls := 0
	fsutil.CaseSensitiveProbe = func(string) (bool, error) {
		probeCalls++
		if probeCalls == 1 {
			return false, errors.New("transient probe outage")
		}
		return false, nil // definitive INSENSITIVE on recovery (would fold case)
	}

	ledger := newSweepBusyClaimLedger()
	dest := "/w50/Poster.JPG"

	releaseCount := 0
	ctx, cancel := context.WithCancel(context.Background())
	_, untrack := ledger.record(ctx, nil, dest, "", func() { releaseCount++ })
	require.Equal(t, 1, probeCalls, "the record derives its key under the first (failing) probe")

	cancel() // the sweep is abandoned at the deadline
	require.True(t, ledger.reclaim(dest),
		"the frozen record key survives the probe drift — the reclaim finds the abandoned claim")
	require.Equal(t, 1, releaseCount, "the recorded release ran exactly once")
	require.Equal(t, 1, probeCalls, "the ledger resolver never re-probes — the posture froze at record time")
	untrack()
	require.False(t, ledger.reclaim(dest))
}
