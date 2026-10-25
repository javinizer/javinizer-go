package database

import (
	"context"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresssync"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type actressSyncTaskProblem struct {
	TaskID       uint
	ProblemCode  string
	Attempts     int
	RetryAfterMS int64 `gorm:"column:retry_after_ms"`
	UpdatedAt    time.Time
}

// admitOfflineNameSyncTx must run after write admission and the idempotency read.
// Counting only up to the configured caps bounds work even for legacy databases
// that predate queue admission. Replacement confirmations are conservatively
// counted as new work until the whole submit transaction has committed.
func admitOfflineNameSyncTx(tx *gorm.DB, incoming int, limits ActressSyncQueueLimits) error {
	if limits.MaxQueuedJobs == 0 {
		limits.MaxQueuedJobs = DefaultActressSyncQueuedJobs
	}
	if limits.MaxQueuedTasks == 0 {
		limits.MaxQueuedTasks = DefaultActressSyncQueuedTasks
	}
	if limits.MaxQueuedJobs < 1 || limits.MaxQueuedJobs > MaxActressSyncQueuedJobs || limits.MaxQueuedTasks < 1 || limits.MaxQueuedTasks > MaxActressSyncQueuedTasks {
		return ErrActressSyncConflict
	}
	var jobs, tasks int64
	if err := tx.Raw(`SELECT COUNT(*) FROM (
 SELECT DISTINCT t.job_id FROM actress_sync_tasks t JOIN actress_sync_jobs j ON j.id = t.job_id
 WHERE j.cancelled = 0 AND t.status IN ('pending', 'leased', 'applying') LIMIT ?
)`, limits.MaxQueuedJobs).Scan(&jobs).Error; err != nil {
		return err
	}
	if jobs >= int64(limits.MaxQueuedJobs) {
		return ErrActressSyncQueueFull
	}
	if err := tx.Raw(`SELECT COUNT(*) FROM (
 SELECT t.id FROM actress_sync_tasks t JOIN actress_sync_jobs j ON j.id = t.job_id
 WHERE j.cancelled = 0 AND t.status IN ('pending', 'leased', 'applying') LIMIT ?
)`, limits.MaxQueuedTasks).Scan(&tasks).Error; err != nil {
		return err
	}
	if incoming > limits.MaxQueuedTasks-int(tasks) {
		return ErrActressSyncQueueFull
	}
	return nil
}

func validSyncJobCursor(id string) bool {
	return id == "" || (strings.HasPrefix(id, "name-") && validSyncHash(strings.TrimPrefix(id, "name-")))
}

func syncPageLimit(limit, defaultLimit, maxLimit int) (int, error) {
	if limit == 0 {
		return defaultLimit, nil
	}
	if limit < 1 || limit > maxLimit {
		return 0, ErrActressSyncConflict
	}
	return limit, nil
}

// claimableOfflineNameSyncTasks skips live leases and persisted backoff in SQL,
// before LIMIT. A blocked or currently leased earlier task cannot hide due work.
func claimableOfflineNameSyncTasks(tx *gorm.DB, now time.Time) *gorm.DB {
	return tx.Model(&ActressSyncTask{}).
		Where("(status = 'pending' OR (status = 'leased' AND lease_until_ms <= ?))", now.UnixMilli()).
		Where(`NOT EXISTS (SELECT 1 FROM actress_sync_task_problems p
 WHERE p.task_id = actress_sync_tasks.id AND p.retry_after_ms > ?)`, now.UnixMilli())
}

// DueOfflineNameSyncJobs returns metadata only, in stable ascending ID order.
// Workers carry the last returned ID between bounded passes and wrap to an empty
// cursor after an empty page. This lets later jobs progress even when earlier
// jobs have many pending tasks. No evidence is selected, loaded, or replaced here.
func (r *ActressRepository) DueOfflineNameSyncJobs(ctx context.Context, now time.Time, afterID string, limit int) ([]ActressSyncJob, error) {
	if !syncClockValid(now) || !validSyncJobCursor(afterID) {
		return nil, ErrActressSyncConflict
	}
	limit, err := syncPageLimit(limit, DefaultActressSyncDueJobs, MaxActressSyncDueJobs)
	if err != nil {
		return nil, err
	}
	var jobs []ActressSyncJob
	db := r.GetDB().WithContext(ctx)
	due := claimableOfflineNameSyncTasks(db, now).Select("job_id")
	err = db.Where("cancelled = 0 AND id > ?", afterID).Where("id IN (?)", due).
		Order("id ASC").Limit(limit).Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// ListOfflineNameSyncJobs returns metadata-only history, newest first. beforeID
// is the last job ID on the previous page, resolved in the same read transaction;
// equal creation times are ordered deterministically by descending ID.
func (r *ActressRepository) ListOfflineNameSyncJobs(ctx context.Context, beforeID string, limit int) ([]ActressSyncJob, error) {
	if !validSyncJobCursor(beforeID) {
		return nil, ErrActressSyncConflict
	}
	limit, err := syncPageLimit(limit, DefaultActressSyncHistoryJobs, MaxActressSyncHistoryJobs)
	if err != nil {
		return nil, err
	}
	var jobs []ActressSyncJob
	err = r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&ActressSyncJob{})
		if beforeID != "" {
			var before ActressSyncJob
			if err := tx.First(&before, "id = ?", beforeID).Error; err != nil {
				return err
			}
			query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", before.CreatedAt, before.CreatedAt, before.ID)
		}
		return query.Order("created_at DESC").Order("id DESC").Limit(limit).Find(&jobs).Error
	})
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// OfflineNameSyncJobByRequestKey resolves server-generated confirmation replay
// after restart. The service must bind the key to its authenticated principal;
// the repository does not authenticate users or accept a client review as consent.
func (r *ActressRepository) OfflineNameSyncJobByRequestKey(ctx context.Context, requestKey string) (*ActressSyncJob, error) {
	if !syncOpaque(requestKey) {
		return nil, ErrActressSyncConflict
	}
	var job *ActressSyncJob
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var found ActressSyncJob
		if err := tx.Where("request_key = ?", requestKey).Take(&found).Error; err != nil {
			return err
		}
		var err error
		job, err = offlineNameSyncJobTx(tx, found.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func validSyncProblem(code string) bool {
	return code == ActressSyncProblemSnapshotMissing || code == ActressSyncProblemSnapshotInvalid || code == ActressSyncProblemApplyFailed
}

// RecordOfflineNameSyncJobProblem is for failed exact-snapshot reads before
// claiming. Only due, claimable tasks are touched under write admission; live
// leases, cancelled jobs and terminal receipts cannot be overwritten.
func (r *ActressRepository) RecordOfflineNameSyncJobProblem(ctx context.Context, jobID, code string, now time.Time) error {
	if jobID == "" || !validSyncJobCursor(jobID) || !validSyncProblem(code) || !syncClockValid(now) {
		return ErrActressSyncConflict
	}
	return retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("UPDATE actress_sync_jobs SET cancelled = cancelled WHERE id = ?", jobID).Error; err != nil {
				return err
			}
			var job ActressSyncJob
			if err := tx.First(&job, "id = ?", jobID).Error; err != nil {
				return err
			}
			if job.Cancelled {
				return ErrActressSyncLease
			}
			var tasks []ActressSyncTask
			if err := claimableOfflineNameSyncTasks(tx, now).Where("job_id = ?", jobID).
				Order("id").Limit(actresssync.MaxSelection + 1).Find(&tasks).Error; err != nil {
				return err
			}
			if len(tasks) == 0 {
				return ErrActressSyncLease
			}
			if len(tasks) > actresssync.MaxSelection {
				return ErrActressSyncConflict
			}
			for _, task := range tasks {
				// The query admits only pending tasks and expired leases. Release
				// an expired owner under the same write lock so durable status
				// describes blocked work, not a worker that no longer owns it.
				// Retain the generation; the next claim increments it.
				if task.Status == syncTaskLeased {
					if err := tx.Model(&ActressSyncTask{}).Where("id = ?", task.ID).
						Updates(map[string]any{colStatus: syncTaskPending, "lease_owner": "", "lease_until_ms": 0}).Error; err != nil {
						return err
					}
				}
				if err := recordOfflineNameSyncProblemTx(tx, task.ID, code, now); err != nil {
					return err
				}
			}
			return ctx.Err()
		})
	})
}

// RecordOfflineNameSyncLeaseProblem records a failed apply only while its exact
// owner/generation/unexpired lease still holds. Returning to pending invalidates
// that token immediately; the next claim must increment the retained generation.
func (r *ActressRepository) RecordOfflineNameSyncLeaseProblem(ctx context.Context, lease ActressSyncLease, code string, now time.Time) error {
	if lease.TaskID == 0 || lease.Generation <= 0 || !syncOpaque(lease.Owner) || !syncClockValid(now) {
		return ErrActressSyncLease
	}
	if !validSyncProblem(code) {
		return ErrActressSyncConflict
	}
	return retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&ActressSyncTask{}).Where("id = ? AND status = 'leased' AND lease_generation = ? AND lease_owner = ? AND lease_until_ms > ? AND job_id IN (SELECT id FROM actress_sync_jobs WHERE cancelled = 0)", lease.TaskID, lease.Generation, lease.Owner, now.UnixMilli()).
				Updates(map[string]any{colStatus: syncTaskPending, "lease_owner": "", "lease_until_ms": 0})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrActressSyncLease
			}
			if err := recordOfflineNameSyncProblemTx(tx, lease.TaskID, code, now); err != nil {
				return err
			}
			return ctx.Err()
		})
	})
}

func recordOfflineNameSyncProblemTx(tx *gorm.DB, taskID uint, code string, now time.Time) error {
	var previous actressSyncTaskProblem
	if err := tx.Where("task_id = ?", taskID).Limit(1).Find(&previous).Error; err != nil {
		return err
	}
	attempts := previous.Attempts + 1
	if attempts > 16 {
		attempts = 16
	}
	backoff := 5 * time.Second * time.Duration(1<<uint(attempts-1))
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	problem := actressSyncTaskProblem{TaskID: taskID, ProblemCode: code, Attempts: attempts, RetryAfterMS: now.Add(backoff).UnixMilli(), UpdatedAt: now.UTC().Round(0)}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "task_id"}}, DoUpdates: clause.AssignmentColumns([]string{"problem_code", "attempts", "retry_after_ms", "updated_at"})}).Create(&problem).Error
}

func readOfflineNameSyncProblemsTx(tx *gorm.DB, tasks []ActressSyncTask) error {
	ids := make([]uint, 0, len(tasks))
	for _, task := range tasks {
		if task.Status == syncTaskPending || task.Status == syncTaskLeased {
			ids = append(ids, task.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var problems []actressSyncTaskProblem
	if err := tx.Where("task_id IN ?", ids).Limit(actresssync.MaxSelection).Find(&problems).Error; err != nil {
		return err
	}
	byID := make(map[uint]actressSyncTaskProblem, len(problems))
	for _, problem := range problems {
		byID[problem.TaskID] = problem
	}
	for i := range tasks {
		if problem, ok := byID[tasks[i].ID]; ok {
			tasks[i].ProblemCode, tasks[i].RetryAfterMS, tasks[i].ProblemAttempts = problem.ProblemCode, problem.RetryAfterMS, problem.Attempts
		}
	}
	return nil
}
