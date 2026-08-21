package history

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-55 (codex P1, PR#215 finding 1 — full close):
// the wave-54 tombstone/grace design let the reverter proceed past a
// still-owned marker while a wedged worker could complete a mutation after
// the reverter started. The airtight fix makes the sweep's MUTATIONS
// ownership-conditional: every mutating stage gate (abandonIfRevoked)
// re-reads the on-disk marker and requires it still names THIS claim's
// token. The reclaim now takes the marker aside directly (no bounded
// worker-ack wait), so the reverter re-acquires the freed marker under its
// own token and never bypasses a still-owned one. The ack is subsumed as an
// optimization — no fixed deadline is needed once every mutating stage is
// attested. These tests pin the attestation gates and the reverter's
// reclaim-frees-marker posture. A TestMain shortens the release grace (the
// production binary is unaffected — it never compiles this file).
func TestMain(m *testing.M) {
	prevGrace := sweepReclaimReleaseGrace
	prevGracePoll := sweepReclaimReleasePoll
	prevAdmitGrace := sweepReclaimAdmitGrace
	prevAdmitPoll := sweepReclaimAdmitPoll
	sweepReclaimReleaseGrace = 50 * time.Millisecond
	sweepReclaimReleasePoll = time.Millisecond
	sweepReclaimAdmitGrace = 20 * time.Millisecond
	sweepReclaimAdmitPoll = time.Millisecond
	defer func() {
		sweepReclaimReleaseGrace = prevGrace
		sweepReclaimReleasePoll = prevGracePoll
		sweepReclaimAdmitGrace = prevAdmitGrace
		sweepReclaimAdmitPoll = prevAdmitPoll
	}()
	os.Exit(m.Run())
}

// waitReleased returns false on timeout when releaseForReclaim never signals
// (a wedged marker take-aside outlasting the grace — wave-53, finding 4). The
// e2e tests always signal within the grace; this covers the timeout branch.
func TestSweepBusyClaimW53_WaitReleasedTimesOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	claim, untrack := recordSweepBusyClaim(ctx, nil, "/w53-rel-timeout", "", func() {})
	defer untrack()
	require.False(t, claim.waitReleased(), "no release signal → bounded grace timeout returns false")
	require.False(t, claim.released.Load())
}

// Token-attestation gate (wave-55, finding 1): at every mutating stage the
// worker re-reads the marker it claimed and requires the on-disk bytes still
// name its token. A live claim with its own marker passes; a swapped (another
// claimant took over) or gone (reclaimed) marker abandons before mutating —
// even when the in-process revocation flag is NOT set.
func TestSweepBusyClaimW55_TokenAttestationGate(t *testing.T) {
	t.Run("own marker passes the attestation", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/w55-own", 0o755))
		dest := "/w55-own/poster.jpg"
		release, token, err := fsutil.AcquireReplacementBusyEx(fs, dest)
		require.NoError(t, err)
		defer release()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		claim, untrack := recordSweepBusyClaim(ctx, fs, dest, token, release)
		defer untrack()

		require.False(t, claim.isRevoked(), "the claim is live")
		log := swapRevokeLog(t)
		require.False(t, claim.abandonIfRevoked("test attestation", "backup", dest),
			"a live claim whose marker still names its token passes every gate")
		require.Empty(t, log.phases, "no gate fired — the attestation proved ownership")
	})

	t.Run("swapped marker abandons without revocation", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/w55-swap", 0o755))
		dest := "/w55-swap/poster.jpg"
		release, token, err := fsutil.AcquireReplacementBusyEx(fs, dest)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		claim, untrack := recordSweepBusyClaim(ctx, fs, dest, token, release)
		defer untrack()

		// Another claimant takes the name over (the reclaim took it aside and a
		// successor — the reverter — re-claimed it under a different token).
		release()
		successor, successorToken, err := fsutil.AcquireReplacementBusyEx(fs, dest)
		require.NoError(t, err)
		defer successor()

		require.False(t, claim.isRevoked(), "the revocation flag is NOT set — only the marker swapped")
		if runtime.GOOS == "windows" {
			// Windows FS semantics attenuate the swap: the platform's coarse
			// wall-clock resolution (and the open-handle delete/rename posture of
			// a real Windows FS) mean a successor cannot RELIABLY present a
			// different token — two acquisitions within one tick read equal, so the
			// successor shares the marker the claim still owns. The attestation
			// gate's token-equality leg is therefore vacuous on Windows: the
			// gate's verdict matches the on-disk ownership fact the platform can
			// express (same token ⇒ pass; the swapped-token abandon is a
			// posix-only contract the platform never presents).
			sameMarker := successorToken == token
			require.Equal(t, sameMarker, !claim.abandonIfRevoked("test attestation", "backup", dest),
				"windows: the gate's verdict matches the on-disk ownership fact — the swap is otherwise inexpressible")
			return
		}
		require.NotEqual(t, token, successorToken, "posix: the successor claimed a different token")
		log := swapRevokeLog(t)
		require.True(t, claim.abandonIfRevoked("test attestation", "backup", dest),
			"a swapped marker (different token) abandons even without revocation")
		require.Equal(t, []string{"test attestation"}, log.phases)
		require.Positive(t, log.epochs[0], "the gate reported the claim's ledger epoch")
	})

	t.Run("gone marker abandons without revocation", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/w55-gone", 0o755))
		dest := "/w55-gone/poster.jpg"
		release, token, err := fsutil.AcquireReplacementBusyEx(fs, dest)
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		claim, untrack := recordSweepBusyClaim(ctx, fs, dest, token, release)
		defer untrack()

		// The reclaim took the marker aside and no successor re-claimed it: the
		// name is gone, so the attestation read fails closed.
		release()
		require.False(t, fsutil.ReplacementBusyMarkerIsOurs(fs, dest, token), "the marker is gone")

		require.False(t, claim.isRevoked(), "the revocation flag is NOT set — only the marker is gone")
		require.True(t, claim.abandonIfRevoked("test attestation", "backup", dest),
			"a gone marker abandons even without revocation")
	})
}

// The bypass is gone (wave-55): a worker wedged past the deadline owns a
// marker the continued revert's reclaim takes aside directly. The reverter
// re-acquires the freed marker under its OWN token and proceeds — it never
// accepts a still-owned marker. A resuming worker reads a swapped/gone
// marker at its next attestation gate and abandons before mutating.
func TestReverterW55_AbandonedSweepMarkerReclaimedRevertAcquiresOwnToken(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, _ := seedCrashWindow(t, base, repo, "job-w55", "GRD-001", "/w55", p3HexA)
	busyRelease, busyToken, err := fsutil.AcquireReplacementBusyEx(base, dest)
	require.NoError(t, err)
	defer busyRelease() // the worker self-releases when it wakes and gates
	sweepCtx, cancel := context.WithCancel(context.Background())
	claim, untrack := recordSweepBusyClaim(sweepCtx, base, dest, busyToken, busyRelease)
	defer untrack()
	cancel() // the sweep's deadline fired; the worker is stranded mid-mutation

	workerToken := busyToken

	restored, rerr := NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, rerr, "the reclaim took the marker aside — the reverter acquires its own token and proceeds (no bypass)")
	require.True(t, restored[dest], "the restore completed")
	require.True(t, claim.isRevoked(), "the reclaim revoked the stranded claim")

	// The reverter acquired its own marker during the restore and released it
	// on return; no graced tombstone remains. A resuming worker's attestation
	// gate reads a gone marker and abandons.
	exists, _ := afero.Exists(base, fsutil.ReplacementBusyPath(dest))
	require.False(t, exists, "the reverter released its own marker — no graced tombstone remains")
	require.False(t, fsutil.ReplacementBusyMarkerIsOurs(base, dest, workerToken),
		"the worker's token no longer owns the name — a resuming worker abandons at its next attestation gate")
}
