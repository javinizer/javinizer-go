package history

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING wave-8 (codex P2 follow-up on wave-7's bounded
// sweep): the 30s budget is enforced OUTSIDE the sweep call — a sweep stuck
// INSIDE afero.ReadDir (dead network filesystem) never observes its context,
// so the caller must stop waiting at the deadline, log the overrun, and
// proceed with the revert.

// TestRunPreRevertSweepW8_StuckSweepReleasesCallerAtBudget: the sweep stub
// deliberately never reads its context (a wedged ReadDir stand-in). The
// caller returns at the (shortened, restored-in-cleanup) budget, logs the
// overrun through the logger seam, and the abandoned goroutine unblocks
// cleanly once the "filesystem" finally answers.
func TestRunPreRevertSweepW8_StuckSweepReleasesCallerAtBudget(t *testing.T) {
	_ = swapSweepSeams(t) // restores the production seams in cleanup

	oldTimeout := preRevertSweepTimeout
	preRevertSweepTimeout = 50 * time.Millisecond
	t.Cleanup(func() { preRevertSweepTimeout = oldTimeout })

	unblock := make(chan struct{})
	unblocked := make(chan struct{})
	preRevertFindOps = func(context.Context, database.BatchFileOperationRepositoryInterface, string) ([]models.BatchFileOperation, error) {
		return []models.BatchFileOperation{{BatchJobID: "job-w8-stuck", OriginalPath: "/src/s.mkv"}}, nil
	}
	preRevertScopedSweep = func(ctx context.Context, _ afero.Fs, _ database.BatchFileOperationRepositoryInterface, _ []string) {
		<-unblock // wedged afero.ReadDir stand-in: deliberately never observes ctx
		close(unblocked)
	}
	preRevertFullSweep = func(context.Context, afero.Fs, database.BatchFileOperationRepositoryInterface) {
		t.Error("full sweep must not run when op resolution succeeds")
	}

	var logBuf bytes.Buffer
	restoreLog := logging.SetOutput(&logBuf)
	defer restoreLog()

	start := time.Now()
	runPreRevertReplacementSweep(context.Background(), nil, "job-w8-stuck")
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 50*time.Millisecond,
		"the caller waits out the (shortened) budget before giving up on the sweep")
	require.Less(t, elapsed, 250*time.Millisecond,
		"the caller stops waiting at the deadline (budget + ~200ms) instead of parking on the wedged sweep")

	logText := logBuf.String()
	require.Contains(t, logText, "sweep exceeded 50ms budget", "the deadline overrun is logged via the logger seam")
	require.Contains(t, logText, "continuing with revert")
	require.Contains(t, logText, "job-w8-stuck")

	// The abandoned goroutine is parked on the wedged sweep (the accepted leak
	// tradeoff) — unstick the "filesystem" and prove it drains cleanly rather
	// than deadlocking on the (buffered) done channel.
	close(unblock)
	select {
	case <-unblocked:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned sweep goroutine must unblock once the filesystem answers")
	}
}

// TestRunPreRevertSweepW8_HungFullSweepFallbackAlsoReleases: the deadline is
// enforced identically when op resolution failed and the all-roots fallback
// sweep is the one that wedges.
func TestRunPreRevertSweepW8_HungFullSweepFallbackAlsoReleases(t *testing.T) {
	_ = swapSweepSeams(t)

	oldTimeout := preRevertSweepTimeout
	preRevertSweepTimeout = 50 * time.Millisecond
	t.Cleanup(func() { preRevertSweepTimeout = oldTimeout })

	unblock := make(chan struct{})
	unblocked := make(chan struct{})
	sentinel := context.DeadlineExceeded // any resolution error routes to the full sweep
	preRevertFindOps = func(context.Context, database.BatchFileOperationRepositoryInterface, string) ([]models.BatchFileOperation, error) {
		return nil, sentinel
	}
	preRevertScopedSweep = func(context.Context, afero.Fs, database.BatchFileOperationRepositoryInterface, []string) {
		t.Error("scoped sweep must not run without a resolved op set")
	}
	preRevertFullSweep = func(ctx context.Context, _ afero.Fs, _ database.BatchFileOperationRepositoryInterface) {
		<-unblock
		close(unblocked)
	}

	var logBuf bytes.Buffer
	restoreLog := logging.SetOutput(&logBuf)
	defer restoreLog()

	start := time.Now()
	runPreRevertReplacementSweep(context.Background(), nil, "job-w8-stuck-full")
	elapsed := time.Since(start)

	require.Less(t, elapsed, 250*time.Millisecond, "the fallback sweep is deadline-bounded the same way")
	require.Contains(t, logBuf.String(), "sweep exceeded 50ms budget")

	close(unblock)
	select {
	case <-unblocked:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned fallback sweep goroutine must unblock once the filesystem answers")
	}
}
