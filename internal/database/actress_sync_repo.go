package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

const (
	actressSyncAttemptCap        = 3
	actressSyncStaleRetryCap     = 3
	actressSyncTerminalRetention = 20
)

// ErrActressSyncLeaseLost is returned when a sync task lease fence fails to
// match (lease stolen, expired, or task requeued). Exported: phase-3 manager
// heartbeat maps this to a worker retry.
var ErrActressSyncLeaseLost = errors.New("actress sync task lease lost")

var errActressSyncLeaseLost = ErrActressSyncLeaseLost

// ErrActressSyncNoCandidates is returned when a sync job would have no
// candidate tasks. Exported: phase 4 maps this to HTTP 409.
var ErrActressSyncNoCandidates = errors.New("actress sync has no candidates")

// errActressSyncJobCancelled fences task-scoped mutations against a parent
// job whose cancellation committed while its lease is still valid.
var errActressSyncJobCancelled = errors.New("actress sync job cancellation requested")

// ErrActressSyncCanonicalTaskRunning ...
var ErrActressSyncCanonicalTaskRunning = errors.New("canonical actress sync task is already running")

// ErrActressSyncIdentityChanged ...
var ErrActressSyncIdentityChanged = errors.New("actress sync identity changed during merge")

// ActressSyncRepository ...
type ActressSyncRepository struct{ db *DB }

// NewActressSyncRepository ...
func NewActressSyncRepository(db *DB) *ActressSyncRepository { return &ActressSyncRepository{db: db} }

// CreateJob ...
func (r *ActressSyncRepository) CreateJob(job *models.ActressSyncJob, tasks []models.ActressSyncTask) error {
	if len(tasks) == 0 {
		return ErrActressSyncNoCandidates
	}
	// P0: with SQLite foreign keys disabled a mismatched task.JobID would
	// silently orphan rows — bind empty IDs and reject mismatches up front.
	for i := range tasks {
		if tasks[i].JobID != "" && tasks[i].JobID != job.ID {
			return fmt.Errorf("task %s belongs to job %s, not %s: %w", tasks[i].ID, tasks[i].JobID, job.ID, ErrInvalidLookup)
		}
		tasks[i].JobID = job.ID
	}
	err := retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(job).Error; err != nil {
				return err
			}
			for i := range tasks {
				if tasks[i].ActressID != nil {
					if err := tx.Select("id").First(&models.Actress{}, "id = ?", *tasks[i].ActressID).Error; err != nil {
						return fmt.Errorf("validate actress %d: %w", *tasks[i].ActressID, err)
					}
					var conflict models.ActressSyncTask
					lookupErr := tx.Table("actress_sync_tasks AS task").Select("task.*").Joins("JOIN actress_sync_jobs AS job ON job.id = task.job_id").Where("task.actress_id = ? AND task.status IN ? AND job.cancel_requested = 0", *tasks[i].ActressID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).Order("CASE WHEN job.scope = 'selected' THEN 0 ELSE 1 END, task.created_at ASC, task.id ASC").First(&conflict).Error
					if lookupErr == nil {
						var conflictJob models.ActressSyncJob
						if lookupErr := tx.First(&conflictJob, "id = ?", conflict.JobID).Error; lookupErr != nil {
							return lookupErr
						}
						prepareActressSyncDuplicateTask(&tasks[i], job.Scope, conflictJob.Scope, time.Now().UTC())
						if err := tx.Create(&tasks[i]).Error; err != nil {
							return err
						}
						continue
					}
					if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
						return lookupErr
					}
				}
				if err := createActressSyncTaskTx(tx, &tasks[i], job); err != nil {
					return err
				}
			}
			if err := r.refreshJobTx(tx, job.ID, time.Now().UTC()); err != nil {
				return err
			}
			return r.pruneTerminalJobsTx(tx)
		})
	})
	if err != nil {
		return wrapDBErr("create", "actress sync job", err)
	}
	fresh, err := r.FindJob(job.ID)
	if err == nil {
		*job = *fresh
	}
	return err
}

func createActressSyncTaskTx(tx *gorm.DB, task *models.ActressSyncTask, job *models.ActressSyncJob) error {
	if err := tx.Create(task).Error; err != nil {
		if !IsUniqueConstraint(err) {
			return err
		}
		var conflict models.ActressSyncTask
		if lookupErr := tx.Where("dedupe_key = ? AND status IN ?", task.DedupeKey, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).First(&conflict).Error; lookupErr != nil {
			if errors.Is(lookupErr, gorm.ErrRecordNotFound) {
				// PK collision or another unique index: surface the real error
				// instead of a misleading dedupe lookup failure.
				return err
			}
			return lookupErr
		}
		var conflictJob models.ActressSyncJob
		if lookupErr := tx.First(&conflictJob, "id = ?", conflict.JobID).Error; lookupErr != nil {
			return lookupErr
		}
		if conflictJob.CancelRequested {
			// The existing task's job is being cancelled: supersede it so the
			// fresh request stays runnable, instead of skipping the duplicate
			// and leaving nothing to run. Match supersedeCancelledDedupeHolderTx:
			// a pending (not just running) holder keeps the partial-index slot.
			if updErr := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status IN ?", conflict.ID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).Update("dedupe_key", fmt.Sprintf("actress:%d:superseded:%s", actressIDOrZero(conflict.ActressID), conflict.ID)).Error; updErr != nil {
				return updErr
			}
			return tx.Create(task).Error
		}
		prepareActressSyncDuplicateTask(task, job.Scope, conflictJob.Scope, time.Now().UTC())
		if err := tx.Create(task).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *ActressSyncRepository) pruneTerminalJobsTx(tx *gorm.DB) error {
	var jobIDs []string
	if err := tx.Model(&models.ActressSyncJob{}).
		Where("status IN ?", []string{models.ActressSyncJobCompleted, models.ActressSyncJobCancelled}).
		Order("COALESCE(completed_at, created_at) DESC, created_at DESC, id DESC").
		Offset(actressSyncTerminalRetention).Limit(-1).Pluck("id", &jobIDs).Error; err != nil {
		return err
	}
	if len(jobIDs) == 0 {
		return nil
	}
	if err := tx.Where("job_id IN ?", jobIDs).Delete(&models.ActressSyncTask{}).Error; err != nil {
		return err
	}
	return tx.Where("id IN ?", jobIDs).Delete(&models.ActressSyncJob{}).Error
}

// FindJob ...
func (r *ActressSyncRepository) FindJob(id string) (*models.ActressSyncJob, error) {
	var job models.ActressSyncJob
	if err := r.db.First(&job, "id = ?", id).Error; err != nil {
		// Map to the repository sentinel like BaseRepository.FindByID: unknown
		// or pruned jobs are ErrNotFound (404), not a database failure.
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find actress sync job %v: %w", id, ErrNotFound)
		}
		return nil, wrapDBErr("find", "actress sync job", err)
	}
	return &job, nil
}

// ListActiveJobs ...
func (r *ActressSyncRepository) ListActiveJobs() ([]models.ActressSyncJob, error) {
	jobs := make([]models.ActressSyncJob, 0)
	err := r.db.Where("status IN ?", []string{models.ActressSyncJobPending, models.ActressSyncJobRunning}).Order("created_at ASC").Find(&jobs).Error
	return jobs, err
}

const (
	// listTasksDefaultLimit bounds the default all-tasks view so a huge sync
	// job cannot force an unbounded read + JSON payload.
	listTasksDefaultLimit = 500
	listTasksMaxLimit     = 1000
)

// ListTasks ...
func (r *ActressSyncRepository) ListTasks(jobID string, limit int) ([]models.ActressSyncTask, error) {
	if limit <= 0 {
		limit = listTasksDefaultLimit
	}
	if limit > listTasksMaxLimit {
		limit = listTasksMaxLimit // clamp, don't collapse: 1001 must not look identical to "no limit"
	}
	tasks := make([]models.ActressSyncTask, 0)
	err := r.db.Where("job_id = ?", jobID).Order("created_at ASC, id ASC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// ListRunningTasks returns currently running tasks for a sync job.
func (r *ActressSyncRepository) ListRunningTasks(jobID string) ([]models.ActressSyncTask, error) {
	tasks := make([]models.ActressSyncTask, 0)
	err := r.db.Where("job_id = ? AND status = ?", jobID, models.ActressSyncTaskRunning).Order("started_at ASC, id ASC").Find(&tasks).Error
	return tasks, err
}

// ListDiagnosticTasks returns a bounded terminal-task diagnostic history for a sync job.
func (r *ActressSyncRepository) ListDiagnosticTasks(jobID string, limit int) ([]models.ActressSyncTask, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > listTasksMaxLimit {
		limit = listTasksMaxLimit
	}
	tasks := make([]models.ActressSyncTask, 0)
	err := r.db.Where("job_id = ? AND (status IN ? OR TRIM(COALESCE(warning, '')) <> '' OR TRIM(COALESCE(error_message, '')) <> '')", jobID, []string{models.ActressSyncTaskSkipped, models.ActressSyncTaskConflict, models.ActressSyncTaskFailed, models.ActressSyncTaskCancelled}).
		Order("completed_at DESC, created_at DESC, id DESC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// CountTasks returns the real (unbounded) number of tasks for a job matching
// the given view: "" counts all, "active" counts running, "diagnostics"
// mirrors ListDiagnosticTasks' filter.
func (r *ActressSyncRepository) CountTasks(jobID, view string) (int64, error) {
	switch view {
	case "", "active", "diagnostics":
	default:
		// Same strictness as resolveActressFilter: an unknown view must not
		// silently degrade to count-all, or phase-4 callers would misreport.
		return 0, wrapDBErr("count", "actress sync tasks", ErrInvalidLookup)
	}
	query := r.db.Model(&models.ActressSyncTask{}).Where("job_id = ?", jobID)
	switch view {
	case "active":
		query = query.Where("status = ?", models.ActressSyncTaskRunning)
	case "diagnostics":
		query = query.Where("status IN ? OR TRIM(COALESCE(warning, '')) <> '' OR TRIM(COALESCE(error_message, '')) <> ''", []string{models.ActressSyncTaskSkipped, models.ActressSyncTaskConflict, models.ActressSyncTaskFailed, models.ActressSyncTaskCancelled})
	}
	var count int64
	err := query.Count(&count).Error
	return count, err
}

// ClaimNext ...
func (r *ActressSyncRepository) ClaimNext(owner string, leaseUntil time.Time) (*models.ActressSyncTask, error) {
	leaseUntil = leaseUntil.UTC() // callers may hand local-time values; storage+comparison are UTC
	var claimed models.ActressSyncTask
	err := retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			now, token := time.Now().UTC(), uuid.NewString()
			pendingTaskID := tx.Table("actress_sync_tasks AS task").Joins("JOIN actress_sync_jobs AS job ON job.id = task.job_id").
				Where("task.status = ? AND task.attempts < ? AND job.cancel_requested = 0", models.ActressSyncTaskPending, actressSyncAttemptCap).
				Where(`NOT (
					task.actress_id IS NOT NULL AND (
						EXISTS (SELECT 1 FROM actress_sync_tasks AS deferred WHERE deferred.id <> task.id AND deferred.actress_id = task.actress_id AND deferred.dedupe_key LIKE ? AND deferred.status = ?)
						OR (task.dedupe_key LIKE ? AND EXISTS (SELECT 1 FROM actress_sync_tasks AS canonical WHERE canonical.id <> task.id AND canonical.dedupe_key = ('actress:' || task.actress_id) AND canonical.status IN (?, ?)))
					)
				)`, "%:deferred:%", models.ActressSyncTaskRunning, "%:deferred:%", models.ActressSyncTaskPending, models.ActressSyncTaskRunning).
				Order("task.created_at ASC, task.id ASC").Limit(1).Select("task.id")
			res := tx.Model(&models.ActressSyncTask{}).Where("id IN (?) AND status = ?", pendingTaskID, models.ActressSyncTaskPending).Updates(map[string]any{
				"status": models.ActressSyncTaskRunning, "stage": "resolving", "lease_owner": owner, "lease_token": token,
				"heartbeat_at": now, "lease_expires_at": leaseUntil, "attempts": gorm.Expr("attempts + 1"), "started_at": gorm.Expr("COALESCE(started_at, ?)", now),
			})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return nil
			}
			if err := tx.Where("lease_token = ? AND lease_owner = ?", token, owner).First(&claimed).Error; err != nil {
				return err
			}
			if claimed.ActressID != nil && isDeferredActressSyncDedupeKey(claimed.DedupeKey) {
				canonicalKey := fmt.Sprintf("actress:%d", *claimed.ActressID)
				result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND lease_token = ?", claimed.ID, models.ActressSyncTaskRunning, token).Update("dedupe_key", canonicalKey)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return errActressSyncLeaseLost
				}
				claimed.DedupeKey = canonicalKey
			}
			if err := tx.Model(&models.ActressSyncJob{}).Where("id = ? AND status = ?", claimed.JobID, models.ActressSyncJobPending).Updates(map[string]any{"status": models.ActressSyncJobRunning, "started_at": now}).Error; err != nil {
				return err
			}
			return nil
		})
	})
	if err != nil {
		return nil, wrapDBErr("claim", "actress sync task", err)
	}
	if claimed.ID == "" {
		return nil, nil
	}
	return &claimed, nil
}

// RecoverExpiredLeases ...
func (r *ActressSyncRepository) RecoverExpiredLeases(now time.Time) error {
	now = now.UTC() // leases are written UTC; text DATETIME compares are offset-blind
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			var tasks []models.ActressSyncTask
			if err := tx.Where("status = ? AND (lease_expires_at IS NULL OR lease_expires_at <= ?)", models.ActressSyncTaskRunning, now).Find(&tasks).Error; err != nil {
				return err
			}
			jobs := map[string]struct{}{}
			for _, task := range tasks {
				var job models.ActressSyncJob
				if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
					return err
				}
				transitioned, err := r.recoverExpiredTask(tx, task, job.CancelRequested, now)
				if err != nil {
					return err
				}
				if transitioned {
					jobs[task.JobID] = struct{}{}
				}
			}
			for id := range jobs {
				if err := r.refreshJobTx(tx, id, now); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

func (r *ActressSyncRepository) recoverExpiredTask(tx *gorm.DB, task models.ActressSyncTask, cancelRequested bool, now time.Time) (bool, error) {
	updates := map[string]any{"lease_owner": "", "lease_token": "", "lease_expires_at": nil}
	switch {
	case cancelRequested:
		updates["status"], updates["stage"], updates["outcome"], updates["completed_at"] = models.ActressSyncTaskCancelled, "completed", "cancelled", now
	case task.Attempts >= actressSyncAttemptCap:
		updates["status"], updates["stage"], updates["outcome"], updates["error_message"], updates["completed_at"] = models.ActressSyncTaskFailed, "completed", "failed", "attempt_cap_reached", now
	default:
		updates["status"], updates["stage"] = models.ActressSyncTaskPending, "queued"
	}
	query := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", task.ID, models.ActressSyncTaskRunning, task.LeaseOwner, task.LeaseToken)
	if task.LeaseExpiresAt == nil {
		query = query.Where("lease_expires_at IS NULL")
	} else {
		query = query.Where("lease_expires_at = ? AND lease_expires_at <= ?", *task.LeaseExpiresAt, now)
	}
	res := query.Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *ActressSyncRepository) releaseOwnerTask(tx *gorm.DB, task models.ActressSyncTask, cancelRequested bool, now time.Time) (bool, error) {
	updates := map[string]any{"status": models.ActressSyncTaskPending, "stage": "queued", "lease_owner": "", "lease_token": "", "heartbeat_at": nil, "lease_expires_at": nil}
	switch {
	case cancelRequested:
		updates["status"], updates["stage"], updates["outcome"], updates["completed_at"] = models.ActressSyncTaskCancelled, "completed", "cancelled", now
		if task.Attempts > 0 {
			updates["attempts"] = task.Attempts - 1
		}
	case task.Attempts >= actressSyncAttemptCap:
		updates["status"], updates["stage"], updates["outcome"], updates["error_message"], updates["completed_at"] = models.ActressSyncTaskFailed, "completed", "failed", "attempt_cap_reached", now
	}
	query := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND lease_owner = ? AND lease_token = ?", task.ID, models.ActressSyncTaskRunning, task.LeaseOwner, task.LeaseToken)
	if task.LeaseExpiresAt == nil {
		query = query.Where("lease_expires_at IS NULL")
	} else {
		query = query.Where("lease_expires_at = ?", *task.LeaseExpiresAt)
	}
	res := query.Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ReleaseOwnerLeases ...
func (r *ActressSyncRepository) ReleaseOwnerLeases(owner string) error {
	if owner == "" {
		return nil
	}
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			var tasks []models.ActressSyncTask
			if err := tx.Where("status = ? AND lease_owner = ?", models.ActressSyncTaskRunning, owner).Find(&tasks).Error; err != nil {
				return err
			}
			now := time.Now().UTC()
			jobs := map[string]struct{}{}
			for _, task := range tasks {
				var job models.ActressSyncJob
				if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
					return err
				}
				transitioned, err := r.releaseOwnerTask(tx, task, job.CancelRequested, now)
				if err != nil {
					return err
				}
				if transitioned {
					jobs[task.JobID] = struct{}{}
				}
			}
			for id := range jobs {
				if err := r.refreshJobTx(tx, id, now); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// MergeForSyncTask ...
func (r *ActressRepository) MergeForSyncTask(ctx context.Context, targetID, sourceID uint, resolutions map[string]string, taskID, leaseToken string) (*ActressMergeResult, error) {
	return r.MergeForSyncTaskWithSource(ctx, targetID, sourceID, resolutions, models.Actress{}, taskID, leaseToken)
}

// MergeForSyncTaskWithSource ...
func (r *ActressRepository) MergeForSyncTaskWithSource(ctx context.Context, targetID, sourceID uint, resolutions map[string]string, expectedSource models.Actress, taskID, leaseToken string) (*ActressMergeResult, error) {
	return r.MergeForSyncTaskWithTargetAndSource(ctx, targetID, sourceID, resolutions, models.Actress{}, expectedSource, taskID, leaseToken)
}

// MergeForSyncTaskWithTargetAndSource ...
func (r *ActressRepository) MergeForSyncTaskWithTargetAndSource(ctx context.Context, targetID, sourceID uint, resolutions map[string]string, expectedTarget, expectedSource models.Actress, taskID, leaseToken string) (*ActressMergeResult, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(leaseToken) == "" {
		return nil, ErrInvalidLookup
	}
	plan, err := r.merger.PlanMerge(ctx, targetID, sourceID, resolutions)
	if err != nil {
		return nil, err
	}
	syncRepo := NewActressSyncRepository(r.GetDB())
	return r.merger.executeMerge(ctx, plan, r.GetDB(), targetAndSourceIdentityPrecondition(expectedTarget, expectedSource), func(tx *gorm.DB, canonicalID, duplicateID uint) error {
		return syncRepo.reassignTaskActressTx(tx, taskID, leaseToken, canonicalID, duplicateID)
	})
}

// MergeWithSource ...
func (r *ActressRepository) MergeWithSource(ctx context.Context, targetID, sourceID uint, resolutions map[string]string, expectedSource models.Actress) (*ActressMergeResult, error) {
	plan, err := r.merger.PlanMerge(ctx, targetID, sourceID, resolutions)
	if err != nil {
		return nil, err
	}
	return r.merger.executeMerge(ctx, plan, r.GetDB(), sourceIdentityPrecondition(expectedSource), nil)
}

func targetAndSourceIdentityPrecondition(expectedTarget, expectedSource models.Actress) func(*gorm.DB, *models.Actress, *models.Actress) error {
	return func(tx *gorm.DB, target, source *models.Actress) error {
		if expectedTarget.ID > 0 && !cachedIdentitySourceMatches(expectedTarget, *target) {
			return ErrActressSyncIdentityChanged
		}
		return sourceIdentityPrecondition(expectedSource)(tx, target, source)
	}
}

func sourceIdentityPrecondition(expected models.Actress) func(*gorm.DB, *models.Actress, *models.Actress) error {
	return func(_ *gorm.DB, _, source *models.Actress) error {
		if expected.ID > 0 && !cachedIdentitySourceMatches(expected, *source) {
			return ErrActressSyncIdentityChanged
		}
		return nil
	}
}

func cachedIdentityPrecondition(expectedDMMID int, expectedSource models.Actress) func(*gorm.DB, *models.Actress, *models.Actress) error {
	return func(_ *gorm.DB, target, source *models.Actress) error {
		if expectedDMMID <= 0 || target.DMMID != expectedDMMID || source.DMMID > 0 {
			return ErrActressSyncIdentityChanged
		}
		return sourceIdentityPrecondition(expectedSource)(nil, target, source)
	}
}

func cachedIdentitySourceMatches(expected, actual models.Actress) bool {
	return expected.ID == actual.ID &&
		expected.DMMID == actual.DMMID &&
		expected.FirstName == actual.FirstName &&
		expected.LastName == actual.LastName &&
		expected.JapaneseName == actual.JapaneseName &&
		expected.ThumbURL == actual.ThumbURL &&
		expected.Aliases == actual.Aliases &&
		expected.CreatedAt.Equal(actual.CreatedAt) &&
		expected.UpdatedAt.Equal(actual.UpdatedAt)
}

// MergeCachedIdentity ...
func (r *ActressRepository) MergeCachedIdentity(ctx context.Context, targetID, sourceID uint, expectedDMMID int) (*ActressMergeResult, error) {
	return r.MergeCachedIdentityWithSource(ctx, targetID, sourceID, expectedDMMID, models.Actress{})
}

// MergeCachedIdentityWithSource ...
func (r *ActressRepository) MergeCachedIdentityWithSource(ctx context.Context, targetID, sourceID uint, expectedDMMID int, expectedSource models.Actress) (*ActressMergeResult, error) {
	plan, err := r.merger.PlanMerge(ctx, targetID, sourceID, nil)
	if err != nil {
		return nil, err
	}
	return r.merger.executeMerge(ctx, plan, r.GetDB(), cachedIdentityPrecondition(expectedDMMID, expectedSource), nil)
}

// MergeCachedIdentityForSyncTask ...
func (r *ActressRepository) MergeCachedIdentityForSyncTask(ctx context.Context, targetID, sourceID uint, expectedDMMID int, taskID, leaseToken string) (*ActressMergeResult, error) {
	return r.MergeCachedIdentityForSyncTaskWithSource(ctx, targetID, sourceID, expectedDMMID, models.Actress{}, taskID, leaseToken)
}

// MergeCachedIdentityForSyncTaskWithSource ...
func (r *ActressRepository) MergeCachedIdentityForSyncTaskWithSource(ctx context.Context, targetID, sourceID uint, expectedDMMID int, expectedSource models.Actress, taskID, leaseToken string) (*ActressMergeResult, error) {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(leaseToken) == "" {
		return nil, ErrInvalidLookup
	}
	plan, err := r.merger.PlanMerge(ctx, targetID, sourceID, nil)
	if err != nil {
		return nil, err
	}
	syncRepo := NewActressSyncRepository(r.GetDB())
	return r.merger.executeMerge(ctx, plan, r.GetDB(), cachedIdentityPrecondition(expectedDMMID, expectedSource), func(tx *gorm.DB, canonicalID, duplicateID uint) error {
		return syncRepo.reassignTaskActressTx(tx, taskID, leaseToken, canonicalID, duplicateID)
	})
}

func migrateActiveActressSyncTasksTx(tx *gorm.DB, actressID, sourceID uint) error {
	var sourceTasks []models.ActressSyncTask
	if err := tx.Where("actress_id = ? AND status IN ?", sourceID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).Order("created_at ASC, id ASC").Find(&sourceTasks).Error; err != nil {
		return err
	}
	jobIDs := make(map[string]struct{})
	for _, sourceTask := range sourceTasks {
		jobIDs[sourceTask.JobID] = struct{}{}
		var sourceJob models.ActressSyncJob
		if err := tx.First(&sourceJob, "id = ?", sourceTask.JobID).Error; err != nil {
			return err
		}
		var targetTask models.ActressSyncTask
		err := tx.Table("actress_sync_tasks AS task").Select("task.*").Joins("JOIN actress_sync_jobs AS job ON job.id = task.job_id").Where("task.actress_id = ? AND task.id <> ? AND task.status IN ? AND job.cancel_requested = 0", actressID, sourceTask.ID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).Order("CASE WHEN job.scope = 'selected' THEN 0 ELSE 1 END, task.created_at ASC, task.id ASC").First(&targetTask).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// The winner lookup skips cancel-requested jobs, but a still-running
			// task of such a job holds the canonical dedupe key; migrating onto
			// it would violate the unique index and roll back the whole merge.
			if supErr := supersedeCancelledDedupeHolderTx(tx, actressID, sourceTask.ID); supErr != nil {
				return supErr
			}
			if err := migrateActiveActressSyncTaskTx(tx, sourceTask, actressID, false, sourceJob.CancelRequested); err != nil {
				return err
			}
			continue
		}
		var targetJob models.ActressSyncJob
		if err := tx.First(&targetJob, "id = ?", targetTask.JobID).Error; err != nil {
			return err
		}
		// Skipping a winner to migrate the source onto the canonical key is
		// only safe when the winner actually holds that key — a deferred
		// holder does not, and the running canonical holder would collide.
		winnerHoldsCanonical := targetTask.DedupeKey == fmt.Sprintf("actress:%d", actressID)
		if isDeferredActressSyncDedupeKey(targetTask.DedupeKey) {
			// The deferred task already queues this actress for its canonical
			// run; migrating the source too would duplicate that work later.
			if sourceTask.Status == models.ActressSyncTaskPending {
				if err := skipMergedActressSyncTaskTx(tx, sourceTask, actressID); err != nil {
					return err
				}
				continue
			}
			// Report truthfully: the source's job was not necessarily cancelled —
			// the running task is deferred (requeued), not marked cancelled.
			if err := migrateActiveActressSyncTaskTx(tx, sourceTask, actressID, true, sourceJob.CancelRequested); err != nil {
				return err
			}
			continue
		}
		if targetTask.Status == models.ActressSyncTaskPending && actressSyncScopePriority(sourceJob.Scope) >= actressSyncScopePriority(targetJob.Scope) && sourceTask.Status == models.ActressSyncTaskRunning && !sourceJob.CancelRequested && winnerHoldsCanonical {
			if err := skipActiveActressSyncTaskTx(tx, targetTask); err != nil {
				return err
			}
			jobIDs[targetTask.JobID] = struct{}{}
			if err := migrateActiveActressSyncTaskTx(tx, sourceTask, actressID, false, sourceJob.CancelRequested); err != nil {
				return err
			}
			continue
		}
		if sourceTask.Status == models.ActressSyncTaskPending && actressSyncScopePriority(targetJob.Scope) >= actressSyncScopePriority(sourceJob.Scope) {
			if err := skipMergedActressSyncTaskTx(tx, sourceTask, actressID); err != nil {
				return err
			}
			continue
		}
		// A cancel-requested source must not displace the pending winner: its
		// migration would settle as cancelled, leaving nothing runnable.
		if targetTask.Status == models.ActressSyncTaskPending && actressSyncScopePriority(sourceJob.Scope) > actressSyncScopePriority(targetJob.Scope) && !sourceJob.CancelRequested && winnerHoldsCanonical {
			if err := skipActiveActressSyncTaskTx(tx, targetTask); err != nil {
				return err
			}
			jobIDs[targetTask.JobID] = struct{}{}
			if err := migrateActiveActressSyncTaskTx(tx, sourceTask, actressID, false, sourceJob.CancelRequested); err != nil {
				return err
			}
			continue
		}
		if err := migrateActiveActressSyncTaskTx(tx, sourceTask, actressID, true, sourceJob.CancelRequested); err != nil {
			return err
		}
	}
	for jobID := range jobIDs {
		if err := (&ActressSyncRepository{}).refreshJobTx(tx, jobID, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

// supersedeCancelledDedupeHolderTx frees the canonical dedupe key held by a
// task whose job was cancelled: the key must be reusable for migrations that
// run during the cancellation window.
func supersedeCancelledDedupeHolderTx(tx *gorm.DB, canonicalActressID uint, excludeTaskID string) error {
	var holder models.ActressSyncTask
	err := tx.Table("actress_sync_tasks AS task").Joins("JOIN actress_sync_jobs AS job ON job.id = task.job_id").
		Where("task.actress_id = ? AND task.id <> ? AND task.dedupe_key = ? AND task.status IN ? AND job.cancel_requested = 1",
			canonicalActressID, excludeTaskID, fmt.Sprintf("actress:%d", canonicalActressID), []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).
		First(&holder).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return tx.Model(&models.ActressSyncTask{}).
		Where("id = ? AND status IN ?", holder.ID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).
		Update("dedupe_key", fmt.Sprintf("actress:%d:superseded:%s", canonicalActressID, holder.ID)).Error
}

func migrateActiveActressSyncTaskTx(tx *gorm.DB, task models.ActressSyncTask, actressID uint, deferred, cancelRequested bool) error {
	fields, _ := appendSyncTaskFields(task.UpdatedFields, []string{"merged_duplicate"})
	key := fmt.Sprintf("actress:%d", actressID)
	if deferred {
		key = deferredActressSyncDedupeKey(actressID, task.ID)
	}
	updates := map[string]any{
		"actress_id": actressID, "dedupe_key": key, "updated_fields": fields,
	}
	if task.Status == models.ActressSyncTaskRunning {
		updates["status"] = models.ActressSyncTaskPending
		updates["stage"] = "queued"
		updates["outcome"] = ""
		updates["error_message"] = ""
		updates["completed_at"] = nil
		updates["lease_owner"] = ""
		updates["lease_token"] = ""
		updates["heartbeat_at"] = nil
		updates["lease_expires_at"] = nil
		updates["attempts"] = gorm.Expr("CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END")
		if cancelRequested {
			updates["status"] = models.ActressSyncTaskCancelled
			updates["stage"] = "completed"
			updates["outcome"] = "cancelled"
			updates["completed_at"] = time.Now().UTC()
			updates["attempts"] = task.Attempts
		}
	}
	result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND actress_id = ? AND status IN ?", task.ID, *task.ActressID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	// Mirror the sibling migrations: a no-op must not look like success.
	if result.RowsAffected != 1 {
		return fmt.Errorf("actress sync task %s state changed during merge", task.ID)
	}
	return nil
}

func skipActiveActressSyncTaskTx(tx *gorm.DB, task models.ActressSyncTask) error {
	now := time.Now().UTC()
	messages, _ := json.Marshal([]string{"coalesced_into_merged_task"})
	// Free the canonical dedupe key: the running task replacing this winner is
	// migrated onto it next, and a lingering key on this skipped row would trip
	// the unique index and roll back the whole merge.
	actressIDRef := uint(0)
	if task.ActressID != nil {
		actressIDRef = *task.ActressID
	}
	result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ?", task.ID, models.ActressSyncTaskPending).Updates(map[string]any{
		"status": models.ActressSyncTaskSkipped, "stage": "completed", "outcome": "skipped", "messages": string(messages), "completed_at": now,
		"dedupe_key": fmt.Sprintf("actress:%d:skipped:%s", actressIDRef, task.ID),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("actress sync task %s was not pending during merge", task.ID)
	}
	return nil
}

func skipMergedActressSyncTaskTx(tx *gorm.DB, task models.ActressSyncTask, actressID uint) error {
	now := time.Now().UTC()
	messages, _ := json.Marshal([]string{"coalesced_into_merged_task"})
	result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ?", task.ID, models.ActressSyncTaskPending).Updates(map[string]any{
		"actress_id": actressID, "dedupe_key": fmt.Sprintf("actress:%d:merged:%s", actressID, task.ID), "status": models.ActressSyncTaskSkipped, "stage": "completed", "outcome": "skipped", "messages": string(messages), "completed_at": now,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("actress sync task %s was not pending during merge", task.ID)
	}
	return nil
}

func (r *ActressSyncRepository) reassignTaskActressTx(tx *gorm.DB, id, token string, actressID, expectedActressID uint) error {
	leaseNow := time.Now().UTC()
	var task models.ActressSyncTask
	if err := tx.Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", id, models.ActressSyncTaskRunning, token, leaseNow).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errActressSyncLeaseLost
		}
		return err
	}
	if task.ActressID == nil || *task.ActressID != expectedActressID {
		return errActressSyncLeaseLost
	}
	dedupeKey := fmt.Sprintf("actress:%d", actressID)
	currentDedupeKey := dedupeKey
	var taskJob models.ActressSyncJob
	if err := tx.First(&taskJob, "id = ?", task.JobID).Error; err != nil {
		return err
	}
	if taskJob.CancelRequested {
		// Fence on job cancellation here too: lease validity alone must not
		// allow post-cancel commits from the task-scoped merge paths.
		return errActressSyncJobCancelled
	}
	var conflict models.ActressSyncTask
	holderPriority := actressSyncScopePriority(taskJob.Scope)
	err := tx.Where("id <> ? AND dedupe_key = ? AND status IN ?", id, dedupeKey, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).First(&conflict).Error
	if err == nil {
		if conflict.Status == models.ActressSyncTaskRunning {
			return fmt.Errorf("%w: actress %d", ErrActressSyncCanonicalTaskRunning, actressID)
		}
		var conflictJob models.ActressSyncJob
		if err := tx.First(&conflictJob, "id = ?", conflict.JobID).Error; err != nil {
			return err
		}
		if actressSyncScopePriority(conflictJob.Scope) > actressSyncScopePriority(taskJob.Scope) {
			currentDedupeKey = deferredActressSyncDedupeKey(actressID, id)
			holderPriority = actressSyncScopePriority(conflictJob.Scope)
		} else {
			now := time.Now().UTC()
			messages, _ := json.Marshal([]string{"coalesced_into_merged_task"})
			skipped := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ?", conflict.ID, models.ActressSyncTaskPending).Updates(map[string]any{
				"status": models.ActressSyncTaskSkipped, "stage": "completed", "outcome": "skipped", "messages": string(messages), "completed_at": now,
			})
			if skipped.Error != nil {
				return skipped.Error
			}
			if skipped.RowsAffected != 1 {
				return fmt.Errorf("actress sync task %s was not pending during reassign", conflict.ID)
			}
			if err := r.refreshJobTx(tx, conflict.JobID, now); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var deferredTasks []models.ActressSyncTask
	if err := tx.Where("id <> ? AND actress_id = ? AND status = ?", id, expectedActressID, models.ActressSyncTaskPending).Find(&deferredTasks).Error; err != nil {
		return err
	}
	for _, deferredTask := range deferredTasks {
		var deferredJob models.ActressSyncJob
		if err := tx.First(&deferredJob, "id = ?", deferredTask.JobID).Error; err != nil {
			return err
		}
		if actressSyncScopePriority(deferredJob.Scope) <= holderPriority {
			now := time.Now().UTC()
			messages, _ := json.Marshal([]string{"coalesced_into_merged_task"})
			result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND actress_id = ?", deferredTask.ID, models.ActressSyncTaskPending, expectedActressID).Updates(map[string]any{
				"status": models.ActressSyncTaskSkipped, "stage": "completed", "outcome": "skipped", "messages": string(messages), "completed_at": now,
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errActressSyncLeaseLost
			}
			if err := r.refreshJobTx(tx, deferredJob.ID, now); err != nil {
				return err
			}
			continue
		}
		if err := migrateActressSyncTaskTx(tx, deferredTask, actressID, expectedActressID); err != nil {
			return err
		}
	}
	fields, _ := appendSyncTaskFields(task.UpdatedFields, []string{"merged_duplicate"})
	result := tx.Model(&models.ActressSyncTask{}).
		Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ? AND actress_id = ?", id, models.ActressSyncTaskRunning, token, leaseNow, expectedActressID).
		Updates(map[string]any{"actress_id": actressID, "dedupe_key": currentDedupeKey, "updated_fields": fields})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errActressSyncLeaseLost
	}
	return nil
}

func migrateActressSyncTaskTx(tx *gorm.DB, task models.ActressSyncTask, actressID, expectedActressID uint) error {
	fields, _ := appendSyncTaskFields(task.UpdatedFields, []string{"merged_duplicate"})
	result := tx.Model(&models.ActressSyncTask{}).
		Where("id = ? AND status = ? AND actress_id = ?", task.ID, models.ActressSyncTaskPending, expectedActressID).
		Updates(map[string]any{"actress_id": actressID, "dedupe_key": deferredActressSyncDedupeKey(actressID, task.ID), "updated_fields": fields})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errActressSyncLeaseLost
	}
	return nil
}

func actressSyncScopePriority(scope string) int {
	if strings.EqualFold(strings.TrimSpace(scope), "selected") {
		return 2
	}
	return 1
}

func deferredActressSyncDedupeKey(actressID uint, taskID string) string {
	return fmt.Sprintf("actress:%d:deferred:%s", actressID, taskID)
}

func isDeferredActressSyncDedupeKey(key string) bool {
	return strings.Contains(key, ":deferred:")
}

func prepareActressSyncDuplicateTask(task *models.ActressSyncTask, incomingScope, conflictScope string, now time.Time) {
	if task.ActressID != nil && actressSyncScopePriority(incomingScope) > actressSyncScopePriority(conflictScope) {
		task.DedupeKey = deferredActressSyncDedupeKey(*task.ActressID, task.ID)
		task.Messages = []string{"deferred_to_stronger_sync_task"}
		return
	}
	task.Status, task.Stage, task.Outcome = models.ActressSyncTaskSkipped, "completed", "skipped"
	task.Messages = []string{"duplicate_active_task"}
	task.CompletedAt = &now
	task.DedupeKey += ":duplicate:" + task.ID
}

func mergeSyncTaskFields(existing, additional []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additional))
	fields := make([]string, 0, len(existing)+len(additional))
	for _, field := range append(append([]string(nil), existing...), additional...) {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}
	return fields
}

func appendSyncTaskFields(existing, additional []string) (string, error) {
	data, err := json.Marshal(mergeSyncTaskFields(existing, additional))
	return string(data), err
}

// Heartbeat ...
func (r *ActressSyncRepository) Heartbeat(id, token string, until time.Time) error {
	until = until.UTC() // callers may hand local-time values; storage+comparison are UTC
	return retryOnLocked(func() error {
		now := time.Now().UTC()
		res := r.db.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", id, models.ActressSyncTaskRunning, token, now).Updates(map[string]any{"heartbeat_at": now, "lease_expires_at": until})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errActressSyncLeaseLost
		}
		return nil
	})
}

// UpdateStage ...
func (r *ActressSyncRepository) UpdateStage(id, token, stage string) error {
	return retryOnLocked(func() error {
		res := r.db.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", id, models.ActressSyncTaskRunning, token, time.Now().UTC()).Update("stage", stage)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errActressSyncLeaseLost
		}
		return nil
	})
}

// ActressSyncRequeueOptions controls how RequeueTask accounts attempts.
type ActressSyncRequeueOptions struct {
	// ConsumeAttempt keeps the current attempt consumed (attempts kept).
	// When false, the interrupted attempt is handed back (attempts - 1).
	ConsumeAttempt bool
	// StaleRetry increments the persisted stale-retry counter without
	// touching attempts; the counter survives restarts and is capped by
	// the phase-3 engine (3, then terminal-fail).
	StaleRetry bool
}

// RequeueTask returns a running task to the pending queue. The fence matches
// exclusively on the immutable per-claim leaseToken (heartbeats only extend
// lease_expires_at, never rotate the token); lease_expires_at is checked for
// liveness only, never as the fence. It returns the persisted stale-retry
// count (post-increment when opts.StaleRetry is set).
func (r *ActressSyncRepository) RequeueTask(ctx context.Context, taskID, leaseToken string, opts ActressSyncRequeueOptions) (int, error) {
	if strings.TrimSpace(taskID) == "" {
		return 0, ErrInvalidLookup
	}
	staleCount := 0
	err := retryOnLocked(func() error {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			now := time.Now().UTC()
			var task models.ActressSyncTask
			if err := tx.First(&task, "id = ?", taskID).Error; err != nil {
				return err
			}
			var job models.ActressSyncJob
			if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
				return err
			}
			updates := map[string]any{
				"status": models.ActressSyncTaskPending, "stage": "queued", "outcome": "", "error_message": "", "completed_at": nil,
				"lease_owner": "", "lease_token": "", "heartbeat_at": nil, "lease_expires_at": nil,
			}
			if !job.CancelRequested {
				switch {
				case opts.StaleRetry && task.StaleRetryCount >= actressSyncStaleRetryCap:
					// Repo backstop for the phase-3 engine's policy cap: an
					// over-stale task settles failed instead of cycling forever.
					updates["status"] = models.ActressSyncTaskFailed
					updates["stage"] = "completed"
					updates["outcome"] = "failed"
					updates["error_message"] = "stale_retry_cap_reached"
					updates["completed_at"] = now
				case opts.StaleRetry:
					// Stale leases never consume the attempt under requeue:
					// bump the persisted counter and hand the attempt back.
					updates["stale_retry_count"] = gorm.Expr("stale_retry_count + 1")
					updates["attempts"] = gorm.Expr("CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END")
				case !opts.ConsumeAttempt:
					updates["attempts"] = gorm.Expr("CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END")
				case task.Attempts >= actressSyncAttemptCap:
					// The final attempt is consumed: ClaimNext only offers
					// attempts < cap, so requeueing to pending would park the task
					// forever. Settle it as failed so the job can terminate.
					updates["status"] = models.ActressSyncTaskFailed
					updates["stage"] = "completed"
					updates["outcome"] = "failed"
					updates["error_message"] = "attempt_cap_reached"
					updates["completed_at"] = now
				}
			} else {
				updates["status"] = models.ActressSyncTaskCancelled
				updates["stage"] = "completed"
				updates["outcome"] = "cancelled"
				updates["completed_at"] = now
			}
			// Fence on the immutable lease token; the expiry check is liveness
			// only — StaleRetry exists to recover already-expired leases, so it
			// must not be gated on them.
			query := tx.Model(&models.ActressSyncTask{}).
				Where("id = ? AND status = ? AND lease_token = ?", taskID, models.ActressSyncTaskRunning, leaseToken)
			if !opts.StaleRetry {
				query = query.Where("lease_expires_at > ?", now)
			}
			result := query.Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrActressSyncLeaseLost
			}
			if err := tx.Select("stale_retry_count").First(&task, "id = ?", taskID).Error; err != nil {
				return err
			}
			staleCount = task.StaleRetryCount
			return r.refreshJobTx(tx, task.JobID, now)
		})
	})
	return staleCount, err
}

// CompleteTask ...
func (r *ActressSyncRepository) CompleteTask(task *models.ActressSyncTask, token string) error {
	if task == nil {
		return ErrInvalidLookup
	}
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			now := time.Now().UTC()
			var current models.ActressSyncTask
			if err := tx.Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", task.ID, models.ActressSyncTaskRunning, token, now).First(&current).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return errActressSyncLeaseLost
				}
				return err
			}
			var job models.ActressSyncJob
			if err := tx.Select("cancel_requested").First(&job, "id = ?", current.JobID).Error; err != nil {
				return err
			}
			if job.CancelRequested && task.Status != models.ActressSyncTaskCancelled {
				// Cancellation committed after the task finished: fence inside the
				// same transaction so it never settles as completed/skipped/failed.
				task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskCancelled, "cancelled", ""
			}
			mergedFields := mergeSyncTaskFields(current.UpdatedFields, task.UpdatedFields)
			if task.Status == models.ActressSyncTaskFailed && len(mergedFields) > 0 {
				task.Status = models.ActressSyncTaskCompleted
				task.Outcome = "updated_with_warning"
				if strings.TrimSpace(task.Warning) == "" {
					task.Warning = "partial_sync_error"
				}
			}
			// Terminal settle must carry a terminal status: pending/running here
			// would park the row unclaimable beside spent attempts (the same
			// limbo the requeue cap-settle avoids).
			switch task.Status {
			case models.ActressSyncTaskCompleted, models.ActressSyncTaskSkipped, models.ActressSyncTaskConflict, models.ActressSyncTaskFailed, models.ActressSyncTaskCancelled:
			default:
				task.Status, task.Outcome, task.ErrorMessage = models.ActressSyncTaskFailed, "failed", "invalid_terminal_status"
			}
			task.UpdatedFields = mergedFields
			messages, _ := json.Marshal(task.Messages)
			fields, _ := json.Marshal(mergedFields)
			res := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", task.ID, models.ActressSyncTaskRunning, token, now).Updates(map[string]any{
				"status": task.Status, "stage": "completed", "outcome": task.Outcome, "messages": string(messages), "updated_fields": string(fields),
				"warning": task.Warning, "error_message": task.ErrorMessage, "completed_at": now, "lease_owner": "", "lease_token": "", "lease_expires_at": nil,
			})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return errActressSyncLeaseLost
			}
			return r.refreshJobTx(tx, current.JobID, now)
		})
	})
}

// actressIDOrZero dereferences an optional actress reference for diagnostics.
func actressIDOrZero(id *uint) uint {
	if id == nil {
		return 0
	}
	return *id
}

// CancelJob ...
func (r *ActressSyncRepository) CancelJob(jobID string) error {
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			now := time.Now().UTC()
			res := tx.Model(&models.ActressSyncJob{}).Where("id = ? AND status IN ?", jobID, []string{models.ActressSyncJobPending, models.ActressSyncJobRunning}).Update("cancel_requested", true)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				var n int64
				if err := tx.Model(&models.ActressSyncJob{}).Where("id = ?", jobID).Count(&n).Error; err != nil {
					return err
				}
				if n == 0 {
					return ErrNotFound
				}
				return nil
			}
			if err := tx.Model(&models.ActressSyncTask{}).Where("job_id = ? AND status = ?", jobID, models.ActressSyncTaskPending).Updates(map[string]any{"status": models.ActressSyncTaskCancelled, "stage": "completed", "outcome": "cancelled", "completed_at": now}).Error; err != nil {
				return err
			}
			return r.refreshJobTx(tx, jobID, now)
		})
	})
}

func (r *ActressSyncRepository) refreshJobTx(tx *gorm.DB, jobID string, now time.Time) error {
	type totals struct{ Total, Terminal, Updated, Warnings, Skipped, Conflicts, Failed, Cancelled int }
	var c totals
	if err := tx.Raw(`SELECT COUNT(*) total,
COALESCE(SUM(CASE WHEN status IN ('completed','skipped','conflict','failed','cancelled') THEN 1 ELSE 0 END),0) terminal,
COALESCE(SUM(CASE WHEN outcome IN ('updated','updated_with_warning') THEN 1 ELSE 0 END),0) updated,
COALESCE(SUM(CASE WHEN outcome = 'updated_with_warning' OR TRIM(COALESCE(warning,'')) <> '' THEN 1 ELSE 0 END),0) warnings,
COALESCE(SUM(CASE WHEN status = 'skipped' THEN 1 ELSE 0 END),0) skipped,
COALESCE(SUM(CASE WHEN status = 'conflict' THEN 1 ELSE 0 END),0) conflicts,
COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END),0) failed,
COALESCE(SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END),0) cancelled FROM actress_sync_tasks WHERE job_id = ?`, jobID).Scan(&c).Error; err != nil {
		return err
	}
	var job models.ActressSyncJob
	if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
		return err
	}
	status := job.Status
	completedAt := job.CompletedAt
	if c.Total == c.Terminal {
		status = models.ActressSyncJobCompleted
		if job.CancelRequested {
			status = models.ActressSyncJobCancelled
		}
		completedAt = &now
	}
	if err := tx.Model(&models.ActressSyncJob{}).Where("id = ?", jobID).Updates(map[string]any{"status": status, "total_tasks": c.Total, "completed": c.Terminal, "updated": c.Updated, "warnings": c.Warnings, "skipped": c.Skipped, "conflicts": c.Conflicts, "failed": c.Failed, "cancelled": c.Cancelled, "completed_at": completedAt}).Error; err != nil {
		return err
	}
	if status == models.ActressSyncJobCompleted || status == models.ActressSyncJobCancelled {
		return r.pruneTerminalJobsTx(tx)
	}
	return nil
}

// actressSyncCandidateClause is the canonical SQL predicate for sync
// candidacy. The thumbnail arm delegates to the registered
// javinizer_missing_actress_thumbnail SQL function (DBC-03): no hand-rolled
// LIKE/INSTR duplicate rule may live here.
// NULL dmm_id satisfies neither comparison in SQL, so COALESCE first:
// legacy/imported rows without a DMM ID must count as "missing".
const actressSyncCandidateClause = `(COALESCE(dmm_id, 0) > 0 AND (
TRIM(COALESCE(japanese_name,'')) = '' OR
(TRIM(COALESCE(first_name,'')) = '' AND TRIM(COALESCE(last_name,'')) = '') OR
` + missingActressThumbnailClause + `
)) OR (COALESCE(dmm_id, 0) <= 0 AND (
TRIM(COALESCE(japanese_name,'')) <> '' OR TRIM(COALESCE(aliases,'')) <> '' OR
TRIM(COALESCE(first_name,'')) <> '' OR TRIM(COALESCE(last_name,'')) <> ''
))`

// ListSyncCandidates returns every actress matching the candidate predicate,
// ordered by id. The predicate is fully SQL-exact (the registered
// javinizer_missing_actress_thumbnail function mirrors
// models.IsKnownInvalidDMMActressThumbnail), so no Go-side re-filter runs
// here and paged reads over the same predicate stay consistent.
func (r *ActressRepository) ListSyncCandidates(ctx context.Context) ([]models.Actress, error) {
	actresses := make([]models.Actress, 0)
	err := r.GetDB().WithContext(ctx).Where(actressSyncCandidateClause).Order("id ASC").Find(&actresses).Error
	if err != nil {
		return nil, err
	}
	return actresses, nil
}

// ListSyncCandidatesPaged pages the sync-candidate set with a stable
// ORDER BY id (R3). filter optionally ANDs one of the registered actress
// filter clauses (see ValidActressFilter); an unknown filter is an error.
// limit <= 0 returns all matching rows; total is the full filtered set size.
func (r *ActressRepository) ListSyncCandidatesPaged(ctx context.Context, filter string, limit, offset int) ([]models.Actress, int, error) {
	db := r.GetDB().WithContext(ctx).Model(&models.Actress{}).Where(actressSyncCandidateClause)
	if strings.TrimSpace(filter) != "" {
		clause, ok := ValidActressFilter(filter)
		if !ok {
			return nil, 0, wrapDBErr("list", "actress sync candidates", ErrInvalidLookup)
		}
		db = db.Where(clause)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]models.Actress, 0)
	q := db.Order("id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	if err := q.Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, int(total), nil
}

// FillBlankMetadata ...
func (r *ActressRepository) FillBlankMetadata(ctx context.Context, id uint, dmmID int, source models.ActressInfo) ([]string, error) {
	return r.fillBlankMetadata(ctx, id, dmmID, source, "", "")
}

// FillBlankMetadataForSyncTask ...
func (r *ActressRepository) FillBlankMetadataForSyncTask(ctx context.Context, id uint, dmmID int, source models.ActressInfo, taskID, leaseToken string) ([]string, error) {
	return r.fillBlankMetadata(ctx, id, dmmID, source, taskID, leaseToken)
}

func (r *ActressRepository) fillBlankMetadata(ctx context.Context, id uint, dmmID int, source models.ActressInfo, taskID, leaseToken string) ([]string, error) {
	before, err := r.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if before.DMMID <= 0 || before.DMMID != dmmID || source.DMMID != dmmID {
		return nil, ErrInvalidLookup
	}
	sourceThumb := strings.TrimSpace(source.ThumbURL)
	nameSources := []struct {
		column, value string
	}{
		{"japanese_name", strings.TrimSpace(source.JapaneseName)},
		{"first_name", strings.TrimSpace(source.FirstName)},
		{"last_name", strings.TrimSpace(source.LastName)},
	}
	fields := make([]string, 0, 4)
	if err := retryOnLocked(func() error {
		fields = fields[:0]
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := ensureSyncTaskLeaseTx(tx, taskID, leaseToken); err != nil {
				return err
			}
			// Decide from values read inside the transaction: a snapshot taken
			// before it may be stale (a user could have filled a blank field
			// meanwhile), which would also misattribute that user's edit to
			// this sync in the recorded updated fields.
			var current models.Actress
			if err := tx.First(&current, "id = ? AND dmm_id = ?", id, dmmID).Error; err != nil {
				return err
			}
			updates := map[string]any{}
			for _, nf := range nameSources {
				switch nf.column {
				case "japanese_name":
					if strings.TrimSpace(current.JapaneseName) == "" && nf.value != "" {
						updates[nf.column] = nf.value
					}
				case "first_name":
					if strings.TrimSpace(current.FirstName) == "" && nf.value != "" {
						updates[nf.column] = nf.value
					}
				case "last_name":
					if strings.TrimSpace(current.LastName) == "" && nf.value != "" {
						updates[nf.column] = nf.value
					}
				}
			}
			// Skip the write entirely when nothing can change: an all-no-op
			// update still bumps updated_at and spuriously invalidates
			// timestamp-fenced merge previews.
			if len(updates) > 0 {
				if err := tx.Model(&models.Actress{}).Where("id = ? AND dmm_id = ?", id, dmmID).Updates(updates).Error; err != nil {
					return err
				}
			}
			thumbUpdated := false
			if sourceThumb != "" && !models.IsKnownInvalidDMMActressThumbnail(sourceThumb) {
				query := tx.Model(&models.Actress{}).Where("id = ? AND dmm_id = ?", id, dmmID)
				switch {
				case strings.TrimSpace(current.ThumbURL) == "":
					query = query.Where("TRIM(COALESCE(thumb_url,'')) = ''")
				case models.IsKnownInvalidDMMActressThumbnail(current.ThumbURL):
					query = query.Where("thumb_url = ?", current.ThumbURL)
				default:
					query = nil
				}
				if query != nil {
					result := query.Update("thumb_url", sourceThumb)
					if result.Error != nil {
						return result.Error
					}
					thumbUpdated = result.RowsAffected == 1
				}
			}
			if thumbUpdated {
				fields = append(fields, "thumb_url")
			}
			for _, nf := range nameSources {
				if _, ok := updates[nf.column]; ok {
					fields = append(fields, nf.column)
				}
			}
			return recordSyncTaskFieldsTx(tx, taskID, leaseToken, fields)
		})
	}); err != nil {
		return nil, err
	}
	return append([]string(nil), fields...), nil
}

// ReplaceThumbnail ...
func (r *ActressRepository) ReplaceThumbnail(ctx context.Context, id uint, dmmID int, expected, replacement string) (bool, error) {
	return r.replaceThumbnail(ctx, id, dmmID, expected, replacement, "", "")
}

// ReplaceThumbnailForSyncTask ...
func (r *ActressRepository) ReplaceThumbnailForSyncTask(ctx context.Context, id uint, dmmID int, expected, replacement, taskID, leaseToken string) (bool, error) {
	return r.replaceThumbnail(ctx, id, dmmID, expected, replacement, taskID, leaseToken)
}

func (r *ActressRepository) replaceThumbnail(ctx context.Context, id uint, dmmID int, expected, replacement, taskID, leaseToken string) (bool, error) {
	// Validation trims, but the CAS predicate must compare against the raw
	// stored value: a legacy thumb_url with surrounding whitespace would
	// otherwise never match its own row and the sync could never repair it.
	rawExpected := expected
	expected = strings.TrimSpace(expected)
	replacement = strings.TrimSpace(replacement)
	if id == 0 || dmmID <= 0 || expected == "" || replacement == "" || models.IsKnownInvalidDMMActressThumbnail(replacement) {
		return false, ErrInvalidLookup
	}
	var replaced bool
	if err := retryOnLocked(func() error {
		replaced = false
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := ensureSyncTaskLeaseTx(tx, taskID, leaseToken); err != nil {
				return err
			}
			result := tx.Model(&models.Actress{}).Where("id = ? AND dmm_id = ? AND thumb_url = ?", id, dmmID, rawExpected).Update("thumb_url", replacement)
			if result.Error != nil {
				return result.Error
			}
			replaced = result.RowsAffected == 1
			if replaced {
				return recordSyncTaskFieldsTx(tx, taskID, leaseToken, []string{"thumb_url"})
			}
			return nil
		})
	}); err != nil {
		return false, err
	}
	return replaced, nil
}

func ensureSyncTaskLeaseTx(tx *gorm.DB, taskID, leaseToken string) error {
	if strings.TrimSpace(taskID) == "" {
		return nil
	}
	var task models.ActressSyncTask
	if err := tx.Select("job_id").Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", taskID, models.ActressSyncTaskRunning, leaseToken, time.Now().UTC()).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errActressSyncLeaseLost
		}
		return err
	}
	// Fence on parent-job cancellation: a task-scoped mutation must not
	// commit after the job was cancelled, even while its lease stays valid
	// and the worker has not yet observed the cancelled context.
	var cancelled int64
	if err := tx.Model(&models.ActressSyncJob{}).Where("id = ? AND cancel_requested = 1", task.JobID).Count(&cancelled).Error; err != nil {
		return err
	}
	if cancelled == 1 {
		return errActressSyncJobCancelled
	}
	return nil
}

func recordSyncTaskFieldsTx(tx *gorm.DB, taskID, leaseToken string, additional []string) error {
	if strings.TrimSpace(taskID) == "" || len(additional) == 0 {
		return nil
	}
	var task models.ActressSyncTask
	if err := tx.Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", taskID, models.ActressSyncTaskRunning, leaseToken, time.Now().UTC()).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errActressSyncLeaseLost
		}
		return err
	}
	fields, _ := appendSyncTaskFields(task.UpdatedFields, additional)
	result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", taskID, models.ActressSyncTaskRunning, leaseToken, time.Now().UTC()).Update("updated_fields", fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errActressSyncLeaseLost
	}
	return nil
}

// AssignDMMIDIfMissing ...
func (r *ActressRepository) AssignDMMIDIfMissing(ctx context.Context, id uint, dmmID int) (bool, error) {
	return r.assignDMMIDIfMissing(ctx, id, dmmID, models.Actress{}, "", "")
}

// AssignDMMIDIfMissingForSyncTask ...
func (r *ActressRepository) AssignDMMIDIfMissingForSyncTask(ctx context.Context, id uint, dmmID int, taskID, leaseToken string) (bool, error) {
	return r.assignDMMIDIfMissing(ctx, id, dmmID, models.Actress{}, taskID, leaseToken)
}

// AssignDMMIDIfMissingWithSource ...
func (r *ActressRepository) AssignDMMIDIfMissingWithSource(ctx context.Context, id uint, dmmID int, expectedSource models.Actress) (bool, error) {
	return r.assignDMMIDIfMissing(ctx, id, dmmID, expectedSource, "", "")
}

// AssignDMMIDIfMissingForSyncTaskWithSource ...
func (r *ActressRepository) AssignDMMIDIfMissingForSyncTaskWithSource(ctx context.Context, id uint, dmmID int, expectedSource models.Actress, taskID, leaseToken string) (bool, error) {
	return r.assignDMMIDIfMissing(ctx, id, dmmID, expectedSource, taskID, leaseToken)
}

func (r *ActressRepository) assignDMMIDIfMissing(ctx context.Context, id uint, dmmID int, expectedSource models.Actress, taskID, leaseToken string) (bool, error) {
	if id == 0 || dmmID <= 0 {
		return false, ErrInvalidLookup
	}
	var assigned bool
	if err := retryOnLocked(func() error {
		assigned = false
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := ensureSyncTaskLeaseTx(tx, taskID, leaseToken); err != nil {
				return err
			}
			// Non-positive counts as missing: NULL (legacy rows, direct imports)
			// and negative surrogate IDs (scraper aggregation) alike — matching
			// the candidate/filter classification so scheduled rows stay repairable.
			query := tx.Model(&models.Actress{}).Where("id = ? AND COALESCE(dmm_id, 0) <= 0", id)
			if expectedSource.ID > 0 {
				// Nullable legacy columns scan as "" but store NULL; bare equality
				// would never match and the assign would silently no-op.
				query = query.Where("COALESCE(first_name,'') = ? AND COALESCE(last_name,'') = ? AND COALESCE(japanese_name,'') = ? AND COALESCE(thumb_url,'') = ? AND COALESCE(aliases,'') = ?", expectedSource.FirstName, expectedSource.LastName, expectedSource.JapaneseName, expectedSource.ThumbURL, expectedSource.Aliases)
			}
			result := query.Update("dmm_id", dmmID)
			if result.Error != nil {
				return result.Error
			}
			assigned = result.RowsAffected == 1
			if assigned {
				return recordSyncTaskFieldsTx(tx, taskID, leaseToken, []string{"dmm_id"})
			}
			return nil
		})
	}); err != nil {
		return false, err
	}
	return assigned, nil
}
