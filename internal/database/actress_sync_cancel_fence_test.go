package database

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// CompleteTask must fence on job cancellation inside its transaction: a task
// finishing after cancel_requested committed settles as cancelled, never as
// a contradictory completed/failed outcome.
func TestCompleteTaskFencesCancelledJob(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	repo := NewActressSyncRepository(db)

	job := models.ActressSyncJob{ID: "job-fence", Status: models.ActressSyncJobRunning, Scope: "missing", CancelRequested: true}
	require.NoError(t, db.Create(&job).Error)
	leaseUntil := time.Now().UTC().Add(time.Minute)
	task := models.ActressSyncTask{
		ID:             "task-fence",
		JobID:          job.ID,
		Label:          "task-fence",
		DedupeKey:      "task-fence",
		Status:         models.ActressSyncTaskRunning,
		Stage:          "running",
		Messages:       []string{},
		UpdatedFields:  []string{},
		LeaseToken:     "tok",
		LeaseExpiresAt: &leaseUntil,
	}
	require.NoError(t, db.Create(&task).Error)

	task.Status, task.Outcome = models.ActressSyncTaskCompleted, "updated"
	task.UpdatedFields = []string{"japanese_name"}
	require.NoError(t, repo.CompleteTask(&task, "tok"))

	var stored models.ActressSyncTask
	require.NoError(t, db.First(&stored, "id = ?", task.ID).Error)
	require.Equal(t, models.ActressSyncTaskCancelled, stored.Status)
	require.Equal(t, "cancelled", stored.Outcome)
}

func TestEnsureSyncTaskLeaseFencesCancelledJob(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	newLeasedTask := func(t *testing.T, jobID string) models.ActressSyncTask {
		t.Helper()
		leaseUntil := time.Now().UTC().Add(time.Minute)
		task := models.ActressSyncTask{
			ID:             "task-" + jobID,
			JobID:          jobID,
			Label:          "task-" + jobID,
			DedupeKey:      "task-" + jobID,
			Status:         models.ActressSyncTaskRunning,
			Stage:          "running",
			Messages:       []string{},
			UpdatedFields:  []string{},
			LeaseToken:     "tok-" + jobID,
			LeaseExpiresAt: &leaseUntil,
		}
		require.NoError(t, db.Create(&task).Error)
		return task
	}

	cancelled := models.ActressSyncJob{ID: "job-cancelled", Status: models.ActressSyncJobRunning, Scope: "missing", CancelRequested: true}
	require.NoError(t, db.Create(&cancelled).Error)
	active := models.ActressSyncJob{ID: "job-active", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&active).Error)
	cancelledTask := newLeasedTask(t, cancelled.ID)
	activeTask := newLeasedTask(t, active.ID)

	err = db.Transaction(func(tx *gorm.DB) error {
		return ensureSyncTaskLeaseTx(tx, cancelledTask.ID, "tok-"+cancelled.ID)
	})
	require.ErrorIs(t, err, errActressSyncJobCancelled)

	err = db.Transaction(func(tx *gorm.DB) error {
		return ensureSyncTaskLeaseTx(tx, activeTask.ID, "tok-"+active.ID)
	})
	require.NoError(t, err)

	// Empty task IDs (non-task callers) stay exempt.
	require.NoError(t, ensureSyncTaskLeaseTx(db.DB, "", ""))
}
