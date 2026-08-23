package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// JobRepository persists and queries Job records via GORM, composing BaseRepository for common operations.
type JobRepository struct {
	*BaseRepository[models.Job, string]

	pruneMu   sync.RWMutex
	pruneHook func(context.Context, []models.BatchFileOperation) error
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

// SetOrganizedJobPruneHook installs the post-commit cleanup invoked after
// organized jobs and their operation rows are pruned. The hook is optional so
// database-only callers can retain the repository's original behavior.
func (r *JobRepository) SetOrganizedJobPruneHook(hook func(context.Context, []models.BatchFileOperation) error) {
	r.pruneMu.Lock()
	r.pruneHook = hook
	r.pruneMu.Unlock()
}

func (r *JobRepository) organizedJobPruneHook() func(context.Context, []models.BatchFileOperation) error {
	r.pruneMu.RLock()
	defer r.pruneMu.RUnlock()
	return r.pruneHook
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
// Filesystem-owned replacement backups are cleaned BEFORE the rows are deleted;
// a crash during cleanup therefore leaves their ledger rows available to startup
// reconciliation rather than creating an unowned backup.
func (r *JobRepository) DeleteOrganizedOlderThan(ctx context.Context, date time.Time) error {
	var prunedJobs []models.Job
	var prunedOps []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("status = ? AND organized_at < ?", models.JobStatusOrganized, date).Find(&prunedJobs).Error; err != nil {
			return wrapDBErr("find", "organized jobs", err)
		}
		if len(prunedJobs) == 0 {
			return nil
		}
		jobIDs := make([]string, 0, len(prunedJobs))
		for _, job := range prunedJobs {
			jobIDs = append(jobIDs, job.ID)
		}
		if err := tx.Where("batch_job_id IN ?", jobIDs).Find(&prunedOps).Error; err != nil {
			return wrapDBErr("find", "organized job operations", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	hook := r.organizedJobPruneHook()
	if hasReplacementLedger(prunedOps) && hook == nil {
		return fmt.Errorf("prune organized jobs: replacement cleanup hook is not configured")
	}
	if hook != nil && len(prunedOps) > 0 {
		if err := hook(ctx, prunedOps); err != nil {
			return wrapDBErr("prune", "organized job backups", err)
		}
	}

	jobIDs := make([]string, 0, len(prunedJobs))
	for _, job := range prunedJobs {
		jobIDs = append(jobIDs, job.ID)
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("batch_job_id IN ?", jobIDs).Delete(&models.BatchFileOperation{}).Error; err != nil {
			return wrapDBErr("delete", "organized job operations", err)
		}
		if err := tx.Where("id IN ?", jobIDs).Delete(&models.Job{}).Error; err != nil {
			return wrapDBErr("delete", "organized jobs", err)
		}
		return nil
	})
}

func hasReplacementLedger(ops []models.BatchFileOperation) bool {
	for _, op := range ops {
		if strings.Contains(op.GeneratedFiles, `"replacements"`) {
			return true
		}
	}
	return false
}
