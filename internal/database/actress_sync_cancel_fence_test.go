package database

import (
	"context"
	"testing"
	"time"

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
