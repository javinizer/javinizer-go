package history

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
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

func (m *mockBatchFileOpRepo) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	matched := make([]models.BatchFileOperation, 0, len(m.ops))
	for _, op := range m.ops {
		if op.GeneratedFiles != "" {
			matched = append(matched, *op)
		}
	}
	return matched, nil
}

func (m *mockBatchFileOpRepo) FindOperationsWithReplacements(ctx context.Context) ([]models.BatchFileOperation, error) {
	matched := make([]models.BatchFileOperation, 0, 1)
	for _, op := range m.ops {
		gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
		if err == nil && len(gf.Replacements) > 0 {
			matched = append(matched, *op)
		}
	}
	return matched, nil
}

func (m *mockBatchFileOpRepo) FindOperationsByDestination(ctx context.Context, destination string) ([]models.BatchFileOperation, error) {
	matched := make([]models.BatchFileOperation, 0, 1)
	for _, op := range m.ops {
		gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
		if err != nil {
			continue
		}
		for _, rep := range gf.Replacements {
			if rep.Destination == destination {
				matched = append(matched, *op)
				break
			}
		}
	}
	return matched, nil
}

func (m *mockBatchFileOpRepo) Update(ctx context.Context, op *models.BatchFileOperation) error {
	m.ops[op.ID] = op
	return nil
}

// UpdateNonJournalFields mirrors the wave-15 production contract in-memory:
// non-journal columns follow op while the stored row keeps its journal and,
// when the stored row is already reverted while op carries a completion
// status, its reverted status (the typed race error surfaces, exactly like
// the sqlite repository's ErrOperationRowReverted).
func (m *mockBatchFileOpRepo) UpdateNonJournalFields(ctx context.Context, op *models.BatchFileOperation) error {
	cp := *op
	if stored, ok := m.ops[op.ID]; ok {
		cp.GeneratedFiles = stored.GeneratedFiles
		if stored.RevertStatus == models.RevertStatusReverted && op.RevertStatus != models.RevertStatusReverted {
			cp.RevertStatus = stored.RevertStatus
			cp.RevertedAt = stored.RevertedAt
			m.ops[op.ID] = &cp
			return fmt.Errorf("w15 mirror: %w: batch file operation %d", database.ErrOperationRowReverted, op.ID)
		}
	}
	m.ops[op.ID] = &cp
	return nil
}

// UpdateJournalInTx mirrors the production journal transaction for the
// in-memory fixture: the stored row is re-read lean and the merge result
// replaces its generated-files ledger only when persist is requested.
func (m *mockBatchFileOpRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	stored, ok := m.ops[id]
	if !ok {
		return fmt.Errorf("update journal tx row %d: %w", id, database.ErrNotFound)
	}
	current := &models.BatchFileOperation{
		ID:             stored.ID,
		GeneratedFiles: stored.GeneratedFiles,
		RevertStatus:   stored.RevertStatus,
	}
	next, persist, err := fn(current)
	if err != nil {
		return err
	}
	if persist {
		stored.GeneratedFiles = models.MarshalLedgerJSON(next)
	}
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
