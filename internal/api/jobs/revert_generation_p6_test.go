package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/require"
)

type revertStatusRepoP6 struct {
	database.JobRepositoryInterface
	latest      *models.Job
	firstErr    error
	findErr     error
	secondErr   error
	returnNil   bool
	updateCalls int
}

func (r *revertStatusRepoP6) Update(_ context.Context, job *models.Job) error {
	r.updateCalls++
	switch r.updateCalls {
	case 1:
		if r.firstErr != nil {
			return r.firstErr
		}
	case 2:
		if r.secondErr != nil {
			return r.secondErr
		}
	}
	if r.latest != nil {
		*r.latest = *job
	}
	return nil
}

func (r *revertStatusRepoP6) FindByID(_ context.Context, _ string) (*models.Job, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	if r.returnNil || r.latest == nil {
		return nil, nil
	}
	latest := *r.latest
	return &latest, nil
}

func TestPersistRevertedStatusRetriesStaleGeneration(t *testing.T) {
	revertedAt := time.Now().UTC()
	repo := &revertStatusRepoP6{latest: &models.Job{
		ID:                 "p6-revert-retry",
		Status:             models.JobStatusOrganized,
		EnvelopeGeneration: 7,
	}}
	// The initial loaded object is one generation behind the durable row.
	repo.firstErr = database.ErrStaleEnvelopeGeneration
	job := &models.Job{ID: "p6-revert-retry", Status: models.JobStatusReverted, RevertedAt: &revertedAt, EnvelopeGeneration: 6}

	require.NoError(t, persistRevertedStatus(context.Background(), repo, job))
	require.Equal(t, 2, repo.updateCalls)
	require.Equal(t, models.JobStatusReverted, job.Status)
	require.Equal(t, uint64(7), job.EnvelopeGeneration)
	require.Equal(t, &revertedAt, repo.latest.RevertedAt)
}

func TestPersistRevertedStatusBranches(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := &revertStatusRepoP6{}
		job := &models.Job{ID: "p6-revert-success"}
		require.NoError(t, persistRevertedStatus(context.Background(), repo, job))
		require.Equal(t, 1, repo.updateCalls)
	})
	t.Run("non-stale failure", func(t *testing.T) {
		errWant := errors.New("write failed")
		repo := &revertStatusRepoP6{firstErr: errWant}
		err := persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-revert-error"})
		require.ErrorIs(t, err, errWant)
		require.Equal(t, 1, repo.updateCalls)
	})
	t.Run("reload failure", func(t *testing.T) {
		errWant := errors.New("reload failed")
		repo := &revertStatusRepoP6{firstErr: database.ErrStaleEnvelopeGeneration, findErr: errWant}
		err := persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-revert-reload-error"})
		require.ErrorIs(t, err, errWant)
	})
	t.Run("missing reload", func(t *testing.T) {
		repo := &revertStatusRepoP6{firstErr: database.ErrStaleEnvelopeGeneration, returnNil: true}
		err := persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-revert-missing"})
		require.ErrorIs(t, err, database.ErrNotFound)
	})
	t.Run("retry failure", func(t *testing.T) {
		errWant := errors.New("retry failed")
		repo := &revertStatusRepoP6{
			firstErr:  database.ErrStaleEnvelopeGeneration,
			secondErr: errWant,
			latest:    &models.Job{ID: "p6-revert-retry-error", EnvelopeGeneration: 2},
		}
		err := persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-revert-retry-error", EnvelopeGeneration: 1})
		require.ErrorIs(t, err, errWant)
		require.Equal(t, 2, repo.updateCalls)
	})
}

func TestSyncLiveEnvelopeGenerationUsesOptionalStoreSeam(t *testing.T) {
	store := worker.NewInMemoryJobStore()
	job := store.CreateJobBatch(nil)

	syncLiveEnvelopeGeneration(store, job.ID.String(), 4)
	require.False(t, store.SyncEnvelopeGeneration(job.ID.String(), 3))
	syncLiveEnvelopeGeneration(store, "missing-p6-job", 5)
}
