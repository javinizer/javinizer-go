package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// JobRepository persists and queries Job records via GORM, composing BaseRepository for common operations.
type JobRepository struct {
	*BaseRepository[models.Job, string]
}

// NewJobRepository constructs a JobRepository ordered by started_at then id for deterministic pagination.
func NewJobRepository(db *DB) *JobRepository {
	return &JobRepository{
		BaseRepository: NewBaseRepository[models.Job, string](
			db, "job",
			func(j models.Job) string { return j.ID },
			// Tiebreak on id DESC so pagination (LIMIT/OFFSET) is deterministic
			// when multiple jobs share the same started_at (same-ms creation in
			// tight test loops or batch-enqueue bursts). Without the tiebreaker,
			// two paginated queries can return rows in inconsistent order across
			// separateLIMIT/OFFSET calls.
			withDefaultOrder[models.Job, string]("started_at DESC, id DESC"),
			WithNewEntity[models.Job, string](func() models.Job { return models.Job{} }),
		),
	}
}

// Create inserts a new job record, delegating to the base repository.
func (r *JobRepository) Create(ctx context.Context, job *models.Job) error {
	return r.BaseRepository.Create(ctx, job)
}

// Update saves all fields of the given job record.
func (r *JobRepository) Update(ctx context.Context, job *models.Job) error {
	if err := r.GetDB().WithContext(ctx).Save(job).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("job %s", job.ID), err)
	}
	return nil
}

// Upsert inserts or replaces the given job record by primary key.
func (r *JobRepository) Upsert(ctx context.Context, job *models.Job) error {
	if err := r.GetDB().WithContext(ctx).Save(job).Error; err != nil {
		return wrapDBErr("upsert", fmt.Sprintf("job %s", job.ID), err)
	}
	return nil
}

// FindByID loads a job record by its primary key, delegating to the base repository.
func (r *JobRepository) FindByID(ctx context.Context, id string) (*models.Job, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

// List returns all job records ordered by the base repository's default order.
func (r *JobRepository) List(ctx context.Context) ([]models.Job, error) {
	return r.ListAll(ctx)
}

// Delete removes the job record with the given primary key, delegating to the base repository.
func (r *JobRepository) Delete(ctx context.Context, id string) error {
	return r.BaseRepository.Delete(ctx, id)
}

// DeleteOrganizedOlderThan removes organized jobs whose organized_at predates the given date.
func (r *JobRepository) DeleteOrganizedOlderThan(ctx context.Context, date time.Time) error {
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		oldJobs := tx.Model(&models.Job{}).
			Select("id").
			Where("status = ? AND organized_at < ?", models.JobStatusOrganized, date)
		if err := tx.Where("batch_job_id IN (?)", oldJobs).Delete(&models.BatchFileOperation{}).Error; err != nil {
			return wrapDBErr("delete", "organized job operations", err)
		}
		if err := tx.Where("status = ? AND organized_at < ?", models.JobStatusOrganized, date).Delete(&models.Job{}).Error; err != nil {
			return wrapDBErr("delete", "organized jobs", err)
		}
		return nil
	})
	return err
}
