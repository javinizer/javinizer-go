package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCompleteTaskClosedDB(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	task := models.ActressSyncTask{ID: "t", JobID: "j", Label: "t", DedupeKey: "k", Status: models.ActressSyncTaskCompleted, Messages: []string{}, UpdatedFields: []string{}}
	now := time.Now().UTC()
	completed := now
	_ = completed
	require.NoError(t, db.Close())
	require.Error(t, repo.CompleteTask(&task, "tok"))
}

func TestEnsureSyncTaskLeaseClosedDB(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.Close())
	_, err := repo.FillBlankMetadataForSyncTask(context.Background(), 1, 1, models.ActressInfo{}, "t", "tok")
	require.Error(t, err)
}

func TestActressIDOrZero(t *testing.T) {
	require.Equal(t, uint(0), actressIDOrZero(nil))
	id := uint(31)
	require.Equal(t, id, actressIDOrZero(&id))
}

func TestCountTasks(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	job := models.ActressSyncJob{ID: "job-count", Status: models.ActressSyncJobRunning, Scope: "missing"}
	require.NoError(t, db.Create(&job).Error)
	done := time.Now().UTC()
	mkTask := func(id, status, warning string) {
		task := models.ActressSyncTask{ID: id, JobID: job.ID, Label: id, DedupeKey: id, Status: status, Stage: "x", Messages: []string{}, UpdatedFields: []string{}, Warning: warning, CompletedAt: &done}
		require.NoError(t, db.Create(&task).Error)
	}
	mkTask("t1", models.ActressSyncTaskPending, "")
	mkTask("t2", models.ActressSyncTaskRunning, "")
	mkTask("t3", models.ActressSyncTaskCancelled, "")
	mkTask("t4", models.ActressSyncTaskCompleted, "w")

	total, err := repo.CountTasks(job.ID, "")
	require.NoError(t, err)
	require.Equal(t, int64(4), total)
	active, err := repo.CountTasks(job.ID, "active")
	require.NoError(t, err)
	require.Equal(t, int64(1), active)
	diag, err := repo.CountTasks(job.ID, "diagnostics")
	require.NoError(t, err)
	require.Equal(t, int64(2), diag, "cancelled + warning-bearing")
}

func TestCountTasksClosedDBErrors(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressSyncRepository(db)
	require.NoError(t, db.Close())
	_, err := repo.CountTasks("job", "")
	require.Error(t, err)
}
