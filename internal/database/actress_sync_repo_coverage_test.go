package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newActressSyncJobAndTask(t *testing.T, db *DB, actressID *uint, key string) (*ActressSyncRepository, *models.ActressSyncJob, models.ActressSyncTask) {
	t.Helper()
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: actressID, Label: key, DedupeKey: key, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	repo := NewActressSyncRepository(db)
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	return repo, job, task
}

func TestActressSyncCreateJobConflictLookupErrors(t *testing.T) {
	t.Run("active task lookup", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		now := time.Now().UTC()
		existingJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
		existingTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: existingJob.ID, Label: "existing", DedupeKey: "lookup:task", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, repo.CreateJob(existingJob, []models.ActressSyncTask{existingTask}))
		name := "coverage:create-task-lookup:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) { tx.AddError(errForcedActressCoverage) }))
		defer func() { require.NoError(t, db.DB.Callback().Query().Remove(name)) }()
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now.Add(time.Second)}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: existingTask.ActressID, Label: "selected", DedupeKey: existingTask.DedupeKey, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.ErrorIs(t, repo.CreateJob(job, []models.ActressSyncTask{task}), errForcedActressCoverage)
	})

	t.Run("active actress task lookup", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actress := &models.Actress{JapaneseName: "actress lookup"}
		require.NoError(t, NewActressRepository(db).Create(context.Background(), actress))
		repo := NewActressSyncRepository(db)
		remove := forceQueryErrorOnCall(t, db, 2)
		defer remove()
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: time.Now().UTC()}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "selected", DedupeKey: "lookup:actress", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: time.Now().UTC()}
		require.ErrorIs(t, repo.CreateJob(job, []models.ActressSyncTask{task}), errForcedActressCoverage)
	})

	t.Run("active task create error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		actress := &models.Actress{JapaneseName: "active task create"}
		require.NoError(t, actressRepo.Create(context.Background(), actress))
		now := time.Now().UTC()
		existingJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
		existing := models.ActressSyncTask{ID: uuid.NewString(), JobID: existingJob.ID, ActressID: &actress.ID, Label: "existing", DedupeKey: "actress:existing", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, db.Create(existingJob).Error)
		require.NoError(t, db.Create(&existing).Error)
		repo := NewActressSyncRepository(db)
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now.Add(time.Second)}
		task := models.ActressSyncTask{ID: existing.ID, JobID: job.ID, ActressID: &actress.ID, Label: "selected", DedupeKey: "actress:selected", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.Error(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	})

	t.Run("dedupe conflict job lookup", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		now := time.Now().UTC()
		existing := models.ActressSyncTask{ID: uuid.NewString(), JobID: "missing-job", Label: "existing", DedupeKey: "lookup:fallback-job", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, db.Create(&existing).Error)
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now.Add(time.Second)}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, Label: "selected", DedupeKey: existing.DedupeKey, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.Error(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	})

	t.Run("active conflict job lookup", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		actress := &models.Actress{JapaneseName: "active conflict job"}
		require.NoError(t, actressRepo.Create(context.Background(), actress))
		repo := NewActressSyncRepository(db)
		now := time.Now().UTC()
		existingJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
		existing := models.ActressSyncTask{ID: uuid.NewString(), JobID: existingJob.ID, ActressID: &actress.ID, Label: "existing", DedupeKey: "lookup:active-job", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, db.Create(existingJob).Error)
		require.NoError(t, db.Create(&existing).Error)
		remove := forceQueryErrorOnCall(t, db, 3)
		defer remove()
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now.Add(time.Second)}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "selected", DedupeKey: existing.DedupeKey, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.ErrorIs(t, repo.CreateJob(job, []models.ActressSyncTask{task}), errForcedActressCoverage)
	})
}

func forceDeferredClaimUpdate(t *testing.T, db *DB, zeroRows bool) func() {
	t.Helper()
	name := "coverage:deferred-claim:" + uuid.NewString()
	require.NoError(t, db.DB.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		sql := tx.Statement.SQL.String()
		if !strings.Contains(sql, "SET `dedupe_key`") && !strings.Contains(sql, "SET dedupe_key") {
			return
		}
		if zeroRows {
			tx.RowsAffected = 0
			return
		}
		tx.AddError(errForcedActressCoverage)
	}))
	return func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }
}

func TestActressSyncCreateJobRevalidatesActresses(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	actressID := uint(999999)
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actressID, Label: "stale", DedupeKey: "actress:stale", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.Error(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	_, err := repo.FindJob(job.ID)
	require.Error(t, err)
}

func TestActressSyncDeferredTaskReacquiresCanonicalKey(t *testing.T) {
	t.Run("active deferred request is deduplicated", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		actress := &models.Actress{JapaneseName: "deferred active"}
		require.NoError(t, actressRepo.Create(context.Background(), actress))
		now := time.Now().UTC()
		missingJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
		missingTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: missingJob.ID, ActressID: &actress.ID, Label: "missing", DedupeKey: fmt.Sprintf("actress:%d", actress.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		existingJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now.Add(time.Second)}
		existingTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: existingJob.ID, ActressID: &actress.ID, Label: "existing", DedupeKey: deferredActressSyncDedupeKey(actress.ID, "existing"), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.NoError(t, db.Create(missingJob).Error)
		require.NoError(t, db.Create(&missingTask).Error)
		require.NoError(t, db.Create(existingJob).Error)
		require.NoError(t, db.Create(&existingTask).Error)
		repo := NewActressSyncRepository(db)
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now.Add(time.Second)}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "duplicate", DedupeKey: fmt.Sprintf("actress:%d", actress.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
		stored, err := repo.ListTasks(job.ID)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		require.Equal(t, models.ActressSyncTaskSkipped, stored[0].Status)
	})

	t.Run("claim promotes deferred key", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		actress := &models.Actress{JapaneseName: "deferred claim"}
		require.NoError(t, actressRepo.Create(context.Background(), actress))
		now := time.Now().UTC()
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "deferred", DedupeKey: deferredActressSyncDedupeKey(actress.ID, "claim"), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, db.Create(job).Error)
		require.NoError(t, db.Create(&task).Error)
		repo := NewActressSyncRepository(db)
		claimed, err := repo.ClaimNext("owner", now.Add(time.Hour))
		require.NoError(t, err)
		require.NotNil(t, claimed)
		require.Equal(t, fmt.Sprintf("actress:%d", actress.ID), claimed.DedupeKey)
		var stored models.ActressSyncTask
		require.NoError(t, db.First(&stored, "id = ?", task.ID).Error)
		require.Equal(t, claimed.DedupeKey, stored.DedupeKey)
	})

	t.Run("blocked deferred key waits for canonical task", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		actress := &models.Actress{JapaneseName: "deferred blocked"}
		require.NoError(t, actressRepo.Create(context.Background(), actress))
		now := time.Now().UTC()
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
		canonical := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "canonical", DedupeKey: fmt.Sprintf("actress:%d", actress.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, Attempts: actressSyncAttemptCap, CreatedAt: now}
		deferred := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "deferred", DedupeKey: deferredActressSyncDedupeKey(actress.ID, "blocked"), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.NoError(t, db.Create(job).Error)
		require.NoError(t, db.Create(&canonical).Error)
		require.NoError(t, db.Create(&deferred).Error)
		repo := NewActressSyncRepository(db)
		claimed, err := repo.ClaimNext("owner", now.Add(time.Hour))
		require.NoError(t, err)
		require.Nil(t, claimed)
	})

	t.Run("canonical key waits for deferred task", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		actress := &models.Actress{JapaneseName: "canonical blocked"}
		require.NoError(t, actressRepo.Create(context.Background(), actress))
		now := time.Now().UTC()
		expires := now.Add(time.Hour)
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobRunning, Scope: "selected", CreatedAt: now}
		canonical := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "canonical", DedupeKey: fmt.Sprintf("actress:%d", actress.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		deferred := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "deferred", DedupeKey: deferredActressSyncDedupeKey(actress.ID, "running"), Status: models.ActressSyncTaskRunning, Stage: "resolving", Messages: []string{}, UpdatedFields: []string{}, LeaseOwner: "owner", LeaseToken: "token", LeaseExpiresAt: &expires, CreatedAt: now}
		require.NoError(t, db.Create(job).Error)
		require.NoError(t, db.Create(&canonical).Error)
		require.NoError(t, db.Create(&deferred).Error)
		claimed, err := NewActressSyncRepository(db).ClaimNext("other-owner", now.Add(time.Hour))
		require.NoError(t, err)
		require.Nil(t, claimed)
	})

	t.Run("claim promotion update errors", func(t *testing.T) {
		for _, zeroRows := range []bool{false, true} {
			t.Run(fmt.Sprintf("zero_rows_%t", zeroRows), func(t *testing.T) {
				db := newDatabaseTestDB(t)
				actressRepo := NewActressRepository(db)
				actress := &models.Actress{JapaneseName: "deferred update"}
				require.NoError(t, actressRepo.Create(context.Background(), actress))
				now := time.Now().UTC()
				job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
				task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "deferred", DedupeKey: deferredActressSyncDedupeKey(actress.ID, "update"), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
				require.NoError(t, db.Create(job).Error)
				require.NoError(t, db.Create(&task).Error)
				remove := forceDeferredClaimUpdate(t, db, zeroRows)
				defer remove()
				_, err := NewActressSyncRepository(db).ClaimNext("owner", now.Add(time.Hour))
				if zeroRows {
					require.ErrorIs(t, err, errActressSyncLeaseLost)
				} else {
					require.ErrorIs(t, err, errForcedActressCoverage)
				}
			})
		}
	})
}

func TestActressSyncTaskViews(t *testing.T) {
	db := newDatabaseTestDB(t)
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobRunning, Scope: "missing", CreatedAt: now}
	require.NoError(t, db.Create(job).Error)
	started := now.Add(time.Second)
	completed := now.Add(2 * time.Second)
	tasks := []models.ActressSyncTask{
		{ID: uuid.NewString(), JobID: job.ID, Label: "running", DedupeKey: "view:running", Status: models.ActressSyncTaskRunning, Stage: "resolving", Messages: []string{}, UpdatedFields: []string{}, StartedAt: &started, CreatedAt: now},
		{ID: uuid.NewString(), JobID: job.ID, Label: "pending", DedupeKey: "view:pending", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now},
		{ID: uuid.NewString(), JobID: job.ID, Label: "skipped", DedupeKey: "view:skipped", Status: models.ActressSyncTaskSkipped, Stage: "completed", Messages: []string{}, UpdatedFields: []string{}, CompletedAt: &completed, CreatedAt: now},
		{ID: uuid.NewString(), JobID: job.ID, Label: "failed", DedupeKey: "view:failed", Status: models.ActressSyncTaskFailed, Stage: "completed", Messages: []string{}, UpdatedFields: []string{}, CompletedAt: &completed, CreatedAt: now.Add(time.Second)},
		{ID: uuid.NewString(), JobID: job.ID, Label: "completed", DedupeKey: "view:completed", Status: models.ActressSyncTaskCompleted, Stage: "completed", Messages: []string{}, UpdatedFields: []string{}, CompletedAt: &completed, CreatedAt: now.Add(2 * time.Second)},
	}
	require.NoError(t, db.Create(&tasks).Error)
	repo := NewActressSyncRepository(db)
	running, err := repo.ListRunningTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, running, 1)
	require.Equal(t, "running", running[0].Label)
	diagnostics, err := repo.ListDiagnosticTasks(job.ID, 1)
	require.NoError(t, err)
	require.Len(t, diagnostics, 1)
	require.Equal(t, "failed", diagnostics[0].Label)
	diagnostics, err = repo.ListDiagnosticTasks(job.ID, 0)
	require.NoError(t, err)
	require.Len(t, diagnostics, 2)
}

func TestActressSyncCreateJobPreservesSelectedDuplicate(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	actress := &models.Actress{JapaneseName: "selected duplicate"}
	require.NoError(t, actressRepo.Create(context.Background(), actress))
	now := time.Now().UTC()
	dedupeKey := fmt.Sprintf("actress:%d", actress.ID)
	missingJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	missingTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: missingJob.ID, ActressID: &actress.ID, DedupeKey: dedupeKey, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	repo := NewActressSyncRepository(db)
	require.NoError(t, repo.CreateJob(missingJob, []models.ActressSyncTask{missingTask}))
	selectedJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now.Add(time.Second)}
	selectedTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: selectedJob.ID, ActressID: &actress.ID, DedupeKey: dedupeKey, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
	require.NoError(t, repo.CreateJob(selectedJob, []models.ActressSyncTask{selectedTask}))
	stored, err := repo.ListTasks(selectedJob.ID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, models.ActressSyncTaskPending, stored[0].Status)
	require.Equal(t, deferredActressSyncDedupeKey(actress.ID, selectedTask.ID), stored[0].DedupeKey)
	require.Equal(t, []string{"deferred_to_stronger_sync_task"}, stored[0].Messages)
}

func TestActressSyncCreateJobCoalescesDuplicateActiveTasks(t *testing.T) {
	db := newDatabaseTestDB(t)
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	key := "actress:duplicate"
	tasks := []models.ActressSyncTask{
		{ID: uuid.NewString(), JobID: job.ID, DedupeKey: key, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now},
		{ID: uuid.NewString(), JobID: job.ID, DedupeKey: key, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Millisecond)},
	}
	repo := NewActressSyncRepository(db)
	require.NoError(t, repo.CreateJob(job, tasks))
	stored, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Equal(t, models.ActressSyncTaskSkipped, stored[1].Status)
	require.Equal(t, []string{"duplicate_active_task"}, stored[1].Messages)
	require.NotNil(t, stored[1].CompletedAt)
	require.Contains(t, stored[1].DedupeKey, ":duplicate:")
	require.Equal(t, 1, job.Completed)
	require.Equal(t, 1, job.Skipped)
}

func TestActressSyncClaimAndLeaseTransitionBranches(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	claimed, err := repo.ClaimNext("nobody", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, claimed)
	require.NoError(t, repo.ReleaseOwnerLeases(""))
	require.ErrorIs(t, repo.CompleteTask(nil, "token"), ErrInvalidLookup)

	for _, tc := range []struct {
		name       string
		cancelled  bool
		attempts   int
		wantStatus string
	}{
		{name: "cancelled", cancelled: true, attempts: 1, wantStatus: models.ActressSyncTaskCancelled},
		{name: "retry", attempts: 1, wantStatus: models.ActressSyncTaskPending},
	} {
		t.Run(tc.name, func(t *testing.T) {
			syncRepo, job, _ := newActressSyncJobAndTask(t, db, nil, "lease:"+tc.name+uuid.NewString())
			claimed, claimErr := syncRepo.ClaimNext("owner-"+tc.name, time.Now().Add(time.Hour))
			require.NoError(t, claimErr)
			require.NotNil(t, claimed)
			require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", claimed.ID).Updates(map[string]any{"attempts": tc.attempts, "lease_expires_at": nil}).Error)
			if tc.cancelled {
				require.NoError(t, db.Model(&models.ActressSyncJob{}).Where("id = ?", job.ID).Update("cancel_requested", true).Error)
			}
			require.NoError(t, syncRepo.RecoverExpiredLeases(time.Now().UTC()))
			stored, listErr := syncRepo.ListTasks(job.ID)
			require.NoError(t, listErr)
			require.Equal(t, tc.wantStatus, stored[0].Status)
		})
	}
}

func TestActressSyncReleaseOwnerAttemptCapAndStaleCompletion(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo, job, _ := newActressSyncJobAndTask(t, db, nil, "release-cap")
	claimed, err := repo.ClaimNext("cap-owner", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", claimed.ID).Update("attempts", actressSyncAttemptCap).Error)
	require.NoError(t, repo.ReleaseOwnerLeases("cap-owner"))
	tasks, err := repo.ListTasks(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncTaskFailed, tasks[0].Status)
	require.Equal(t, "attempt_cap_reached", tasks[0].ErrorMessage)
	require.ErrorIs(t, repo.CompleteTask(claimed, claimed.LeaseToken), errActressSyncLeaseLost)
}

func TestActressSyncReassignRejectsWrongActressAndRunningConflict(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	first := &models.Actress{JapaneseName: "first"}
	second := &models.Actress{JapaneseName: "second"}
	require.NoError(t, actressRepo.Create(context.Background(), first))
	require.NoError(t, actressRepo.Create(context.Background(), second))
	repo, job, task := newActressSyncJobAndTask(t, db, &first.ID, fmt.Sprintf("actress:%d", first.ID))
	other := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &second.ID, Label: "other", DedupeKey: fmt.Sprintf("actress:%d", second.ID), Status: models.ActressSyncTaskRunning, Stage: "resolving", Messages: []string{}, UpdatedFields: []string{}, LeaseOwner: "other", LeaseToken: "other-token", CreatedAt: time.Now().UTC()}
	expires := time.Now().Add(time.Hour)
	other.LeaseExpiresAt = &expires
	require.NoError(t, db.Create(&other).Error)
	claimed, err := repo.ClaimNext("owner", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, task.ID, claimed.ID)
	err = repo.reassignTaskActressTx(db.DB, claimed.ID, claimed.LeaseToken, second.ID, second.ID)
	require.ErrorIs(t, err, errActressSyncLeaseLost)
	err = repo.reassignTaskActressTx(db.DB, claimed.ID, claimed.LeaseToken, second.ID, first.ID)
	require.ErrorIs(t, err, ErrActressSyncCanonicalTaskRunning)
	require.Contains(t, err.Error(), "canonical actress sync task is already running")
}

func TestActressSyncReassignJobScopeErrors(t *testing.T) {
	newPair := func(t *testing.T) (*DB, *ActressSyncRepository, *models.ActressSyncTask, *models.Actress, *models.Actress, *models.ActressSyncJob) {
		t.Helper()
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		from := &models.Actress{JapaneseName: "from"}
		to := &models.Actress{DMMID: 1400, JapaneseName: "to"}
		require.NoError(t, actressRepo.Create(context.Background(), from))
		require.NoError(t, actressRepo.Create(context.Background(), to))
		repo, job, _ := newActressSyncJobAndTask(t, db, &from.ID, fmt.Sprintf("actress:%d", from.ID))
		claimed, err := repo.ClaimNext("scope-owner", time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.NotNil(t, claimed)
		return db, repo, claimed, from, to, job
	}

	t.Run("current job lookup", func(t *testing.T) {
		db, repo, task, from, to, job := newPair(t)
		require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", task.ID).Update("job_id", "missing-job").Error)
		pending := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &to.ID, DedupeKey: fmt.Sprintf("actress:%d", to.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: time.Now().UTC()}
		require.NoError(t, db.Create(&pending).Error)
		require.Error(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID))
	})

	t.Run("conflict job lookup", func(t *testing.T) {
		db, repo, task, from, to, _ := newPair(t)
		pending := models.ActressSyncTask{ID: uuid.NewString(), JobID: "missing-job", ActressID: &to.ID, DedupeKey: fmt.Sprintf("actress:%d", to.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: time.Now().UTC()}
		require.NoError(t, db.Create(&pending).Error)
		require.Error(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID))
	})

	t.Run("job refresh", func(t *testing.T) {
		db, repo, task, from, to, job := newPair(t)
		pending := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &to.ID, DedupeKey: fmt.Sprintf("actress:%d", to.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: time.Now().UTC()}
		require.NoError(t, db.Create(&pending).Error)
		name := "coverage:sync-job-refresh:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "actress_sync_jobs" {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})
}

func TestActressSyncDeferredReassignErrorBranches(t *testing.T) {
	setup := func(t *testing.T, withConflict, withDeferred bool) (*DB, *ActressSyncRepository, models.ActressSyncTask, models.Actress, models.Actress) {
		t.Helper()
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		from := models.Actress{JapaneseName: "deferred from"}
		to := models.Actress{DMMID: 1501, JapaneseName: "deferred to"}
		require.NoError(t, actressRepo.Create(context.Background(), &from))
		require.NoError(t, actressRepo.Create(context.Background(), &to))
		now := time.Now().UTC()
		missingJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobRunning, Scope: "missing", CreatedAt: now}
		selectedJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
		expires := now.Add(time.Hour)
		current := models.ActressSyncTask{ID: uuid.NewString(), JobID: missingJob.ID, ActressID: &from.ID, DedupeKey: fmt.Sprintf("actress:%d", from.ID), Status: models.ActressSyncTaskRunning, Stage: "resolving", Messages: []string{}, UpdatedFields: []string{}, LeaseOwner: "owner", LeaseToken: "token", LeaseExpiresAt: &expires, CreatedAt: now}
		require.NoError(t, db.Create(missingJob).Error)
		require.NoError(t, db.Create(selectedJob).Error)
		require.NoError(t, db.Create(&current).Error)
		if withConflict {
			conflict := models.ActressSyncTask{ID: uuid.NewString(), JobID: selectedJob.ID, ActressID: &to.ID, DedupeKey: fmt.Sprintf("actress:%d", to.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
			require.NoError(t, db.Create(&conflict).Error)
		}
		if withDeferred {
			deferred := models.ActressSyncTask{ID: uuid.NewString(), JobID: selectedJob.ID, ActressID: &from.ID, DedupeKey: deferredActressSyncDedupeKey(from.ID, "source"), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
			require.NoError(t, db.Create(&deferred).Error)
		}
		return db, NewActressSyncRepository(db), current, from, to
	}

	t.Run("migrates stronger deferred task", func(t *testing.T) {
		db, repo, current, from, to := setup(t, false, true)
		require.NoError(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID))
		var migrated models.ActressSyncTask
		require.NoError(t, db.Where("id <> ? AND actress_id = ?", current.ID, to.ID).First(&migrated).Error)
		require.Equal(t, models.ActressSyncTaskPending, migrated.Status)
		require.Equal(t, to.ID, *migrated.ActressID)
		require.Equal(t, deferredActressSyncDedupeKey(to.ID, migrated.ID), migrated.DedupeKey)
	})

	t.Run("deferred job refresh error", func(t *testing.T) {
		db, repo, current, from, to := setup(t, false, true)
		var deferred models.ActressSyncTask
		require.NoError(t, db.Where("id <> ? AND actress_id = ?", current.ID, from.ID).First(&deferred).Error)
		require.NoError(t, db.Model(&models.ActressSyncJob{}).Where("id = ?", deferred.JobID).Update("scope", "missing").Error)
		name := "coverage:deferred-refresh:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "actress_sync_jobs" {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})

	t.Run("conflict query error", func(t *testing.T) {
		db, repo, current, from, to := setup(t, false, false)
		remove := forceQueryErrorOnCall(t, db, 3)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})

	t.Run("deferred task list query error", func(t *testing.T) {
		db, repo, current, from, to := setup(t, false, true)
		remove := forceQueryErrorOnCall(t, db, 4)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})

	t.Run("deferred job lookup error", func(t *testing.T) {
		db, repo, current, from, to := setup(t, false, true)
		remove := forceQueryErrorOnCall(t, db, 5)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})

	t.Run("deferred task update error", func(t *testing.T) {
		db, repo, current, from, to := setup(t, false, true)
		remove := forceUpdateError(t, db)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})

	t.Run("deferred task fenced update", func(t *testing.T) {
		db, repo, current, from, to := setup(t, false, true)
		remove := forceZeroRowsAfterUpdate(t, db)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID), errActressSyncLeaseLost)
	})

	t.Run("coalesced task update error", func(t *testing.T) {
		db, repo, current, from, to := setup(t, true, true)
		remove := forceUpdateError(t, db)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})

	t.Run("coalesced task fenced update", func(t *testing.T) {
		db, repo, current, from, to := setup(t, true, true)
		remove := forceZeroRowsAfterUpdate(t, db)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID), errActressSyncLeaseLost)
	})
}

func TestActressSyncReassignPreservesStrongerPendingSelection(t *testing.T) {
	db := newDatabaseTestDB(t)
	actressRepo := NewActressRepository(db)
	from := &models.Actress{JapaneseName: "from"}
	to := &models.Actress{DMMID: 1401, JapaneseName: "to"}
	require.NoError(t, actressRepo.Create(context.Background(), from))
	require.NoError(t, actressRepo.Create(context.Background(), to))
	now := time.Now().UTC()
	missingJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobRunning, Scope: "missing", CreatedAt: now}
	selectedJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
	expires := now.Add(time.Hour)
	current := models.ActressSyncTask{ID: uuid.NewString(), JobID: missingJob.ID, ActressID: &from.ID, DedupeKey: fmt.Sprintf("actress:%d", from.ID), Status: models.ActressSyncTaskRunning, Stage: "resolving", Messages: []string{}, UpdatedFields: []string{}, LeaseOwner: "owner", LeaseToken: "token", LeaseExpiresAt: &expires, CreatedAt: now}
	pending := models.ActressSyncTask{ID: uuid.NewString(), JobID: selectedJob.ID, ActressID: &to.ID, DedupeKey: fmt.Sprintf("actress:%d", to.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	deferred := models.ActressSyncTask{ID: uuid.NewString(), JobID: selectedJob.ID, ActressID: &from.ID, DedupeKey: deferredActressSyncDedupeKey(from.ID, "selected"), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, db.Create(missingJob).Error)
	require.NoError(t, db.Create(selectedJob).Error)
	require.NoError(t, db.Create(&current).Error)
	require.NoError(t, db.Create(&pending).Error)
	require.NoError(t, db.Create(&deferred).Error)

	repo := NewActressSyncRepository(db)
	require.NoError(t, repo.reassignTaskActressTx(db.DB, current.ID, current.LeaseToken, to.ID, from.ID))
	var storedCurrent, storedPending, storedDeferred models.ActressSyncTask
	require.NoError(t, db.First(&storedCurrent, "id = ?", current.ID).Error)
	require.NoError(t, db.First(&storedPending, "id = ?", pending.ID).Error)
	require.NoError(t, db.First(&storedDeferred, "id = ?", deferred.ID).Error)
	require.Equal(t, to.ID, *storedCurrent.ActressID)
	require.Equal(t, deferredActressSyncDedupeKey(to.ID, current.ID), storedCurrent.DedupeKey)
	require.Equal(t, models.ActressSyncTaskRunning, storedCurrent.Status)
	require.Equal(t, models.ActressSyncTaskPending, storedPending.Status)
	require.Equal(t, models.ActressSyncTaskSkipped, storedDeferred.Status)
	require.Equal(t, []string{"coalesced_into_merged_task"}, storedDeferred.Messages)
}

func TestSourceFencedActressOperations(t *testing.T) {
	ctx := context.Background()
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	source := &models.Actress{JapaneseName: "source"}
	require.NoError(t, repo.Create(ctx, source))
	expected := *source
	assigned, err := repo.AssignDMMIDIfMissingWithSource(ctx, source.ID, 1301, expected)
	require.NoError(t, err)
	require.True(t, assigned)

	stale := &models.Actress{JapaneseName: "stale"}
	require.NoError(t, repo.Create(ctx, stale))
	staleExpected := *stale
	require.NoError(t, db.Model(stale).Update("japanese_name", "edited").Error)
	assigned, err = repo.AssignDMMIDIfMissingWithSource(ctx, stale.ID, 1302, staleExpected)
	require.NoError(t, err)
	require.False(t, assigned)

	taskSource := &models.Actress{JapaneseName: "task source"}
	require.NoError(t, repo.Create(ctx, taskSource))
	taskExpected := *taskSource
	syncRepo, _, _ := newActressSyncJobAndTask(t, db, &taskSource.ID, "source-fenced-task")
	claimed, err := syncRepo.ClaimNext("source-fenced-owner", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assigned, err = repo.AssignDMMIDIfMissingForSyncTaskWithSource(ctx, taskSource.ID, 1303, taskExpected, claimed.ID, claimed.LeaseToken)
	require.NoError(t, err)
	require.True(t, assigned)

	target := &models.Actress{DMMID: 1304, JapaneseName: "target"}
	mergeSource := &models.Actress{JapaneseName: "merge source"}
	require.NoError(t, repo.Create(ctx, target))
	require.NoError(t, repo.Create(ctx, mergeSource))
	mergeExpected := *mergeSource
	require.NoError(t, db.Model(mergeSource).Update("japanese_name", "edited merge source").Error)
	_, err = repo.MergeWithSource(ctx, 0, mergeSource.ID, nil, mergeExpected)
	require.Error(t, err)
	_, err = repo.MergeWithSource(ctx, target.ID, mergeSource.ID, nil, mergeExpected)
	require.ErrorIs(t, err, ErrActressSyncIdentityChanged)

	matchingTarget := &models.Actress{DMMID: 1306, JapaneseName: "matching target"}
	matchingSource := &models.Actress{JapaneseName: "matching source"}
	require.NoError(t, repo.Create(ctx, matchingTarget))
	require.NoError(t, repo.Create(ctx, matchingSource))
	_, err = repo.MergeWithSource(ctx, matchingTarget.ID, matchingSource.ID, nil, *matchingSource)
	require.NoError(t, err)

	taskTarget := &models.Actress{DMMID: 1305, JapaneseName: "task target"}
	taskMergeSource := &models.Actress{JapaneseName: "task merge source"}
	require.NoError(t, repo.Create(ctx, taskTarget))
	require.NoError(t, repo.Create(ctx, taskMergeSource))
	taskMergeExpected := *taskMergeSource
	taskSyncRepo, taskJob, _ := newActressSyncJobAndTask(t, db, &taskMergeSource.ID, "source-fenced-merge-task")
	taskClaimed, err := taskSyncRepo.ClaimNext("source-fenced-merge-owner", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, taskClaimed)
	require.NoError(t, db.Model(taskMergeSource).Update("japanese_name", "edited task merge source").Error)
	_, err = repo.MergeForSyncTaskWithSource(ctx, taskTarget.ID, taskMergeSource.ID, nil, taskMergeExpected, taskClaimed.ID, taskClaimed.LeaseToken)
	require.ErrorIs(t, err, ErrActressSyncIdentityChanged)
	require.Equal(t, taskJob.ID, taskClaimed.JobID)
}

func TestActressSyncFieldMergingAndMutationValidation(t *testing.T) {
	require.Equal(t, []string{"first", "FIRST", "second"}, mergeSyncTaskFields([]string{" first ", ""}, []string{"first", "FIRST", "second"}))
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	_, err := repo.MergeForSyncTask(context.Background(), 1, 2, nil, " ", "token")
	require.ErrorIs(t, err, ErrInvalidLookup)

	for _, tc := range []struct {
		id          uint
		dmmID       int
		expected    string
		replacement string
	}{
		{dmmID: 1, expected: "old", replacement: "new"},
		{id: 1, expected: "old", replacement: "new"},
		{id: 1, dmmID: 1, replacement: "new"},
		{id: 1, dmmID: 1, expected: "old"},
		{id: 1, dmmID: 1, expected: "old", replacement: "https://pics.dmm.co.jp/mono/noimage/now_printing.jpg"},
	} {
		replaced, replaceErr := repo.ReplaceThumbnail(context.Background(), tc.id, tc.dmmID, tc.expected, tc.replacement)
		require.False(t, replaced)
		require.ErrorIs(t, replaceErr, ErrInvalidLookup)
	}
	assigned, err := repo.AssignDMMIDIfMissing(context.Background(), 0, 1)
	require.False(t, assigned)
	require.ErrorIs(t, err, ErrInvalidLookup)
	assigned, err = repo.AssignDMMIDIfMissing(context.Background(), 1, 0)
	require.False(t, assigned)
	require.ErrorIs(t, err, ErrInvalidLookup)
}

func TestActressSyncCancelMissingAndCompletedJobs(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	require.ErrorIs(t, repo.CancelJob("missing"), ErrNotFound)
	repo, job, _ := newActressSyncJobAndTask(t, db, nil, "cancel-twice")
	require.NoError(t, repo.CancelJob(job.ID))
	require.NoError(t, repo.CancelJob(job.ID))
}

func TestActressRepositoryChangedQueryErrors(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	_, err := repo.ListFiltered(ctx, "", 1, 0, "invalid", "asc")
	require.Error(t, err)
	_, err = repo.SearchFiltered(ctx, "name", "", 1, 0, "invalid_field", "asc")
	require.Error(t, err)

	require.NoError(t, db.Close())
	_, err = repo.FindAllByJapaneseName(ctx, "name")
	require.Error(t, err)
	_, err = repo.ListFiltered(ctx, "missing_dmm", 1, 0, "id", "asc")
	require.Error(t, err)
	_, err = repo.SearchFiltered(ctx, "name", "has_dmm", 1, 0, "id", "asc")
	require.Error(t, err)
	_, err = repo.CountFiltered(ctx, "missing_dmm")
	require.Error(t, err)
	_, err = repo.CountSearchFiltered(ctx, "name", "has_dmm")
	require.Error(t, err)
}

func TestActressMergeChangedBranches(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	_, err := repo.merger.ExecuteMerge(context.Background(), nil, db)
	require.Error(t, err)
	_, err = repo.merger.ExecuteMerge(context.Background(), &MergePlan{}, nil)
	require.Error(t, err)

	target := &models.Actress{DMMID: 101, JapaneseName: "target"}
	source := &models.Actress{DMMID: 202, JapaneseName: "source"}
	require.NoError(t, repo.Create(context.Background(), target))
	require.NoError(t, repo.Create(context.Background(), source))
	plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, map[string]string{"dmm_id": "source"})
	require.NoError(t, err)
	require.NoError(t, db.Model(target).Updates(map[string]any{
		"japanese_name": "changed",
		"updated_at":    time.Now().UTC().Add(time.Second),
	}).Error)
	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.ErrorIs(t, err, ErrActressMergeStalePlan)

	plan, err = repo.merger.PlanMerge(context.Background(), target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NoError(t, db.Delete(source).Error)
	_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
	require.Error(t, err)

	require.False(t, errors.Is(err, ErrActressMergeUniqueConstraint))

	defaultTarget := &models.Actress{DMMID: 303, JapaneseName: "default-target"}
	defaultSource := &models.Actress{DMMID: 404, JapaneseName: "default-source"}
	require.NoError(t, repo.Create(context.Background(), defaultTarget))
	require.NoError(t, repo.Create(context.Background(), defaultSource))
	result, err := repo.merger.ExecuteMerge(context.Background(), &MergePlan{
		TargetID: defaultTarget.ID, SourceID: defaultSource.ID, Resolutions: map[string]string{},
	}, db)
	require.NoError(t, err)
	require.Equal(t, defaultTarget.DMMID, result.MergedActress.DMMID)
	require.Equal(t, defaultTarget.JapaneseName, result.MergedActress.JapaneseName)
}

func TestActressSyncRepositoryDatabaseErrors(t *testing.T) {
	ctx := context.Background()
	withClosedRepo := func(t *testing.T) (*DB, *ActressSyncRepository) {
		t.Helper()
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		require.NoError(t, db.Close())
		return db, repo
	}

	t.Run("create", func(t *testing.T) {
		_, repo := withClosedRepo(t)
		err := repo.CreateJob(&models.ActressSyncJob{ID: uuid.NewString()}, nil)
		require.Error(t, err)
	})
	t.Run("find", func(t *testing.T) {
		_, repo := withClosedRepo(t)
		_, err := repo.FindJob("missing")
		require.Error(t, err)
	})
	t.Run("claim", func(t *testing.T) {
		_, repo := withClosedRepo(t)
		_, err := repo.ClaimNext("owner", time.Now().Add(time.Hour))
		require.Error(t, err)
	})
	t.Run("recover", func(t *testing.T) {
		_, repo := withClosedRepo(t)
		require.Error(t, repo.RecoverExpiredLeases(time.Now()))
	})
	t.Run("release", func(t *testing.T) {
		_, repo := withClosedRepo(t)
		require.Error(t, repo.ReleaseOwnerLeases("owner"))
	})
	t.Run("heartbeat", func(t *testing.T) {
		_, repo := withClosedRepo(t)
		require.Error(t, repo.Heartbeat("task", "token", time.Now().Add(time.Hour)))
	})
	t.Run("stage", func(t *testing.T) {
		_, repo := withClosedRepo(t)
		require.Error(t, repo.UpdateStage("task", "token", "saving"))
	})
	t.Run("cancel", func(t *testing.T) {
		_, repo := withClosedRepo(t)
		require.Error(t, repo.CancelJob("job"))
	})
	t.Run("candidate", func(t *testing.T) {
		db, _ := withClosedRepo(t)
		_, err := NewActressRepository(db).ListSyncCandidates(ctx)
		require.Error(t, err)
	})
	t.Run("fill", func(t *testing.T) {
		db, _ := withClosedRepo(t)
		_, err := NewActressRepository(db).FillBlankMetadata(ctx, 1, 1, models.ActressInfo{DMMID: 1})
		require.Error(t, err)
	})
}

func seedTerminalActressSyncHistory(t *testing.T, db *DB, count int) []string {
	t.Helper()
	ids := make([]string, 0, count)
	base := time.Now().UTC().Add(-time.Duration(count) * time.Hour)
	for i := 0; i < count; i++ {
		completedAt := base.Add(time.Duration(i) * time.Hour)
		job := models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobCompleted, Scope: "missing", CreatedAt: completedAt, CompletedAt: &completedAt}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, DedupeKey: "history:" + job.ID, Status: models.ActressSyncTaskCompleted, Stage: "completed", CreatedAt: completedAt, CompletedAt: &completedAt, Messages: []string{}, UpdatedFields: []string{}}
		require.NoError(t, db.Create(&job).Error)
		require.NoError(t, db.Create(&task).Error)
		ids = append(ids, job.ID)
	}
	return ids
}

func TestActressSyncTerminalHistoryRetention(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	ids := seedTerminalActressSyncHistory(t, db, actressSyncTerminalRetention+2)
	active := models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobRunning, Scope: "missing", CreatedAt: time.Now().UTC().Add(-48 * time.Hour)}
	require.NoError(t, db.Create(&active).Error)

	newJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: time.Now().UTC()}
	newTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: newJob.ID, DedupeKey: "new:" + newJob.ID, Status: models.ActressSyncTaskPending, Stage: "queued", CreatedAt: newJob.CreatedAt, Messages: []string{}, UpdatedFields: []string{}}
	require.NoError(t, repo.CreateJob(newJob, []models.ActressSyncTask{newTask}))

	var terminalCount int64
	require.NoError(t, db.Model(&models.ActressSyncJob{}).Where("status IN ?", []string{models.ActressSyncJobCompleted, models.ActressSyncJobCancelled}).Count(&terminalCount).Error)
	require.EqualValues(t, actressSyncTerminalRetention, terminalCount)
	var oldTaskCount int64
	require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("job_id IN ?", ids[:2]).Count(&oldTaskCount).Error)
	require.Zero(t, oldTaskCount)
	var retainedCount int64
	require.NoError(t, db.Model(&models.ActressSyncJob{}).Where("id IN ?", []string{active.ID, newJob.ID, ids[len(ids)-1]}).Count(&retainedCount).Error)
	require.EqualValues(t, 3, retainedCount)
	require.NoError(t, repo.pruneTerminalJobsTx(db.DB))
}

func TestActressSyncTerminalHistoryRetentionOnRefresh(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	seedTerminalActressSyncHistory(t, db, actressSyncTerminalRetention+1)
	now := time.Now().UTC()
	job := models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobRunning, Scope: "selected", CreatedAt: now}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, DedupeKey: "refresh:" + job.ID, Status: models.ActressSyncTaskCompleted, Stage: "completed", CreatedAt: now, CompletedAt: &now, Messages: []string{}, UpdatedFields: []string{}}
	require.NoError(t, db.Create(&job).Error)
	require.NoError(t, db.Create(&task).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return repo.refreshJobTx(tx, job.ID, now) }))

	stored, err := repo.FindJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncJobCompleted, stored.Status)
	var terminalCount int64
	require.NoError(t, db.Model(&models.ActressSyncJob{}).Where("status IN ?", []string{models.ActressSyncJobCompleted, models.ActressSyncJobCancelled}).Count(&terminalCount).Error)
	require.EqualValues(t, actressSyncTerminalRetention, terminalCount)
}

func TestActressSyncTerminalHistoryRetentionErrors(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		require.NoError(t, db.Close())
		require.Error(t, repo.pruneTerminalJobsTx(db.DB))
	})
	for _, table := range []string{"actress_sync_tasks", "actress_sync_jobs"} {
		t.Run(table, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			repo := NewActressSyncRepository(db)
			seedTerminalActressSyncHistory(t, db, actressSyncTerminalRetention+1)
			name := "retention:delete:" + table + ":" + uuid.NewString()
			require.NoError(t, db.DB.Callback().Delete().Before("gorm:delete").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					tx.AddError(errForcedActressCoverage)
				}
			}))
			defer func() { require.NoError(t, db.DB.Callback().Delete().Remove(name)) }()
			require.ErrorIs(t, repo.pruneTerminalJobsTx(db.DB), errForcedActressCoverage)
		})
	}
}
func TestMergeCachedIdentityPreconditions(t *testing.T) {
	newPair := func(t *testing.T) (*DB, *ActressRepository, *models.Actress, *models.Actress) {
		t.Helper()
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		target := &models.Actress{DMMID: 1201, JapaneseName: "canonical"}
		source := &models.Actress{JapaneseName: "duplicate"}
		require.NoError(t, repo.Create(t.Context(), target))
		require.NoError(t, repo.Create(t.Context(), source))
		return db, repo, target, source
	}
	t.Run("success", func(t *testing.T) {
		_, repo, target, source := newPair(t)
		merged, err := repo.MergeCachedIdentity(t.Context(), target.ID, source.ID, target.DMMID)
		require.NoError(t, err)
		require.Equal(t, target.ID, merged.MergedActress.ID)
		_, err = repo.FindByID(t.Context(), source.ID)
		require.Error(t, err)
	})
	for _, tc := range []struct {
		name     string
		expected int
		assign   bool
	}{
		{name: "canonical changed", expected: 9999},
		{name: "source assigned", expected: 1201, assign: true},
		{name: "invalid expected identity", expected: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, repo, target, source := newPair(t)
			if tc.assign {
				require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", source.ID).Update("dmm_id", 1202).Error)
			}
			_, err := repo.MergeCachedIdentity(t.Context(), target.ID, source.ID, tc.expected)
			require.ErrorIs(t, err, ErrActressSyncIdentityChanged)
			_, err = repo.FindByID(t.Context(), source.ID)
			require.NoError(t, err)
		})
	}
	t.Run("task validation and plan failure", func(t *testing.T) {
		_, repo, target, source := newPair(t)
		_, err := repo.MergeCachedIdentityForSyncTask(t.Context(), target.ID, source.ID, target.DMMID, "", "token")
		require.ErrorIs(t, err, ErrInvalidLookup)
		_, err = repo.MergeCachedIdentityForSyncTask(t.Context(), 0, source.ID, target.DMMID, "task", "token")
		require.ErrorIs(t, err, ErrActressMergeInvalidID)
		_, err = repo.MergeCachedIdentity(t.Context(), 0, source.ID, target.DMMID)
		require.ErrorIs(t, err, ErrActressMergeInvalidID)
	})
	t.Run("leased success", func(t *testing.T) {
		db, repo, target, source := newPair(t)
		syncRepo, _, _ := newActressSyncJobAndTask(t, db, &source.ID, "cached-identity-lease:"+uuid.NewString())
		claimed, err := syncRepo.ClaimNext("owner", time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.NotNil(t, claimed)
		merged, err := repo.MergeCachedIdentityForSyncTask(t.Context(), target.ID, source.ID, target.DMMID, claimed.ID, claimed.LeaseToken)
		require.NoError(t, err)
		require.Equal(t, target.ID, merged.MergedActress.ID)
	})
}

func TestMergeCachedIdentityRejectsChangedSourceIdentity(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	target := &models.Actress{DMMID: 1201, JapaneseName: "canonical"}
	source := &models.Actress{JapaneseName: "duplicate"}
	require.NoError(t, repo.Create(t.Context(), target))
	require.NoError(t, repo.Create(t.Context(), source))
	expectedSource := *source
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", source.ID).Update("japanese_name", "changed").Error)

	_, err := repo.MergeCachedIdentityWithSource(t.Context(), target.ID, source.ID, target.DMMID, expectedSource)
	require.ErrorIs(t, err, ErrActressSyncIdentityChanged)
	_, err = repo.FindByID(t.Context(), source.ID)
	require.NoError(t, err)
}
