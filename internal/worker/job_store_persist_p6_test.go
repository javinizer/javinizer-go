package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type generationPersistRepoP6 struct {
	*mockJobRepoForPersist
	expected atomic.Uint64
	accepted atomic.Uint64
}

func (r *generationPersistRepoP6) CommitEnvelope(_ context.Context, job *models.Job, expected uint64) (uint64, error) {
	r.expected.Store(expected)
	accepted := expected + 1
	job.EnvelopeGeneration = accepted
	r.accepted.Store(accepted)
	return accepted, nil
}

func TestJobStore_PersistFlightForInitializesDirectStore(t *testing.T) {
	store := &JobStore{}
	first := store.persistFlightFor(models.MustJobID("p6-flight-map"))
	require.NotNil(t, first)
	require.Same(t, first, store.persistFlightFor(models.MustJobID("p6-flight-map")))

	job := &BatchJob{ID: models.MustJobID("p6-bind-map")}
	bound := store.persistFlightForJob(job)
	store.persistFlightsMu.Lock()
	store.persistFlights = nil
	store.persistFlightsMu.Unlock()
	store.bindPersistFlight(job)
	store.persistFlightsMu.Lock()
	require.Same(t, bound, store.persistFlights[job.ID])
	store.persistFlightsMu.Unlock()
}

func TestDeleteJob_ExistingExclusiveFlightReturnsBusy(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/p6/delete.mp4", "P6-DELETE")
	flight := store.persistFlightFor(job.ID)
	release, err := flight.acquireExclusive(context.Background())
	require.NoError(t, err)
	defer release()

	require.ErrorIs(t, store.DeleteJob(job.ID.String()), ErrJobBusy)
}

func TestJobStore_SyncEnvelopeGenerationNeverLowers(t *testing.T) {
	store := NewInMemoryJobStore()
	job := store.CreateJobBatch(nil)

	require.True(t, store.SyncEnvelopeGeneration(job.ID.String(), 4))
	require.False(t, store.SyncEnvelopeGeneration(job.ID.String(), 3))
	require.False(t, store.SyncEnvelopeGeneration("missing-p6-job", 5))
}

func TestPersistFlight_IsolatedAcrossIDReuse(t *testing.T) {
	store := &JobStore{}
	oldJob := &BatchJob{ID: models.MustJobID("p6-reused-id")}
	oldFlight := store.persistFlightForJob(oldJob)
	_, err := oldFlight.acquireExclusive(context.Background())
	require.NoError(t, err)
	oldFlight.sealExclusive(ErrJobGone)
	store.resetPersistFlight(oldJob.ID)

	newJob := &BatchJob{ID: oldJob.ID}
	newFlight := store.persistFlightForJob(newJob)
	require.NotSame(t, oldFlight, newFlight)
	require.ErrorIs(t, oldFlight.do(context.Background(), func() error { return nil }), ErrJobGone)
	require.NoError(t, newFlight.do(context.Background(), func() error { return nil }))
}

func TestDeleteJob_EvictsSealedPersistFlight(t *testing.T) {
	store := NewInMemoryJobStore()
	job := seedOneMovie(t, store, "/p6/evict.mp4", "P6-EVICT")
	require.NoError(t, store.DeleteJob(job.ID.String()))

	store.persistFlightsMu.Lock()
	_, retained := store.persistFlights[job.ID]
	store.persistFlightsMu.Unlock()
	require.False(t, retained)
	require.ErrorIs(t, store.PersistJob(job), ErrJobGone)
}

func TestReconstructBatchJob_EnvelopeGenerationSurvivesRestart(t *testing.T) {
	repo := &generationPersistRepoP6{mockJobRepoForPersist: &mockJobRepoForPersist{}}
	store := NewJobStore(repo, nil, nil, t.TempDir(), nil, nil)

	dbJob := &models.Job{
		ID:                 "p6-reconstructed-generation",
		EnvelopeGeneration: 12,
		Status:             models.JobStatusCompleted,
		TotalFiles:         1,
		Completed:          1,
		Progress:           100,
		Files:              `["/p6/restart.mp4"]`,
		Results:            `{"domain":{}}`,
		StartedAt:          time.Now().UTC(),
	}
	reconstructed := store.reconstructBatchJob(dbJob)
	require.Equal(t, uint64(12), reconstructed.envelopeGeneration)
	require.NoError(t, store.PersistJob(reconstructed))
	require.Equal(t, uint64(12), repo.expected.Load())
	require.Equal(t, uint64(13), repo.accepted.Load())
	require.Equal(t, uint64(13), reconstructed.envelopeGeneration)
}

func TestReconstructBatchJob_LegacyEnvelopeGenerationStartsAtZero(t *testing.T) {
	repo := &generationPersistRepoP6{mockJobRepoForPersist: &mockJobRepoForPersist{}}
	store := NewJobStore(repo, nil, nil, t.TempDir(), nil, nil)
	dbJob := &models.Job{
		ID:         "p6-legacy-generation",
		Status:     models.JobStatusCompleted,
		TotalFiles: 1,
		Completed:  1,
		Progress:   100,
		Files:      `["/p6/legacy.mp4"]`,
		Results:    `{"domain":{}}`,
		StartedAt:  time.Now().UTC(),
	}
	reconstructed := store.reconstructBatchJob(dbJob)
	require.Zero(t, reconstructed.envelopeGeneration)
	require.NoError(t, store.PersistJob(reconstructed))
	require.Equal(t, uint64(0), repo.expected.Load())
	require.Equal(t, uint64(1), repo.accepted.Load())
}
