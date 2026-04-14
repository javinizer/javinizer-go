package database

import (
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
	assert.Equal(t, "applied", models.RevertStatusApplied)
	assert.Equal(t, "reverted", models.RevertStatusReverted)
	assert.Equal(t, "failed", models.RevertStatusFailed)

	// D-06: RevertOutcome constants
	assert.Equal(t, "reverted", models.RevertOutcomeReverted)
	assert.Equal(t, "skipped", models.RevertOutcomeSkipped)
	assert.Equal(t, "failed", models.RevertOutcomeFailed)

	// D-06: RevertReason constants
	assert.Equal(t, "anchor_missing", models.RevertReasonAnchorMissing)
	assert.Equal(t, "destination_conflict", models.RevertReasonDestinationConflict)
	assert.Equal(t, "access_denied", models.RevertReasonAccessDenied)
	assert.Equal(t, "unexpected_path_state", models.RevertReasonUnexpectedPathState)
	assert.Equal(t, "nfo_restore_failed", models.RevertReasonNFORestoreFailed)
	assert.Equal(t, "generated_cleanup_failed", models.RevertReasonGeneratedCleanupFailed)

	assert.Equal(t, "move", models.OperationTypeMove)
	assert.Equal(t, "copy", models.OperationTypeCopy)
	assert.Equal(t, "hardlink", models.OperationTypeHardlink)
	assert.Equal(t, "symlink", models.OperationTypeSymlink)
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
		Operation:  "organize",
		Status:     "success",
	}
	err := repo.Create(history)
	require.NoError(t, err)
	assert.NotZero(t, history.ID)

	// Read it back
	found, err := repo.FindByID(history.ID)
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
		Operation:  "scrape",
		Status:     "success",
	}
	err := repo.Create(history)
	require.NoError(t, err)

	// Read it back — BatchJobID should be nil
	found, err := repo.FindByID(history.ID)
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
		Operation:    "scrape",
		Status:       "success",
		OriginalPath: "/path/to/original.mp4",
		NewPath:      "/path/to/new.mp4",
		ErrorMessage: "",
		Metadata:     `{"key":"value"}`,
		DryRun:       false,
	}

	err := repo.Create(history)
	require.NoError(t, err)
	assert.NotZero(t, history.ID)

	found, err := repo.FindByID(history.ID)
	require.NoError(t, err)
	assert.Equal(t, "BACKCOMPAT-001", found.MovieID)
	assert.Equal(t, "scrape", found.Operation)
	assert.Equal(t, "/path/to/original.mp4", found.OriginalPath)
	assert.Equal(t, `/path/to/new.mp4`, found.NewPath)
}
