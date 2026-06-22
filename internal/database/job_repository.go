package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type JobRepository struct {
	*BaseRepository[models.Job, string]
}

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

func (r *JobRepository) Create(ctx context.Context, job *models.Job) error {
	return r.BaseRepository.Create(ctx, job)
}

func (r *JobRepository) Update(ctx context.Context, job *models.Job) error {
	if err := r.GetDB().WithContext(ctx).Save(job).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("job %s", job.ID), err)
	}
	return nil
}

func (r *JobRepository) Upsert(ctx context.Context, job *models.Job) error {
	if err := r.GetDB().WithContext(ctx).Save(job).Error; err != nil {
		return wrapDBErr("upsert", fmt.Sprintf("job %s", job.ID), err)
	}
	return nil
}

func (r *JobRepository) FindByID(ctx context.Context, id string) (*models.Job, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

func (r *JobRepository) List(ctx context.Context) ([]models.Job, error) {
	return r.ListAll(ctx)
}

func (r *JobRepository) Delete(ctx context.Context, id string) error {
	return r.BaseRepository.Delete(ctx, id)
}

func (r *JobRepository) DeleteOrganizedOlderThan(ctx context.Context, date time.Time) error {
	if err := r.GetDB().WithContext(ctx).Where("status = ? AND organized_at < ?", models.JobStatusOrganized, date).Delete(&models.Job{}).Error; err != nil {
		return wrapDBErr("delete", "organized jobs", err)
	}
	return nil
}
