package history

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

// mockBatchFileOpRepo implements database.BatchFileOperationRepositoryInterface for testing
type mockBatchFileOpRepo struct {
	ops    map[uint]*models.BatchFileOperation
	nextID uint
}

func newMockBatchFileOpRepo() *mockBatchFileOpRepo {
	return &mockBatchFileOpRepo{ops: make(map[uint]*models.BatchFileOperation), nextID: 1}
}

func (m *mockBatchFileOpRepo) Create(ctx context.Context, op *models.BatchFileOperation) error {
	op.ID = m.nextID
	m.nextID++
	m.ops[op.ID] = op
	return nil
}

func (m *mockBatchFileOpRepo) CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error {
	for _, op := range ops {
		op.ID = m.nextID
		m.nextID++
		m.ops[op.ID] = op
	}
	return nil
}

func (m *mockBatchFileOpRepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	if op, ok := m.ops[id]; ok {
		return op, nil
	}
	return nil, errors.New("not found")
}

func (m *mockBatchFileOpRepo) FindByBatchJobID(ctx context.Context, batchJobID string) ([]models.BatchFileOperation, error) {
	return nil, nil
}

func (m *mockBatchFileOpRepo) FindByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, revertStatus models.RevertStatusEnum) ([]models.BatchFileOperation, error) {
	return nil, nil
}

func (m *mockBatchFileOpRepo) Update(ctx context.Context, op *models.BatchFileOperation) error {
	m.ops[op.ID] = op
	return nil
}

func (m *mockBatchFileOpRepo) UpdateRevertStatus(ctx context.Context, id uint, status models.RevertStatusEnum) error {
	if op, ok := m.ops[id]; ok {
		op.RevertStatus = status
		return nil
	}
	return errors.New("not found")
}

func (m *mockBatchFileOpRepo) CountByBatchJobID(ctx context.Context, batchJobID string) (int64, error) {
	return 0, nil
}

func (m *mockBatchFileOpRepo) CountByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, status models.RevertStatusEnum) (int64, error) {
	return 0, nil
}

func (m *mockBatchFileOpRepo) CountByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error) {
	return nil, nil
}

func (m *mockBatchFileOpRepo) CountRevertedByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error) {
	return nil, nil
}

func TestReverter_SkipRevertDeep2(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newMockBatchFileOpRepo()
	r := NewReverter(fs, repo)

	op := &models.BatchFileOperation{
		ID:           1,
		MovieID:      "ABC-123",
		OriginalPath: "/original/path/video.mp4",
		NewPath:      "/new/path/video.mp4",
	}

	result := r.skipRevert(op, models.RevertReasonAnchorMissing)
	assert.Equal(t, models.RevertOutcomeSkipped, result.Outcome)
	assert.Equal(t, models.RevertReasonAnchorMissing, result.Reason)
	assert.Equal(t, "ABC-123", result.MovieID)
}

func TestErrBatchAlreadyRevertedDeep2(t *testing.T) {
	assert.EqualError(t, ErrBatchAlreadyReverted, "batch already reverted")
}

func TestErrCopyModeNotRevertibleDeep2(t *testing.T) {
	assert.EqualError(t, ErrCopyModeNotRevertible, "copy-mode operations cannot be reverted")
}

func TestErrNoOperationsFoundDeep2(t *testing.T) {
	assert.EqualError(t, ErrNoOperationsFound, "no operations found for batch")
}
