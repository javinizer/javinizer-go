package history

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
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
	repo := &revertStatusRepoP6{
		firstErr: database.ErrStaleEnvelopeGeneration,
		latest: &models.Job{
			ID:                 "p6-cli-revert-retry",
			Status:             models.JobStatusOrganized,
			EnvelopeGeneration: 7,
		},
	}
	job := &models.Job{ID: "p6-cli-revert-retry", Status: models.JobStatusReverted, RevertedAt: &revertedAt, EnvelopeGeneration: 6}

	require.NoError(t, persistRevertedStatus(context.Background(), repo, job))
	require.Equal(t, 2, repo.updateCalls)
	require.Equal(t, models.JobStatusReverted, job.Status)
	require.Equal(t, uint64(7), job.EnvelopeGeneration)
}

func TestPersistRevertedStatusBranches(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := &revertStatusRepoP6{}
		require.NoError(t, persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-cli-success"}))
	})
	t.Run("non-stale failure", func(t *testing.T) {
		errWant := errors.New("write failed")
		repo := &revertStatusRepoP6{firstErr: errWant}
		require.ErrorIs(t, persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-cli-error"}), errWant)
	})
	t.Run("reload failure", func(t *testing.T) {
		errWant := errors.New("reload failed")
		repo := &revertStatusRepoP6{firstErr: database.ErrStaleEnvelopeGeneration, findErr: errWant}
		require.ErrorIs(t, persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-cli-reload-error"}), errWant)
	})
	t.Run("missing reload", func(t *testing.T) {
		repo := &revertStatusRepoP6{firstErr: database.ErrStaleEnvelopeGeneration, returnNil: true}
		require.ErrorIs(t, persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-cli-missing"}), database.ErrNotFound)
	})
	t.Run("retry failure", func(t *testing.T) {
		errWant := errors.New("retry failed")
		repo := &revertStatusRepoP6{
			firstErr:  database.ErrStaleEnvelopeGeneration,
			secondErr: errWant,
			latest:    &models.Job{ID: "p6-cli-retry-error", EnvelopeGeneration: 2},
		}
		require.ErrorIs(t, persistRevertedStatus(context.Background(), repo, &models.Job{ID: "p6-cli-retry-error", EnvelopeGeneration: 1}), errWant)
	})
}
