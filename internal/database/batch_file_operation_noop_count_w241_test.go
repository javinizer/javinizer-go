package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// codex P2 (PR #241 F2): CountNoOpByBatchJobIDs aggregates the terminal
// completed-noop rows the list endpoints subtract from revertible totals.

func TestBFOCountNoOpByBatchJobIDs_W241(t *testing.T) {
	db := missDB(t)
	repo := NewBatchFileOperationRepository(db)

	seed := func(jobID, movieID string, status models.RevertStatusEnum) {
		require.NoError(t, repo.Create(context.TODO(), &models.BatchFileOperation{
			BatchJobID:    jobID,
			MovieID:       movieID,
			OriginalPath:  "/src/" + movieID + ".mp4",
			NewPath:       "/dest/" + movieID + ".mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  status,
		}))
	}

	seed("bj-noop-a", "APP-001", models.RevertStatusApplied)
	seed("bj-noop-a", "DUP-001", models.RevertStatusNoOp)
	seed("bj-noop-a", "DUP-002", models.RevertStatusNoOp)
	seed("bj-noop-a", "REV-001", models.RevertStatusReverted)
	seed("bj-noop-b", "APP-002", models.RevertStatusApplied)

	result, err := repo.CountNoOpByBatchJobIDs(context.TODO(), []string{"bj-noop-a", "bj-noop-b", "bj-noop-absent"})
	require.NoError(t, err)
	assert.Equal(t, int64(2), result["bj-noop-a"], "only terminal noop rows count")
	assert.NotContains(t, result, "bj-noop-b", "jobs with no noop rows have no bucket")
	assert.NotContains(t, result, "bj-noop-absent")

	// Empty input short-circuits without touching the DB.
	emptyResult, err := repo.CountNoOpByBatchJobIDs(context.TODO(), nil)
	require.NoError(t, err)
	assert.Empty(t, emptyResult)
}

func TestBFOCountNoOpByBatchJobIDs_W241_ErrorBranch(t *testing.T) {
	db := covDB2(t)
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, db.DB.Exec("DROP TABLE batch_file_operations").Error)
	_, err := repo.CountNoOpByBatchJobIDs(context.TODO(), []string{"batch-1"})
	assert.Error(t, err)
}
