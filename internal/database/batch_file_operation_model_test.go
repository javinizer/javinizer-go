package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchFileOperation_TableName(t *testing.T) {
	t.Parallel()
	op := models.BatchFileOperation{}
	assert.Equal(t, "batch_file_operations", op.TableName())
}

func TestBatchFileOperation_Constants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, models.RevertStatusEnum("applied"), models.RevertStatusApplied)
	assert.Equal(t, models.RevertStatusEnum("reverted"), models.RevertStatusReverted)
	assert.Equal(t, models.RevertStatusEnum("failed"), models.RevertStatusFailed)

	assert.Equal(t, models.RevertOutcomeEnum("reverted"), models.RevertOutcomeReverted)
	assert.Equal(t, models.RevertOutcomeEnum("skipped"), models.RevertOutcomeSkipped)
	assert.Equal(t, models.RevertOutcomeEnum("failed"), models.RevertOutcomeFailed)

	assert.Equal(t, models.RevertReasonEnum("anchor_missing"), models.RevertReasonAnchorMissing)
	assert.Equal(t, models.RevertReasonEnum("destination_conflict"), models.RevertReasonDestinationConflict)
	assert.Equal(t, models.RevertReasonEnum("access_denied"), models.RevertReasonAccessDenied)
	assert.Equal(t, models.RevertReasonEnum("unexpected_path_state"), models.RevertReasonUnexpectedPathState)
	assert.Equal(t, models.RevertReasonEnum("nfo_restore_failed"), models.RevertReasonNFORestoreFailed)
	assert.Equal(t, models.RevertReasonEnum("generated_cleanup_failed"), models.RevertReasonGeneratedCleanupFailed)

	assert.Equal(t, models.OperationTypeEnum("move"), models.OperationTypeMove)
	assert.Equal(t, models.OperationTypeEnum("copy"), models.OperationTypeCopy)
	assert.Equal(t, models.OperationTypeEnum("hardlink"), models.OperationTypeHardlink)
	assert.Equal(t, models.OperationTypeEnum("symlink"), models.OperationTypeSymlink)
}

func TestMigration_BatchFileOperationsTable(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Verify batch_file_operations table exists by inserting a record
	op := &models.BatchFileOperation{
		BatchJobID:     "test-batch-001",
		MovieID:        "ABC-001",
		OriginalPath:   "/original/path/file.mp4",
		NewPath:        "/new/path/file.mp4",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: `["/path/poster.jpg","/path/nfo.xml"]`,
	}
	err := db.Create(op).Error
	require.NoError(t, err)
	assert.NotZero(t, op.ID)
}

func TestHistory_BatchJobID_RoundTrip(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	// Create a history record with BatchJobID set
	batchJobID := "test-batch-002"
	history := &models.History{
		MovieID:    "ABC-002",
		BatchJobID: &batchJobID,
		Operation:  models.HistoryOpOrganize,
		Status:     models.HistoryStatusSuccess,
	}
	err := repo.Create(context.TODO(), history)
	require.NoError(t, err)
	assert.NotZero(t, history.ID)

	// Read it back
	found, err := repo.FindByID(context.TODO(), history.ID)
	require.NoError(t, err)
	require.NotNil(t, found.BatchJobID)
	assert.Equal(t, batchJobID, *found.BatchJobID)
}

func TestHistory_BatchJobID_Null(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	// Create a history record without BatchJobID (historical record)
	history := &models.History{
		MovieID:    "ABC-003",
		BatchJobID: nil,
		Operation:  models.HistoryOpScrape,
		Status:     models.HistoryStatusSuccess,
	}
	err := repo.Create(context.TODO(), history)
	require.NoError(t, err)

	// Read it back — BatchJobID should be nil
	found, err := repo.FindByID(context.TODO(), history.ID)
	require.NoError(t, err)
	assert.Nil(t, found.BatchJobID)
}

func TestHistory_ExistingTestsStillPass(t *testing.T) {
	// This test ensures backward compatibility: existing history CRUD still works
	// after adding BatchJobID field
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	history := &models.History{
		MovieID:      "BACKCOMPAT-001",
		Operation:    models.HistoryOpScrape,
		Status:       models.HistoryStatusSuccess,
		OriginalPath: "/path/to/original.mp4",
		NewPath:      "/path/to/new.mp4",
		ErrorMessage: "",
		Metadata:     `{"key":"value"}`,
		DryRun:       false,
	}

	err := repo.Create(context.TODO(), history)
	require.NoError(t, err)
	assert.NotZero(t, history.ID)

	found, err := repo.FindByID(context.TODO(), history.ID)
	require.NoError(t, err)
	assert.Equal(t, "BACKCOMPAT-001", found.MovieID)
	assert.Equal(t, models.HistoryOpScrape, found.Operation)
	assert.Equal(t, "/path/to/original.mp4", found.OriginalPath)
	assert.Equal(t, `/path/to/new.mp4`, found.NewPath)
}
