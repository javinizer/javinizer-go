package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatedApplyPhase reproduces Run's real ordering: the terminal Mark* happens
// mid-body (closing lifecycle.done), while the deferred persistence executes
// only when Run returns. The release gate stands in for that deferred persist.
type gatedApplyPhase struct {
	marked  chan struct{} // closed right after the terminal Mark*
	release chan struct{} // test closes this to let Run (and its persist) finish
	exited  chan struct{} // closed when Run returns
}

func (g *gatedApplyPhase) Run(_ context.Context, inputs applyPhaseInputs, _ ApplyPhaseConfig) {
	defer close(g.exited)
	inputs.Lifecycle.MarkCompleted()
	close(g.marked)
	<-g.release
}

// TestWait_JoinsPhaseReturn_NotJustTerminalStatus pins the guarantee teardown
// drains rely on: Wait must not unblock at the terminal lifecycle mark —
// deferred persistence still runs after that point — but only once the phase
// goroutine has fully returned.
func TestWait_JoinsPhaseReturn_NotJustTerminalStatus(t *testing.T) {
	store := NewJobStore(nil, nil, nil, "", nil, nil)
	job := store.CreateJobBatch([]string{})
	job.Controller().SetWorkflow(&stubApplyWorkflow{})
	job.Controller().SetJobStatus(models.JobStatusCompleted)

	gated := &gatedApplyPhase{
		marked:  make(chan struct{}),
		release: make(chan struct{}),
		exited:  make(chan struct{}),
	}
	job.applyPhase = gated

	require.NoError(t, job.Controller().StartApply(context.Background(), ApplyPhaseConfig{}))

	// Terminal status is set now, but Run is still in its "persist" phase.
	<-gated.marked
	require.Equal(t, models.JobStatusCompleted, job.Lifecycle().GetJobStatus())

	waitReturned := make(chan error, 1)
	go func() { waitReturned <- job.Controller().Wait() }()

	select {
	case err := <-waitReturned:
		t.Fatalf("Wait returned (%v) while the phase's deferred persistence was still in flight — teardown would race the final DB writes", err)
	case <-time.After(100 * time.Millisecond):
		// Still blocked: correct.
	}

	close(gated.release)

	select {
	case err := <-waitReturned:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after the phase goroutine finished")
	}
	<-gated.exited
}

// TestWait_FallsBackToDoneWhenNoPhase covers jobs that reach a terminal state
// without ever starting a phase: phaseDone stays nil and Wait joins on the
// terminal-status channel instead.
func TestWait_FallsBackToDoneWhenNoPhase(t *testing.T) {
	store := NewJobStore(nil, nil, nil, "", nil, nil)
	job := store.CreateJobBatch([]string{})

	job.Lifecycle().Cancel() // Cancelled without any phase having started

	err := job.Controller().Wait()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

// TestStartApply_RejectsNonCompletedJob exercises markStarted's compare-and-swap
// failure path (wrong expected status), which the diff's error-return line needs
// for patch coverage.
func TestStartApply_RejectsNonCompletedJob(t *testing.T) {
	store := NewJobStore(nil, nil, nil, "", nil, nil)
	job := store.CreateJobBatch([]string{})
	job.Controller().SetWorkflow(&stubApplyWorkflow{})
	job.Controller().SetJobStatus(models.JobStatusRunning)

	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot start")
}

// TestStartScrape_RejectsNonPendingJob exercises markStarted's compare-and-swap
// failure from StartScrape (expected status Pending vs. actual), which the
// diff's StartScrape error-return line needs for patch coverage.
func TestStartScrape_RejectsNonPendingJob(t *testing.T) {
	store := NewJobStore(nil, nil, nil, "", nil, nil)
	job := store.CreateJobBatch([]string{})
	job.Controller().SetWorkflow(&integrationScrapeWF{})
	job.Controller().SetJobStatus(models.JobStatusRunning)

	err := job.Controller().StartScrape(context.Background(), []string{}, ScrapePhaseConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot start")
}
