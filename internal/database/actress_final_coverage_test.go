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

var errForcedActressCoverage = errors.New("forced actress coverage error")

func claimActressCoverageTask(t *testing.T, db *DB, actressID *uint) (*ActressSyncRepository, *models.ActressSyncJob, *models.ActressSyncTask) {
	t.Helper()
	repo, job, _ := newActressSyncJobAndTask(t, db, actressID, "coverage:"+uuid.NewString())
	claimed, err := repo.ClaimNext("coverage-owner", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	return repo, job, claimed
}

func forceUpdateError(t *testing.T, db *DB) func() {
	t.Helper()
	name := "coverage:update:" + uuid.NewString()
	require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		tx.AddError(errForcedActressCoverage)
	}))
	return func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }
}

func TestActressRenameNameFieldsRemainingBranches(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	require.ErrorIs(t, repo.RenameNameFields(context.Background(), 0, "", "", ""), ErrInvalidLookup)
	require.NoError(t, db.Close())
	require.Error(t, repo.RenameNameFields(context.Background(), 1, "a", "b", "c"))
}

func TestActressSyncRecoveryAndReleaseErrorPaths(t *testing.T) {
	t.Run("recovery cannot load owning job", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		now := time.Now().UTC()
		expires := now.Add(-time.Minute)
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: "missing", DedupeKey: uuid.NewString(), Status: models.ActressSyncTaskRunning, LeaseOwner: "owner", LeaseToken: "token", LeaseExpiresAt: &expires, Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, db.Create(&task).Error)
		require.Error(t, repo.RecoverExpiredLeases(now))
	})

	t.Run("recovery transition update fails", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", task.ID).Update("lease_expires_at", time.Now().Add(-time.Hour)).Error)
		remove := forceUpdateError(t, db)
		defer remove()
		require.ErrorIs(t, repo.RecoverExpiredLeases(time.Now()), errForcedActressCoverage)
	})

	t.Run("release cannot load owning job", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		now := time.Now().UTC()
		expires := now.Add(time.Hour)
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: "missing", DedupeKey: uuid.NewString(), Status: models.ActressSyncTaskRunning, LeaseOwner: "owner", LeaseToken: "token", LeaseExpiresAt: &expires, Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, db.Create(&task).Error)
		require.Error(t, repo.ReleaseOwnerLeases("owner"))
	})

	t.Run("release transition update fails", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, _ := claimActressCoverageTask(t, db, nil)
		remove := forceUpdateError(t, db)
		defer remove()
		require.ErrorIs(t, repo.ReleaseOwnerLeases("coverage-owner"), errForcedActressCoverage)
	})

	t.Run("release retries ordinary task", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, job, _ := claimActressCoverageTask(t, db, nil)
		require.NoError(t, repo.ReleaseOwnerLeases("coverage-owner"))
		tasks, err := repo.ListTasks(job.ID)
		require.NoError(t, err)
		require.Equal(t, models.ActressSyncTaskPending, tasks[0].Status)
		require.Nil(t, tasks[0].LeaseExpiresAt)
	})
}

func TestActressSyncCompletionAndRefreshRemainingPaths(t *testing.T) {
	t.Run("preserves explicit warning", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		task.Status = models.ActressSyncTaskFailed
		task.Outcome = "failed"
		task.Warning = "resolver warning"
		task.UpdatedFields = []string{"thumb_url"}
		require.NoError(t, repo.CompleteTask(task, task.LeaseToken))
		tasks, err := repo.ListTasks(task.JobID)
		require.NoError(t, err)
		require.Equal(t, "resolver warning", tasks[0].Warning)
		require.Equal(t, "updated_with_warning", tasks[0].Outcome)
	})

	t.Run("completion query error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		require.NoError(t, db.Close())
		require.Error(t, repo.CompleteTask(task, task.LeaseToken))
	})

	t.Run("completion update error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		remove := forceUpdateError(t, db)
		defer remove()
		task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated"
		require.ErrorIs(t, repo.CompleteTask(task, task.LeaseToken), errForcedActressCoverage)
	})

	t.Run("refresh raw and job lookup errors", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, job, _ := newActressSyncJobAndTask(t, db, nil, uuid.NewString())
		require.NoError(t, db.Exec("DROP TABLE actress_sync_tasks").Error)
		require.Error(t, repo.refreshJobTx(db.DB, job.ID, time.Now()))

		db2 := newDatabaseTestDB(t)
		repo2, job2, _ := newActressSyncJobAndTask(t, db2, nil, uuid.NewString())
		require.NoError(t, db2.Exec("DROP TABLE actress_sync_jobs").Error)
		require.Error(t, repo2.refreshJobTx(db2.DB, job2.ID, time.Now()))
	})
}

func TestActressSyncMutationJournalingRemainingPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("thumbnail records task field", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		actress := &models.Actress{DMMID: 71, ThumbURL: "old"}
		require.NoError(t, actressRepo.Create(ctx, actress))
		syncRepo, _, task := claimActressCoverageTask(t, db, &actress.ID)
		replaced, err := actressRepo.ReplaceThumbnailForSyncTask(ctx, actress.ID, actress.DMMID, "old", "new", task.ID, task.LeaseToken)
		require.NoError(t, err)
		require.True(t, replaced)
		stored, err := syncRepo.ListTasks(task.JobID)
		require.NoError(t, err)
		require.Contains(t, stored[0].UpdatedFields, "thumb_url")
	})

	t.Run("thumbnail update error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		actress := &models.Actress{DMMID: 72, ThumbURL: "old"}
		require.NoError(t, repo.Create(ctx, actress))
		remove := forceUpdateError(t, db)
		defer remove()
		replaced, err := repo.ReplaceThumbnail(ctx, actress.ID, actress.DMMID, "old", "new")
		require.ErrorIs(t, err, errForcedActressCoverage)
		require.False(t, replaced)
	})

	t.Run("dmm assignment records task field", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		actressRepo := NewActressRepository(db)
		actress := &models.Actress{JapaneseName: "missing id"}
		require.NoError(t, actressRepo.Create(ctx, actress))
		syncRepo, _, task := claimActressCoverageTask(t, db, &actress.ID)
		assigned, err := actressRepo.AssignDMMIDIfMissingForSyncTask(ctx, actress.ID, 73, task.ID, task.LeaseToken)
		require.NoError(t, err)
		require.True(t, assigned)
		stored, err := syncRepo.ListTasks(task.JobID)
		require.NoError(t, err)
		require.Contains(t, stored[0].UpdatedFields, "dmm_id")
	})

	t.Run("dmm assignment update error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		actress := &models.Actress{}
		require.NoError(t, repo.Create(ctx, actress))
		remove := forceUpdateError(t, db)
		defer remove()
		assigned, err := repo.AssignDMMIDIfMissing(ctx, actress.ID, 74)
		require.ErrorIs(t, err, errForcedActressCoverage)
		require.False(t, assigned)
	})

	t.Run("record fields lease loss and database errors", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		require.NoError(t, recordSyncTaskFieldsTx(db.DB, "", "", []string{"x"}))
		require.NoError(t, recordSyncTaskFieldsTx(db.DB, "missing", "token", nil))
		require.ErrorIs(t, recordSyncTaskFieldsTx(db.DB, "missing", "token", []string{"x"}), errActressSyncLeaseLost)
		require.NoError(t, db.Close())
		require.Error(t, recordSyncTaskFieldsTx(db.DB, "task", "token", []string{"x"}))
	})
}

func TestActressMergeRemainingTransactionPaths(t *testing.T) {
	ctx := context.Background()

	t.Run("rejects third actress dmm conflict", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		target := &models.Actress{DMMID: 801, JapaneseName: "target"}
		source := &models.Actress{DMMID: 802, JapaneseName: "source"}
		third := &models.Actress{DMMID: 803, JapaneseName: "third"}
		for _, actress := range []*models.Actress{target, source, third} {
			require.NoError(t, repo.Create(ctx, actress))
		}
		// Remove the schema guard so the repository's explicit conflict check can
		// observe a legacy database containing duplicate identifiers.
		require.NoError(t, db.Exec("DROP INDEX idx_actresses_dmm_id_positive").Error)
		require.NoError(t, db.Model(source).Update("dmm_id", third.DMMID).Error)
		plan, err := repo.merger.PlanMerge(ctx, target.ID, source.ID, map[string]string{"dmm_id": MergeResolutionSource})
		require.NoError(t, err)
		_, err = repo.merger.ExecuteMerge(ctx, plan, db)
		require.ErrorIs(t, err, ErrActressMergeUniqueConstraint)
	})

	t.Run("task hook rolls transaction back", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		target := &models.Actress{JapaneseName: "target"}
		source := &models.Actress{JapaneseName: "source"}
		require.NoError(t, repo.Create(ctx, target))
		require.NoError(t, repo.Create(ctx, source))
		plan, err := repo.merger.PlanMerge(ctx, target.ID, source.ID, nil)
		require.NoError(t, err)
		_, err = repo.merger.executeMerge(ctx, plan, db, nil, func(*gorm.DB, uint, uint) error { return errForcedActressCoverage })
		require.ErrorIs(t, err, errForcedActressCoverage)
		_, err = repo.FindByID(ctx, source.ID)
		require.NoError(t, err)
	})

	t.Run("target update errors are wrapped", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		target := &models.Actress{JapaneseName: "target"}
		source := &models.Actress{JapaneseName: "source"}
		require.NoError(t, repo.Create(ctx, target))
		require.NoError(t, repo.Create(ctx, source))
		plan, err := repo.merger.PlanMerge(ctx, target.ID, source.ID, nil)
		require.NoError(t, err)
		remove := forceUpdateError(t, db)
		defer remove()
		_, err = repo.merger.ExecuteMerge(ctx, plan, db)
		require.ErrorIs(t, err, errForcedActressCoverage)
	})
}

func forceQueryErrorOnCall(t *testing.T, db *DB, call int) func() {
	t.Helper()
	name := "coverage:query:" + uuid.NewString()
	seen := 0
	require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		seen++
		if seen == call {
			tx.AddError(errForcedActressCoverage)
		}
	}))
	return func() { require.NoError(t, db.DB.Callback().Query().Remove(name)) }
}

func forceZeroRowsAfterUpdate(t *testing.T, db *DB) func() {
	t.Helper()
	name := "coverage:zero:" + uuid.NewString()
	require.NoError(t, db.DB.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) { tx.RowsAffected = 0 }))
	return func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }
}

func TestActressSyncRequeueTaskErrorBranches(t *testing.T) {
	t.Run("update error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		remove := forceUpdateError(t, db)
		defer remove()
		require.ErrorIs(t, repo.RequeueTask(task, task.LeaseToken), errForcedActressCoverage)
	})

	t.Run("fenced update", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		remove := forceZeroRowsAfterUpdate(t, db)
		defer remove()
		require.ErrorIs(t, repo.RequeueTask(task, task.LeaseToken), errActressSyncLeaseLost)
	})

	t.Run("refresh error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		name := "coverage:requeue-refresh:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "actress_sync_tasks" {
				tx.AddError(tx.Exec("DROP TABLE actress_sync_jobs").Error)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		require.Error(t, repo.RequeueTask(task, task.LeaseToken))
	})

	t.Run("job lookup error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		require.NoError(t, db.Exec("DROP TABLE actress_sync_jobs").Error)
		require.Error(t, repo.RequeueTask(task, task.LeaseToken))
	})
}

func TestActressSyncRepositoryInjectedDatabaseBranches(t *testing.T) {
	t.Run("recovery initial query and refresh fail", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		remove := forceQueryErrorOnCall(t, db, 1)
		require.ErrorIs(t, repo.RecoverExpiredLeases(time.Now()), errForcedActressCoverage)
		remove()

		repo, _, task := claimActressCoverageTask(t, db, nil)
		require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", task.ID).Update("lease_expires_at", time.Now().Add(-time.Hour)).Error)
		name := "coverage:refresh:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "actress_sync_jobs" {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		require.ErrorIs(t, repo.RecoverExpiredLeases(time.Now()), errForcedActressCoverage)
	})

	t.Run("release initial query nil expiry and refresh fail", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		remove := forceQueryErrorOnCall(t, db, 1)
		require.ErrorIs(t, repo.ReleaseOwnerLeases("owner"), errForcedActressCoverage)
		remove()

		repo, job, task := claimActressCoverageTask(t, db, nil)
		require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", task.ID).Update("lease_expires_at", nil).Error)
		require.NoError(t, repo.ReleaseOwnerLeases("coverage-owner"))
		stored, err := repo.ListTasks(job.ID)
		require.NoError(t, err)
		require.Equal(t, models.ActressSyncTaskPending, stored[0].Status)

		repo, _, _ = claimActressCoverageTask(t, db, nil)
		name := "coverage:release-refresh:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "actress_sync_jobs" {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		require.ErrorIs(t, repo.ReleaseOwnerLeases("coverage-owner"), errForcedActressCoverage)
	})

	t.Run("complete loses lease between query and update", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		remove := forceZeroRowsAfterUpdate(t, db)
		defer remove()
		task.Status = models.ActressSyncTaskCompleted
		require.ErrorIs(t, repo.CompleteTask(task, task.LeaseToken), errActressSyncLeaseLost)
	})
}

func TestActressSyncReassignmentInjectedBranches(t *testing.T) {
	newPair := func(t *testing.T) (*DB, *ActressSyncRepository, *models.ActressSyncTask, *models.Actress, *models.Actress) {
		t.Helper()
		db := newDatabaseTestDB(t)
		ar := NewActressRepository(db)
		from, to := &models.Actress{JapaneseName: "from"}, &models.Actress{JapaneseName: "to"}
		require.NoError(t, ar.Create(context.Background(), from))
		require.NoError(t, ar.Create(context.Background(), to))
		repo, _, task := claimActressCoverageTask(t, db, &from.ID)
		return db, repo, task, from, to
	}

	t.Run("task query error", func(t *testing.T) {
		db, repo, task, from, to := newPair(t)
		remove := forceQueryErrorOnCall(t, db, 1)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})

	t.Run("conflict query error", func(t *testing.T) {
		db, repo, task, from, to := newPair(t)
		remove := forceQueryErrorOnCall(t, db, 2)
		defer remove()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
	})

	t.Run("conflict update and refresh errors", func(t *testing.T) {
		db, repo, task, from, to := newPair(t)
		now := time.Now().UTC()
		conflict := models.ActressSyncTask{ID: uuid.NewString(), JobID: task.JobID, ActressID: &to.ID, DedupeKey: "actress:" + fmt.Sprint(to.ID), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, db.Create(&conflict).Error)
		remove := forceUpdateError(t, db)
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
		remove()

		require.NoError(t, db.Model(&models.ActressSyncTask{}).Where("id = ?", conflict.ID).Update("job_id", "missing-job").Error)
		require.Error(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID))
	})

	t.Run("final update error and fenced row", func(t *testing.T) {
		db, repo, task, from, to := newPair(t)
		remove := forceUpdateError(t, db)
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID), errForcedActressCoverage)
		remove()

		zero := forceZeroRowsAfterUpdate(t, db)
		defer zero()
		require.ErrorIs(t, repo.reassignTaskActressTx(db.DB, task.ID, task.LeaseToken, to.ID, from.ID), errActressSyncLeaseLost)
	})
}

func TestActressSyncMutationLeaseAndJournalErrors(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T, dmmID int) (*DB, *ActressRepository, *models.Actress, *models.ActressSyncTask) {
		t.Helper()
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		actress := &models.Actress{DMMID: dmmID, ThumbURL: "old"}
		require.NoError(t, repo.Create(ctx, actress))
		_, _, task := claimActressCoverageTask(t, db, &actress.ID)
		return db, repo, actress, task
	}

	t.Run("thumbnail stale lease", func(t *testing.T) {
		_, repo, actress, task := setup(t, 901)
		replaced, err := repo.ReplaceThumbnailForSyncTask(ctx, actress.ID, actress.DMMID, "old", "new", task.ID, "stale")
		require.False(t, replaced)
		require.ErrorIs(t, err, errActressSyncLeaseLost)
	})

	t.Run("thumbnail journal update error", func(t *testing.T) {
		db, repo, actress, task := setup(t, 902)
		name := "coverage:second-update:" + uuid.NewString()
		seen := 0
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			seen++
			if seen == 2 {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		_, err := repo.ReplaceThumbnailForSyncTask(ctx, actress.ID, actress.DMMID, "old", "new", task.ID, task.LeaseToken)
		require.ErrorIs(t, err, errForcedActressCoverage)
	})

	t.Run("dmm stale lease", func(t *testing.T) {
		_, repo, actress, task := setup(t, 0)
		assigned, err := repo.AssignDMMIDIfMissingForSyncTask(ctx, actress.ID, 903, task.ID, "stale")
		require.False(t, assigned)
		require.ErrorIs(t, err, errActressSyncLeaseLost)
	})

	t.Run("journal direct update errors and fencing", func(t *testing.T) {
		db, _, _, task := setup(t, 904)
		remove := forceUpdateError(t, db)
		require.ErrorIs(t, recordSyncTaskFieldsTx(db.DB, task.ID, task.LeaseToken, []string{"x"}), errForcedActressCoverage)
		remove()
		zero := forceZeroRowsAfterUpdate(t, db)
		defer zero()
		require.ErrorIs(t, recordSyncTaskFieldsTx(db.DB, task.ID, task.LeaseToken, []string{"x"}), errActressSyncLeaseLost)
	})
}

func TestActressMergeInjectedErrorPaths(t *testing.T) {
	ctx := context.Background()
	pair := func(t *testing.T) (*DB, *ActressRepository, *MergePlan) {
		t.Helper()
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		target, source := &models.Actress{DMMID: 1001, JapaneseName: "target"}, &models.Actress{DMMID: 1002, JapaneseName: "source"}
		require.NoError(t, repo.Create(ctx, target))
		require.NoError(t, repo.Create(ctx, source))
		plan, err := repo.merger.PlanMerge(ctx, target.ID, source.ID, nil)
		require.NoError(t, err)
		return db, repo, plan
	}

	t.Run("invalid precomputed resolution", func(t *testing.T) {
		db, repo, plan := pair(t)
		plan.Resolutions["dmm_id"] = "invalid"
		_, err := repo.merger.ExecuteMerge(ctx, plan, db)
		require.Error(t, err)
	})

	t.Run("dmm lookup query error", func(t *testing.T) {
		db, repo, plan := pair(t)
		remove := forceQueryErrorOnCall(t, db, 3)
		defer remove()
		_, err := repo.merger.ExecuteMerge(ctx, plan, db)
		require.ErrorIs(t, err, errForcedActressCoverage)
	})

	t.Run("duplicated target update", func(t *testing.T) {
		db, repo, plan := pair(t)
		name := "coverage:duplicate:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) { tx.AddError(gorm.ErrDuplicatedKey) }))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		_, err := repo.merger.ExecuteMerge(ctx, plan, db)
		require.ErrorIs(t, err, ErrActressMergeUniqueConstraint)
	})

	t.Run("delete error", func(t *testing.T) {
		db, repo, plan := pair(t)
		name := "coverage:delete:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Delete().Before("gorm:delete").Register(name, func(tx *gorm.DB) { tx.AddError(errForcedActressCoverage) }))
		defer func() { require.NoError(t, db.DB.Callback().Delete().Remove(name)) }()
		_, err := repo.merger.ExecuteMerge(ctx, plan, db)
		require.ErrorIs(t, err, errForcedActressCoverage)
	})
}

func TestActressSyncCreateClaimAndTransitionInjectedBranches(t *testing.T) {
	t.Run("create job and task failures", func(t *testing.T) {
		for _, failAt := range []int{1, 2, 3} {
			db := newDatabaseTestDB(t)
			repo := NewActressSyncRepository(db)
			now := time.Now().UTC()
			job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, CreatedAt: now}
			key := "duplicate:" + uuid.NewString()
			tasks := []models.ActressSyncTask{
				{ID: uuid.NewString(), JobID: job.ID, DedupeKey: key, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now},
				{ID: uuid.NewString(), JobID: job.ID, DedupeKey: key, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now},
			}
			name := "coverage:create:" + uuid.NewString()
			seen := 0
			require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
				seen++
				if seen == failAt {
					tx.AddError(errForcedActressCoverage)
				}
			}))
			err := repo.CreateJob(job, tasks)
			require.Error(t, err)
			require.NoError(t, db.DB.Callback().Create().Remove(name))
		}
	})

	t.Run("create refresh failure", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		now := time.Now().UTC()
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, CreatedAt: now}
		name := "coverage:create-refresh:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) { tx.AddError(errForcedActressCoverage) }))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		require.ErrorIs(t, repo.CreateJob(job, nil), errForcedActressCoverage)
	})

	t.Run("claim reload and job update failures", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, _ := newActressSyncJobAndTask(t, db, nil, uuid.NewString())
		removeQuery := forceQueryErrorOnCall(t, db, 2)
		_, err := repo.ClaimNext("owner", time.Now().Add(time.Hour))
		require.ErrorIs(t, err, errForcedActressCoverage)
		removeQuery()

		db2 := newDatabaseTestDB(t)
		repo2, _, _ := newActressSyncJobAndTask(t, db2, nil, uuid.NewString())
		name := "coverage:claim-job:" + uuid.NewString()
		seen := 0
		require.NoError(t, db2.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			seen++
			if seen == 2 {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db2.DB.Callback().Update().Remove(name)) }()
		_, err = repo2.ClaimNext("owner", time.Now().Add(time.Hour))
		require.ErrorIs(t, err, errForcedActressCoverage)
	})

	t.Run("successful heartbeat and stage", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		require.NoError(t, repo.Heartbeat(task.ID, task.LeaseToken, time.Now().Add(2*time.Hour)))
		require.NoError(t, repo.UpdateStage(task.ID, task.LeaseToken, "saving"))
	})
}

func TestActressSyncCancelAndMutationAdditionalErrors(t *testing.T) {
	t.Run("cancel update and count errors", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, job, _ := newActressSyncJobAndTask(t, db, nil, uuid.NewString())
		remove := forceUpdateError(t, db)
		require.ErrorIs(t, repo.CancelJob(job.ID), errForcedActressCoverage)
		remove()

		require.NoError(t, db.Model(&models.ActressSyncJob{}).Where("id = ?", job.ID).Update("status", models.ActressSyncJobCompleted).Error)
		removeQuery := forceQueryErrorOnCall(t, db, 1)
		require.ErrorIs(t, repo.CancelJob(job.ID), errForcedActressCoverage)
		removeQuery()
	})

	t.Run("cancel pending task update error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, job, _ := newActressSyncJobAndTask(t, db, nil, uuid.NewString())
		name := "coverage:cancel-task:" + uuid.NewString()
		seen := 0
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			seen++
			if seen == 2 {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		require.ErrorIs(t, repo.CancelJob(job.ID), errForcedActressCoverage)
	})

	t.Run("thumbnail no match and lease query error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		actress := &models.Actress{DMMID: 1101, ThumbURL: "actual"}
		require.NoError(t, repo.Create(context.Background(), actress))
		replaced, err := repo.ReplaceThumbnail(context.Background(), actress.ID, actress.DMMID, "other", "new")
		require.NoError(t, err)
		require.False(t, replaced)
		_, _, task := claimActressCoverageTask(t, db, &actress.ID)
		remove := forceQueryErrorOnCall(t, db, 1)
		_, err = repo.ReplaceThumbnailForSyncTask(context.Background(), actress.ID, actress.DMMID, "actual", "new", task.ID, task.LeaseToken)
		require.ErrorIs(t, err, errForcedActressCoverage)
		remove()
	})
}

type finalLookupFailingActressRepository struct {
	ActressRepositoryInterface
}

func (finalLookupFailingActressRepository) FindByID(context.Context, uint) (*models.Actress, error) {
	return nil, errForcedActressCoverage
}

func TestActressFinalFeasibleErrorBranches(t *testing.T) {
	t.Run("duplicate fallback create fails", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		now := time.Now().UTC()
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, CreatedAt: now}
		key := "duplicate:" + uuid.NewString()
		tasks := []models.ActressSyncTask{
			{ID: uuid.NewString(), JobID: job.ID, DedupeKey: key, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now},
			{ID: uuid.NewString(), JobID: job.ID, DedupeKey: key, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now},
		}
		name := "coverage:fallback-create:" + uuid.NewString()
		seen := 0
		require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			seen++
			if seen == 4 {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Create().Remove(name)) }()
		require.ErrorIs(t, repo.CreateJob(job, tasks), errForcedActressCoverage)
	})

	t.Run("claim update fails", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, _ := newActressSyncJobAndTask(t, db, nil, uuid.NewString())
		remove := forceUpdateError(t, db)
		defer remove()
		_, err := repo.ClaimNext("owner", time.Now().Add(time.Hour))
		require.ErrorIs(t, err, errForcedActressCoverage)
	})

	t.Run("merge for task planning fails", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		_, err := NewActressRepository(db).MergeForSyncTask(context.Background(), 0, 1, nil, "task", "token")
		require.ErrorIs(t, err, ErrActressMergeInvalidID)
	})

	t.Run("complete current task query fails", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		remove := forceQueryErrorOnCall(t, db, 1)
		defer remove()
		require.ErrorIs(t, repo.CompleteTask(task, task.LeaseToken), errForcedActressCoverage)
	})

	t.Run("fill metadata update thumbnail and reload fail", func(t *testing.T) {
		for _, failAt := range []int{1, 2} {
			db := newDatabaseTestDB(t)
			repo := NewActressRepository(db)
			actress := &models.Actress{DMMID: 1200 + failAt}
			require.NoError(t, repo.Create(context.Background(), actress))
			name := "coverage:fill-update:" + uuid.NewString()
			seen := 0
			require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
				seen++
				if seen == failAt {
					tx.AddError(errForcedActressCoverage)
				}
			}))
			_, err := repo.FillBlankMetadata(context.Background(), actress.ID, actress.DMMID, models.ActressInfo{DMMID: actress.DMMID, FirstName: "first", ThumbURL: "new"})
			require.ErrorIs(t, err, errForcedActressCoverage)
			require.NoError(t, db.DB.Callback().Update().Remove(name))
		}

		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		actress := &models.Actress{DMMID: 1203}
		require.NoError(t, repo.Create(context.Background(), actress))
		remove := forceQueryErrorOnCall(t, db, 2)
		defer remove()
		_, err := repo.FillBlankMetadata(context.Background(), actress.ID, actress.DMMID, models.ActressInfo{DMMID: actress.DMMID, FirstName: "first"})
		require.ErrorIs(t, err, errForcedActressCoverage)
	})

	t.Run("merge final repository lookup fails", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		target, source := &models.Actress{JapaneseName: "target"}, &models.Actress{JapaneseName: "source"}
		require.NoError(t, repo.Create(context.Background(), target))
		require.NoError(t, repo.Create(context.Background(), source))
		plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, nil)
		require.NoError(t, err)
		repo.merger.repo = finalLookupFailingActressRepository{ActressRepositoryInterface: repo}
		_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
		require.ErrorIs(t, err, errForcedActressCoverage)
	})
}
