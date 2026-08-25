package database

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedEnvelopeGenerationJobP6(t *testing.T, repo *JobRepository, id string) *models.Job {
	t.Helper()
	job := &models.Job{
		ID:        id,
		Status:    models.JobStatusRunning,
		Files:     "[]",
		Results:   "initial",
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(context.Background(), job))
	return job
}

func TestJobRepository_CommitEnvelope_GenerationCAS(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	job := seedEnvelopeGenerationJobP6(t, repo, "p6-generation-cas")

	candidate := *job
	candidate.Results = "generation-one"
	accepted, err := repo.CommitEnvelope(context.Background(), &candidate, 0)
	require.NoError(t, err)
	require.Equal(t, uint64(1), accepted)
	require.Equal(t, uint64(1), candidate.EnvelopeGeneration)

	loaded, err := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), loaded.EnvelopeGeneration)
	require.Equal(t, "generation-one", loaded.Results)

	stale := *loaded
	stale.Results = "stale-writer"
	_, err = repo.CommitEnvelope(context.Background(), &stale, 0)
	require.ErrorIs(t, err, ErrStaleEnvelopeGeneration)

	future := *loaded
	future.Results = "future-writer"
	_, err = repo.CommitEnvelope(context.Background(), &future, 99)
	require.ErrorIs(t, err, ErrStaleEnvelopeGeneration)

	unchanged, err := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), unchanged.EnvelopeGeneration)
	require.Equal(t, "generation-one", unchanged.Results)
}

func TestJobRepository_VersionedSaveRejectsStaleEnvelope(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	job := seedEnvelopeGenerationJobP6(t, repo, "p6-versioned-save-stale")
	preserveDurableEnvelopeColumns(&models.Job{}, &models.Job{})
	applyPlan := `{"phase":"apply"}`
	now := time.Now().UTC()
	job.ApplyPlan = &applyPlan
	job.CompletedAt = &now
	job.OrganizedAt = &now
	job.RevertedAt = &now
	require.NoError(t, db.DB.Model(&models.Job{}).Where("id = ?", job.ID).Updates(map[string]any{
		"apply_plan":   applyPlan,
		"completed_at": now,
		"organized_at": now,
		"reverted_at":  now,
	}).Error)

	accepted := *job
	accepted.Results = "accepted-envelope"
	_, err := repo.CommitEnvelope(context.Background(), &accepted, 0)
	require.NoError(t, err)

	staleUpdate := *job
	staleUpdate.Status = models.JobStatusReverted
	staleUpdate.Completed = 99
	staleUpdate.Results = "stale-update"
	require.ErrorIs(t, repo.Update(context.Background(), &staleUpdate), ErrStaleEnvelopeGeneration)

	staleUpsert := *job
	staleUpsert.Status = models.JobStatusRunning
	staleUpsert.Results = "stale-upsert"
	require.ErrorIs(t, repo.Upsert(context.Background(), &staleUpsert), ErrStaleEnvelopeGeneration)

	loaded, err := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), loaded.EnvelopeGeneration)
	require.Equal(t, "accepted-envelope", loaded.Results)
	require.Equal(t, models.JobStatusRunning, loaded.Status)
	require.Zero(t, loaded.Completed)

	validUpdate := *loaded
	validUpdate.Status = models.JobStatusReverted
	validUpdate.Completed = 7
	validUpdate.Results = "must-be-ignored"
	require.NoError(t, repo.Update(context.Background(), &validUpdate))
	loaded, err = repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(2), loaded.EnvelopeGeneration)
	require.Equal(t, "accepted-envelope", loaded.Results)
	require.Equal(t, models.JobStatusReverted, loaded.Status)
	require.Equal(t, 7, loaded.Completed)

	sameGeneration := *loaded
	sameGeneration.Status = models.JobStatusRunning
	sameGeneration.Results = "same-generation-stale"
	require.NoError(t, repo.Upsert(context.Background(), &sameGeneration))
	loaded, err = repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(3), loaded.EnvelopeGeneration)
	require.Equal(t, models.JobStatusRunning, loaded.Status)
	require.Equal(t, "accepted-envelope", loaded.Results)
}

func TestJobRepository_VersionedSaveFailureDoesNotPublishGeneration(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	job := seedEnvelopeGenerationJobP6(t, repo, "p6-versioned-save-failure")
	updates := 0
	const callbackName = "test:cancel_versioned_save"
	require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "jobs" {
			updates++
			if updates == 2 {
				_ = tx.AddError(context.Canceled)
			}
		}
	}))
	defer func() { _ = db.DB.Callback().Update().Remove(callbackName) }()

	candidate := *job
	candidate.Status = models.JobStatusCompleted
	err := repo.Update(context.Background(), &candidate)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "save failure should preserve cancellation: %v", err)
	require.Zero(t, candidate.EnvelopeGeneration)
	loaded, findErr := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, findErr)
	require.Zero(t, loaded.EnvelopeGeneration)
	require.Equal(t, models.JobStatusRunning, loaded.Status)
}

func TestJobRepository_CommitEnvelope_SaveFailureDoesNotPublishGeneration(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	job := seedEnvelopeGenerationJobP6(t, repo, "p6-commit-save-failure")
	updates := 0
	const callbackName = "test:cancel_commit_save"
	require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "jobs" {
			updates++
			if updates == 2 {
				_ = tx.AddError(context.Canceled)
			}
		}
	}))
	defer func() { _ = db.DB.Callback().Update().Remove(callbackName) }()

	candidate := *job
	_, err := repo.CommitEnvelope(context.Background(), &candidate, 0)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled), "save failure should preserve cancellation: %v", err)
	require.Zero(t, candidate.EnvelopeGeneration)
	require.Zero(t, candidate.PruneVersion)
	loaded, findErr := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, findErr)
	require.Zero(t, loaded.EnvelopeGeneration)
}

func TestJobRepository_VersionedSaveMissingRowCreateFailure(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	const callbackName = "test:versioned_save_create_failure"
	require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "jobs" {
			_ = tx.AddError(context.Canceled)
		}
	}))
	defer func() { _ = db.DB.Callback().Create().Remove(callbackName) }()

	err := repo.Update(context.Background(), &models.Job{ID: "p6-versioned-save-create-failure", Status: models.JobStatusRunning})
	require.ErrorIs(t, err, context.Canceled)
}

func TestJobRepository_CommitEnvelope_ErrorBranches(t *testing.T) {
	t.Run("nil job", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		_, err := NewJobRepository(db).CommitEnvelope(context.Background(), nil, 0)
		require.Error(t, err)
	})

	t.Run("update failure", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewJobRepository(db)
		job := seedEnvelopeGenerationJobP6(t, repo, "p6-generation-update-error")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := repo.CommitEnvelope(ctx, job, 0)
		require.Error(t, err)
	})
	t.Run("versioned status lookup failure", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewJobRepository(db)
		job := seedEnvelopeGenerationJobP6(t, repo, "p6-versioned-status-error")
		ctx, cancel := context.WithCancel(context.Background())
		const callbackName = "test:cancel_versioned_status_lookup"
		require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
			cancel()
		}))
		defer func() { _ = db.DB.Callback().Query().Remove(callbackName) }()
		candidate := *job
		candidate.EnvelopeGeneration = 99
		err := repo.Update(ctx, &candidate)
		require.Error(t, err)
		require.True(t, errors.Is(err, context.Canceled), "status lookup failure should preserve cancellation: %v", err)
	})
	t.Run("update result error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewJobRepository(db)
		job := seedEnvelopeGenerationJobP6(t, repo, "p6-generation-result-error")
		require.NoError(t, db.DB.Exec("DROP TABLE jobs").Error)
		_, err := repo.CommitEnvelope(context.Background(), job, 0)
		require.Error(t, err)
	})

	cancelAfterUpdate := func(t *testing.T, name string, expected uint64) error {
		t.Helper()
		db := newDatabaseTestDB(t)
		repo := NewJobRepository(db)
		job := seedEnvelopeGenerationJobP6(t, repo, name)
		ctx, cancel := context.WithCancel(context.Background())
		fired := false
		require.NoError(t, db.DB.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
			if !fired && tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "jobs" {
				fired = true
				cancel()
			}
		}))
		defer func() { _ = db.DB.Callback().Update().Remove(name) }()
		candidate := *job
		candidate.Results = name
		_, err := repo.CommitEnvelope(ctx, &candidate, expected)
		return err
	}

	t.Run("status lookup failure", func(t *testing.T) {
		err := cancelAfterUpdate(t, "p6-generation-status-error", 99)
		require.Error(t, err)
		require.True(t, errors.Is(err, context.Canceled), "status lookup failure should preserve cancellation: %v", err)
	})
	t.Run("missing row", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewJobRepository(db)
		job := seedEnvelopeGenerationJobP6(t, repo, "p6-generation-missing")
		require.NoError(t, repo.Delete(context.Background(), job.ID))
		_, err := repo.CommitEnvelope(context.Background(), job, 99)
		require.ErrorIs(t, err, ErrNotFound)
	})
	t.Run("generation scan failure", func(t *testing.T) {
		err := cancelAfterUpdate(t, "p6-generation-scan-error", 0)
		require.Error(t, err)
		require.True(t, errors.Is(err, context.Canceled), "generation scan failure should preserve cancellation: %v", err)
	})
}

func TestJobRepository_CommitEnvelope_RespectsPruneFence(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	job := seedEnvelopeGenerationJobP6(t, repo, "p6-generation-pruning")
	require.NoError(t, db.DB.Model(&models.Job{}).Where("id = ?", job.ID).Update("status", "pruning").Error)

	candidate := *job
	candidate.Results = "must-not-land-during-prune"
	_, err := repo.CommitEnvelope(context.Background(), &candidate, 0)
	require.ErrorIs(t, err, ErrJobPruning)

	loaded, findErr := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, findErr)
	require.Equal(t, models.JobStatus("pruning"), loaded.Status)
	require.Zero(t, loaded.EnvelopeGeneration)
	require.Equal(t, "initial", loaded.Results)
}

func TestJobRepository_CommitEnvelope_FailedTransactionDoesNotAdvanceGeneration(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	job := seedEnvelopeGenerationJobP6(t, repo, "p6-generation-cancel")

	candidate := *job
	candidate.Results = "must-not-land"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repo.CommitEnvelope(ctx, &candidate, 0)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrStaleEnvelopeGeneration))

	loaded, findErr := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, findErr)
	require.Zero(t, loaded.EnvelopeGeneration)
	require.Equal(t, "initial", loaded.Results)
}

func TestJobRepository_CommitEnvelope_ConcurrentSameBaseHasOneWinner(t *testing.T) {
	db := newDatabaseTestDB(t)
	repoA := NewJobRepository(db)
	repoB := NewJobRepository(db)
	job := seedEnvelopeGenerationJobP6(t, repoA, "p6-generation-race")

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, repo := range []*JobRepository{repoA, repoB} {
		wg.Add(1)
		go func(i int, repo *JobRepository) {
			defer wg.Done()
			<-start
			candidate := *job
			candidate.Results = "winner-" + string(rune('a'+i))
			_, err := repo.CommitEnvelope(context.Background(), &candidate, 0)
			results <- err
		}(i, repo)
	}
	close(start)
	wg.Wait()
	close(results)

	var accepted, stale int
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrStaleEnvelopeGeneration):
			stale++
		default:
			t.Fatalf("unexpected concurrent commit error: %v", err)
		}
	}
	require.Equal(t, 1, accepted)
	require.Equal(t, 1, stale)
	loaded, err := repoA.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), loaded.EnvelopeGeneration)
}
