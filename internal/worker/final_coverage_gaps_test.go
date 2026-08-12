package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCovFinal_StopLeaseReleaseError(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := database.NewActressSyncRepository(db)
	ctx, cancel := context.WithCancel(context.Background())
	manager := &ActressSyncManager{repo: repo, owner: "test", ctx: ctx, cancel: cancel, started: true}
	_ = db.Close()
	manager.Stop()
}

func TestCovFinal_DispatchPanicAndRecovery(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	panicCount := atomic.Int32{}
	ctx, cancel := context.WithCancel(context.Background())
	manager := &ActressSyncManager{
		repo: repo, owner: "test", wake: make(chan struct{}, 1),
		recoveryInterval: 50 * time.Millisecond,
		deps: ActressSyncManagerDeps{
			DB: db,
			Snapshot: func() (*config.Config, *scraperutil.ScraperRegistry) {
				if panicCount.Add(1) == 1 {
					panic("test panic")
				}
				return nil, nil
			},
		},
	}
	manager.wg.Add(1)
	go manager.runDispatch(ctx)
	time.Sleep(500 * time.Millisecond)
	cancel()
	manager.wg.Wait()
}

func TestCovFinal_ClaimNextError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo, owner: "test"}
	_ = db.Close()
	task, err := manager.claimAndTrack(context.Background(), 60*time.Second)
	assert.Error(t, err)
	assert.Nil(t, task)
}

func TestCovFinal_CreateJobFindByIDError(t *testing.T) {
	db := newActressEditTestDB(t)
	actressRepo := database.NewActressRepository(db)
	movieRepo := database.NewMovieRepository(db)
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: actressRepo, MovieRepo: movieRepo})
	_ = db.Close()
	_, _, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{999}})
	assert.Error(t, err)
}

func TestCovFinal_RequeueStaleTaskSuccess(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo, owner: "test"}
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-stale", Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: "task-stale", JobID: "job-stale", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := repo.ClaimNext("test", now.Add(90*time.Second))
	require.NoError(t, err)
	require.NotNil(t, claimed)
	result := manager.requeueStaleTask(claimed, database.ErrActressSyncIdentityChanged)
	assert.True(t, result)
}

func TestCovFinal_RequeueStaleTaskError(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo, owner: "test"}
	task := &models.ActressSyncTask{ID: "nonexistent", LeaseToken: "badtoken"}
	result := manager.requeueStaleTask(task, database.ErrActressSyncIdentityChanged)
	assert.False(t, result)
}

type nilMultiErr struct{}

func (e *nilMultiErr) Error() string   { return "nil-multi" }
func (e *nilMultiErr) Unwrap() []error { return []error{nil} }

type dupMultiErr struct{ e error }

func (e *dupMultiErr) Error() string   { return "dup-multi" }
func (e *dupMultiErr) Unwrap() []error { return []error{e.e, e.e} }

func TestCovFinal_IsRetryableNilAndDup(t *testing.T) {
	assert.False(t, isRetryableActressSyncError(&nilMultiErr{}))
	assert.False(t, isRetryableActressSyncError(&dupMultiErr{e: errors.New("test")}))
}

func TestCovFinal_HeartbeatSuccess(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo, owner: "test"}
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-hb", Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: "task-hb", JobID: "job-hb", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := repo.ClaimNext("test", now.Add(90*time.Second))
	require.NoError(t, err)
	require.NotNil(t, claimed)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go manager.heartbeat(ctx, claimed.ID, claimed.LeaseToken, 3*time.Second, done, func() {})
	time.Sleep(1500 * time.Millisecond)
	cancel()
	close(done)
	time.Sleep(100 * time.Millisecond)
}

func TestCovFinal_HeartbeatBackoff(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := database.NewActressSyncRepository(db)
	manager := &ActressSyncManager{repo: repo, owner: "test"}
	now := time.Now().UTC()
	job := &models.ActressSyncJob{ID: "job-hb2", Status: models.ActressSyncJobPending, Scope: "missing", CreatedAt: now}
	task := models.ActressSyncTask{ID: "task-hb2", JobID: "job-hb2", Status: models.ActressSyncTaskPending, Stage: "queued", Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, repo.CreateJob(job, []models.ActressSyncTask{task}))
	claimed, err := repo.ClaimNext("test", now.Add(90*time.Second))
	require.NoError(t, err)
	require.NotNil(t, claimed)

	_ = db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go manager.heartbeat(ctx, claimed.ID, claimed.LeaseToken, 3*time.Second, done, func() {})
	time.Sleep(2500 * time.Millisecond)
	cancel()
	close(done)
	time.Sleep(100 * time.Millisecond)
}
