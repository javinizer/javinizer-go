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
	actressSyncTerminalRetention = 20
)

var errActressSyncLeaseLost = errors.New("actress sync task lease lost")

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
					lookupErr := tx.Table("actress_sync_tasks AS task").Select("task.*").Joins("JOIN actress_sync_jobs AS job ON job.id = task.job_id").Where("task.actress_id = ? AND task.status IN ?", *tasks[i].ActressID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).Order("CASE WHEN job.scope = 'selected' THEN 0 ELSE 1 END, task.created_at ASC, task.id ASC").First(&conflict).Error
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
		if !strings.Contains(strings.ToLower(err.Error()), "unique") {
			return err
		}
		var conflict models.ActressSyncTask
		if lookupErr := tx.Where("dedupe_key = ? AND status IN ?", task.DedupeKey, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).First(&conflict).Error; lookupErr != nil {
			return lookupErr
		}
		var conflictJob models.ActressSyncJob
		if lookupErr := tx.First(&conflictJob, "id = ?", conflict.JobID).Error; lookupErr != nil {
			return lookupErr
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

// ListTasks ...
func (r *ActressSyncRepository) ListTasks(jobID string) ([]models.ActressSyncTask, error) {
	tasks := make([]models.ActressSyncTask, 0)
	err := r.db.Where("job_id = ?", jobID).Order("created_at ASC, id ASC").Find(&tasks).Error
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
	tasks := make([]models.ActressSyncTask, 0)
	err := r.db.Where("job_id = ? AND (status IN ? OR TRIM(COALESCE(warning, '')) <> '' OR TRIM(COALESCE(error_message, '')) <> '')", jobID, []string{models.ActressSyncTaskSkipped, models.ActressSyncTaskConflict, models.ActressSyncTaskFailed, models.ActressSyncTaskCancelled}).
		Order("completed_at DESC, created_at DESC, id DESC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// ClaimNext ...
func (r *ActressSyncRepository) ClaimNext(owner string, leaseUntil time.Time) (*models.ActressSyncTask, error) {
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
			res := tx.Model(&models.ActressSyncTask{}).Where("id IN (?)", pendingTaskID).Updates(map[string]any{
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
		err := tx.Table("actress_sync_tasks AS task").Select("task.*").Joins("JOIN actress_sync_jobs AS job ON job.id = task.job_id").Where("task.actress_id = ? AND task.id <> ? AND task.status IN ?", actressID, sourceTask.ID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).Order("task.created_at ASC, task.id ASC").First(&targetTask).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := moveActiveActressSyncTaskTx(tx, sourceTask, actressID, false); err != nil {
				return err
			}
			continue
		}
		var targetJob models.ActressSyncJob
		if err := tx.First(&targetJob, "id = ?", targetTask.JobID).Error; err != nil {
			return err
		}
		if targetTask.Status == models.ActressSyncTaskPending && actressSyncScopePriority(sourceJob.Scope) >= actressSyncScopePriority(targetJob.Scope) && sourceTask.Status == models.ActressSyncTaskRunning {
			if err := skipActiveActressSyncTaskTx(tx, targetTask); err != nil {
				return err
			}
			jobIDs[targetTask.JobID] = struct{}{}
			if err := moveActiveActressSyncTaskTx(tx, sourceTask, actressID, false); err != nil {
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
		if targetTask.Status == models.ActressSyncTaskPending && actressSyncScopePriority(sourceJob.Scope) > actressSyncScopePriority(targetJob.Scope) {
			if err := skipActiveActressSyncTaskTx(tx, targetTask); err != nil {
				return err
			}
			jobIDs[targetTask.JobID] = struct{}{}
			if err := moveActiveActressSyncTaskTx(tx, sourceTask, actressID, false); err != nil {
				return err
			}
			continue
		}
		if err := moveActiveActressSyncTaskTx(tx, sourceTask, actressID, true); err != nil {
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

func moveActiveActressSyncTaskTx(tx *gorm.DB, task models.ActressSyncTask, actressID uint, deferred bool) error {
	fields, _ := appendSyncTaskFields(task.UpdatedFields, []string{"merged_duplicate"})
	key := fmt.Sprintf("actress:%d", actressID)
	if deferred {
		key = deferredActressSyncDedupeKey(actressID, task.ID)
	}
	result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND actress_id = ? AND status IN ?", task.ID, *task.ActressID, []string{models.ActressSyncTaskPending, models.ActressSyncTaskRunning}).Updates(map[string]any{
		"actress_id": actressID, "dedupe_key": key, "updated_fields": fields,
	})
	return result.Error
}

func skipActiveActressSyncTaskTx(tx *gorm.DB, task models.ActressSyncTask) error {
	now := time.Now().UTC()
	messages, _ := json.Marshal([]string{"coalesced_into_merged_task"})
	result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ?", task.ID, models.ActressSyncTaskPending).Updates(map[string]any{
		"status": models.ActressSyncTaskSkipped, "stage": "completed", "outcome": "skipped", "messages": string(messages), "completed_at": now,
	})
	return result.Error
}

func skipMergedActressSyncTaskTx(tx *gorm.DB, task models.ActressSyncTask, actressID uint) error {
	now := time.Now().UTC()
	messages, _ := json.Marshal([]string{"coalesced_into_merged_task"})
	result := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ?", task.ID, models.ActressSyncTaskPending).Updates(map[string]any{
		"actress_id": actressID, "dedupe_key": fmt.Sprintf("actress:%d:merged:%s", actressID, task.ID), "status": models.ActressSyncTaskSkipped, "stage": "completed", "outcome": "skipped", "messages": string(messages), "completed_at": now,
	})
	return result.Error
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
			if err := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ?", conflict.ID, models.ActressSyncTaskPending).Updates(map[string]any{
				"status": models.ActressSyncTaskSkipped, "stage": "completed", "outcome": "skipped", "messages": string(messages), "completed_at": now,
			}).Error; err != nil {
				return err
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

// RequeueTask ...
func (r *ActressSyncRepository) RequeueTask(task *models.ActressSyncTask, token string) error {
	if task == nil {
		return ErrInvalidLookup
	}
	return retryOnLocked(func() error {
		return r.db.Transaction(func(tx *gorm.DB) error {
			now := time.Now().UTC()
			var job models.ActressSyncJob
			if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
				return err
			}
			updates := map[string]any{
				"status": models.ActressSyncTaskPending, "stage": "queued", "outcome": "", "error_message": "", "completed_at": nil,
				"lease_owner": "", "lease_token": "", "heartbeat_at": nil, "lease_expires_at": nil,
			}
			if !job.CancelRequested {
				updates["attempts"] = gorm.Expr("CASE WHEN attempts > 0 THEN attempts - 1 ELSE 0 END")
			} else {
				updates["status"] = models.ActressSyncTaskCancelled
				updates["stage"] = "completed"
				updates["outcome"] = "cancelled"
				updates["completed_at"] = now
			}
			result := tx.Model(&models.ActressSyncTask{}).
				Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", task.ID, models.ActressSyncTaskRunning, token, now).
				Updates(updates)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errActressSyncLeaseLost
			}
			if err := r.refreshJobTx(tx, task.JobID, now); err != nil {
				return err
			}
			if job.CancelRequested {
				task.Status = models.ActressSyncTaskCancelled
				task.Stage = "completed"
				task.Outcome = "cancelled"
				task.CompletedAt = &now
			} else {
				task.Status = models.ActressSyncTaskPending
				task.Stage = "queued"
				task.Outcome = ""
				task.CompletedAt = nil
				if task.Attempts > 0 {
					task.Attempts--
				}
			}
			task.ErrorMessage = ""
			task.LeaseOwner = ""
			task.LeaseToken = ""
			task.HeartbeatAt = nil
			task.LeaseExpiresAt = nil
			return nil
		})
	})
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
			mergedFields := mergeSyncTaskFields(current.UpdatedFields, task.UpdatedFields)
			if task.Status == models.ActressSyncTaskFailed && len(mergedFields) > 0 {
				task.Status = models.ActressSyncTaskCompleted
				task.Outcome = "updated_with_warning"
				if strings.TrimSpace(task.Warning) == "" {
					task.Warning = "partial_sync_error"
				}
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
			return r.refreshJobTx(tx, task.JobID, now)
		})
	})
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
SUM(CASE WHEN status IN ('completed','skipped','conflict','failed','cancelled') THEN 1 ELSE 0 END) terminal,
SUM(CASE WHEN outcome IN ('updated','updated_with_warning') THEN 1 ELSE 0 END) updated,
SUM(CASE WHEN outcome = 'updated_with_warning' OR TRIM(COALESCE(warning,'')) <> '' THEN 1 ELSE 0 END) warnings,
SUM(CASE WHEN status = 'skipped' THEN 1 ELSE 0 END) skipped,
SUM(CASE WHEN status = 'conflict' THEN 1 ELSE 0 END) conflicts,
SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) failed,
SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) cancelled FROM actress_sync_tasks WHERE job_id = ?`, jobID).Scan(&c).Error; err != nil {
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

// ListSyncCandidates ...
func (r *ActressRepository) ListSyncCandidates(ctx context.Context) ([]models.Actress, error) {
	potential := make([]models.Actress, 0)
	err := r.GetDB().WithContext(ctx).Where(`(dmm_id > 0 AND (
TRIM(COALESCE(thumb_url,'')) = '' OR
TRIM(COALESCE(japanese_name,'')) = '' OR
(TRIM(COALESCE(first_name,'')) = '' AND TRIM(COALESCE(last_name,'')) = '') OR
LOWER(COALESCE(thumb_url,'')) LIKE ? OR
LOWER(COALESCE(thumb_url,'')) LIKE ?
)) OR (dmm_id <= 0 AND (
TRIM(COALESCE(japanese_name,'')) <> '' OR TRIM(COALESCE(aliases,'')) <> '' OR
TRIM(COALESCE(first_name,'')) <> '' OR TRIM(COALESCE(last_name,'')) <> ''
))`, "%/mono/actjpgs/%", "%/mono/noimage/now_printing.jpg%").Order("id ASC").Find(&potential).Error
	if err != nil {
		return nil, err
	}
	actresses := make([]models.Actress, 0, len(potential))
	for _, actress := range potential {
		if actress.DMMID <= 0 {
			if strings.TrimSpace(actress.JapaneseName) != "" ||
				strings.TrimSpace(actress.Aliases) != "" ||
				strings.TrimSpace(actress.FirstName) != "" ||
				strings.TrimSpace(actress.LastName) != "" {
				actresses = append(actresses, actress)
			}
			continue
		}
		if strings.TrimSpace(actress.ThumbURL) == "" ||
			models.IsKnownInvalidDMMActressThumbnail(actress.ThumbURL) ||
			strings.TrimSpace(actress.JapaneseName) == "" ||
			(strings.TrimSpace(actress.FirstName) == "" && strings.TrimSpace(actress.LastName) == "") {
			actresses = append(actresses, actress)
		}
	}
	return actresses, nil
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
	updates := map[string]any{
		"japanese_name": gorm.Expr("CASE WHEN TRIM(COALESCE(japanese_name,'')) = '' THEN ? ELSE japanese_name END", strings.TrimSpace(source.JapaneseName)),
		"first_name":    gorm.Expr("CASE WHEN TRIM(COALESCE(first_name,'')) = '' THEN ? ELSE first_name END", strings.TrimSpace(source.FirstName)),
		"last_name":     gorm.Expr("CASE WHEN TRIM(COALESCE(last_name,'')) = '' THEN ? ELSE last_name END", strings.TrimSpace(source.LastName)),
	}
	sourceThumb := strings.TrimSpace(source.ThumbURL)
	fields := make([]string, 0, 4)
	if err := retryOnLocked(func() error {
		fields = fields[:0]
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := ensureSyncTaskLeaseTx(tx, taskID, leaseToken); err != nil {
				return err
			}
			if err := tx.Model(&models.Actress{}).Where("id = ? AND dmm_id = ?", id, dmmID).Updates(updates).Error; err != nil {
				return err
			}
			thumbUpdated := false
			if sourceThumb != "" && !models.IsKnownInvalidDMMActressThumbnail(sourceThumb) {
				query := tx.Model(&models.Actress{}).Where("id = ? AND dmm_id = ?", id, dmmID)
				switch {
				case strings.TrimSpace(before.ThumbURL) == "":
					query = query.Where("TRIM(COALESCE(thumb_url,'')) = ''")
				case models.IsKnownInvalidDMMActressThumbnail(before.ThumbURL):
					query = query.Where("thumb_url = ?", before.ThumbURL)
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
			var after models.Actress
			if err := tx.First(&after, "id = ?", id).Error; err != nil {
				return err
			}
			if thumbUpdated && strings.TrimSpace(after.ThumbURL) != "" {
				fields = append(fields, "thumb_url")
			}
			if strings.TrimSpace(before.JapaneseName) == "" && strings.TrimSpace(after.JapaneseName) != "" {
				fields = append(fields, "japanese_name")
			}
			if strings.TrimSpace(before.FirstName) == "" && strings.TrimSpace(after.FirstName) != "" {
				fields = append(fields, "first_name")
			}
			if strings.TrimSpace(before.LastName) == "" && strings.TrimSpace(after.LastName) != "" {
				fields = append(fields, "last_name")
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
			result := tx.Model(&models.Actress{}).Where("id = ? AND dmm_id = ? AND thumb_url = ?", id, dmmID, expected).Update("thumb_url", replacement)
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
	var count int64
	if err := tx.Model(&models.ActressSyncTask{}).Where("id = ? AND status = ? AND lease_token = ? AND lease_expires_at > ?", taskID, models.ActressSyncTaskRunning, leaseToken, time.Now().UTC()).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return errActressSyncLeaseLost
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
			query := tx.Model(&models.Actress{}).Where("id = ? AND dmm_id = 0", id)
			if expectedSource.ID > 0 {
				query = query.Where("first_name = ? AND last_name = ? AND japanese_name = ? AND thumb_url = ? AND aliases = ?", expectedSource.FirstName, expectedSource.LastName, expectedSource.JapaneseName, expectedSource.ThumbURL, expectedSource.Aliases)
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
