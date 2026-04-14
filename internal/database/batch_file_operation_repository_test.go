package database

import (
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchFileOperationRepository_Create(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:     "batch-create-001",
		MovieID:        "ABC-001",
		OriginalPath:   "/original/path/file.mp4",
		NewPath:        "/new/path/file.mp4",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: `["/path/poster.jpg"]`,
	}

	err := repo.Create(op)
	require.NoError(t, err)
	assert.NotZero(t, op.ID)
}

func TestBatchFileOperationRepository_CreateBatch(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	ops := []*models.BatchFileOperation{
		{
			BatchJobID:    "batch-createbatch-001",
			MovieID:       "ABC-010",
			OriginalPath:  "/original/file1.mp4",
			NewPath:       "/new/file1.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
		{
			BatchJobID:    "batch-createbatch-001",
			MovieID:       "ABC-011",
			OriginalPath:  "/original/file2.mp4",
			NewPath:       "/new/file2.mp4",
			OperationType: models.OperationTypeCopy,
			RevertStatus:  models.RevertStatusApplied,
		},
	}

	err := repo.CreateBatch(ops)
	require.NoError(t, err)
	assert.NotZero(t, ops[0].ID)
	assert.NotZero(t, ops[1].ID)

	// Verify both records exist
	results, err := repo.FindByBatchJobID("batch-createbatch-001")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestBatchFileOperationRepository_FindByID(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:    "batch-findbyid-001",
		OriginalPath:  "/original/find.mp4",
		NewPath:       "/new/find.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(op))

	found, err := repo.FindByID(op.ID)
	require.NoError(t, err)
	assert.Equal(t, op.BatchJobID, found.BatchJobID)
	assert.Equal(t, "/original/find.mp4", found.OriginalPath)
}

func TestBatchFileOperationRepository_FindByID_NotFound(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	_, err := repo.FindByID(99999)
	assert.Error(t, err)
}

func TestBatchFileOperationRepository_FindByBatchJobID(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	// Create operations for batch A
	for i := 0; i < 3; i++ {
		op := &models.BatchFileOperation{
			BatchJobID:    "batch-filter-A",
			OriginalPath:  "/original/a.mp4",
			NewPath:       "/new/a.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(op))
	}

	// Create operations for batch B
	for i := 0; i < 2; i++ {
		op := &models.BatchFileOperation{
			BatchJobID:    "batch-filter-B",
			OriginalPath:  "/original/b.mp4",
			NewPath:       "/new/b.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(op))
	}

	// Find for batch A only
	resultsA, err := repo.FindByBatchJobID("batch-filter-A")
	require.NoError(t, err)
	assert.Len(t, resultsA, 3)

	// Find for batch B only
	resultsB, err := repo.FindByBatchJobID("batch-filter-B")
	require.NoError(t, err)
	assert.Len(t, resultsB, 2)

	// All results for batch A should have the correct batch_job_id
	for _, r := range resultsA {
		assert.Equal(t, "batch-filter-A", r.BatchJobID)
	}
}

func TestBatchFileOperationRepository_FindByBatchJobIDAndRevertStatus(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	// Create operations with different revert statuses for the same batch
	ops := []*models.BatchFileOperation{
		{
			BatchJobID:    "batch-revert-001",
			OriginalPath:  "/original/pending1.mp4",
			NewPath:       "/new/pending1.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
		{
			BatchJobID:    "batch-revert-001",
			OriginalPath:  "/original/reverted1.mp4",
			NewPath:       "/new/reverted1.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusReverted,
		},
		{
			BatchJobID:    "batch-revert-001",
			OriginalPath:  "/original/pending2.mp4",
			NewPath:       "/new/pending2.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
	}
	for _, op := range ops {
		require.NoError(t, repo.Create(op))
	}

	// Find only pending operations
	pending, err := repo.FindByBatchJobIDAndRevertStatus("batch-revert-001", models.RevertStatusApplied)
	require.NoError(t, err)
	assert.Len(t, pending, 2)

	// Find only reverted operations
	reverted, err := repo.FindByBatchJobIDAndRevertStatus("batch-revert-001", models.RevertStatusReverted)
	require.NoError(t, err)
	assert.Len(t, reverted, 1)

	// Find failed (none exist)
	failed, err := repo.FindByBatchJobIDAndRevertStatus("batch-revert-001", models.RevertStatusFailed)
	require.NoError(t, err)
	assert.Len(t, failed, 0)
}

func TestBatchFileOperationRepository_UpdateRevertStatus(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:    "batch-update-revert-001",
		OriginalPath:  "/original/update.mp4",
		NewPath:       "/new/update.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(op))

	beforeUpdate := time.Now().UTC()

	// Update to reverted
	err := repo.UpdateRevertStatus(op.ID, models.RevertStatusReverted)
	require.NoError(t, err)

	// Verify status and reverted_at timestamp
	found, err := repo.FindByID(op.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RevertStatusReverted, found.RevertStatus)
	require.NotNil(t, found.RevertedAt)
	assert.True(t, found.RevertedAt.After(beforeUpdate) || found.RevertedAt.Equal(beforeUpdate))

	// Update to failed (should not change reverted_at)
	err = repo.UpdateRevertStatus(op.ID, models.RevertStatusFailed)
	require.NoError(t, err)

	found, err = repo.FindByID(op.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RevertStatusFailed, found.RevertStatus)
}

func TestBatchFileOperationRepository_CountByBatchJobID(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	for i := 0; i < 5; i++ {
		op := &models.BatchFileOperation{
			BatchJobID:    "batch-count-001",
			OriginalPath:  "/original/count.mp4",
			NewPath:       "/new/count.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		}
		require.NoError(t, repo.Create(op))
	}

	count, err := repo.CountByBatchJobID("batch-count-001")
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)

	// Non-existent batch
	count, err = repo.CountByBatchJobID("nonexistent-batch")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestBatchFileOperationRepository_CountByBatchJobIDAndRevertStatus(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	ops := []*models.BatchFileOperation{
		{
			BatchJobID:    "batch-count-status-001",
			OriginalPath:  "/original/cs1.mp4",
			NewPath:       "/new/cs1.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
		{
			BatchJobID:    "batch-count-status-001",
			OriginalPath:  "/original/cs2.mp4",
			NewPath:       "/new/cs2.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
		{
			BatchJobID:    "batch-count-status-001",
			OriginalPath:  "/original/cs3.mp4",
			NewPath:       "/new/cs3.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusReverted,
		},
	}
	for _, op := range ops {
		require.NoError(t, repo.Create(op))
	}

	pendingCount, err := repo.CountByBatchJobIDAndRevertStatus("batch-count-status-001", models.RevertStatusApplied)
	require.NoError(t, err)
	assert.Equal(t, int64(2), pendingCount)

	revertedCount, err := repo.CountByBatchJobIDAndRevertStatus("batch-count-status-001", models.RevertStatusReverted)
	require.NoError(t, err)
	assert.Equal(t, int64(1), revertedCount)
}

func TestHistoryRepository_FindByBatchJobID(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	// Create history records for a specific batch
	batchJobID1 := "history-batch-001"
	for i := 0; i < 3; i++ {
		history := &models.History{
			MovieID:      "ABC-HIST-BATCH",
			BatchJobID:   &batchJobID1,
			Operation:    "organize",
			Status:       "success",
			OriginalPath: "/original/file.mp4",
			NewPath:      "/new/file.mp4",
		}
		require.NoError(t, repo.Create(history))
	}

	// Create a record for a different batch
	batchJobID2 := "history-batch-002"
	history := &models.History{
		MovieID:      "ABC-HIST-OTHER",
		BatchJobID:   &batchJobID2,
		Operation:    "organize",
		Status:       "success",
		OriginalPath: "/original/other.mp4",
		NewPath:      "/new/other.mp4",
	}
	require.NoError(t, repo.Create(history))

	// Find by batch job ID
	results, err := repo.FindByBatchJobID("history-batch-001")
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// All results should belong to the same batch
	for _, r := range results {
		require.NotNil(t, r.BatchJobID)
		assert.Equal(t, "history-batch-001", *r.BatchJobID)
	}
}

func TestHistoryRepository_FindByBatchJobID_NonExistent(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	// Create a record without batch job ID
	history := &models.History{
		MovieID:   "NO-BATCH",
		Operation: "scrape",
		Status:    "success",
	}
	require.NoError(t, repo.Create(history))

	// Query for non-existent batch job ID
	results, err := repo.FindByBatchJobID("nonexistent-batch-999")
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestHistoryRepository_FindByBatchJobID_NullBatchJobID(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	// Create a record with NULL batch_job_id
	history := &models.History{
		MovieID:    "NULL-BATCH",
		BatchJobID: nil,
		Operation:  "scrape",
		Status:     "success",
	}
	require.NoError(t, repo.Create(history))

	// SQL `= NULL` never matches, so NULL batch_job_id records are excluded
	results, err := repo.FindByBatchJobID("some-batch")
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestBatchFileOperationRepository_AllFields(t *testing.T) {
	t.Parallel()
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:      "batch-all-fields-001",
		MovieID:         "XYZ-999",
		OriginalPath:    "/original/dir/file.mp4",
		NewPath:         "/new/dir/file.mp4",
		OperationType:   models.OperationTypeHardlink,
		NFOSnapshot:     `<?xml version="1.0"?><nfo></nfo>`,
		GeneratedFiles:  `["poster.jpg","cover.jpg","nfo.xml"]`,
		RevertStatus:    models.RevertStatusApplied,
		InPlaceRenamed:  true,
		OriginalDirPath: "/original/dir",
	}
	require.NoError(t, repo.Create(op))

	found, err := repo.FindByID(op.ID)
	require.NoError(t, err)
	assert.Equal(t, "batch-all-fields-001", found.BatchJobID)
	assert.Equal(t, "XYZ-999", found.MovieID)
	assert.Equal(t, "/original/dir/file.mp4", found.OriginalPath)
	assert.Equal(t, "/new/dir/file.mp4", found.NewPath)
	assert.Equal(t, models.OperationTypeHardlink, found.OperationType)
	assert.Equal(t, `<?xml version="1.0"?><nfo></nfo>`, found.NFOSnapshot)
	assert.Equal(t, `["poster.jpg","cover.jpg","nfo.xml"]`, found.GeneratedFiles)
	assert.Equal(t, models.RevertStatusApplied, found.RevertStatus)
	assert.Nil(t, found.RevertedAt)
	assert.True(t, found.InPlaceRenamed)
	assert.Equal(t, "/original/dir", found.OriginalDirPath)
}
