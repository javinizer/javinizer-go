package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// BatchFileOperationRepository persists batch file operation records used for revert tracking.
type BatchFileOperationRepository struct {
	*BaseRepository[models.BatchFileOperation, uint]
}

// NewBatchFileOperationRepository returns a repository backed by db for batch file operations.
func NewBatchFileOperationRepository(db *DB) *BatchFileOperationRepository {
	return &BatchFileOperationRepository{
		BaseRepository: NewBaseRepository[models.BatchFileOperation, uint](
			db, "batch file operation",
			func(op models.BatchFileOperation) string { return fmt.Sprintf("%d", op.ID) },
			WithNewEntity[models.BatchFileOperation, uint](func() models.BatchFileOperation { return models.BatchFileOperation{} }),
		),
	}
}

// Create inserts a single batch file operation record.
func (r *BatchFileOperationRepository) Create(ctx context.Context, op *models.BatchFileOperation) error {
	return r.BaseRepository.Create(ctx, op)
}

// CreateBatch inserts multiple batch file operation records in a single transaction.
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

// FindByID returns the batch file operation with the given primary key.
func (r *BatchFileOperationRepository) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

// FindByBatchJobID returns all file operations for a batch job, ordered by id.
func (r *BatchFileOperationRepository) FindByBatchJobID(ctx context.Context, batchJobID string) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).Where("batch_job_id = ?", batchJobID).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return ops, nil
}

// FindByBatchJobIDAndRevertStatus returns a batch job's operations filtered by revert status, ordered by id.
func (r *BatchFileOperationRepository) FindByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, revertStatus models.RevertStatusEnum) ([]models.BatchFileOperation, error) {
	var ops []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).Where("batch_job_id = ? AND revert_status = ?", batchJobID, revertStatus).Order("id ASC").Find(&ops).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, revertStatus), err)
	}
	return ops, nil
}

// UpdateRevertStatus sets the revert status of an operation, stamping reverted_at when the status is reverted.
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

// CountByBatchJobID returns the number of file operations for a batch job.
func (r *BatchFileOperationRepository) CountByBatchJobID(ctx context.Context, batchJobID string) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).Where("batch_job_id = ?", batchJobID).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s", batchJobID), err)
	}
	return count, nil
}

// CountByBatchJobIDAndRevertStatus returns the number of operations for a batch job with the given revert status.
func (r *BatchFileOperationRepository) CountByBatchJobIDAndRevertStatus(ctx context.Context, batchJobID string, status models.RevertStatusEnum) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.BatchFileOperation{}).Where("batch_job_id = ? AND revert_status = ?", batchJobID, status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("batch file operations for job %s with status %s", batchJobID, status), err)
	}
	return count, nil
}

// Update saves all fields of the given batch file operation record.
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

// FindOperationsByDestination returns every operation whose generated-files
// ledger journals a replacement for destination. SQL LIKE pre-filters
// candidates; entries are matched exactly in-process so path substrings and
// LIKE metacharacters can neither over- nor under-match.
func (r *BatchFileOperationRepository) FindOperationsByDestination(ctx context.Context, destination string) ([]models.BatchFileOperation, error) {
	escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(destination)
	likePattern := "%" + escaped + "%"
	var candidates []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).
		Where("generated_files LIKE ? ESCAPE '\\'", likePattern).
		Order("id ASC").Find(&candidates).Error
	if err != nil {
		return nil, wrapDBErr("find", "batch file operations by destination", err)
	}
	matched := make([]models.BatchFileOperation, 0, len(candidates))
	for _, op := range candidates {
		gf, perr := models.ParseGeneratedFiles(op.GeneratedFiles)
		if perr != nil {
			continue // unparsable legacy rows never match a destination journal
		}
		for _, rep := range gf.Replacements {
			if rep.Destination == destination {
				matched = append(matched, op)
				break
			}
		}
	}
	return matched, nil
}

// FindOperationsWithReplacements returns every operation whose generated-files
// ledger journals at least one replacement entry (any revert status — the
// sweeper must see applied and failed rows alike).
func (r *BatchFileOperationRepository) FindOperationsWithReplacements(ctx context.Context) ([]models.BatchFileOperation, error) {
	var candidates []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).
		Where("generated_files LIKE ?", "%\"replacements\"%").
		Order("id ASC").Find(&candidates).Error
	if err != nil {
		return nil, wrapDBErr("find", "batch file operations with replacements", err)
	}
	matched := make([]models.BatchFileOperation, 0, len(candidates))
	for _, op := range candidates {
		gf, perr := models.ParseGeneratedFiles(op.GeneratedFiles)
		if perr != nil {
			continue
		}
		if len(gf.Replacements) > 0 {
			matched = append(matched, op)
		}
	}
	return matched, nil
}

// FindOperationsWithLedger returns every operation carrying a non-empty
// generated-files ledger of any shape.
func (r *BatchFileOperationRepository) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	var rows []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).
		Where("generated_files IS NOT NULL AND generated_files <> ''").
		Order("id ASC").Find(&rows).Error
	if err != nil {
		return nil, wrapDBErr("find", "batch file operations with ledger", err)
	}
	return rows, nil
}
