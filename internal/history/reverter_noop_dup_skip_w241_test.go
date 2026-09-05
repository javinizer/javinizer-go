package history

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// codex P2 (PR #241 F2): a completed-noop row (authorized intra-batch
// duplicate skip) is terminal with an empty NewPath; guardDoubleRevert must
// reject it like an already-reverted row instead of probing a "" anchor
// forever. The guard's NoOp branch (reverter.go L348-350) was the PR's
// remaining strict-gate miss — no test ever attempted to revert a NoOp row.
// These pins invoke the guard directly AND through the full revertFile
// path (DB re-read → guard) with a mocked repository, deterministically.

func TestGuardDoubleRevert_RejectsNoOpDuplicateSkipRow(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	op := &models.BatchFileOperation{
		RevertStatus:  models.RevertStatusNoOp,
		OperationType: models.OperationTypeMove,
	}
	result, err := r.guardDoubleRevert(context.Background(), op)
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrBatchAlreadyReverted)
}

func TestRevertFile_NoOpDuplicateSkipRowRejectedWithoutSideEffects(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	// Only the fresh-row re-read is expected; any persistence-side effect
	// (UpdateRevertStatus et al.) would fail the strict mock immediately.
	mockRepo.On("FindByID", mock.Anything, uint(913)).Return(&models.BatchFileOperation{
		ID:            913,
		RevertStatus:  models.RevertStatusNoOp,
		OperationType: models.OperationTypeMove,
		OriginalPath:  "/src/DUP-913.mp4",
		NewPath:       "", // empty by construction for a skipped duplicate
	}, nil)
	r := NewReverter(fs, mockRepo)

	result, err := r.revertFile(context.Background(), &models.BatchFileOperation{ID: 913})
	assert.Nil(t, result)
	require.ErrorIs(t, err, ErrBatchAlreadyReverted)
	mockRepo.AssertExpectations(t)
}
