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
	if job == nil {
		return fmt.Errorf("update job: job must not be nil")
	}
	return r.saveVersioned(ctx, job, "update")
}

// Upsert inserts or replaces the given job record by primary key.
func (r *JobRepository) Upsert(ctx context.Context, job *models.Job) error {
	if job == nil {
		return fmt.Errorf("upsert job: job must not be nil")
	}
	return r.saveVersioned(ctx, job, "upsert")
}

// saveVersioned increments prune_version in the database before saving the
// caller's fields. The first write acquires SQLite's writer lock, so stale
// callers cannot reuse a version observed before a concurrent writer commit.
func (r *JobRepository) saveVersioned(ctx context.Context, job *models.Job, operation string) error {
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.Job{}).Where("id = ?", job.ID).
			UpdateColumn("prune_version", gorm.Expr("prune_version + 1"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			job.PruneVersion = 0
			return tx.Create(job).Error
		}
		var version uint64
		if err := tx.Model(&models.Job{}).Where("id = ?", job.ID).Pluck("prune_version", &version).Error; err != nil {
			return err
		}
		job.PruneVersion = version
		return tx.Save(job).Error
	})
	if err != nil {
		return wrapDBErr(operation, fmt.Sprintf("job %s", job.ID), err)
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
	if len(prunedJobs) == 0 {
		return nil
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

	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for start := 0; start < len(prunedJobs); start += pruneDeleteBatchSize {
			end := start + pruneDeleteBatchSize
			if end > len(prunedJobs) {
				end = len(prunedJobs)
			}
			jobChunk := prunedJobs[start:end]
			jobIDs := make(map[string]struct{}, len(jobChunk))
			for _, job := range jobChunk {
				jobIDs[job.ID] = struct{}{}
			}
			opChunk := make([]models.BatchFileOperation, 0)
			for _, op := range prunedOps {
				if _, ok := jobIDs[op.BatchJobID]; ok {
					opChunk = append(opChunk, op)
				}
			}
			if err := deletePrunedJobChunk(tx, jobChunk, opChunk); err != nil {
				return err
			}
		}
		return nil
	})
}

const pruneDeleteBatchSize = 200

func deletePrunedJobChunk(tx *gorm.DB, jobs []models.Job, ops []models.BatchFileOperation) error {
	jobPredicates := make([]string, 0, len(jobs))
	jobArgs := make([]any, 0, len(jobs)*2)
	for _, job := range jobs {
		jobPredicates = append(jobPredicates, "(id = ? AND prune_version = ?)")
		jobArgs = append(jobArgs, job.ID, job.PruneVersion)
	}
	jobWhere := "status = ? AND (" + strings.Join(jobPredicates, " OR ") + ")"
	jobWhereArgs := append([]any{models.JobStatusOrganized}, jobArgs...)
	eligibleJobs := tx.Model(&models.Job{}).Select("id").Where(jobWhere, jobWhereArgs...)
	for start := 0; start < len(ops); start += pruneDeleteBatchSize {
		end := start + pruneDeleteBatchSize
		if end > len(ops) {
			end = len(ops)
		}
		opPredicates := make([]string, 0, end-start)
		opArgs := make([]any, 0, (end-start)*2)
		for _, op := range ops[start:end] {
			opPredicates = append(opPredicates, "(id = ? AND updated_at = ?)")
			opArgs = append(opArgs, op.ID, op.UpdatedAt)
		}
		opWhere := "(" + strings.Join(opPredicates, " OR ") + ") AND batch_job_id IN (?)"
		if err := tx.Where(opWhere, append(opArgs, eligibleJobs)...).Delete(&models.BatchFileOperation{}).Error; err != nil {
			return wrapDBErr("delete", "organized job operations", err)
		}
	}
	if err := tx.Where("id IN (?) AND NOT EXISTS (SELECT 1 FROM batch_file_operations WHERE batch_job_id = jobs.id)", eligibleJobs).Delete(&models.Job{}).Error; err != nil {
		return wrapDBErr("delete", "organized jobs", err)
	}
	return nil
}

func hasReplacementLedger(ops []models.BatchFileOperation) bool {
	for _, op := range ops {
		if strings.TrimSpace(op.GeneratedFiles) == "" {
			continue
		}
		gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
		if err != nil || len(gf.Replacements) > 0 {
			return true
		}
	}
	return false
}
