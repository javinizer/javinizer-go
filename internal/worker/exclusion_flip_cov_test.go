package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// Legacy (no committer) arm: publish marks everything excluded, cancelIfAll
// runs Cancel() on any Pending/Running job — the all-excluded auto-cancel
// contract the review pane's bulk-exclude relies on (codex r31).
func TestExcludeFamilyLegacyCancelsWhenAllExcluded(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "FC-L1", "")
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	pe := NewPosterEditor(store, store, nil)
	pe.attachEnv(&posterEditEnv{lifecycle: lc, persistFn: func() error { return nil }})
	m := &LockedMovieOps{pe: pe, movieID: "FC-L1"}
	require.NoError(t, m.ExcludeFamily(context.Background()))
	assert.Equal(t, models.JobStatusCancelled, lc.GetJobStatus())
}

// commit-leg tx failure: the pre-commit flipped Cancelled marker must roll
// back with the transaction so the in-memory job remains its original state.
func TestExcludeFamilyCommitterTxnFailureRestoresFlip(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-x", "FC-T1", "")
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-FCX", newKeyedMutexRegistry())
	pe := NewPosterEditor(store, store, nil)
	pe.attachEnv(&posterEditEnv{
		committer: committer,
		envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
			return &models.Job{}, nil
		},
		lifecycle: lc,
	})
	m := &LockedMovieOps{pe: pe, movieID: "FC-T1"}
	require.ErrorContains(t, m.ExcludeFamily(context.Background()), "tx wedged")
	assert.Equal(t, models.JobStatusRunning, lc.GetJobStatus(), "tx failure rolls the pre-commit cancellation back")
}

// The flip helper must not fire when there are no results to speak of.
func TestExcludeFamilyFlipEmptySnapshotSkips(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"}) // no results registered
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	assert.Nil(t, flipPendingCancelIfTerminal(lc, map[string]bool{"/f/a.mp4": true}, store))
}
