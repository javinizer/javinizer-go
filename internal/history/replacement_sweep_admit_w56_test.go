package history

// POSTER-WRITE-HARDENING wave-56 (codex P1, PR#215 finding F1): the per-claim
// admit gate serializes reclaim against an in-flight admitted mutation.
// reclaim's releaseForReclaim polls TryLock on the claim's admitMu (bounded
// by sweepReclaimAdmitGrace) and frees the dest lock + takes the marker aside
// only AFTER the admitted stage completes (or the grace times out for a
// wedged stage) — so the reverter never mutates concurrently with an
// already-admitted sweep mutation. These tests pin both legs of the finding's
// coverage requirement.

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// reclaim during an admitted mutation stage WAITS for the stage to complete:
// releaseForReclaim is blocked on the admit gate (the marker is NOT taken
// aside and the dest lock is NOT freed while the stage is in flight), and it
// proceeds only once the stage calls releaseAdmit — the reverter then
// re-acquires the freed marker under its own token AFTER the stage, never
// concurrently. A long admit grace makes the wait observable rather than the
// timeout, and a short release grace keeps the reclaim's waitReleased fast.
func TestSweepBusyClaimW56_AdmitGateSerializesReclaimAgainstAdmittedStage(t *testing.T) {
	prevAdmit := sweepReclaimAdmitGrace
	prevRelease := sweepReclaimReleaseGrace
	sweepReclaimAdmitGrace = time.Second // the wait is observable, not the timeout
	sweepReclaimReleaseGrace = 15 * time.Millisecond
	t.Cleanup(func() {
		sweepReclaimAdmitGrace = prevAdmit
		sweepReclaimReleaseGrace = prevRelease
	})

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w56-admit", 0o755))
	dest := "/w56-admit/poster.jpg"
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, fs, dest, busyToken, busyRelease)
	defer untrack()
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)),
		"the sweep's pending dest-lock cell takes the release")

	// Admit a mutation stage — the gate is held across it.
	require.False(t, claim.abandonIfRevoked("destination publish", "backup", dest),
		"a live admitted claim proceeds past the gate")

	// Reclaim (the sweep's deadline fired mid-stage). The reclaim revokes and
	// detaches releaseForReclaim, but releaseForReclaim is BLOCKED on the admit
	// gate (the stage is in flight): the marker is NOT taken aside and the dest
	// lock is NOT freed. The reverter cannot proceed — no concurrent mutation.
	cancel()
	require.True(t, reclaimAbandonedSweepBusyMarker(dest),
		"the abandoned claim reclaims (revoke + detached release)")
	markerPath := filepath.ToSlash(fsutil.ReplacementBusyPath(dest))
	exists, _ := afero.Exists(fs, markerPath)
	require.True(t, exists,
		"reclaim waits for the admitted stage — the marker is still in place (releaseForReclaim blocked on the admit gate, dest lock still held)")

	// The admitted stage completes — releaseForReclaim proceeds: the dest lock
	// is freed (in-process, first) and the marker is taken aside. The reverter
	// proceeds AFTER the stage, never concurrently.
	claim.releaseAdmit()
	require.Eventually(t, func() bool {
		e, _ := afero.Exists(fs, markerPath)
		return !e
	}, time.Second, time.Millisecond,
		"the marker is taken aside once the admitted stage completes — the reverter proceeds after the stage")
}

// reclaim during idle (no admitted stage) proceeds without an admit-gate
// wait: the gate is free, releaseForReclaim's TryLock succeeds immediately, and
// the marker is taken aside at once — the same posture as before wave-56.
func TestSweepBusyClaimW56_AdmitGateIdleReclaimIsUnchanged(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w56-idle", 0o755))
	dest := "/w56-idle/poster.jpg"
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	defer busyRelease()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, fs, dest, busyToken, busyRelease)
	defer untrack()
	require.True(t, claim.bindDestLock(fsutil.SharedDestLocks().Acquire(dest)),
		"the sweep's pending dest-lock cell takes the release")
	// No admitted stage — the gate is free.
	cancel()
	start := time.Now()
	require.True(t, reclaimAbandonedSweepBusyMarker(dest), "an idle claim reclaims immediately")
	require.Less(t, time.Since(start), sweepReclaimAdmitGrace,
		"no admitted stage → no admit-gate wait (same as before wave-56)")
	markerPath := filepath.ToSlash(fsutil.ReplacementBusyPath(dest))
	exists, _ := afero.Exists(fs, markerPath)
	require.False(t, exists, "the marker was taken aside at once — the idle reclaim is unchanged")
}

// The admit-gate TryLock contention arm (wave-56 finding F1): a sweep stage
// resuming past its sweep's abandonment finds the admit gate held by reclaim's
// releaseForReclaim (it is polling TryLock on admitMu to free the dest lock
// and take the marker aside only after the admitted stage completes) and
// ABANDONS without mutating — reporting through the revocation-gate seam and
// leaving dest/journal/backup exactly as the pre-mutation state classified
// them. Production reaches this only via reclaim holding the gate for its
// release; this test pins the arm directly by holding the gate externally
// (the same posture reclaim presents) and calling the admission entry.
func TestSweepBusyClaimW56_AdmitGateTryLockContentionAbandons(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/w56-contention", 0o755))
	dest := "/w56-contention/poster.jpg"
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
	require.NoError(t, err)
	defer busyRelease()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, fs, dest, busyToken, busyRelease)
	defer untrack()

	log := swapRevokeLog(t)

	// Hold the admit gate externally — the posture reclaim's releaseForReclaim
	// presents while it waits to free the dest lock and take the marker aside.
	claim.admitMu.Lock()
	t.Cleanup(claim.admitMu.Unlock)

	require.True(t, claim.abandonIfRevoked("destination publish", "backup", dest),
		"a stage resuming against a gate held by reclaim abandons without mutating")
	require.False(t, claim.admitHeld,
		"the contention arm never sets admitHeld — no gate is held for the caller to release")
	require.False(t, claim.isRevoked(),
		"the contention arm is ownership-of-gate, not the revocation flag — the claim is otherwise live")
	require.Equal(t, []string{"destination publish"}, log.phases)
	require.Equal(t, []uint64{claim.epoch}, log.epochs)

	// No mutation surface was entered: the marker still stands under this
	// claim's token and the destination is byte-for-byte unchanged.
	require.True(t, fsutil.ReplacementBusyMarkerIsOurs(fs, dest, busyToken),
		"the marker is untouched — the stage abandoned before any attested mutation")
}
