package database

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestJobRepository_UpdateUpsert_Nil(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	require.Error(t, repo.Update(context.Background(), nil))
	require.Error(t, repo.Upsert(context.Background(), nil))
}

func TestJobRepository_UpsertVersioned_PluckFailure(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	job := &models.Job{ID: "versioned-pluck-failure", Status: models.JobStatusOrganized, Files: "[]", StartedAt: time.Now().UTC()}
	require.NoError(t, repo.Create(context.Background(), job))
	ctx, cancel := context.WithCancel(context.Background())
	fired := false
	const callbackName = "test:cancel_job_prune_pluck"
	require.NoError(t, db.DB.Callback().Update().After("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if !fired && tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "jobs" {
			fired = true
			cancel()
		}
	}))
	defer func() { _ = db.DB.Callback().Update().Remove(callbackName) }()

	loaded, err := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	err = repo.Upsert(ctx, loaded)
	require.Error(t, err)
}

func TestJobRepository_UpsertVersionedNewAndExisting(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	job := &models.Job{ID: "versioned-upsert", Status: models.JobStatusOrganized, Files: "[]", StartedAt: time.Now().UTC()}
	require.NoError(t, repo.Upsert(context.Background(), job))
	require.Zero(t, job.PruneVersion)
	loaded, err := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	require.NoError(t, repo.Upsert(context.Background(), loaded))
	require.Equal(t, uint64(1), loaded.PruneVersion)
}

func TestJobRepository_Create(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{
		ID:         "test-job-1",
		Status:     models.JobStatusRunning,
		TotalFiles: 10,
		Completed:  5,
		Failed:     0,
		Progress:   50.0,
		Files:      `["file1.mp4","file2.mp4"]`,
		StartedAt:  time.Now(),
	}

	err := repo.Create(context.TODO(), job)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "test-job-1")
	require.NoError(t, err)
	assert.Equal(t, "test-job-1", found.ID)
	assert.Equal(t, 10, found.TotalFiles)
}

func TestJobRepository_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	now := time.Now()
	jobs := []*models.Job{
		{ID: "job-1", Status: models.JobStatusRunning, TotalFiles: 5, Files: "[]", StartedAt: now.Add(-1 * time.Hour)},
		{ID: "job-2", Status: models.JobStatusCompleted, TotalFiles: 3, Files: "[]", StartedAt: now},
	}

	for _, j := range jobs {
		require.NoError(t, repo.Create(context.TODO(), j))
	}

	list, err := repo.List(context.TODO())
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "job-2", list[0].ID)
}

func TestJobRepository_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{
		ID:         "to-delete",
		Status:     models.JobStatusCompleted,
		TotalFiles: 1,
		Files:      "[]",
		StartedAt:  time.Now(),
	}
	require.NoError(t, repo.Create(context.TODO(), job))

	err := repo.Delete(context.TODO(), "to-delete")
	require.NoError(t, err)

	_, err = repo.FindByID(context.TODO(), "to-delete")
	assert.Error(t, err)
}

func TestJobRepository_DeleteOrganizedOlderThan(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	now := time.Now()
	twoDaysAgo := now.Add(-48 * time.Hour)

	organizedOld := &models.Job{
		ID:          "organized-old",
		Status:      models.JobStatusOrganized,
		TotalFiles:  1,
		Files:       "[]",
		StartedAt:   twoDaysAgo.Add(-1 * time.Hour),
		OrganizedAt: &twoDaysAgo,
	}
	organizedRecent := &models.Job{
		ID:          "organized-recent",
		Status:      models.JobStatusOrganized,
		TotalFiles:  1,
		Files:       "[]",
		StartedAt:   now.Add(-1 * time.Hour),
		OrganizedAt: ptrTime(now.Add(-12 * time.Hour)),
	}

	require.NoError(t, repo.Create(context.TODO(), organizedOld))
	require.NoError(t, repo.Create(context.TODO(), organizedRecent))

	err := repo.DeleteOrganizedOlderThan(context.TODO(), now.Add(-24*time.Hour))
	require.NoError(t, err)

	list, err := repo.List(context.TODO())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "organized-recent", list[0].ID)
}

func TestJobRepository_DeleteOrganizedOlderThan_PrunesOperationsBeforeJobs(t *testing.T) {
	db := newDatabaseTestDB(t)
	jobRepo := NewJobRepository(db)
	opRepo := NewBatchFileOperationRepository(db)
	now := time.Now().UTC()
	oldAt := now.Add(-48 * time.Hour)

	oldJob := &models.Job{
		ID: "organized-with-ledger-old", Status: models.JobStatusOrganized,
		StartedAt: oldAt.Add(-time.Hour), OrganizedAt: &oldAt,
	}
	recentAt := now.Add(-12 * time.Hour)
	recentJob := &models.Job{
		ID: "organized-with-ledger-recent", Status: models.JobStatusOrganized,
		StartedAt: recentAt.Add(-time.Hour), OrganizedAt: &recentAt,
	}
	require.NoError(t, jobRepo.Create(context.Background(), oldJob))
	require.NoError(t, jobRepo.Create(context.Background(), recentJob))

	oldOp := &models.BatchFileOperation{
		BatchJobID: oldJob.ID, OriginalPath: "/old/source.mp4", NewPath: "/old/dest.mp4",
		OperationType: models.OperationTypeMove,
	}
	recentOp := &models.BatchFileOperation{
		BatchJobID: recentJob.ID, OriginalPath: "/recent/source.mp4", NewPath: "/recent/dest.mp4",
		OperationType: models.OperationTypeMove,
	}
	require.NoError(t, opRepo.Create(context.Background(), oldOp))
	require.NoError(t, opRepo.Create(context.Background(), recentOp))

	require.NoError(t, jobRepo.DeleteOrganizedOlderThan(context.Background(), now.Add(-24*time.Hour)))
	_, err := jobRepo.FindByID(context.Background(), oldJob.ID)
	require.Error(t, err)
	_, err = opRepo.FindByID(context.Background(), oldOp.ID)
	require.Error(t, err, "operation rows must be pruned with the old job")
	_, err = jobRepo.FindByID(context.Background(), recentJob.ID)
	require.NoError(t, err)
	_, err = opRepo.FindByID(context.Background(), recentOp.ID)
	require.NoError(t, err, "recent job ledger must remain")
}

func TestJobRepository_DeleteOrganizedOlderThan_JobDeleteFailureRollsBack(t *testing.T) {
	db := newDatabaseTestDB(t)
	jobRepo := NewJobRepository(db)
	opRepo := NewBatchFileOperationRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	job := &models.Job{
		ID: "organized-trigger-failure", Status: models.JobStatusOrganized,
		StartedAt: organizedAt.Add(-time.Hour), OrganizedAt: &organizedAt,
	}
	require.NoError(t, jobRepo.Create(context.Background(), job))
	op := &models.BatchFileOperation{
		BatchJobID: job.ID, OriginalPath: "/trigger/source.mp4", NewPath: "/trigger/dest.mp4",
		OperationType: models.OperationTypeMove,
	}
	require.NoError(t, opRepo.Create(context.Background(), op))
	require.NoError(t, db.DB.Exec(
		`CREATE TRIGGER fail_organized_job_delete BEFORE DELETE ON jobs
		 WHEN OLD.id = 'organized-trigger-failure'
		 BEGIN SELECT RAISE(ABORT, 'forced organized delete failure'); END;`,
	).Error)

	err := jobRepo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete organized jobs")
	_, err = jobRepo.FindByID(context.Background(), job.ID)
	require.NoError(t, err, "transaction rolls the job delete back")
	_, err = opRepo.FindByID(context.Background(), op.ID)
	require.NoError(t, err, "transaction rolls the ops-first delete back")
}

func TestJobRepository_DeleteOrganizedOlderThan_PruneHookRunsBeforeDelete(t *testing.T) {
	db := newDatabaseTestDB(t)
	jobRepo := NewJobRepository(db)
	opRepo := NewBatchFileOperationRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	job := &models.Job{
		ID: "organized-prune-hook", Status: models.JobStatusOrganized,
		StartedAt: organizedAt.Add(-time.Hour), OrganizedAt: &organizedAt,
	}
	require.NoError(t, jobRepo.Create(context.Background(), job))
	op := &models.BatchFileOperation{
		BatchJobID: job.ID, OriginalPath: "/hook/source.mp4", NewPath: "/hook/dest.mp4",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: "/hook/poster.jpg", Backup: "/hook/poster.jpg.dlbak.0123456789abcdef",
		}}}),
	}
	require.NoError(t, opRepo.Create(context.Background(), op))

	var pruned []models.BatchFileOperation
	jobRepo.SetOrganizedJobPruneHook(func(_ context.Context, ops []models.BatchFileOperation) error {
		pruned = append(pruned, ops...)
		_, jobErr := jobRepo.FindByID(context.Background(), job.ID)
		require.NoError(t, jobErr, "job row must remain while cleanup owns its ledger")
		_, opErr := opRepo.FindByID(context.Background(), op.ID)
		require.NoError(t, opErr, "operation row must remain while cleanup owns its ledger")
		return nil
	})

	require.NoError(t, jobRepo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour)))
	require.Len(t, pruned, 1)
	require.Equal(t, op.ID, pruned[0].ID)
	require.Equal(t, op.GeneratedFiles, pruned[0].GeneratedFiles)
}

func TestJobRepository_DeleteOrganizedOlderThan_HookFailureRestoresRows(t *testing.T) {
	db := newDatabaseTestDB(t)
	jobRepo := NewJobRepository(db)
	opRepo := NewBatchFileOperationRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	job := &models.Job{
		ID: "organized-prune-hook-failure", Status: models.JobStatusOrganized,
		StartedAt: organizedAt.Add(-time.Hour), OrganizedAt: &organizedAt,
	}
	require.NoError(t, jobRepo.Create(context.Background(), job))
	op := &models.BatchFileOperation{
		BatchJobID: job.ID, OriginalPath: "/hook-failure/source.mp4", NewPath: "/hook-failure/dest.mp4",
		OperationType: models.OperationTypeUpdate,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
			Destination: "/hook-failure/poster.jpg", Backup: "/hook-failure/poster.jpg.dlbak.0123456789abcdef", Installed: true,
		}}}),
	}
	require.NoError(t, opRepo.Create(context.Background(), op))
	wantErr := errors.New("cleanup unavailable")
	jobRepo.SetOrganizedJobPruneHook(func(context.Context, []models.BatchFileOperation) error { return wantErr })

	err := jobRepo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	require.ErrorIs(t, err, wantErr)
	_, err = jobRepo.FindByID(context.Background(), job.ID)
	require.NoError(t, err, "failed cleanup must retain the job for retry")
	_, err = opRepo.FindByID(context.Background(), op.ID)
	require.NoError(t, err, "failed cleanup must retain the ledger ownership record")
}

func TestJobRepository_DeleteOrganizedOlderThan_OperationLookupFailure(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, repo.Create(context.Background(), &models.Job{ID: "organized-operation-lookup-failure", Status: models.JobStatusOrganized, StartedAt: organizedAt, OrganizedAt: &organizedAt}))
	require.NoError(t, db.DB.Exec("DROP TABLE batch_file_operations").Error)

	err := repo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find organized job operations")
}

func TestJobRepository_DeleteOrganizedOlderThan_OperationDeleteFailure(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	job := &models.Job{ID: "organized-operation-delete-failure", Status: models.JobStatusOrganized, StartedAt: organizedAt, OrganizedAt: &organizedAt}
	require.NoError(t, repo.Create(context.Background(), job))
	opRepo := NewBatchFileOperationRepository(db)
	require.NoError(t, opRepo.Create(context.Background(), &models.BatchFileOperation{BatchJobID: job.ID, OriginalPath: "/failure/source", NewPath: "/failure/dest", OperationType: models.OperationTypeMove}))
	require.NoError(t, db.DB.Exec(
		`CREATE TRIGGER fail_pruned_operation_delete BEFORE DELETE ON batch_file_operations
		 BEGIN SELECT RAISE(ABORT, 'forced operation delete failure'); END;`,
	).Error)

	err := repo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete organized job operations")
}

func TestJobRepository_DeleteOrganizedOlderThan_NoJobs(t *testing.T) {
	db := newDatabaseTestDB(t)
	require.NoError(t, NewJobRepository(db).DeleteOrganizedOlderThan(context.Background(), time.Now().UTC()))
}

func TestJobRepository_DeleteOrganizedOlderThan_ReplacementHookRequired(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	job := &models.Job{ID: "organized-hook-required", Status: models.JobStatusOrganized, StartedAt: organizedAt, OrganizedAt: &organizedAt}
	require.NoError(t, repo.Create(context.Background(), job))
	opRepo := NewBatchFileOperationRepository(db)
	require.NoError(t, opRepo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID: job.ID, OriginalPath: "/hook-required/source", NewPath: "/hook-required/dest",
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Destination: "/hook-required/dest", Backup: "/hook-required/dest.dlbak.a"}}}),
	}))

	err := repo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cleanup hook is not configured")
	assert.True(t, hasReplacementLedger([]models.BatchFileOperation{{GeneratedFiles: `{"Replacements":[{"backup":"/x"}]}`}}))
	assert.True(t, hasReplacementLedger([]models.BatchFileOperation{{GeneratedFiles: "not-json"}}))
	_, err = repo.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
}

func TestJobRepository_DeleteOrganizedOlderThan_VersionFenceKeepsConcurrentMutation(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	opRepo := NewBatchFileOperationRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	job := &models.Job{ID: "organized-version-fence", Status: models.JobStatusOrganized, StartedAt: organizedAt, OrganizedAt: &organizedAt}
	require.NoError(t, repo.Create(context.Background(), job))
	op := &models.BatchFileOperation{BatchJobID: job.ID, OriginalPath: "/fence/source", NewPath: "/fence/dest", OperationType: models.OperationTypeMove}
	require.NoError(t, opRepo.Create(context.Background(), op))
	repo.SetOrganizedJobPruneHook(func(ctx context.Context, _ []models.BatchFileOperation) error {
		currentJob, err := repo.FindByID(ctx, job.ID)
		require.NoError(t, err)
		currentJob.Progress = 0.5
		jobErr := repo.Update(ctx, currentJob)
		require.ErrorIs(t, jobErr, ErrJobPruning)
		currentOp, err := opRepo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		currentOp.NewPath = "/fence/concurrent-dest"
		opErr := opRepo.Update(context.Background(), currentOp)
		require.ErrorIs(t, opErr, ErrJobPruning)
		createErr := opRepo.Create(context.Background(), &models.BatchFileOperation{BatchJobID: job.ID, OriginalPath: "/fence/create", NewPath: "/fence/create-dest"})
		require.ErrorIs(t, createErr, ErrJobPruning)
		createBatchErr := opRepo.CreateBatch(context.Background(), []*models.BatchFileOperation{{BatchJobID: job.ID, OriginalPath: "/fence/new", NewPath: "/fence/new-dest"}})
		require.ErrorIs(t, createBatchErr, ErrJobPruning)
		journalErr := opRepo.UpdateJournalInTx(context.Background(), op.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			return models.GeneratedFilesJSON{}, true, nil
		})
		require.ErrorIs(t, journalErr, ErrJobPruning)
		revertErr := opRepo.UpdateRevertStatus(context.Background(), op.ID, models.RevertStatusReverted)
		require.ErrorIs(t, revertErr, ErrJobPruning)
		nonJournalErr := opRepo.UpdateNonJournalFields(context.Background(), currentOp)
		require.ErrorIs(t, nonJournalErr, ErrJobPruning)
		return errors.Join(jobErr, opErr, createErr, createBatchErr, journalErr, revertErr, nonJournalErr)
	})

	err := repo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	require.ErrorIs(t, err, ErrJobPruning)
	gotJob, findErr := repo.FindByID(context.Background(), job.ID)
	require.NoError(t, findErr, "the fenced job must remain for retry")
	require.Equal(t, pruningJobStatus, gotJob.Status)
	gotOp, findErr := opRepo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr, "the fenced operation must remain")
	require.Equal(t, "/fence/dest", gotOp.NewPath)
}

func TestJobRepository_DeleteOrganizedOlderThan_ClaimedJobLookupFailure(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, repo.Create(context.Background(), &models.Job{ID: "organized-claim-query-failure", Status: models.JobStatusOrganized, StartedAt: organizedAt, OrganizedAt: &organizedAt}))
	fired := false
	const callbackName = "test:fail_prune_claim_query"
	require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if !fired && tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "jobs" {
			fired = true
			tx.AddError(errors.New("forced prune claim lookup failure"))
		}
	}))
	t.Cleanup(func() { _ = db.DB.Callback().Query().Remove(callbackName) })

	err := repo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find organized jobs")
}

func TestEnsureOperationWritable_PruneDiagnostics(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	opRepo := NewBatchFileOperationRepository(db)
	job := &models.Job{ID: "operation-fence-diagnostic", Status: models.JobStatusOrganized, StartedAt: time.Now(), OrganizedAt: ptrTime(time.Now().Add(-48 * time.Hour))}
	require.NoError(t, repo.Create(context.Background(), job))
	op := &models.BatchFileOperation{BatchJobID: job.ID, OriginalPath: "/diag/src", NewPath: "/diag/dest"}
	require.NoError(t, opRepo.Create(context.Background(), op))
	require.NoError(t, db.DB.Exec("UPDATE jobs SET status = ? WHERE id = ?", pruningJobStatus, job.ID).Error)
	require.ErrorIs(t, ensureOperationWritable(db.DB, op.ID), ErrJobPruning)
	require.NoError(t, db.DB.Exec("UPDATE jobs SET status = ? WHERE id = ?", models.JobStatusOrganized, job.ID).Error)
	require.NoError(t, ensureOperationWritable(db.DB, 99999))
	require.NoError(t, db.DB.Exec("DROP TABLE batch_file_operations").Error)
	require.Error(t, ensureOperationWritable(db.DB, op.ID))
	require.NoError(t, db.DB.Exec("DROP TABLE jobs").Error)
	require.Error(t, ensureJobWritable(db.DB, job.ID))
}

func TestJobRepository_DeleteOrganizedOlderThan_BatchesLargeRetentionSet(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	for i := 0; i < 401; i++ {
		job := &models.Job{ID: fmt.Sprintf("organized-batch-%03d", i), Status: models.JobStatusOrganized, StartedAt: organizedAt, OrganizedAt: &organizedAt}
		require.NoError(t, repo.Create(context.Background(), job))
	}
	require.NoError(t, repo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour)))
	jobs, err := repo.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, jobs)
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
