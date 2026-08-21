package history

// POSTER-WRITE-HARDENING wave-57 (codex P1, PR#215 — "keep arbitration held
// when admission times out"): reclaim's bounded admit-wait false-result used to
// free BOTH holds anyway, so a revert could mutate while the sweep's syscall was
// in flight. Now releaseForReclaim retains both holds (terminal "wedged" state,
// revocation flag set) when the admit gate cannot be drained; the reverter
// consults sweepClaimIsWedged and skips that destination (busy-class) without
// overhanging the loop. The wedged claim self-releases when the fs unblocks —
// the worker's next attestation gate reads revoked and abandons, its deferred
// releases fire (once-guards), and a later retry succeeds. These tests pin the
// wedged posture end-to-end. A TestMain shortens the admit/release grace windows
// (the production binary is unaffected — it never compiles this file).

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// A wedged claim (admission timeout, dest lock owned) retains BOTH holds: the
// marker is NOT taken aside, the revocation flag IS set, and the claim stays
// wedged in the ledger so the reverter's consult skips it. A second consult on
// the already-wedged claim refuses (no re-detach). When the wedge unblocks the
// worker abandons at its next attestation gate and its deferred releases
// self-fire — a later retry finds nothing wedged.
func TestSweepBusyClaimW57_AdmissionTimeoutWedgesRetainsHolds(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w57-unit", 0o755))
	dest := "/w57-unit/poster.jpg"
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, fs, dest, busyToken, busyRelease)
	defer untrack()
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)),
		"the claim owns its dest lock — the wedge fires only for a bound claim")

	// Admit a mutation stage — the gate is held across it (the in-flight syscall
	// shape). The worker is parked mid-mutation.
	require.False(t, claim.abandonIfRevoked("destination publish", "backup", dest),
		"a live admitted claim proceeds past the gate")

	require.False(t, sweepClaimIsWedged(dest), "the claim is not yet wedged")
	cancel() // the sweep's deadline fired; the worker is stranded mid-mutation
	require.True(t, reclaimAbandonedSweepBusyMarker(dest),
		"the abandoned claim reclaims (revoke + detached release)")
	require.True(t, claim.isRevoked(), "the reclaim revoked the stranded claim")
	require.True(t, sweepClaimIsWedged(dest), "the claim is wedged (admission timeout)")

	// NO release fired: the marker still stands under this claim's token.
	require.True(t, fsutil.ReplacementBusyMarkerIsOurs(fs, dest, busyToken),
		"the marker was NOT taken aside — the wedged claim retains both holds")

	// A second consult on the already-wedged claim refuses (holds retained).
	require.False(t, reclaimAbandonedSweepBusyMarker(dest), "an already-wedged claim is not re-reclaimed")

	// The wedge unblocks: the admitted stage completes (releaseAdmit). The
	// worker's next attestation gate reads revoked and abandons without mutating.
	log := swapRevokeLog(t)
	claim.releaseAdmit()
	require.True(t, claim.abandonIfRevoked("backup removal", "backup", dest),
		"the worker abandons at the next gate — the revocation flag was set")
	require.Equal(t, []string{"backup removal"}, log.phases)

	// The wedged claim self-released: the worker's deferred releases fired
	// (once-guards — first callers), the marker is gone, and the claim left the
	// ledger — a later retry finds nothing wedged.
	claim.releaseDestLock()
	busyRelease()
	untrack()
	require.False(t, sweepClaimIsWedged(dest), "the wedged claim self-released")
	markerGone, _ := afero.Exists(fs, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.False(t, markerGone, "the marker is gone after self-release")
}

// End-to-end at the reverter: a wedged sweep claim makes restoreReplacementJournal
// skip THAT destination (busy-class, NOT overhanging the loop) and fail busy so
// the op stays Applied for a later retry. Once the wedge self-releases the later
// retry restores the destination normally.
func TestReverterW57_WedgedClaimSkipsDestinationAndLaterRetrySucceeds(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, _ := seedCrashWindow(t, base, repo, "job-w57", "W57-001", "/w57", p3HexA)

	// The stranded sweep shape: marker + dest lock both owned, ctx dead, and an
	// admitted mutation in flight (the gate held across it).
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(base, dest)
	require.NoError(t, err)
	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	claim, untrack := recordSweepBusyClaim(sweepCtx, base, dest, busyToken, busyRelease)
	defer untrack()
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)),
		"the sweep's pending dest-lock cell takes the release")
	require.False(t, claim.abandonIfRevoked("destination publish", dest+".dlbak."+p3HexA, dest),
		"a live admitted claim proceeds past the gate")
	sweepCancel() // the wave-8 deadline fired; the worker is stranded mid-mutation

	// The revert consults the ledger, finds the wedged claim, and skips the dest
	// (busy-class) — it does NOT block on the still-held dest lock nor mutate
	// concurrently with the in-flight sweep mutation. The op fails busy so it
	// stays Applied for a later retry.
	restored, rerr := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, rerr, fsutil.ErrReplacementBusy, "the wedged dest fails busy (busy-class)")
	require.ErrorContains(t, rerr, "wedged by an in-flight sweep mutation")
	require.False(t, restored[dest], "the wedged dest was skipped — not restored")
	require.True(t, claim.isRevoked(), "the reclaim revoked the stranded claim")
	require.True(t, fsutil.ReplacementBusyMarkerIsOurs(base, dest, busyToken),
		"NO release fired — the marker still stands under the sweep's token while the mutation is in flight")
	require.Len(t, requireLedgerReplacements(t, repo, op.ID), 1, "the journal entry stays armed for retry")

	// The wedge unblocks: the admitted stage completes, the worker abandons at
	// its next attestation gate, and its deferred releases self-fire (once-guards)
	// — the marker is gone and the dest lock is free.
	claim.releaseAdmit()
	require.True(t, claim.abandonIfRevoked("backup removal", dest+".dlbak."+p3HexA, dest),
		"the worker abandons at the next gate — no stale work lands")
	claim.releaseDestLock()
	busyRelease()
	untrack()
	require.False(t, sweepClaimIsWedged(dest), "the wedged claim self-released")

	// A LATER retry succeeds: the reverter acquires its own marker and restores
	// the destination, consuming the armed entry.
	restored, rerr = NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, rerr, "the later retry succeeds once the wedge self-releases")
	require.True(t, restored[dest], "the destination is restored on retry")
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID), "the journal entry is consumed")
	markerGone, _ := afero.Exists(base, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.False(t, markerGone, "the reverter released its own marker")
}
