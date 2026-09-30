package history

// Wave-64 (PR#276 — "the wedge consult must never sample a cross-goroutine
// wall-clock race", the Windows CI 10m hang): wave-57 kept a wedged claim's
// arbitration held (admission timeout → skip the destination for a later
// retry), but DISCOVERING that wedge rode on two independent wall-clock
// deadlines measured on two different goroutines — reclaim's bounded
// waitReleased grace vs. the detached drain's wave-56 admission grace. On a
// healthy host the drain publishes the wedge first; on a coarse or
// oversubscribed Windows CI runner the release grace expires first, the
// reinserted record never lands, sweepClaimIsWedged samples "nothing wedged",
// and the restore falls through to the BLOCKING destination-lock acquisition
// behind the still-held lock — a deadlock bounded only by the package
// timeout (TestReverterW57_WedgedClaimSkipsDestinationAndLaterRetrySucceeds,
// 9m41s on the 10m deadline).
//
// The fix is structural, not a tuning of the graces: the claim record stays
// in the ledger across the whole drain, the drain closes the claim's
// admitResolved channel the instant its admission decision is FINAL, and the
// reverter-side consult AWAITS that channel — a wait on a runnable
// goroutine's strictly in-process, deadline-bounded TryLock polling that can
// never strand on the wedged filesystem. These tests pin the posture with the
// grace relationship INVERTED (the decision deliberately lands after the
// release grace), which reproduces the Windows runner shape on every host:
// pre-wave-64 this wedges the reverter; wave-64 fails busy in bounded time
// and the later retry still restores.

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// invertW64Graces makes the drain's admission decision land AFTER reclaim's
// waitReleased grace — the pre-wave-64 race window — on any platform, without
// timing assumptions (both windows are large enough that ordinary scheduling
// noise cannot reorder them, small enough to keep the suite fast).
func invertW64Graces(t *testing.T) {
	t.Helper()
	prevAdmit, prevRelease := sweepReclaimAdmitGrace, sweepReclaimReleaseGrace
	sweepReclaimAdmitGrace = 300 * time.Millisecond
	sweepReclaimReleaseGrace = 10 * time.Millisecond
	t.Cleanup(func() { sweepReclaimAdmitGrace, sweepReclaimReleaseGrace = prevAdmit, prevRelease })
}

// The headline leg, decision-late: the wedged consult awaits the drain's
// FINAL admission decision deterministically instead of sampling it after a
// wall-clock grace — the destination skips busy and the later retry restores.
func TestReverterW64_DecisionAfterReleaseGraceStillSkipsThenRetrySucceeds(t *testing.T) {
	invertW64Graces(t)

	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, _ := seedCrashWindow(t, base, repo, "job-w64", "W64-001", "/w64", p3HexA)

	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(base, dest)
	require.NoError(t, err)
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	defer sweepCancel()
	claim, untrack := recordSweepBusyClaim(sweepCtx, base, dest, busyToken, busyRelease)
	// Unstick every hold even on a regression: a wave-64 break must FAIL this
	// test in seconds, never re-create the 10m package hang.
	t.Cleanup(func() { claim.releaseAdmit(); claim.releaseDestLock(); busyRelease(); untrack() })
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)),
		"the sweep owns the dest lock — a sampled non-wedge deadlocks on it")
	require.False(t, claim.abandonIfRevoked("destination publish", dest+".dlbak."+p3HexA, dest),
		"a live admitted claim proceeds past the gate")
	sweepCancel() // the sweep's deadline fired; the worker is stranded mid-mutation

	// The reverter runs on its own goroutine with a watchdog: reclaim's
	// waitReleased grace (10ms) expires long before the drain's admission
	// decision (300ms) — the consult must AWAIT the decision, timeout-free.
	type outcome struct {
		restored map[string]bool
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		var oc outcome
		oc.restored, oc.err = NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
		done <- oc
	}()
	var first outcome
	select {
	case first = <-done:
		require.ErrorIs(t, first.err, fsutil.ErrReplacementBusy,
			"the wedged dest fails busy (busy-class) even when the decision lands after the release grace")
		require.ErrorContains(t, first.err, "wedged by an in-flight sweep mutation")
		require.False(t, first.restored[dest], "the wedged dest was skipped — never touched concurrently")
	case <-time.After(5 * time.Second):
		t.Fatal("the reverter wedged behind the still-held dest lock — the pre-wave-64 Windows CI hang")
	}
	require.True(t, claim.isRevoked(), "the reclaim revoked the stranded claim")
	require.True(t, fsutil.ReplacementBusyMarkerIsOurs(base, dest, busyToken),
		"NO release fired — the wedged claim retains both holds")
	require.Len(t, requireLedgerReplacements(t, repo, op.ID), 1, "the journal entry stays armed for retry")

	// The wedge unblocks: the worker abandons at its next gate and its
	// deferred releases self-fire — a later retry restores normally.
	claim.releaseAdmit()
	require.True(t, claim.abandonIfRevoked("backup removal", dest+".dlbak."+p3HexA, dest),
		"the worker abandons at the next gate — no stale work lands")
	claim.releaseDestLock()
	busyRelease()
	untrack()
	require.False(t, sweepClaimIsWedged(dest), "the wedged claim self-released")

	restored, rerr := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, rerr, "the later retry succeeds once the wedge self-releases")
	require.True(t, restored[dest], "the destination is restored on retry")
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID), "the journal entry is consumed")
}

// Unit leg: with the decision landing late, the consult called mid-drain
// BLOCKS (bounded by the in-process admission grace) and returns the final
// wedge verdict — never a sampled false — and a settled non-wedged drain
// leaves nothing in the ledger for a later consult to await.
func TestSweepBusyClaimW64_ConsultAwaitsFinalDecisionNotGraceSample(t *testing.T) {
	invertW64Graces(t)

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w64-unit", 0o755))
	dest := "/w64-unit/poster.jpg"
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, fs, dest, busyToken, busyRelease)
	defer untrack()
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)),
		"the claim owns its dest lock — a sampled non-wedge would deadlock")
	require.False(t, claim.abandonIfRevoked("destination publish", "backup", dest),
		"a live admitted claim proceeds past the gate")
	cancel()

	require.True(t, reclaimAbandonedSweepBusyMarker(dest),
		"the abandoned claim reclaims (revoke + detached release)")
	require.True(t, claim.reclaimStarted, "the drain is detached exactly once")

	// Consult mid-drain: the admission decision (300ms out) postdates reclaim's
	// return (10ms grace) — the consult awaits it deterministically.
	start := time.Now()
	require.True(t, sweepClaimIsWedged(dest), "the consult awaits the FINAL decision — never a grace-sampled false")
	require.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond,
		"the consult genuinely waited for the late decision (not a cached flag)")
	require.True(t, claim.isRevoked())
	require.True(t, fsutil.ReplacementBusyMarkerIsOurs(fs, dest, busyToken),
		"the marker still stands under the sweep's token — holds retained")

	require.False(t, reclaimAbandonedSweepBusyMarker(dest),
		"an in-flight/already-drained claim is never re-detached (reclaimStarted)")

	// The wedge self-releases; the ledger retains nothing for a later consult.
	claim.releaseAdmit()
	require.True(t, claim.abandonIfRevoked("backup removal", "backup", dest))
	claim.releaseDestLock()
	busyRelease()
	untrack()
	require.False(t, sweepClaimIsWedged(dest))
}

// Settled non-wedged leg: an admitted stage that completes WITHIN the grace
// drains normally; the consult never reports wedged and the ledger forgets
// the settled record (no stale decision for a later consult to trip over).
func TestSweepBusyClaimW64_SettledDrainLeavesNoRecord(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w64-settled", 0o755))
	dest := "/w64-settled/poster.jpg"
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, fs, dest, busyToken, busyRelease)
	defer untrack()
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)))
	cancel() // idle claim — no admitted stage; the drain admits immediately

	require.True(t, reclaimAbandonedSweepBusyMarker(dest), "an idle abandoned claim reclaims")
	require.Eventually(t, func() bool {
		e, _ := afero.Exists(fs, fsutil.ReplacementBusyPath(dest))
		return !e
	}, time.Second, time.Millisecond, "the drain frees the marker — the settled posture is unchanged")
	require.False(t, sweepClaimIsWedged(dest), "a settled non-wedged drain consults clean at once")

	key := sweepBusyClaims.resolver.Key(dest)
	sweepBusyClaims.mu.Lock()
	_, present := sweepBusyClaims.byDest[key]
	sweepBusyClaims.mu.Unlock()
	require.False(t, present, "the settled record is forgotten — nothing stale to await later")

	// A fresh claimant proceeds normally against the freed name.
	_, _, err = fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
}
