package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Audit P1: CompleteTask must never write a non-terminal status — pending
// with spent attempts is unclaimable and the job would never terminate.
func TestCompleteTaskInvalidTerminalStatusSettlesFailed(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo, job, claimed := claimActressCoverageTask(t, db, nil)
	claimed.Status = models.ActressSyncTaskPending // caller bug on purpose
	claimed.Outcome = ""
	require.NoError(t, repo.CompleteTask(claimed, claimed.LeaseToken))
	stored, err := repo.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncTaskFailed, stored[0].Status)
	require.Equal(t, "invalid_terminal_status", stored[0].ErrorMessage)
	storedJob, err := repo.FindJob(job.ID)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncJobCompleted, storedJob.Status, "job must terminate")
}

// Audit P2: the repo is the cap backstop — a fourth stale retry settles
// failed instead of resurrecting attempts forever.
func TestStaleRetryCapTerminal(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo, job, first := claimActressCoverageTask(t, db, nil)
	ctx := context.Background()
	claimed := first
	for i := 0; i < actressSyncStaleRetryCap; i++ {
		count, err := repo.RequeueTask(ctx, claimed.ID, claimed.LeaseToken, ActressSyncRequeueOptions{StaleRetry: true})
		require.NoError(t, err)
		require.Equal(t, i+1, count)
		next, err := repo.ClaimNext("owner", time.Now().UTC().Add(time.Hour))
		require.NoError(t, err)
		require.NotNil(t, next)
		claimed = next
	}
	count, err := repo.RequeueTask(ctx, claimed.ID, claimed.LeaseToken, ActressSyncRequeueOptions{StaleRetry: true})
	require.NoError(t, err)
	require.Equal(t, actressSyncStaleRetryCap, count, "counter is not bumped past the cap")
	stored, err := repo.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Equal(t, models.ActressSyncTaskFailed, stored[0].Status)
	require.Equal(t, "stale_retry_cap_reached", stored[0].ErrorMessage)
}

// Audit P2: a primary-key collision must surface the real unique error, not a
// misleading dedupe lookup failure.
func TestCreateJobPKCollisionReturnsUniqueError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo, job, first := newActressSyncJobAndTask(t, db, nil, "actress:pk:"+uuid.NewString())
	_ = job
	job2 := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: "selected", CreatedAt: time.Now().UTC()}
	// Same task PK, different dedupe key, correctly bound to job2.
	dupe := models.ActressSyncTask{ID: first.ID, JobID: job2.ID, Label: "dupe", DedupeKey: "actress:other:" + uuid.NewString(), Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: time.Now().UTC()}
	err := repo.CreateJob(job2, []models.ActressSyncTask{dupe})
	require.Error(t, err)
	require.True(t, IsUniqueConstraint(err) || strings.Contains(err.Error(), "UNIQUE"), "must surface the unique violation: %v", err)
}

// Audit P2: ExecuteMerge re-validates the plan — hand-built self/zero merges
// must be rejected without touching rows.
func TestExecuteMergeRejectsSelfOrZeroIDs(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	actress := &models.Actress{JapaneseName: "x"}
	require.NoError(t, repo.Create(context.Background(), actress))
	for _, plan := range []*MergePlan{
		{TargetID: 0, SourceID: actress.ID},
		{TargetID: actress.ID, SourceID: 0},
		{TargetID: actress.ID, SourceID: actress.ID},
	} {
		_, err := repo.merger.ExecuteMerge(context.Background(), plan, db)
		require.ErrorIs(t, err, ErrInvalidLookup)
	}
}

// Audit P2: Delete removes movie_actresses join rows — FK pragma is off and
// the table has no ON DELETE action, so they would dangle.
func TestDeleteRemovesMovieActressLinks(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	actress := &models.Actress{JapaneseName: "linked"}
	require.NoError(t, repo.Create(ctx, actress))
	require.NoError(t, db.Exec("INSERT INTO movies (content_id, title, created_at, updated_at) VALUES (?, ?, ?, ?)", "mv-1", "m", time.Now().UTC(), time.Now().UTC()).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", "mv-1", actress.ID).Error)

	require.NoError(t, repo.Delete(ctx, actress.ID))
	var left int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM movie_actresses WHERE actress_id = ?", actress.ID).Scan(&left).Error)
	require.Zero(t, left, "join rows must not dangle past Delete")
}

// forceZeroRowsOnTableUpdate zeroes RowsAffected on the n-th UPDATE of the
// given table, simulating a state change between read and write.
func forceZeroRowsOnTableUpdate(t *testing.T, db *DB, table string, n int) func() {
	t.Helper()
	name := "coverage:zero-tbl:" + uuid.NewString()
	seen := 0
	require.NoError(t, db.DB.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			seen++
			if seen == n {
				tx.RowsAffected = 0
			}
		}
	}))
	return func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }
}

// Audit P2: the merge-task mutations (skipActive / skipMerged / migrate) must
// treat a zero-row update as failure, not silent success.
func TestMergeRowsAffectedGuards(t *testing.T) {
	t.Run("skipActive guard", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		target, source := setupCanonicalWinner(t, db, true)
		remove := forceZeroRowsOnTableUpdate(t, db, "actress_sync_tasks", 1)
		defer remove()
		_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
		require.ErrorContains(t, err, "was not pending during merge")
	})

	t.Run("skipMerged guard", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		target, source := setupDeferredWinner(t, db, false)
		remove := forceZeroRowsOnTableUpdate(t, db, "actress_sync_tasks", 1)
		defer remove()
		_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
		require.ErrorContains(t, err, "was not pending during merge")
	})

	t.Run("migrate guard", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		target, source := setupCanonicalWinner(t, db, true)
		remove := forceZeroRowsOnTableUpdate(t, db, "actress_sync_tasks", 2)
		defer remove()
		_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
		require.ErrorContains(t, err, "state changed during merge")
	})
}

// Delete surfaces movie_actresses cleanup failures.
func TestDeleteMovieLinksError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	actress := &models.Actress{JapaneseName: "z"}
	require.NoError(t, repo.Create(ctx, actress))
	require.NoError(t, db.Exec("DROP TABLE movie_actresses").Error)
	require.Error(t, repo.Delete(ctx, actress.ID))
}

// Over-limit requests clamp, not collapse; unknown count views reject.
func TestListTasksLimitClamps(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo, job, _ := newActressSyncJobAndTask(t, db, nil, "actress:clamp:"+uuid.NewString())
	tasks, err := repo.ListTasks(job.ID, 5001)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	diag, err := repo.ListDiagnosticTasks(job.ID, 5001)
	require.NoError(t, err)
	require.Empty(t, diag)
	_, err = repo.CountTasks(job.ID, "bogus")
	require.ErrorIs(t, err, ErrInvalidLookup)
	for _, view := range []string{"", "active", "diagnostics"} {
		n, err := repo.CountTasks(job.ID, view)
		require.NoError(t, err)
		if view == "" {
			require.Equal(t, int64(1), n)
		}
	}
}

// Audit nit: the reassign canonical-conflict skip must not silently no-op.
func TestReassignConflictRowsAffectedGuard(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	target := &models.Actress{JapaneseName: "t"}
	source := &models.Actress{JapaneseName: "s"}
	require.NoError(t, repo.Create(ctx, target))
	require.NoError(t, repo.Create(ctx, source))
	now := time.Now().UTC()
	syncRepo := NewActressSyncRepository(db)
	// Claimed running task on the SOURCE first (oldest created_at wins claim).
	srcTask := seedJobTask(t, db, "missing", "actress:"+itoa(source.ID), &source.ID, now)
	claimed, err := syncRepo.ClaimNext("owner", now.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, srcTask.ID, claimed.ID)
	// Canonical conflict on the TARGET (pending, equal-priority job) created
	// AFTER the claim so it cannot be claimed instead.
	seedJobTask(t, db, "missing", "actress:"+itoa(target.ID), &target.ID, now.Add(time.Second))
	// Zero RowsAffected on the conflict-skip update (1st tasks update in the tx).
	remove := forceZeroRowsOnTableUpdate(t, db, "actress_sync_tasks", 1)
	defer remove()
	_, err = repo.MergeForSyncTask(ctx, target.ID, source.ID, nil, claimed.ID, claimed.LeaseToken)
	require.ErrorContains(t, err, "was not pending during reassign")
}

// Codex P2: FindJob maps unknown/pruned IDs to ErrNotFound so API callers
// can 404 instead of misclassifying as a database failure — same convention
// as BaseRepository.FindByID.
func TestFindJobNotFoundSentinel(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	_, err := repo.FindJob(uuid.NewString())
	require.ErrorIs(t, err, ErrNotFound)
	require.NotErrorIs(t, err, gorm.ErrRecordNotFound, "raw gorm sentinel must not leak once mapped")
	// Fresh-terminal-pruned jobs behave the same way.
	prunedJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobCompleted, Scope: "selected", CreatedAt: time.Now().UTC()}
	require.NoError(t, repo2CreateTerminal(t, repo, prunedJob))
	_, err = repo.FindJob(prunedJob.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

// repo2CreateTerminal inserts 21 terminal jobs so the oldest is pruned.
func repo2CreateTerminal(t *testing.T, repo *ActressSyncRepository, target *models.ActressSyncJob) error {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, dbCreateTerminal(t, repo.db, target, now))
	return nil
}

func dbCreateTerminal(t *testing.T, db *DB, job *models.ActressSyncJob, now time.Time) error {
	t.Helper()
	for i := 0; i < 21; i++ {
		j := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobCompleted, Scope: "missing", CreatedAt: now.Add(time.Duration(i) * time.Second), CompletedAt: &now}
		require.NoError(t, db.Create(j).Error)
		require.NoError(t, repo3Refresh(t, db, j.ID, now))
	}
	return nil
}

func repo3Refresh(t *testing.T, db *DB, jobID string, now time.Time) error {
	t.Helper()
	return db.Transaction(func(tx *gorm.DB) error {
		return NewActressSyncRepository(db).refreshJobTx(tx, jobID, now)
	})
}
