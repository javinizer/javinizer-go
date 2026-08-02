package worker

import (
	"context"
	"testing"

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
	manager.runningTasks["task-1"] = trackedSyncTask{jobID: job.ID, cancel: taskCancel}
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
	manager.runningTasks["task-1"] = trackedSyncTask{jobID: job.ID, cancel: newerCancel, run: 2}
	manager.taskMu.Unlock()
	manager.untrackTask("task-1", 1)
	require.NoError(t, newerCtx.Err())
	manager.taskMu.Lock()
	entry, ok := manager.runningTasks["task-1"]
	manager.taskMu.Unlock()
	require.True(t, ok)
	require.Equal(t, uint64(2), entry.run)
	newerCancel()
	manager.untrackTask("task-1", 2)
	manager.untrackTask("unknown", 3)
}
