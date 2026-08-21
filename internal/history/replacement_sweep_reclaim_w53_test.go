package history

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-53 (codex P1/P5, PR#215 findings 1+4) — the
// reclaim path now waits a BOUNDED worker-ack (~200ms) before freeing the
// stranded sweep's holds and detaches the releases to a goroutine, waiting a
// BOUNDED grace (~250ms) for them to land. Production keeps those bounds; the
// test binary shortens them (the worker is always parked in the choreography,
// so the ack wait times out regardless of the bound — the behavior under test
// is identical, only faster). A TestMain is the single seam that keeps every
// existing reclaim-exercising test fast without per-test churn, and the
// production binary is unaffected (it never compiles this file).
func TestMain(m *testing.M) {
	prevAckBound := sweepReclaimWorkerAckBound
	prevAckPoll := sweepReclaimWorkerAckPoll
	prevGrace := sweepReclaimReleaseGrace
	prevGracePoll := sweepReclaimReleasePoll
	sweepReclaimWorkerAckBound = 5 * time.Millisecond
	sweepReclaimWorkerAckPoll = time.Millisecond
	sweepReclaimReleaseGrace = 50 * time.Millisecond
	sweepReclaimReleasePoll = time.Millisecond
	defer func() {
		sweepReclaimWorkerAckBound = prevAckBound
		sweepReclaimWorkerAckPoll = prevAckPoll
		sweepReclaimReleaseGrace = prevGrace
		sweepReclaimReleasePoll = prevGracePoll
	}()
	os.Exit(m.Run())
}

// waitWorkerAck returns true once the stranded worker acknowledges the
// revocation through an abandonIfRevoked gate within the bound (wave-53,
// finding 1). The existing e2e tests park the worker inside an fs call so the
// wait times out; this covers the ack-in-time branch directly.
func TestSweepBusyClaimW53_WaitWorkerAckReturnsTrueOnAck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, "/w53-ack", func() {})
	defer untrack()
	go func() {
		time.Sleep(time.Millisecond)
		claim.revoke()
		claim.abandonIfRevoked("test gate", "backup", "/w53-ack") // the worker acks
	}()
	require.True(t, claim.waitWorkerAck(), "the worker acked within the bound")
	require.True(t, claim.workerAcked.Load() > 0)
}

// waitWorkerAck returns false on timeout when no worker acks (the bounded
// grace accepts the residual race over an unbounded reverter block).
func TestSweepBusyClaimW53_WaitWorkerAckTimesOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, "/w53-ack-timeout", func() {})
	defer untrack()
	require.False(t, claim.waitWorkerAck(), "no ack → bounded timeout returns false")
	require.Equal(t, int32(0), claim.workerAcked.Load())
}

// waitReleased returns false on timeout when releaseForReclaim never signals
// (a wedged marker take-aside outlasting the grace — wave-53, finding 4). The
// e2e tests always signal within the grace; this covers the timeout branch.
func TestSweepBusyClaimW53_WaitReleasedTimesOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, "/w53-rel-timeout", func() {})
	defer untrack()
	require.False(t, claim.waitReleased(), "no release signal → bounded grace timeout returns false")
	require.False(t, claim.released.Load())
}

// Wave-54 (codex P1, PR#215 finding 1): a worker wedged past the ack bound
// keeps the on-disk marker write-protective (nothing else acquires it); the
// reverter's consult reclaims the dest lock (in-process) but leaves the
// marker, then proceeds graced under the dest lock — the tombstone releases
// the acquirer's busy leg. The worker self-releases when it wakes and gates.
func TestReverterW54_GracedMarkerHeldRevertProceeds(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, _ := seedCrashWindow(t, base, repo, "job-w54", "GRD-001", "/w54", p3HexA)
	busyRelease, err := fsutil.AcquireReplacementBusy(base, dest)
	require.NoError(t, err)
	defer busyRelease() // the worker self-releases when it wakes and gates
	sweepCtx, cancel := context.WithCancel(context.Background())
	_, untrack := recordSweepBusyClaim(sweepCtx, dest, busyRelease)
	defer untrack()
	cancel() // the sweep's deadline fired; the worker is stranded (never acks)
	restored, rerr := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, rerr, "the reverter proceeds graced under the dest lock")
	require.True(t, restored[dest], "the restore completed despite the held marker")
	require.True(t, sweepBusyClaims.markerGraced(dest), "the marker is graced (held, not released)")
	exists, _ := afero.Exists(base, fsutil.ReplacementBusyPath(dest))
	require.True(t, exists, "the marker stays write-protective until the worker self-releases")
}
