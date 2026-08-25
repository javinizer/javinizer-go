package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type staleThenCommitRepoP6 struct {
	*mocks.MockJobRepositoryInterface
	calls  atomic.Int32
	finds  atomic.Int32
	latest atomic.Pointer[models.Job]
}

func (r *staleThenCommitRepoP6) CommitEnvelope(_ context.Context, job *models.Job, _ uint64) (uint64, error) {
	if r.calls.Add(1) == 1 {
		candidate := *job
		candidate.EnvelopeGeneration = 1
		r.latest.Store(&candidate)
		return 0, database.ErrStaleEnvelopeGeneration
	}
	job.EnvelopeGeneration = 2
	r.latest.Store(job)
	return 2, nil
}

func (r *staleThenCommitRepoP6) FindByID(_ context.Context, id string) (*models.Job, error) {
	if r.finds.Add(1) == 1 {
		return &models.Job{ID: id}, nil
	}
	if latest := r.latest.Load(); latest != nil {
		candidate := *latest
		return &candidate, nil
	}
	return &models.Job{ID: id, EnvelopeGeneration: 1}, nil
}

type initialPersistFindErrorRepoP6 struct {
	*mocks.MockJobRepositoryInterface
}

type initialPersistUpsertRepoP6 struct {
	*mocks.MockJobRepositoryInterface
}

func (r *initialPersistUpsertRepoP6) FindByID(_ context.Context, id string) (*models.Job, error) {
	return nil, database.ErrNotFound
}

func (r *initialPersistUpsertRepoP6) Upsert(_ context.Context, job *models.Job) error {
	job.EnvelopeGeneration = 5
	return nil
}

func (r *initialPersistUpsertRepoP6) CommitEnvelope(context.Context, *models.Job, uint64) (uint64, error) {
	return 0, errors.New("unexpected CommitEnvelope call")
}

func (r *initialPersistFindErrorRepoP6) FindByID(context.Context, string) (*models.Job, error) {
	return nil, errors.New("initial lookup failed")
}

func (r *initialPersistFindErrorRepoP6) CommitEnvelope(context.Context, *models.Job, uint64) (uint64, error) {
	return 0, database.ErrStaleEnvelopeGeneration
}

func TestSamePersistEnvelopeRejectsNilOrDifferentJob(t *testing.T) {
	require.False(t, samePersistEnvelope(nil, &models.Job{ID: "p6-envelope"}))
	require.False(t, samePersistEnvelope(&models.Job{ID: "p6-envelope"}, nil))
	require.False(t, samePersistEnvelope(&models.Job{ID: "p6-envelope"}, &models.Job{ID: "other"}))

	now := time.Now()
	a := &models.Job{ID: "p6-envelope", StartedAt: now, CompletedAt: &now, OrganizedAt: &now, RevertedAt: &now}
	b := *a
	b.StartedAt = now.Round(0)
	require.True(t, samePersistEnvelope(a, &b))
}

type lowerAcceptedGenerationRepoP6 struct {
	*mocks.MockJobRepositoryInterface
}

func (r *lowerAcceptedGenerationRepoP6) CommitEnvelope(_ context.Context, job *models.Job, _ uint64) (uint64, error) {
	job.PruneVersion = 6
	return 1, nil
}

func TestPersistToDatabase_DoesNotLowerLiveGeneration(t *testing.T) {
	repo := &lowerAcceptedGenerationRepoP6{MockJobRepositoryInterface: mocks.NewMockJobRepositoryInterface(t)}
	job := newBatchJob([]string{"/p6-monotonic-generation.mp4"})
	job.mu.Lock()
	job.envelopeGeneration = 5
	job.mu.Unlock()

	require.NoError(t, persistToDatabase(repo, job))
	job.mu.RLock()
	generation := job.envelopeGeneration
	pruneVersion := job.pruneVersion
	job.mu.RUnlock()
	require.Equal(t, uint64(5), generation)
	require.Equal(t, uint64(6), pruneVersion)
}

func TestPersistToDatabase_InitialUpsertPublishesAcceptedGeneration(t *testing.T) {
	repo := &initialPersistUpsertRepoP6{MockJobRepositoryInterface: mocks.NewMockJobRepositoryInterface(t)}
	job := newBatchJob([]string{"/p6-initial-upsert.mp4"})

	require.NoError(t, persistToDatabase(repo, job))
	job.mu.RLock()
	generation := job.envelopeGeneration
	job.mu.RUnlock()
	require.Equal(t, uint64(5), generation)
}

func TestPersistToDatabase_InitialLookupFailureSurfaces(t *testing.T) {
	repo := &initialPersistFindErrorRepoP6{MockJobRepositoryInterface: mocks.NewMockJobRepositoryInterface(t)}
	job := newBatchJob([]string{"/p6-initial-lookup-error.mp4"})
	require.ErrorContains(t, persistToDatabase(repo, job), "initial lookup failed")
}

type persistFindErrorRepoP6 struct {
	*mocks.MockJobRepositoryInterface
	findCalls atomic.Int32
}

func (r *persistFindErrorRepoP6) FindByID(_ context.Context, id string) (*models.Job, error) {
	if r.findCalls.Add(1) == 1 {
		return &models.Job{ID: id, EnvelopeGeneration: 0}, nil
	}
	return nil, errors.New("lookup failed")
}

func (r *persistFindErrorRepoP6) CommitEnvelope(context.Context, *models.Job, uint64) (uint64, error) {
	return 0, database.ErrStaleEnvelopeGeneration
}

type staleDifferentEnvelopeRepoP6 struct {
	*mocks.MockJobRepositoryInterface
	findCalls   atomic.Int32
	commitCalls atomic.Int32
}

func (r *staleDifferentEnvelopeRepoP6) FindByID(_ context.Context, id string) (*models.Job, error) {
	if r.findCalls.Add(1) == 1 {
		return &models.Job{ID: id}, nil
	}
	return &models.Job{ID: id, EnvelopeGeneration: 1, Results: "durable-newer-envelope"}, nil
}

func (r *staleDifferentEnvelopeRepoP6) CommitEnvelope(context.Context, *models.Job, uint64) (uint64, error) {
	r.commitCalls.Add(1)
	return 0, database.ErrStaleEnvelopeGeneration
}

func TestPersistToDatabase_StaleDifferentEnvelopeFailsClosed(t *testing.T) {
	repo := &staleDifferentEnvelopeRepoP6{MockJobRepositoryInterface: mocks.NewMockJobRepositoryInterface(t)}
	job := newBatchJob([]string{"/p6-stale-different-envelope.mp4"})
	err := persistToDatabase(repo, job)
	require.ErrorIs(t, err, database.ErrStaleEnvelopeGeneration)
	require.Equal(t, int32(2), repo.findCalls.Load())
	require.Equal(t, int32(1), repo.commitCalls.Load())
}

func TestPersistToDatabase_RefreshLookupFailureSurfaces(t *testing.T) {
	repo := &persistFindErrorRepoP6{MockJobRepositoryInterface: mocks.NewMockJobRepositoryInterface(t)}
	job := newBatchJob([]string{"/p6-lookup-error.mp4"})
	require.ErrorContains(t, persistToDatabase(repo, job), "lookup failed")
}

type staleNilLatestRepoP6 struct {
	*mocks.MockJobRepositoryInterface
	findCalls   atomic.Int32
	commitCalls atomic.Int32
}

func (r *staleNilLatestRepoP6) FindByID(_ context.Context, id string) (*models.Job, error) {
	if r.findCalls.Add(1) == 1 {
		return &models.Job{ID: id, EnvelopeGeneration: 0}, nil
	}
	return nil, nil
}

func (r *staleNilLatestRepoP6) CommitEnvelope(context.Context, *models.Job, uint64) (uint64, error) {
	r.commitCalls.Add(1)
	return 0, database.ErrStaleEnvelopeGeneration
}

func TestPersistToDatabase_StaleRefreshMissingLatestSurfacesNotFound(t *testing.T) {
	repo := &staleNilLatestRepoP6{MockJobRepositoryInterface: mocks.NewMockJobRepositoryInterface(t)}
	job := newBatchJob([]string{"/p6-refresh-missing.mp4"})
	require.ErrorIs(t, persistToDatabase(repo, job), database.ErrNotFound)
	require.Equal(t, int32(1), repo.commitCalls.Load())
}

func TestPersistToDatabase_StaleGenerationRefreshesOnce(t *testing.T) {
	repo := &staleThenCommitRepoP6{MockJobRepositoryInterface: mocks.NewMockJobRepositoryInterface(t)}
	job := newBatchJob([]string{"/p6-refresh.mp4"})
	job.ID = models.MustJobID("p6-refresh-job")

	require.NoError(t, persistToDatabase(repo, job))
	require.Equal(t, int32(2), repo.calls.Load())
	job.mu.RLock()
	generation := job.envelopeGeneration
	job.mu.RUnlock()
	require.Equal(t, uint64(2), generation)
}
