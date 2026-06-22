package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type BatchFileOperationRepository struct {
	*BaseRepository[models.BatchFileOperation, uint]
}

func NewBatchFileOperationRepository(db *DB) *BatchFileOperationRepository {
	return &BatchFileOperationRepository{
		BaseRepository: NewBaseRepository[models.BatchFileOperation, uint](
			db, "batch file operation",
			func(op models.BatchFileOperation) string { return fmt.Sprintf("%d", op.ID) },
			WithNewEntity[models.BatchFileOperation, uint](func() models.BatchFileOperation { return models.BatchFileOperation{} }),
		),
	}
}

func (r *BatchFileOperationRepository) Create(ctx context.Context, op *models.BatchFileOperation) error {
	return r.BaseRepository.Create(ctx, op)
}

func (r *BatchFileOperationRepository) CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, op := range ops {
			if err := tx.Create(op).Error; err != nil {
				return wrapDBErr("create", fmt.Sprintf("batch file operation %d", op.ID), err)
			}
		}
		return nil
	})
}

func (r *BatchFileOperationRepository) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

func (r *BatchFileOperationRepository) FindByBatchJobID(ctx context.Context, batchJobID string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).Where("batch_job_id = ?", batchJobID).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return ops, nil
}

func (r *BatchFileOperationRepository) FindByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, revertStatus models.RevertStatusEnum) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).Where("batch_job_id = ? AND revert_status = ?", batchJobID, revertStatus).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, revertStatus), err)
	}
	return ops, nil
}

func (r *BatchFileOperationRepository) UpdateRevertStatus(ctx context.Context, id uint, status models.RevertStatusEnum) error {
	updates := map[string]any{
		"revert_status": status,
		"updated_at":    time.Now().UTC(),
	}
	if status == models.RevertStatusReverted {
		updates["reverted_at"] = time.Now().UTC()
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d revert status", id), err)
	}
	return nil
}

func (r *BatchFileOperationRepository) CountByBatchJobID(ctx context.Context, batchJobID string) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).Where("batch_job_id = ?", batchJobID).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return count, nil
}

func (r *BatchFileOperationRepository) CountByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, status models.RevertStatusEnum) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).Where("batch_job_id = ? AND revert_status = ?", batchJobID, status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, status), err)
	}
	return count, nil
}

func (r *BatchFileOperationRepository) Update(ctx context.Context, op *models.BatchFileOperation) error {
	if err := r.GetDB().WithContext(ctx).Save(op).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("batch file operation %d", op.ID), err)
	}
	return nil
}

// countByBatchJobIDsResult is a GORM scan target for GROUP BY queries.
type countByBatchJobIDsResult struct {
	BatchJobID string `gorm:"column:batch_job_id"`
	Count      int64  `gorm:"column:cnt"`
}

// CountByBatchJobIDs returns a map of jobID→count for all given job IDs in a single query.
func (r *BatchFileOperationRepository) CountByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error) {
	if len(jobIDs) == 0 {
		return map[string]int64{}, nil
	}
	var results []countByBatchJobIDsResult
	err := r.GetDB().WithContext(ctx).
		Model(&models.BatchFileOperation{}).
		Select("batch_job_id, count(*) as cnt").
		Where("batch_job_id IN ?", jobIDs).
		Group("batch_job_id").
		Find(&results).Error
	if err != nil {
		return nil, wrapDBErr("count_by_batch_job_ids", "batch file operations", err)
	}
	m := make(map[string]int64, len(results))
	for _, r := range results {
		m[r.BatchJobID] = r.Count
	}
	return m, nil
}

// CountRevertedByBatchJobIDs returns a map of jobID→reverted count for all given job IDs.
func (r *BatchFileOperationRepository) CountRevertedByBatchJobIDs(ctx context.Context, jobIDs []string) (map[string]int64, error) {
	if len(jobIDs) == 0 {
		return map[string]int64{}, nil
	}
	var results []countByBatchJobIDsResult
	err := r.GetDB().WithContext(ctx).
		Model(&models.BatchFileOperation{}).
		Select("batch_job_id, count(*) as cnt").
		Where("batch_job_id IN ?", jobIDs).
		Where("revert_status = ?", models.RevertStatusReverted).
		Group("batch_job_id").
		Find(&results).Error
	if err != nil {
		return nil, wrapDBErr("count_reverted_by_batch_job_ids", "batch file operations", err)
	}
	m := make(map[string]int64, len(results))
	for _, r := range results {
		m[r.BatchJobID] = r.Count
	}
	return m, nil
}
