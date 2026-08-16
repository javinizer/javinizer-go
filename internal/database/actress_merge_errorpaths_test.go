package database

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// forceUpdateErrorOnTable fails the n-th gorm UPDATE against table inside the
// current test — a deterministic mid-transaction error injector.
func forceUpdateErrorOnTable(t *testing.T, db *DB, table string, n int) func() {
	t.Helper()
	name := "coverage:upd:" + uuid.NewString()
	seen := 0
	require.NoError(t, db.DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			seen++
			if seen == n {
				tx.AddError(errForcedActressCoverage)
			}
		}
	}))
	return func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }
}

// CompleteTask: a parent-job lookup failure after the lease fence is an error,
// not a lease loss.
func TestCompleteTaskJobLookupError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo, _, task := claimActressCoverageTask(t, db, nil)
	require.NoError(t, db.Exec("DROP TABLE actress_sync_jobs").Error)
	require.Error(t, repo.CompleteTask(task, task.LeaseToken))
}

// ensureSyncTaskLeaseTx: job-cancellation count failure propagates.
func TestEnsureLeaseJobCountError(t *testing.T) {
	db := newDatabaseTestDB(t)
	actress := &models.Actress{DMMID: 78, JapaneseName: "x"}
	require.NoError(t, NewActressRepository(db).Create(context.Background(), actress))
	_, _, task := claimActressCoverageTask(t, db, &actress.ID)
	require.NoError(t, db.Exec("DROP TABLE actress_sync_jobs").Error)
	_, err := NewActressRepository(db).AssignDMMIDIfMissingForSyncTask(context.Background(), actress.ID, 99, task.ID, task.LeaseToken)
	require.Error(t, err)
}

// executeMerge: the sync-task re-anchor update error is wrapped+returned.
func TestMergeReanchorSyncTaskReferenceError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	ctx := context.Background()
	target := &models.Actress{JapaneseName: "t"}
	source := &models.Actress{JapaneseName: "s"}
	require.NoError(t, repo.Create(ctx, target))
	require.NoError(t, repo.Create(ctx, source))
	remove := forceUpdateErrorOnTable(t, db, "actress_sync_tasks", 1)
	defer remove()
	_, err := repo.Merge(ctx, target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}

// seedJobTask creates a job + single pending task for scenario tests.
func seedJobTask(t *testing.T, db *DB, scope, key string, actressID *uint, createdAt time.Time) models.ActressSyncTask {
	t.Helper()
	repo := NewActressSyncRepository(db)
	job := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: scope, CreatedAt: createdAt}
	task := models.ActressSyncTask{ID: uuid.NewString(), JobID: job.ID, ActressID: actressID, Label: key, DedupeKey: key, Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: createdAt}
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	return task
}

func seedMergeActresses(t *testing.T, db *DB) (target, source *models.Actress) {
	t.Helper()
	repo := NewActressRepository(db)
	target = &models.Actress{JapaneseName: "t"}
	source = &models.Actress{JapaneseName: "s"}
	require.NoError(t, repo.Create(context.Background(), target))
	require.NoError(t, repo.Create(context.Background(), source))
	return target, source
}

// supersedeCancelledDedupeHolderTx: a running task of a cancel-requested job
// holding the canonical key is renamed aside so the merge can claim the key.
func setupSupersedeScenario(t *testing.T, db *DB) (target, source *models.Actress) {
	t.Helper()
	target, source = seedMergeActresses(t, db)
	now := time.Now().UTC()
	// Canonical holder: running task on target inside a job that is then cancelled.
	taskH := seedJobTask(t, db, "missing", "actress:"+itoa(target.ID), &target.ID, now)
	syncRepo := NewActressSyncRepository(db)
	claimed, err := syncRepo.ClaimNext("owner", now.Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, taskH.ID, claimed.ID)
	require.NoError(t, syncRepo.CancelJob(jobIDOf(t, db, taskH)))
	// Source pending task in a live job.
	seedJobTask(t, db, "missing", "actress:"+itoa(source.ID), &source.ID, now.Add(time.Second))
	return target, source
}

func itoa(v uint) string { return fmt.Sprintf("%d", v) }

func jobIDOf(t *testing.T, db *DB, task models.ActressSyncTask) string {
	t.Helper()
	var row models.ActressSyncTask
	require.NoError(t, db.First(&row, "id = ?", task.ID).Error)
	return row.JobID
}

func TestSupersedeCancelledCanonicalHolder(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupSupersedeScenario(t, db)
	require.NoError(t, func() error {
		_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
		return err
	}())
	var holders []models.ActressSyncTask
	require.NoError(t, db.Where("actress_id = ?", target.ID).Find(&holders).Error)
	superseded := false
	for _, h := range holders {
		if strings.Contains(h.DedupeKey, ":superseded:") {
			superseded = true
		}
	}
	require.True(t, superseded, "cancelled-job holder should be renamed aside")
}

func TestSupersedeUpdateError(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupSupersedeScenario(t, db)
	remove := forceUpdateErrorOnTable(t, db, "actress_sync_tasks", 1)
	defer remove()
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}

// Deferred-winner branches: source pending -> skipMerged error; source
// running -> deferred migrate error.
func setupDeferredWinner(t *testing.T, db *DB, sourceRunning bool) (target, source *models.Actress) {
	t.Helper()
	target, source = seedMergeActresses(t, db)
	now := time.Now().UTC()
	if sourceRunning {
		syncRepo := seedJobTaskWithLease(t, db, "missing", "actress:"+itoa(source.ID), &source.ID, now)
		_ = syncRepo
	}
	deferTask := seedJobTask(t, db, "missing", "actress:"+itoa(target.ID)+":deferred:"+uuid.NewString(), &target.ID, now.Add(time.Second))
	_ = deferTask
	if !sourceRunning {
		seedJobTask(t, db, "missing", "actress:"+itoa(source.ID), &source.ID, now.Add(2*time.Second))
	}
	return target, source
}

func seedJobTaskWithLease(t *testing.T, db *DB, scope, key string, actressID *uint, createdAt time.Time) *models.ActressSyncTask {
	t.Helper()
	seedJobTask(t, db, scope, key, actressID, createdAt)
	claimed, err := NewActressSyncRepository(db).ClaimNext("owner", createdAt.Add(time.Hour))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	return claimed
}

func TestDeferredWinnerSkipMergedError(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupDeferredWinner(t, db, false)
	remove := forceUpdateErrorOnTable(t, db, "actress_sync_tasks", 1)
	defer remove()
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}

func TestDeferredWinnerMigrateError(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupDeferredWinner(t, db, true)
	remove := forceUpdateErrorOnTable(t, db, "actress_sync_tasks", 1)
	defer remove()
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}

// Canonical winner, running higher-priority source: winner skip + migrate errors.
func setupCanonicalWinner(t *testing.T, db *DB, sourceRunning bool) (target, source *models.Actress) {
	t.Helper()
	target, source = seedMergeActresses(t, db)
	now := time.Now().UTC()
	if sourceRunning {
		seedJobTaskWithLease(t, db, "selected", "actress:"+itoa(source.ID), &source.ID, now)
		seedJobTask(t, db, "missing", "actress:"+itoa(target.ID), &target.ID, now.Add(time.Second))
	} else {
		seedJobTask(t, db, "missing", "actress:"+itoa(target.ID), &target.ID, now)
		seedJobTask(t, db, "selected", "actress:"+itoa(source.ID), &source.ID, now.Add(time.Second))
	}
	return target, source
}

func TestCanonicalWinnerSkipActiveError(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupCanonicalWinner(t, db, true)
	remove := forceUpdateErrorOnTable(t, db, "actress_sync_tasks", 1)
	defer remove()
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}

func TestCanonicalWinnerMigrateError(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupCanonicalWinner(t, db, true)
	remove := forceUpdateErrorOnTable(t, db, "actress_sync_tasks", 2)
	defer remove()
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}

func TestPendingHigherPrioritySkipActiveError(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupCanonicalWinner(t, db, false)
	remove := forceUpdateErrorOnTable(t, db, "actress_sync_tasks", 1)
	defer remove()
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}

func TestPendingHigherPriorityMigrateError(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupCanonicalWinner(t, db, false)
	remove := forceUpdateErrorOnTable(t, db, "actress_sync_tasks", 2)
	defer remove()
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}

// Deferred winner + running source: clean migrate settles the deferred key.
func TestDeferredWinnerMigrateHappyPath(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupDeferredWinner(t, db, true)
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.NoError(t, err)
}

// supersede holder lookup: a non-NotFound query failure propagates.
func TestSupersedeHolderQueryError(t *testing.T) {
	db := newDatabaseTestDB(t)
	target, source := setupSupersedeScenario(t, db)
	// Register-after-setup: the holder First is the 7th query inside Merge.
	remove := forceQueryErrorOnCall(t, db, 9)
	defer remove()
	_, err := NewActressRepository(db).Merge(context.Background(), target.ID, source.ID, nil)
	require.ErrorIs(t, err, errForcedActressCoverage)
}
