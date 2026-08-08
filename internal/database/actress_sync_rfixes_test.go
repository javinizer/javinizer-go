package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// R8: heartbeats extend lease_expires_at but never rotate the lease_token, so
// a requeue fenced on the original token still matches after a heartbeat.
func TestRequeueHeartbeatFence(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo, job, task := claimActressCoverageTask(t, db, nil)
	original := task.LeaseToken
	require.NotEmpty(t, original)

	// Heartbeat with the original token: expiry moves out, token untouched.
	newExpiry := time.Now().UTC().Add(2 * time.Hour)
	require.NoError(t, repo.Heartbeat(task.ID, original, newExpiry))
	stored, err := repo.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Equal(t, original, stored[0].LeaseToken, "heartbeat must never rotate the lease token")

	// Wrong token is fenced out even though the lease is live.
	_, err = repo.RequeueTask(context.Background(), task.ID, uuid.NewString(), ActressSyncRequeueOptions{})
	require.ErrorIs(t, err, ErrActressSyncLeaseLost)

	// The original (unrotated) token still fences the requeue.
	_, err = repo.RequeueTask(context.Background(), task.ID, original, ActressSyncRequeueOptions{})
	require.NoError(t, err)
}

// R8: StaleRetry increments the persisted counter and returns it without
// consuming the attempt; ConsumeAttempt keeps the spent attempt.
func TestRequeueStaleRetryCounter(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo, job, fresh := claimActressCoverageTask(t, db, nil)
	ctx := context.Background()

	count, err := repo.RequeueTask(ctx, fresh.ID, fresh.LeaseToken, ActressSyncRequeueOptions{StaleRetry: true})
	require.NoError(t, err)
	require.Equal(t, 1, count, "first stale retry returns post-increment count")

	stored, err := repo.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Equal(t, 1, stored[0].StaleRetryCount, "counter is persisted")
	require.Equal(t, models.ActressSyncTaskPending, stored[0].Status)

	// Second stale retry: counter increments again, attempt is handed back.
	again, err := repo.ClaimNext("owner", time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, again.Attempts, "stale requeue handed the attempt back")
	count, err = repo.RequeueTask(ctx, again.ID, again.LeaseToken, ActressSyncRequeueOptions{StaleRetry: true})
	require.NoError(t, err)
	require.Equal(t, 2, count)
	stored, err = repo.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Equal(t, 0, stored[0].Attempts, "StaleRetry must hand the attempt back")

	// ConsumeAttempt keeps the spent attempt.
	third, err := repo.ClaimNext("owner", time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, 1, third.Attempts)
	_, err = repo.RequeueTask(ctx, third.ID, third.LeaseToken, ActressSyncRequeueOptions{ConsumeAttempt: true})
	require.NoError(t, err)
	stored, err = repo.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Equal(t, 1, stored[0].Attempts, "ConsumeAttempt must keep the spent attempt")
}

// DBC-03: the candidate pre-filter delegates to the registered
// javinizer_missing_actress_thumbnail SQL function — a now_printing thumb is
// a candidate, an arbitrary ext-less actjpgs path is not special-cased here.
func TestListSyncCandidatesUsesRegisteredThumbnailFunction(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	candidate := &models.Actress{DMMID: 101, JapaneseName: "\u5973\u512a", ThumbURL: "https://pics.dmm.co.jp/mono/noimage/now_printing.jpg"}
	complete := &models.Actress{DMMID: 102, JapaneseName: "\u5973\u512a\u4e8c", FirstName: "A", LastName: "B", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/abc.jpg"}
	noDMM := &models.Actress{JapaneseName: "\u30c0\u30df\u30fc"}
	blank := &models.Actress{}
	for _, a := range []*models.Actress{candidate, complete, noDMM, blank} {
		require.NoError(t, repo.Create(ctx, a))
	}

	list, err := repo.ListSyncCandidates(ctx)
	require.NoError(t, err)
	ids := make(map[uint]bool, len(list))
	for _, a := range list {
		ids[a.ID] = true
	}
	require.True(t, ids[candidate.ID], "now_printing thumb must be a candidate via the SQL function")
	require.True(t, ids[noDMM.ID], "DMM-less row with a name is a candidate")
	require.False(t, ids[complete.ID], "complete row is not a candidate")
	require.False(t, ids[blank.ID], "blank row is not a candidate")
}

// R3: paged reads share the candidate predicate, filter by the registered
// clauses, keep a stable id order, and report the full filtered total.
func TestListSyncCandidatesPagedContract(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	seeded := make([]*models.Actress, 0, 3)
	for _, a := range []*models.Actress{
		{DMMID: 201, JapaneseName: "\u5973\u512a1"},
		{DMMID: 202, JapaneseName: "\u5973\u512a2"},
		{DMMID: 203, JapaneseName: "\u5973\u512a3"},
	} {
		require.NoError(t, repo.Create(ctx, a))
		seeded = append(seeded, a)
	}
	require.NoError(t, repo.Create(ctx, &models.Actress{}))

	page1, total, err := repo.ListSyncCandidatesPaged(ctx, "missing_thumbnail", 2, 0)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, page1, 2)
	require.Equal(t, seeded[0].ID, page1[0].ID)
	require.Equal(t, seeded[1].ID, page1[1].ID)

	page2, total, err := repo.ListSyncCandidatesPaged(ctx, "missing_thumbnail", 2, 2)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, page2, 1)
	require.Equal(t, seeded[2].ID, page2[0].ID)

	// limit <= 0 returns the full window; unknown filter is an error.
	all, total, err := repo.ListSyncCandidatesPaged(ctx, "", 0, 0)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, all, 3)
	_, _, err = repo.ListSyncCandidatesPaged(ctx, "no_such_filter", 10, 0)
	require.ErrorIs(t, err, ErrInvalidLookup)
}

// DBC-06: refreshing a job whose task rows are gone (or never existed) must
// not fail on NULL SUM aggregates.
func TestRefreshJobEmptyTaskSet(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobRunning, Scope: "missing", CreatedAt: now, StartedAt: &now}
	require.NoError(t, db.Create(job).Error)
	// CancelJob routes through refreshJobTx with zero task rows.
	require.NoError(t, repo.CancelJob(job.ID))
	stored, err := repo.FindJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncJobCancelled, stored.Status)
	require.Zero(t, stored.TotalTasks)
}

// DBC-02: merge moves the source's translation rows to the target; on
// (actress_id, language) collision the target row wins and the source
// duplicate is deleted (pragma FK is off, so nothing cascades).
func TestMergeMovesTranslationsTargetWins(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	target := &models.Actress{JapaneseName: "target"}
	source := &models.Actress{JapaneseName: "source"}
	require.NoError(t, repo.Create(ctx, target))
	require.NoError(t, repo.Create(ctx, source))

	seed := func(tr models.ActressTranslation) {
		require.NoError(t, db.Create(&tr).Error)
	}
	seed(models.ActressTranslation{ActressID: target.ID, Language: "en", DisplayName: "Target EN"})
	seed(models.ActressTranslation{ActressID: source.ID, Language: "en", DisplayName: "Source EN"}) // collides -> target wins
	seed(models.ActressTranslation{ActressID: source.ID, Language: "jp", DisplayName: "Source JP"}) // moves over

	plan, err := repo.merger.PlanMerge(ctx, target.ID, source.ID, nil)
	require.NoError(t, err)
	_, err = repo.merger.ExecuteMerge(ctx, plan, db)
	require.NoError(t, err)

	var rows []models.ActressTranslation
	require.NoError(t, db.Where("actress_id = ?", target.ID).Order("language").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, "en", rows[0].Language)
	require.Equal(t, "Target EN", rows[0].DisplayName, "target row wins on collision")
	require.Equal(t, "jp", rows[1].Language)
	require.Equal(t, "Source JP", rows[1].DisplayName, "non-colliding source row moved to target")

	var orphans int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", source.ID).Count(&orphans).Error)
	require.Zero(t, orphans, "no translation rows may remain on the source")
}

// DBC-02: Delete removes the actress's translation rows outright.
func TestDeleteRemovesTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	actress := &models.Actress{JapaneseName: "del"}
	require.NoError(t, repo.Create(ctx, actress))
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Del EN"}).Error)

	require.NoError(t, repo.Delete(ctx, actress.ID))
	var left int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", actress.ID).Count(&left).Error)
	require.Zero(t, left, "Delete must remove translation rows outright")
}

// moveActressTranslationsTx surfaces SQL errors to the merge caller.
func TestMoveTranslationsErrorPropagates(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	target := &models.Actress{JapaneseName: "t"}
	source := &models.Actress{JapaneseName: "s"}
	require.NoError(t, repo.Create(ctx, target))
	require.NoError(t, repo.Create(ctx, source))
	plan, err := repo.merger.PlanMerge(ctx, target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NoError(t, db.Exec("DROP TABLE actress_translations").Error)
	_, err = repo.merger.ExecuteMerge(ctx, plan, db)
	require.Error(t, err)
}

// Delete: refreshJobTx failure aborts; final actress-row delete failure
// aborts (translations delete must not be mistaken for it).
func TestDeleteRefreshAndRowErrors(t *testing.T) {
	t.Run("refresh failure", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		ctx := context.Background()
		actress := &models.Actress{JapaneseName: "x"}
		require.NoError(t, repo.Create(ctx, actress))
		now := time.Now().UTC()
		job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: now}
		task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: &actress.ID, Label: "l", DedupeKey: "dk:" + uuid.NewString(), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
		require.NoError(t, NewActressSyncRepository(db).CreateJob(job, []models.ActressSyncTask{task}))
		// Fail only the job-row update inside refreshJobTx (2nd update).
		name := "coverage:delrefresh:" + uuid.NewString()
		seen := 0
		require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
			seen++
			if seen == 2 {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }()
		require.ErrorIs(t, repo.Delete(ctx, actress.ID), errForcedActressCoverage)
	})

	t.Run("row delete failure", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		ctx := context.Background()
		actress := &models.Actress{JapaneseName: "y"}
		require.NoError(t, repo.Create(ctx, actress))
		require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actress.ID, Language: "en"}).Error)
		name := "coverage:delrow:" + uuid.NewString()
		require.NoError(t, db.DB.Callback().Delete().Before("gorm:delete").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "actresses" {
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Delete().Remove(name)) }()
		require.Error(t, repo.Delete(ctx, actress.ID))
	})
}

// RequeueTask lookup branches: unknown task, job-table failure, reselect
// failure after the fenced update.
func TestRequeueLookupBranches(t *testing.T) {
	t.Run("unknown task", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressSyncRepository(db)
		_, err := repo.RequeueTask(context.Background(), "no-such-task", "token", ActressSyncRequeueOptions{})
		require.Error(t, err)
	})

	t.Run("job lookup failure", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		require.NoError(t, db.Exec("DROP TABLE actress_sync_jobs").Error)
		_, err := repo.RequeueTask(context.Background(), task.ID, task.LeaseToken, ActressSyncRequeueOptions{})
		require.Error(t, err)
	})

	t.Run("stale counter reselect failure", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo, _, task := claimActressCoverageTask(t, db, nil)
		name := "coverage:reselect:" + uuid.NewString()
		seen := 0
		require.NoError(t, db.DB.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
			seen++
			if seen == 3 { // task First, job First, stale-count reselect
				tx.AddError(errForcedActressCoverage)
			}
		}))
		defer func() { require.NoError(t, db.DB.Callback().Query().Remove(name)) }()
		_, err := repo.RequeueTask(context.Background(), task.ID, task.LeaseToken, ActressSyncRequeueOptions{})
		require.ErrorIs(t, err, errForcedActressCoverage)
	})
}

// ListSyncCandidatesPaged: count and find error branches.
func TestPagedErrorBranches(t *testing.T) {
	t.Run("count error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		remove := forceQueryErrorOnCall(t, db, 1)
		defer remove()
		_, _, err := repo.ListSyncCandidatesPaged(context.Background(), "", 10, 0)
		require.ErrorIs(t, err, errForcedActressCoverage)
	})

	t.Run("find error", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewActressRepository(db)
		remove := forceQueryErrorOnCall(t, db, 2)
		defer remove()
		_, _, err := repo.ListSyncCandidatesPaged(context.Background(), "", 10, 0)
		require.ErrorIs(t, err, errForcedActressCoverage)
	})
}
