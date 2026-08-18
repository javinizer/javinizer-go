package mocks

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Coverage for the BatchFileOperationRepositoryInterface mock delegators and
// the CountByBatchJobID expecter wrappers (Run/Return/RunAndReturn).
func TestMockBatchFileOperationRepositoryInterface_Delegators(t *testing.T) {
	ctx := context.Background()

	t.Run("CountByBatchJobID wrappers", func(t *testing.T) {
		m := NewMockBatchFileOperationRepositoryInterface(t)
		m.EXPECT().CountByBatchJobID(mock.Anything, "job-1").
			Run(func(ctx context.Context, batchJobID string) {}).
			Return(int64(3), nil)
		n, err := m.CountByBatchJobID(ctx, "job-1")
		require.NoError(t, err)
		assert.Equal(t, int64(3), n)

		m2 := NewMockBatchFileOperationRepositoryInterface(t)
		m2.EXPECT().CountByBatchJobID(mock.Anything, "job-2").
			RunAndReturn(func(ctx context.Context, batchJobID string) (int64, error) {
				return int64(7), nil
			})
		n2, err := m2.CountByBatchJobID(ctx, "job-2")
		require.NoError(t, err)
		assert.Equal(t, int64(7), n2)

		m3 := NewMockBatchFileOperationRepositoryInterface(t)
		m3.On("CountByBatchJobID", mock.Anything, "job-3").Return(int64(0), errors.New("boom"))
		_, err = m3.CountByBatchJobID(ctx, "job-3")
		require.Error(t, err)
	})

	// Wave-10: coverage for the UpdateNonJournalFields mock delegator and its
	// expecter wrappers (Run/Return/RunAndReturn).
	t.Run("UpdateNonJournalFields wrappers", func(t *testing.T) {
		op := &models.BatchFileOperation{ID: 42}
		m := NewMockBatchFileOperationRepositoryInterface(t)
		m.EXPECT().UpdateNonJournalFields(mock.Anything, op).
			Run(func(ctx context.Context, op *models.BatchFileOperation) {}).
			Return(nil)
		require.NoError(t, m.UpdateNonJournalFields(ctx, op))

		m2 := NewMockBatchFileOperationRepositoryInterface(t)
		m2.EXPECT().UpdateNonJournalFields(mock.Anything, op).
			RunAndReturn(func(ctx context.Context, op *models.BatchFileOperation) error {
				return nil
			})
		require.NoError(t, m2.UpdateNonJournalFields(ctx, op))

		m3 := NewMockBatchFileOperationRepositoryInterface(t)
		m3.On("UpdateNonJournalFields", mock.Anything, op).Return(errors.New("boom"))
		require.Error(t, m3.UpdateNonJournalFields(ctx, op))

		// Empty varargs Return() leaves no values for the delegator — the
		// mock's missing-return guard panics (same guard every delegator has).
		m4 := &MockBatchFileOperationRepositoryInterface{}
		m4.On("UpdateNonJournalFields", mock.Anything, op).Return()
		require.Panics(t, func() { _ = m4.UpdateNonJournalFields(ctx, op) })
	})

	t.Run("scalar delegators", func(t *testing.T) {
		m := NewMockBatchFileOperationRepositoryInterface(t)
		m.On("CountByBatchJobIDAndRevertStatus", mock.Anything, "j", models.RevertStatusReverted).Return(int64(2), nil)
		if n, err := m.CountByBatchJobIDAndRevertStatus(ctx, "j", models.RevertStatusReverted); assert.NoError(t, err) {
			assert.Equal(t, int64(2), n)
		}

		counts := map[string]int64{"a": 1}
		m.On("CountByBatchJobIDs", mock.Anything, []string{"a"}).Return(counts, nil)
		if got, err := m.CountByBatchJobIDs(ctx, []string{"a"}); assert.NoError(t, err) {
			assert.Equal(t, counts, got)
		}
		m.On("CountRevertedByBatchJobIDs", mock.Anything, []string{"a"}).Return(counts, nil)
		if got, err := m.CountRevertedByBatchJobIDs(ctx, []string{"a"}); assert.NoError(t, err) {
			assert.Equal(t, counts, got)
		}

		op := &models.BatchFileOperation{ID: 9}
		m.On("Create", mock.Anything, op).Return(nil)
		assert.NoError(t, m.Create(ctx, op))
		m.On("CreateBatch", mock.Anything, []*models.BatchFileOperation{op}).Return(nil)
		assert.NoError(t, m.CreateBatch(ctx, []*models.BatchFileOperation{op}))
		m.On("Update", mock.Anything, op).Return(nil)
		assert.NoError(t, m.Update(ctx, op))
		m.On("UpdateNonJournalFields", mock.Anything, op).Return(nil)
		assert.NoError(t, m.UpdateNonJournalFields(ctx, op))
		m.On("UpdateRevertStatus", mock.Anything, uint(9), models.RevertStatusReverted).Return(nil)
		assert.NoError(t, m.UpdateRevertStatus(ctx, uint(9), models.RevertStatusReverted))
		m.On("UpdateJournalInTx", mock.Anything, uint(9), mock.Anything).Return(nil)
		assert.NoError(t, m.UpdateJournalInTx(ctx, uint(9), func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			return models.GeneratedFilesJSON{}, false, nil
		}))
	})

	t.Run("finder delegators", func(t *testing.T) {
		m := NewMockBatchFileOperationRepositoryInterface(t)
		ops := []models.BatchFileOperation{{ID: 1}}
		m.On("FindByBatchJobID", mock.Anything, "j").Return(ops, nil)
		if got, err := m.FindByBatchJobID(ctx, "j"); assert.NoError(t, err) {
			assert.Len(t, got, 1)
		}
		m.On("FindByBatchJobIDAndRevertStatus", mock.Anything, "j", models.RevertStatusReverted).Return(ops, nil)
		if got, err := m.FindByBatchJobIDAndRevertStatus(ctx, "j", models.RevertStatusReverted); assert.NoError(t, err) {
			assert.Len(t, got, 1)
		}
		op := &models.BatchFileOperation{ID: 5}
		m.On("FindByID", mock.Anything, uint(5)).Return(op, nil)
		if got, err := m.FindByID(ctx, uint(5)); assert.NoError(t, err) {
			assert.Equal(t, uint(5), got.ID)
		}
		m.On("FindOperationsByDestination", mock.Anything, "/dest").Return(ops, nil)
		if got, err := m.FindOperationsByDestination(ctx, "/dest"); assert.NoError(t, err) {
			assert.Len(t, got, 1)
		}
		m.On("FindOperationsWithLedger", mock.Anything).Return(ops, nil)
		if got, err := m.FindOperationsWithLedger(ctx); assert.NoError(t, err) {
			assert.Len(t, got, 1)
		}
		m.On("FindOperationsWithReplacements", mock.Anything).Return(ops, nil)
		if got, err := m.FindOperationsWithReplacements(ctx); assert.NoError(t, err) {
			assert.Len(t, got, 1)
		}
	})
}

var _ = database.JournalUpdateFn(nil)
