package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Cancelling a job must abort in-flight task contexts and remember the
// request, so tasks completing afterwards settle as cancelled instead of
// mutating data and lingering on an expiring lease.
func TestCancelJobCancelsRunningTask(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	manager := NewActressSyncManager(ActressSyncManagerDeps{
		DB:          db,
		ActressRepo: database.NewActressRepository(db),
		MovieRepo:   database.NewMovieRepository(db),
	})

	job := models.ActressSyncJob{ID: "job-cancel", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)

	taskCtx, taskCancel := context.WithCancel(context.Background())
	defer taskCancel()
	manager.taskMu.Lock()
	manager.runningTasks["task-1"] = []trackedSyncTask{{jobID: job.ID, cancel: taskCancel}}
	manager.taskMu.Unlock()

	require.NoError(t, manager.CancelJob(job.ID))
	require.ErrorIs(t, taskCtx.Err(), context.Canceled)
	require.True(t, manager.isTaskCancelled("task-1"))

	var stored models.ActressSyncJob
	require.NoError(t, db.First(&stored, "id = ?", job.ID).Error)
	require.True(t, stored.CancelRequested)

	// untrack tolerates stale runs: a retry re-registered under the same task
	// ID must keep its entry (and its live context).
	manager.taskMu.Lock()
	newerCtx, newerCancel := context.WithCancel(context.Background())
	manager.runningTasks["task-1"] = []trackedSyncTask{{jobID: job.ID, cancel: newerCancel, run: 2}}
	manager.taskMu.Unlock()
	manager.untrackTask("task-1", 1)
	require.NoError(t, newerCtx.Err())
	manager.taskMu.Lock()
	runs, ok := manager.runningTasks["task-1"]
	manager.taskMu.Unlock()
	require.True(t, ok)
	require.Len(t, runs, 1)
	require.Equal(t, uint64(2), runs[0].run)
	newerCancel()
	manager.untrackTask("task-1", 2)
	manager.untrackTask("unknown", 3)
}

// Stale selections from merge-deleted actresses must not reject the whole job.
func TestCreateJobSkipsMergedAwayActresses(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	repo := database.NewActressRepository(db)
	valid := &models.Actress{JapaneseName: "still here"}
	require.NoError(t, repo.Create(context.Background(), valid))
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: repo, MovieRepo: database.NewMovieRepository(db)})

	job, err := manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{valid.ID, 999999}})
	require.NoError(t, err)
	tasks, err := manager.ListTasks(job.ID, 0)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, models.ActressSyncTaskPending, tasks[0].Status)

	_, err = manager.CreateJob(context.Background(), ActressSyncCreateRequest{Scope: "selected", ActressIDs: []uint{999998, 999999}})
	require.Error(t, err)
}

// CountTasks mirrors the task lists: view=active counts running, view=
// diagnostics counts the diagnostic set.
func TestManagerCountTasks(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	manager := NewActressSyncManager(ActressSyncManagerDeps{DB: db, ActressRepo: database.NewActressRepository(db), MovieRepo: database.NewMovieRepository(db)})

	job := models.ActressSyncJob{ID: "job-count", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	leaseUntil := time.Now().UTC().Add(time.Minute)
	mk := func(id, status, stage, outcome, warning string) models.ActressSyncTask {
		return models.ActressSyncTask{
			ID: id, JobID: job.ID, Label: id, DedupeKey: id, Status: status, Stage: stage,
			Messages: []string{}, UpdatedFields: []string{}, Warning: warning,
		}
	}
	running := mk("t-run", models.ActressSyncTaskRunning, "running", "", "")
	running.LeaseToken = "tok"
	running.LeaseExpiresAt = &leaseUntil
	done := time.Now()
	success := mk("t-ok", models.ActressSyncTaskCompleted, "completed", "updated", "")
	success.CompletedAt = &done
	flagged := mk("t-warn", models.ActressSyncTaskCompleted, "completed", "updated", "slow")
	flagged.CompletedAt = &done
	for _, task := range []*models.ActressSyncTask{&running, &success, &flagged} {
		require.NoError(t, db.Create(task).Error)
	}

	total, err := manager.CountTasks(job.ID, "")
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	active, err := manager.CountTasks(job.ID, "active")
	require.NoError(t, err)
	require.Equal(t, int64(1), active)
	diag, err := manager.CountTasks(job.ID, "diagnostics")
	require.NoError(t, err)
	require.Equal(t, int64(1), diag, "warning-bearing tasks only")
}
