package database

import (
	"context"
	"errors"
	"fmt"
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
	require.Error(t, err)
	require.Contains(t, err.Error(), "already has a running sync task")
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
