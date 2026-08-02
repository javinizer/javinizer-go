package database

import (
	"context"
	"strconv"
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

// The task-scoped merge/coalesce path must also refuse post-cancel commits.
func TestReassignTaskActressFencesCancelledJob(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	job := models.ActressSyncJob{ID: "job-merge-fence", Status: models.ActressSyncJobRunning, Scope: "missing", CancelRequested: true}
	require.NoError(t, db.Create(&job).Error)
	source := &models.Actress{JapaneseName: "merge source"}
	target := &models.Actress{JapaneseName: "merge target"}
	require.NoError(t, db.Create(source).Error)
	require.NoError(t, db.Create(target).Error)

	leaseUntil := time.Now().UTC().Add(time.Minute)
	task := models.ActressSyncTask{
		ID:             "task-merge-fence",
		JobID:          job.ID,
		Label:          "task-merge-fence",
		DedupeKey:      "task-merge-fence",
		Status:         models.ActressSyncTaskRunning,
		Stage:          "running",
		Messages:       []string{},
		UpdatedFields:  []string{},
		ActressID:      &source.ID,
		LeaseToken:     "tok-merge-fence",
		LeaseExpiresAt: &leaseUntil,
	}
	require.NoError(t, db.Create(&task).Error)

	syncRepo := NewActressSyncRepository(db)
	err = db.Transaction(func(tx *gorm.DB) error {
		return syncRepo.reassignTaskActressTx(tx, task.ID, task.LeaseToken, target.ID, source.ID)
	})
	require.ErrorIs(t, err, errActressSyncJobCancelled)
}

// A cancel-requested running task must not block a fresh request for the same
// actress: its dedupe key is superseded so the new task stays runnable.
func TestCreateJobSupersedesCancelRequestedConflict(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	repo := NewActressSyncRepository(db)

	actress := &models.Actress{JapaneseName: "retry me"}
	require.NoError(t, db.Create(actress).Error)
	canonical := "actress:" + strconv.FormatUint(uint64(actress.ID), 10)

	oldJob := models.ActressSyncJob{ID: "job-cancelling", Status: models.ActressSyncJobRunning, Scope: "missing", CancelRequested: true}
	require.NoError(t, db.Create(&oldJob).Error)
	leaseUntil := time.Now().UTC().Add(time.Minute)
	oldTask := models.ActressSyncTask{
		ID: "task-old", JobID: oldJob.ID, Label: "retry me", DedupeKey: canonical,
		Status: models.ActressSyncTaskRunning, Stage: "running",
		Messages: []string{}, UpdatedFields: []string{},
		ActressID: &actress.ID, LeaseToken: "tok-old", LeaseExpiresAt: &leaseUntil,
	}
	require.NoError(t, db.Create(&oldTask).Error)

	newJob := models.ActressSyncJob{ID: "job-retry", Status: models.ActressSyncJobPending, Scope: "missing"}
	newTask := models.ActressSyncTask{
		ID: "task-new", JobID: newJob.ID, Label: "retry me", DedupeKey: canonical,
		Status: models.ActressSyncTaskPending, Stage: "queued",
		Messages: []string{}, UpdatedFields: []string{},
		ActressID: &actress.ID,
	}
	require.NoError(t, repo.CreateJob(&newJob, []models.ActressSyncTask{newTask}))

	var storedNew models.ActressSyncTask
	require.NoError(t, db.First(&storedNew, "id = ?", newTask.ID).Error)
	require.Equal(t, models.ActressSyncTaskPending, storedNew.Status)
	require.Equal(t, canonical, storedNew.DedupeKey)

	var storedOld models.ActressSyncTask
	require.NoError(t, db.First(&storedOld, "id = ?", oldTask.ID).Error)
	require.Contains(t, storedOld.DedupeKey, ":superseded:")
}

// A cancelling running source must not displace the pending canonical
// winner: keep the winner runnable and migrate the cancelled source aside.
func TestMergeMigrationKeepsCanonicalWinnerDuringCancel(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	source := &models.Actress{JapaneseName: "source"}
	canonical := &models.Actress{JapaneseName: "canonical"}
	require.NoError(t, db.Create(source).Error)
	require.NoError(t, db.Create(canonical).Error)

	leaseUntil := time.Now().UTC().Add(time.Minute)
	srcJob := models.ActressSyncJob{ID: "job-src", Status: models.ActressSyncJobRunning, Scope: "selected", CancelRequested: true}
	require.NoError(t, db.Create(&srcJob).Error)
	srcTask := models.ActressSyncTask{
		ID: "task-src", JobID: srcJob.ID, Label: "source", DedupeKey: "actress:" + strconv.FormatUint(uint64(source.ID), 10),
		Status: models.ActressSyncTaskRunning, Stage: "running",
		Messages: []string{}, UpdatedFields: []string{},
		ActressID: &source.ID, LeaseToken: "tok-src", LeaseExpiresAt: &leaseUntil,
	}
	require.NoError(t, db.Create(&srcTask).Error)

	winnerJob := models.ActressSyncJob{ID: "job-winner", Status: models.ActressSyncJobPending, Scope: "missing"}
	require.NoError(t, db.Create(&winnerJob).Error)
	winnerTask := models.ActressSyncTask{
		ID: "task-winner", JobID: winnerJob.ID, Label: "canonical", DedupeKey: "actress:" + strconv.FormatUint(uint64(canonical.ID), 10),
		Status: models.ActressSyncTaskPending, Stage: "queued",
		Messages: []string{}, UpdatedFields: []string{},
		ActressID: &canonical.ID,
	}
	require.NoError(t, db.Create(&winnerTask).Error)

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return migrateActiveActressSyncTasksTx(tx, canonical.ID, source.ID)
	}))

	var winner models.ActressSyncTask
	require.NoError(t, db.First(&winner, "id = ?", winnerTask.ID).Error)
	require.Equal(t, models.ActressSyncTaskPending, winner.Status, "canonical winner must stay runnable")

	var migrated models.ActressSyncTask
	require.NoError(t, db.First(&migrated, "id = ?", srcTask.ID).Error)
	require.Equal(t, models.ActressSyncTaskCancelled, migrated.Status)
	require.Contains(t, migrated.DedupeKey, ":deferred:")
}
