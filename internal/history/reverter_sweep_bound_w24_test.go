package history

// POSTER-WRITE-HARDENING codex PR#215 wave-24 (P2, #discussion_r3808360868)
// — the reverter's pre-revert targeted sweep inherits the CLI wave-8 bounded
// discipline. The wave-8 pre-sweep ran bounded, but the immediately
// following RevertBatch/RevertScrape drove sweepJournaledDestinations with
// the caller's UNBOUNDED ctx: SweepDestinations observes its context only
// between directory scans, so one stalled network filesystem hung the CLI
// forever inside afero.ReadDir right after the bounded pre-sweep passed.
// The sweep now runs in a goroutine behind a derived deadline, exactly like
// the CLI pre-sweep: on the budget the caller stops waiting, logs the
// overrun, and proceeds with the revert.

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func w24SwapReverterSweep(t *testing.T, sweep func(context.Context, *ReplacementSweeper, []string) (int, error), timeout time.Duration) {
	t.Helper()
	// Route in-package seam swaps through the exported cross-package helper
	// (cmd/javinizer/commands/history uses it too) so its wiring is exercised
	// by this package's own instrumented test run.
	t.Cleanup(SwapReverterSweepForTest(sweep, timeout))
}

// The deadline leg: the sweep stub wedges like an unreachable network
// filesystem (never observes its context), the caller releases at the
// shortened budget, logs the overrun, and the abandoned goroutine drains
// cleanly once the "filesystem" finally answers.
func TestReverterSweepW24_StuckInnerSweepReleasesAtBudget(t *testing.T) {
	dest := "/out/W24-STUCK/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"

	entered := make(chan struct{})
	unblock := make(chan struct{})
	unblocked := make(chan struct{})
	var gotDests []string
	var gotDeadline time.Time
	var gotDeadlineOK bool
	// Wave-34 (finding F4): the substituted seam answers BOTH pre-sweep
	// invocations (destinations, then roots) — the closes stay idempotent
	// for the second call once the first one drained.
	var enteredOnce, unblockedOnce sync.Once
	w24SwapReverterSweep(t, func(ctx context.Context, _ *ReplacementSweeper, dests []string) (int, error) {
		gotDests = append([]string(nil), dests...)
		gotDeadline, gotDeadlineOK = ctx.Deadline()
		enteredOnce.Do(func() { close(entered) })
		<-unblock // wedged afero.ReadDir stand-in: deliberately never observes ctx
		unblockedOnce.Do(func() { close(unblocked) })
		return 0, nil
	}, 75*time.Millisecond)

	var logBuf bytes.Buffer
	restoreLog := logging.SetOutput(&logBuf)
	defer restoreLog()

	r := &Reverter{sweeper: &ReplacementSweeper{}}
	ops := []models.BatchFileOperation{{GeneratedFiles: covW2DJournal(t, dest, backup, 1)}}

	start := time.Now()
	r.sweepJournaledDestinations(context.Background(), ops)
	elapsed := time.Since(start)

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweep seam never engaged")
	}
	require.GreaterOrEqual(t, elapsed, 75*time.Millisecond,
		"the caller waits out the (shortened) budget before giving up on the sweep")
	require.Less(t, elapsed, 3*time.Second,
		"the caller stops waiting at the deadline instead of parking on the wedged sweep")

	require.Equal(t, []string{dest}, gotDests, "the seam receives the journaled destinations")
	require.True(t, gotDeadlineOK, "the sweep runs under a derived deadline")
	require.LessOrEqual(t, time.Until(gotDeadline), 75*time.Millisecond,
		"the derived deadline matches the sweep budget, not an unbounded parent")

	logText := logBuf.String()
	require.Contains(t, logText, "exceeded 75ms budget", "the deadline overrun is logged via the logger seam")
	require.Contains(t, logText, "continuing with revert")

	// The abandoned goroutine is parked on the wedged sweep (the accepted
	// leak tradeoff) — unstick the "filesystem" and prove it drains cleanly
	// rather than deadlocking on the (buffered) done channel.
	close(unblock)
	select {
	case <-unblocked:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned sweep goroutine must unblock once the filesystem answers")
	}
	require.Contains(t, logBuf.String(), "exceeded 75ms budget",
		"no late failure log overrides the deadline classification")
}

// The fast path is unchanged: a sweep that answers inside its budget still
// completes fully BEFORE the function returns — both the success and the
// best-effort failure classifications.
func TestReverterSweepW24_FastSweepStillCompletesSynchronously(t *testing.T) {
	dest := "/out/W24-FAST/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"
	ops := []models.BatchFileOperation{{GeneratedFiles: covW2DJournal(t, dest, backup, 1)}}
	r := &Reverter{sweeper: &ReplacementSweeper{}}

	t.Run("success", func(t *testing.T) {
		completed := false
		w24SwapReverterSweep(t, func(context.Context, *ReplacementSweeper, []string) (int, error) {
			completed = true
			return 7, nil
		}, 5*time.Second)

		var logBuf bytes.Buffer
		restoreLog := logging.SetOutput(&logBuf)
		defer restoreLog()

		r.sweepJournaledDestinations(context.Background(), ops)
		require.True(t, completed, "the sweep result is consumed before the caller proceeds")
		require.NotContains(t, logBuf.String(), "exceeded")
		require.NotContains(t, logBuf.String(), "sweep failed")
	})

	t.Run("sweeper failure stays best effort", func(t *testing.T) {
		sweepErr := errors.New("w24 sweep index unavailable")
		w24SwapReverterSweep(t, func(context.Context, *ReplacementSweeper, []string) (int, error) {
			return 0, sweepErr
		}, 5*time.Second)

		var logBuf bytes.Buffer
		restoreLog := logging.SetOutput(&logBuf)
		defer restoreLog()

		start := time.Now()
		r.sweepJournaledDestinations(context.Background(), ops)
		require.Less(t, time.Since(start), 5*time.Second, "a failing sweep returns at once, not at the budget")
		require.Contains(t, logBuf.String(), "pre-revert replacement sweep failed: w24 sweep index unavailable (continuing with revert)")
	})
}

// End-to-end at the reverter API: RevertBatch with a wedged destination
// filesystem proceeds within the budget and still processes the batch.
func TestReverterSweepW24_RevertBatchProceedsPastWedgedSweep(t *testing.T) {
	dest := "/out/W24-BATCH/poster.jpg"
	backup := dest + ".dlbak.0123456789abcdef"

	repo := newP3OpRepo()
	require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID:     "job-w24-wedged",
		MovieID:        "W24-001",
		OriginalPath:   "/src/w24/w24-001.mp4",
		NewPath:        "/out/W24-BATCH/w24-001/w24-001.mp4",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: covW2DJournal(t, dest, backup, 1),
	}))

	entered := make(chan struct{})
	unblock := make(chan struct{})
	unblocked := make(chan struct{})
	// Wave-34 (finding F4): the seam is invoked for the destinations AND the
	// roots sweep (the second call drains immediately once unblocked).
	var enteredOnce, unblockedOnce sync.Once
	w24SwapReverterSweep(t, func(context.Context, *ReplacementSweeper, []string) (int, error) {
		enteredOnce.Do(func() { close(entered) })
		<-unblock
		unblockedOnce.Do(func() { close(unblocked) })
		return 0, nil
	}, 75*time.Millisecond)

	r := NewReverter(afero.NewMemMapFs(), repo)
	start := time.Now()
	result, err := r.RevertBatch(context.Background(), "job-w24-wedged")
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.GreaterOrEqual(t, elapsed, 75*time.Millisecond,
		"the budget is observed before abandon")
	require.Less(t, elapsed, 3*time.Second,
		"the batch revert proceeds within the budget instead of hanging on the wedged sweep")
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the inner sweep must have engaged before the deadline released the revert")
	}

	close(unblock)
	select {
	case <-unblocked:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned sweep goroutine must drain once the filesystem answers")
	}
}
