package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

const pruningJobStatus models.JobStatus = "pruning"

// ErrJobPruning reports that a mutation raced a durable organized-job prune claim.
var ErrJobPruning = errors.New("organized job is being pruned")

// ErrStaleEnvelopeGeneration reports that an envelope write was based on an
// older (or otherwise invalid) durable generation.
var ErrStaleEnvelopeGeneration = errors.New("stale envelope generation")

type pruneMaintenanceContextKey struct{}

// WithPruneMaintenance authorizes the retention hook to mutate operation
// journals while their owning jobs carry the durable pruning fence.
func WithPruneMaintenance(ctx context.Context) context.Context {
	return context.WithValue(ctx, pruneMaintenanceContextKey{}, true)
}

func pruneMaintenance(ctx context.Context) bool {
	return ctx != nil && ctx.Value(pruneMaintenanceContextKey{}) == true
}

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

// saveVersioned advances both durable fences before saving metadata fields.
// The conditional first write acquires SQLite's writer lock, so a stale
// whole-row caller cannot race a generation-aware envelope commit.
func (r *JobRepository) saveVersioned(ctx context.Context, job *models.Job, operation string) error {
	expectedGeneration := job.EnvelopeGeneration
	var saved models.Job
	var savedOK bool
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The conditional increment is the first statement: either this writer
		// wins the database writer lock before pruning claims the row, or the
		// pruning fence is observed and the mutation is rejected.
		result := tx.Model(&models.Job{}).
			Where("id = ? AND status <> ? AND envelope_generation = ?", job.ID, pruningJobStatus, expectedGeneration).
			Updates(map[string]any{
				"prune_version":       gorm.Expr("prune_version + 1"),
				"envelope_generation": gorm.Expr("envelope_generation + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var existing models.Job
			statusResult := tx.Select("status, envelope_generation").Where("id = ?", job.ID).First(&existing)
			switch {
			case errors.Is(statusResult.Error, gorm.ErrRecordNotFound):
				candidate := *job
				candidate.PruneVersion = 0
				if err := tx.Create(&candidate).Error; err != nil {
					return err
				}
				saved = candidate
				savedOK = true
				return nil
			case statusResult.Error != nil:
				return statusResult.Error
			case existing.Status == pruningJobStatus:
				return ErrJobPruning
			default:
				return ErrStaleEnvelopeGeneration
			}
		}
		var durable models.Job
		if err := tx.Where("id = ?", job.ID).First(&durable).Error; err != nil {
			return err
		}
		// Legacy Update/Upsert may change metadata fields, but envelope columns
		// are owned by CommitEnvelope and must come from the row we just fenced.
		candidate := *job
		preserveDurableEnvelopeColumns(&candidate, &durable)
		candidate.PruneVersion = durable.PruneVersion
		if err := tx.Save(&candidate).Error; err != nil {
			return err
		}
		saved = candidate
		savedOK = true
		return nil
	})
	if err != nil {
		return wrapDBErr(operation, fmt.Sprintf("job %s", job.ID), err)
	}
	if savedOK {
		*job = saved
	}
	return nil
}

func preserveDurableEnvelopeColumns(candidate, durable *models.Job) {
	candidate.EnvelopeGeneration = durable.EnvelopeGeneration
	candidate.ApplyPlan = cloneStringPtr(durable.ApplyPlan)
	candidate.Files = durable.Files
	candidate.Results = durable.Results
	candidate.Excluded = durable.Excluded
	candidate.FileMatchInfo = durable.FileMatchInfo
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// CommitEnvelope compare-and-accepts a job envelope at the next durable
// generation. The conditional generation update is the first transaction
// write, acquiring SQLite's writer lock before the payload is saved; stale
// callers therefore cannot overwrite a newer accepted envelope.
func (r *JobRepository) CommitEnvelope(ctx context.Context, job *models.Job, expectedGeneration uint64) (uint64, error) {
	if job == nil {
		return 0, fmt.Errorf("commit envelope: job must not be nil")
	}

	var (
		accepted      uint64
		acceptedPrune uint64
	)
	// SQLite may report a transient table lock when two same-base commits
	// race. Retry only that transient class; CAS/stale and context errors
	// return immediately.
	err := retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&models.Job{}).
				Where("id = ? AND status <> ? AND envelope_generation = ?", job.ID, pruningJobStatus, expectedGeneration).
				UpdateColumn("envelope_generation", gorm.Expr("envelope_generation + 1"))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				var existing models.Job
				statusResult := tx.Select("status, envelope_generation").Where("id = ?", job.ID).First(&existing)
				switch {
				case errors.Is(statusResult.Error, gorm.ErrRecordNotFound):
					return ErrNotFound
				case statusResult.Error != nil:
					return statusResult.Error
				case existing.Status == pruningJobStatus:
					return ErrJobPruning
				default:
					return ErrStaleEnvelopeGeneration
				}
			}

			var current struct {
				EnvelopeGeneration uint64
				PruneVersion       uint64
			}
			if err := tx.Model(&models.Job{}).
				Select("envelope_generation, prune_version").
				Where("id = ?", job.ID).Scan(&current).Error; err != nil {
				return err
			}
			accepted = current.EnvelopeGeneration
			acceptedPrune = current.PruneVersion
			// The caller's snapshot may predate a retention-fence increment; never
			// let an envelope commit roll that independent fence backward.
			candidate := *job
			candidate.EnvelopeGeneration = accepted
			candidate.PruneVersion = current.PruneVersion
			return tx.Save(&candidate).Error
		})
	})
	if err != nil {
		return 0, wrapDBErr("commit envelope", fmt.Sprintf("job %s", job.ID), err)
	}
	job.EnvelopeGeneration = accepted
	job.PruneVersion = acceptedPrune
	return accepted, nil
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
		// Claim is the first write. A concurrent job/operation writer either
		// commits before this fence and is included in the fresh snapshot, or
		// observes pruningJobStatus and is rejected until cleanup finishes.
		if err := tx.Model(&models.Job{}).
			Where("status = ? AND organized_at < ?", models.JobStatusOrganized, date).
			Update("status", pruningJobStatus).Error; err != nil {
			return wrapDBErr("claim", "organized jobs for pruning", err)
		}
		if err := tx.Where("status = ? AND organized_at < ?", pruningJobStatus, date).Find(&prunedJobs).Error; err != nil {
			return wrapDBErr("find", "organized jobs", err)
		}
		if len(prunedJobs) == 0 {
			return nil
		}
		for start := 0; start < len(prunedJobs); start += pruneDeleteBatchSize {
			end := start + pruneDeleteBatchSize
			if end > len(prunedJobs) {
				end = len(prunedJobs)
			}
			jobIDs := make([]string, 0, end-start)
			for _, job := range prunedJobs[start:end] {
				jobIDs = append(jobIDs, job.ID)
			}
			var chunk []models.BatchFileOperation
			if err := tx.Where("batch_job_id IN ?", jobIDs).Find(&chunk).Error; err != nil {
				return wrapDBErr("find", "organized job operations", err)
			}
			prunedOps = append(prunedOps, chunk...)
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
		if err := hook(WithPruneMaintenance(ctx), prunedOps); err != nil {
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
			if err := deletePrunedJobChunk(tx, jobChunk); err != nil {
				return err
			}
		}
		return nil
	})
}

const pruneDeleteBatchSize = 200

func deletePrunedJobChunk(tx *gorm.DB, jobs []models.Job) error {
	jobPredicates := make([]string, 0, len(jobs))
	jobArgs := make([]any, 0, len(jobs)*2)
	for _, job := range jobs {
		jobPredicates = append(jobPredicates, "(id = ? AND prune_version = ?)")
		jobArgs = append(jobArgs, job.ID, job.PruneVersion)
	}
	jobWhere := "status = ? AND (" + strings.Join(jobPredicates, " OR ") + ")"
	jobWhereArgs := append([]any{pruningJobStatus}, jobArgs...)
	eligibleJobs := tx.Model(&models.Job{}).Select("id").Where(jobWhere, jobWhereArgs...)
	if err := tx.Where("batch_job_id IN (?)", eligibleJobs).Delete(&models.BatchFileOperation{}).Error; err != nil {
		return wrapDBErr("delete", "organized job operations", err)
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
