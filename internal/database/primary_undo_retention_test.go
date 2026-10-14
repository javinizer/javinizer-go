package database

import (
	"context"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPrimaryUndoRetentionSkipsWholeJobsAndMalformedRecovery(t *testing.T) {
	db := newDatabaseTestDB(t)
	jobs := NewJobRepository(db)
	ops := NewBatchFileOperationRepository(db)
	old := time.Now().Add(-48 * time.Hour)
	for i := 0; i < 202; i++ {
		id := fmt.Sprintf("retention-%d", i)
		require.NoError(t, jobs.Create(context.Background(), &models.Job{ID: id, Status: models.JobStatusOrganized, StartedAt: old, OrganizedAt: &old}))
		raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryUndo: &models.PrimaryUndo{State: "copying"}})
		if i == 0 {
			raw = `{"primary_undo":`
		}
		if i == 201 {
			raw = ""
		}
		require.NoError(t, ops.Create(context.Background(), &models.BatchFileOperation{BatchJobID: id, OperationType: models.OperationTypeMove, GeneratedFiles: raw, RevertStatus: models.RevertStatusApplied}))
		require.NoError(t, ops.Create(context.Background(), &models.BatchFileOperation{BatchJobID: id, OperationType: models.OperationTypeCopy, RevertStatus: models.RevertStatusApplied}))
	}
	require.NoError(t, jobs.DeleteOrganizedOlderThan(context.Background(), time.Now().Add(-24*time.Hour)))
	var count int64
	require.NoError(t, db.Model(&models.Job{}).Count(&count).Error)
	require.Equal(t, int64(201), count)
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Count(&count).Error)
	require.Equal(t, int64(402), count)
	require.NoError(t, db.Model(&models.Job{}).Where("status != ?", models.JobStatusOrganized).Count(&count).Error)
	require.Zero(t, count)
}
func TestPrimaryUndoRetentionReleaseFailureRollsBackClaim(t *testing.T) {
	db := newDatabaseTestDB(t)
	jobs := NewJobRepository(db)
	ops := NewBatchFileOperationRepository(db)
	old := time.Now().Add(-48 * time.Hour)
	job := &models.Job{ID: "release-fault", Status: models.JobStatusOrganized, StartedAt: old, OrganizedAt: &old}
	require.NoError(t, jobs.Create(context.Background(), job))
	op := &models.BatchFileOperation{BatchJobID: job.ID, RevertStatus: models.RevertStatusApplied, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryUndo: &models.PrimaryUndo{State: "copying"}})}
	require.NoError(t, ops.Create(context.Background(), op))
	require.NoError(t, db.Exec(`CREATE TRIGGER undo_release_fault BEFORE UPDATE OF status ON jobs WHEN NEW.status = 'organized' BEGIN SELECT RAISE(ABORT, 'synthetic release failure'); END`).Error)
	require.ErrorContains(t, jobs.DeleteOrganizedOlderThan(context.Background(), time.Now().Add(-24*time.Hour)), "synthetic release failure")
	found, err := jobs.FindByID(context.Background(), job.ID)
	require.NoError(t, err)
	require.Equal(t, models.JobStatusOrganized, found.Status)
	_, err = ops.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
}
