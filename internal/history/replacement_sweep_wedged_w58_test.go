package history

// POSTER-WRITE-HARDENING wave-58 (codex P2, PR#215 — "do not resurrect a
// wedged claim after its worker untracks"): the wave-57 wedged-claim
// reinsertion re-added a record whose worker had already finished (its
// deferred untrack + holds released between the reclaim's ledger-unlock and
// the reinsertion's lock-reacquire), pinning the dest wedged until restart.
// The reinsertion is now CONDITIONAL: under the ledger lock it reinserts
// ONLY while the worker's `completed` flag is still clear — the worker's
// untrack flips that flag (same lock) before the delete, so a finished worker
// is never resurrected. Both race orderings converge: worker-finish-before-
// reinsert leaves nothing wedged (dest acquires normally); reinsert-before-
// finish keeps the wedged record until the worker's untrack removes it (the
// wave-57 test pins that leg).

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// reclaim wedges, then the worker finishes (untrack) BEFORE the reinsertion:
// `completed` is set under the ledger lock, so the wedged record is NOT
// resurrected — a later revert finds nothing wedged and acquires the dest
// normally (the worker's once-guarded releases freed the marker + lock).
func TestSweepBusyClaimW58_WorkerFinishBeforeReinsertNoResurrect(t *testing.T) {
	// Slow the release-poll so reclaim's waitReleased does not notice the wedge
	// until the worker has finished and untracked — deterministically exercising
	// the worker-finish-before-reinsert leg rather than the reverse-order race.
	prevPoll := sweepReclaimReleasePoll
	sweepReclaimReleasePoll = 100 * time.Millisecond
	t.Cleanup(func() { sweepReclaimReleasePoll = prevPoll })

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w58", 0o755))
	dest := "/w58/poster.jpg"
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, fs, dest, busyToken, busyRelease)
	defer untrack()
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)),
		"the claim owns its dest lock — the wedge fires only for a bound claim")
	require.False(t, claim.abandonIfRevoked("destination publish", "backup", dest),
		"a live admitted claim proceeds — the admit gate is held across the mutation")
	cancel() // the sweep's deadline fired; the worker is stranded mid-mutation

	reclaimDone := make(chan struct{})
	go func() {
		defer close(reclaimDone)
		reclaimAbandonedSweepBusyMarker(dest)
	}()
	// releaseForReclaim's admit-wait timed out → wedged; it has returned with
	// both holds retained. Complete the worker: its untrack flips `completed`
	// under the ledger lock, and the once-guarded holds self-release.
	require.Eventually(t, func() bool { return claim.wedged.Load() },
		500*time.Millisecond, time.Millisecond, "the wedge fires (admission timeout)")
	claim.releaseAdmit()
	require.True(t, claim.abandonIfRevoked("backup removal", "backup", dest),
		"the worker abandons at the next gate — the revocation flag was set")
	claim.releaseDestLock()
	busyRelease()
	untrack()
	<-reclaimDone // reclaim's reinsertion sees `completed` set → no resurrect

	require.False(t, sweepClaimIsWedged(dest),
		"the worker finished before reinsert — no wedged record resurrected")
	// The dest acquires normally: the worker's releases freed the marker.
	_, _, err = fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err, "the dest acquires normally — no wedged record pins it")
}
