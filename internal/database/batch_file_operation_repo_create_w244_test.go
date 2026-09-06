package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBatchFileOperationRepository_Create_PermanentFailure pins the
// retryOnLocked-wrapped error arm of Create (#244): a non-transient failure
// (missing table) is not classified "locked", so it propagates immediately
// with the wrapped "create" label.
func TestBatchFileOperationRepository_Create_PermanentFailure(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE batch_file_operations").Error)

	err := repo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID:    "job-create-failure",
		OriginalPath:  "/failure/source.mp4",
		NewPath:       "/failure/dest.mp4",
		OperationType: models.OperationTypeMove,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create batch file operation")
}
