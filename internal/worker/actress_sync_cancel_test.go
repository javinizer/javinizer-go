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
	require.True(t, manager.isJobCancelled(job.ID))

	var stored models.ActressSyncJob
	require.NoError(t, db.First(&stored, "id = ?", job.ID).Error)
	require.True(t, stored.CancelRequested)

	// untrackTask tolerates entries already drained by the cancellation sweep.
	manager.untrackTask("task-1")
	manager.untrackTask("unknown")
}
