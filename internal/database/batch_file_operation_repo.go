package database

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// BatchFileOperationRepository provides database operations for batch file operations
type BatchFileOperationRepository struct {
	db *DB
}

// NewBatchFileOperationRepository creates a new batch file operation repository
func NewBatchFileOperationRepository(db *DB) *BatchFileOperationRepository {
	return &BatchFileOperationRepository{db: db}
}

// Create adds a new batch file operation record
func (r *BatchFileOperationRepository) Create(op *models.BatchFileOperation) error {
	if err := r.db.Create(op).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("batch file operation %d", op.ID), err)
	}
	return nil
}

// CreateBatch inserts multiple batch file operation records in a single transaction
func (r *BatchFileOperationRepository) CreateBatch(ops []*models.BatchFileOperation) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, op := range ops {
			if err := tx.Create(op).Error; err != nil {
				return wrapDBErr("create", fmt.Sprintf("batch file operation %d", op.ID), err)
			}
		}
		return nil
	})
}

// FindByID finds a batch file operation by its primary key
func (r *BatchFileOperationRepository) FindByID(id uint) (*models.BatchFileOperation, error) {
	var op models.BatchFileOperation
	err := r.db.First(&op, id).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operation %d", id), err)
	}
	return &op, nil
}

// FindByBatchJobID returns all operations for a specific batch job
func (r *BatchFileOperationRepository) FindByBatchJobID(batchJobID string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.db.Where("batch_job_id = ?", batchJobID).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return ops, nil
}

// FindByBatchJobIDAndRevertStatus returns operations filtered by batch job and revert status
func (r *BatchFileOperationRepository) FindByBatchJobIDAndRevertStatus(batchJobID string, revertStatus string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.db.Where("batch_job_id = ? AND revert_status = ?", batchJobID, revertStatus).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, revertStatus), err)
	}
	return ops, nil
}

// UpdateRevertStatus changes the revert status and sets reverted_at when status is "reverted"
func (r *BatchFileOperationRepository) UpdateRevertStatus(id uint, status string) error {
	updates := map[string]interface{}{
		"revert_status": status,
		"updated_at":    time.Now().UTC(),
	}
	if status == models.RevertStatusReverted {
		updates["reverted_at"] = time.Now().UTC()
	}
	if err := r.db.Model(&models.BatchFileOperation{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d revert status", id), err)
	}
	return nil
}

// CountByBatchJobID returns the count of operations for a specific batch job
func (r *BatchFileOperationRepository) CountByBatchJobID(batchJobID string) (int64, error) {
	var count int64
	err := r.db.Model(&models.BatchFileOperation{}).Where("batch_job_id = ?", batchJobID).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return count, nil
}

// CountByBatchJobIDAndRevertStatus returns the count of operations filtered by batch job and revert status
func (r *BatchFileOperationRepository) CountByBatchJobIDAndRevertStatus(batchJobID string, status string) (int64, error) {
	var count int64
	err := r.db.Model(&models.BatchFileOperation{}).Where("batch_job_id = ? AND revert_status = ?", batchJobID, status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, status), err)
	}
	return count, nil
}

// Update persists changes to an existing BatchFileOperation record using GORM Save (upsert).
// Updates all fields including UpdatedAt.
func (r *BatchFileOperationRepository) Update(op *models.BatchFileOperation) error {
	if err := r.db.Save(op).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d", op.ID), err)
	}
	return nil
}
