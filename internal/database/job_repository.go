package database

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
func (r *JobRepository) DeleteOrganizedOlderThan(ctx context.Context, date time.Time) error {
	var prunedJobs []models.Job
	var prunedOps []models.BatchFileOperation
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		oldJobs := tx.Model(&models.Job{}).
			Select("id").
			Where("status = ? AND organized_at < ?", models.JobStatusOrganized, date)
		if err := tx.Where("status = ? AND organized_at < ?", models.JobStatusOrganized, date).Find(&prunedJobs).Error; err != nil {
			return wrapDBErr("find", "organized jobs", err)
		}
		if err := tx.Where("batch_job_id IN (?)", oldJobs).Find(&prunedOps).Error; err != nil {
			return wrapDBErr("find", "organized job operations", err)
		}
		if err := tx.Where("batch_job_id IN (?)", oldJobs).Delete(&models.BatchFileOperation{}).Error; err != nil {
			return wrapDBErr("delete", "organized job operations", err)
		}
		if err := tx.Where("status = ? AND organized_at < ?", models.JobStatusOrganized, date).Delete(&models.Job{}).Error; err != nil {
			return wrapDBErr("delete", "organized jobs", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if hook := r.organizedJobPruneHook(); hook != nil && len(prunedOps) > 0 {
		if err := hook(ctx, prunedOps); err != nil {
			restoreOps := pruneOpsForRestore(prunedOps, err)
			restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			restoreErr := r.restorePrunedRows(restoreCtx, prunedJobs, restoreOps)
			cancel()
			return errors.Join(wrapDBErr("prune", "organized job backups", err), restoreErr)
		}
	}
	return nil
}

// pruneOpsForRestore removes only entries that the cleanup hook confirms were
// already consumed. This prevents a partial filesystem cleanup from restoring
// a stale ledger reference to bytes that no longer exist.
func pruneOpsForRestore(ops []models.BatchFileOperation, hookErr error) []models.BatchFileOperation {
	type consumedProgress interface {
		ConsumedBackups() map[uint]map[string]struct{}
	}
	var progress consumedProgress
	if !errors.As(hookErr, &progress) {
		return ops
	}
	consumed := progress.ConsumedBackups()
	if len(consumed) == 0 {
		return ops
	}
	restored := make([]models.BatchFileOperation, len(ops))
	copy(restored, ops)
	for i := range restored {
		backups := consumed[restored[i].ID]
		if len(backups) == 0 {
			continue
		}
		gf, err := models.ParseGeneratedFiles(restored[i].GeneratedFiles)
		if err != nil {
			continue
		}
		kept := gf.Replacements[:0]
		for _, entry := range gf.Replacements {
			if _, removed := backups[entry.Backup]; !removed {
				kept = append(kept, entry)
			}
		}
		if len(kept) != len(gf.Replacements) {
			gf.Replacements = kept
			restored[i].GeneratedFiles = models.MarshalLedgerJSON(gf)
		}
	}
	return restored
}

// restorePrunedRows keeps the job and ledger snapshots durable when the
// post-commit filesystem cleanup cannot start or complete. The caller can
// retry pruning later without losing the backup ownership records.
func (r *JobRepository) restorePrunedRows(ctx context.Context, jobs []models.Job, ops []models.BatchFileOperation) error {
	if len(jobs) == 0 && len(ops) == 0 {
		return nil
	}
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range jobs {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&jobs[i]).Error; err != nil {
				return wrapDBErr("restore", fmt.Sprintf("organized job %s", jobs[i].ID), err)
			}
		}
		for i := range ops {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&ops[i]).Error; err != nil {
				return wrapDBErr("restore", fmt.Sprintf("organized job operation %d", ops[i].ID), err)
			}
		}
		return nil
	})
}
