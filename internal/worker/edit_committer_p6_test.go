package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type staleEnvelopeRepoP6 struct {
	*mocks.MockJobRepositoryInterface
	called bool
}

func (r *staleEnvelopeRepoP6) CommitEnvelope(context.Context, *models.Job, uint64) (uint64, error) {
	r.called = true
	return 0, database.ErrStaleEnvelopeGeneration
}

type acceptedEnvelopeRepoP6 struct {
	*mocks.MockJobRepositoryInterface
}

func (r *acceptedEnvelopeRepoP6) CommitEnvelope(_ context.Context, job *models.Job, expected uint64) (uint64, error) {
	accepted := expected + 1
	job.EnvelopeGeneration = accepted
	return accepted, nil
}

type editTxP6 struct {
	jobs database.JobRepositoryInterface
}

func (t editTxP6) WithEditTx(ctx context.Context, fn func(database.EditUnit) error) error {
	return fn(database.EditUnit{Jobs: t.jobs})
}

func TestEditCommitter_PublishFailureDoesNotAdvanceGeneration(t *testing.T) {
	repo := &acceptedEnvelopeRepoP6{MockJobRepositoryInterface: mocks.NewMockJobRepositoryInterface(t)}
	generationPublished := false
	committer := NewEditCommitter(editTxP6{jobs: repo}, newKeyedMutexRegistry(), "p6-publish-failure", nil)

	err := committer.Commit(context.Background(), &EditCommitPlan{
		EnvelopeFn: func() (*models.Job, error) {
			return &models.Job{ID: "p6-publish-failure", Status: models.JobStatusRunning}, nil
		},
		EnvelopeGenerationCommitted: func(uint64) { generationPublished = true },
		Publish:                     func() error { return errors.New("publication failed") },
	})

	require.ErrorContains(t, err, "publication failed")
	require.False(t, generationPublished)
}

func TestEditCommitter_StaleEnvelopeGenerationRollsBackPublication(t *testing.T) {
	base := mocks.NewMockJobRepositoryInterface(t)
	repo := &staleEnvelopeRepoP6{MockJobRepositoryInterface: base}
	published := false

	committer := NewEditCommitter(editTxP6{jobs: repo}, newKeyedMutexRegistry(), "p6-stale-edit", nil)
	err := committer.Commit(context.Background(), &EditCommitPlan{
		EnvelopeFn: func() (*models.Job, error) {
			return &models.Job{ID: "p6-stale-edit", Status: models.JobStatusRunning}, nil
		},
		Publish: func() error {
			published = true
			return nil
		},
	})

	require.ErrorIs(t, err, database.ErrStaleEnvelopeGeneration)
	require.True(t, repo.called, "the edit transaction must use the generation-aware envelope seam")
	require.False(t, published, "a stale candidate must never publish in memory")
}
