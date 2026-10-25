package database

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"gorm.io/gorm"
)

// ClaimOfflineNameSync grants one bounded lease or replaces an expired lease
// with a strictly larger fence. Time and worker identity must be server supplied.
// Expiry has millisecond precision. No task is created implicitly by claiming.
func (r *ActressRepository) ClaimOfflineNameSync(ctx context.Context, jobID, owner string, now time.Time, ttl time.Duration) (*ActressSyncLease, error) {
	if !syncOpaque(owner) || !syncClockValid(now) || ttl < time.Millisecond || ttl > 24*time.Hour {
		return nil, ErrActressSyncLease
	}
	var lease *ActressSyncLease
	err := retryOnLocked(func() error {
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
			var task ActressSyncTask
			if err := claimableOfflineNameSyncTasks(tx, now).Where("job_id = ?", jobID).Order("id").Take(&task).Error; err != nil {
				return err
			}
			if task.LeaseGeneration == math.MaxInt64 {
				return ErrActressSyncLease
			}
			generation := task.LeaseGeneration + 1
			result := tx.Model(&ActressSyncTask{}).Where("id = ? AND lease_generation = ? AND status = ?", task.ID, task.LeaseGeneration, task.Status).Updates(map[string]any{colStatus: syncTaskLeased, "lease_generation": generation, "lease_owner": owner, "lease_until_ms": now.Add(ttl).UnixMilli()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrActressSyncLease
			}
			lease = &ActressSyncLease{task.ID, generation, owner}
			return ctx.Err()
		})
	})
	if err != nil {
		return nil, err
	}
	return lease, nil
}

// CancelOfflineNameSync durably fences pending/leased work. If an apply commits
// first, its receipt remains; cancellation never rolls back committed changes.
func (r *ActressRepository) CancelOfflineNameSync(ctx context.Context, jobID string) error {
	return retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			result := tx.Model(&ActressSyncJob{}).Where("id = ?", jobID).Update("cancelled", true)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
			return tx.Model(&ActressSyncTask{}).Where("job_id = ? AND status IN ?", jobID, []string{syncTaskPending, syncTaskLeased}).Updates(map[string]any{colStatus: "cancelled", syncColReason: "job_cancelled"}).Error
		})
	})
}

// ApplyOfflineNameSync atomically fences a lease, revalidates the explicitly
// confirmed identity/evidence/ownership, performs the canonical name change and
// records its result. A wrong snapshot returns an error without consuming work.
// Expired evidence is a durable skipped outcome. There are no network/file reads
// or background workers here; callers pin/reload the exact immutable snapshot.
func (r *ActressRepository) ApplyOfflineNameSync(ctx context.Context, lease ActressSyncLease, snapshot *actresscache.Snapshot, now time.Time) (*ActressSyncTask, error) {
	if lease.TaskID == 0 || lease.Generation <= 0 || !syncOpaque(lease.Owner) || !syncClockValid(now) {
		return nil, ErrActressSyncLease
	}
	var receipt ActressSyncTask
	err := retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// This CAS acquires write admission before any identity reads. Concurrent
			// cancel/edit/merge either committed first (fencing us) or runs after commit.
			result := tx.Model(&ActressSyncTask{}).Where("id = ? AND status = 'leased' AND lease_generation = ? AND lease_owner = ? AND lease_until_ms > ? AND job_id IN (SELECT id FROM actress_sync_jobs WHERE cancelled = 0)", lease.TaskID, lease.Generation, lease.Owner, now.UnixMilli()).Update(colStatus, "applying")
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrActressSyncLease
			}
			var task ActressSyncTask
			if err := tx.First(&task, lease.TaskID).Error; err != nil {
				return err
			}
			finish := func(status, reason, bindingStatus string) error {
				if err := finishOfflineNameTaskTx(tx, task, lease, status, reason, bindingStatus, now); err != nil {
					return err
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				return tx.First(&receipt, task.ID).Error
			}
			if snapshot == nil || snapshot.Digest() != task.SnapshotDigest {
				return ErrActressSyncStale
			}
			var job ActressSyncJob
			if err := tx.First(&job, "id = ?", task.JobID).Error; err != nil {
				return err
			}
			var binding actressSyncBinding
			if err := tx.First(&binding, task.BindingID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return finish("invalidated", "binding_missing", "revoked")
				}
				return err
			}
			if binding.Status != "active" || binding.Revision != task.BindingRevision || binding.ActressID != task.ActressID || binding.SourceKey != task.SourceKey || binding.SnapshotDigest != task.SnapshotDigest || binding.ActorToken != task.ActorToken || binding.JapaneseName != task.AfterName {
				return finish("invalidated", "binding_changed", "revoked")
			}
			actor, err := syncEligibleActorTx(tx, task.ActressID)
			if err != nil {
				if errors.Is(err, ErrActressSyncProtected) || errors.Is(err, gorm.ErrRecordNotFound) {
					return finish("invalidated", "identity_unavailable", "revoked")
				}
				return err
			}
			if syncActorToken(actor) != task.ActorToken || actor.JapaneseName != task.BeforeName {
				return finish("invalidated", "identity_changed", "revoked")
			}
			evidence, err := syncNameEvidence(snapshot, task.SourceKey, now, time.Duration(job.MaxAgeNS))
			status, reason := "unchanged", ""
			if errors.Is(err, ErrActressSyncStale) {
				status, reason = "skipped", "stale_evidence"
			} else if err != nil {
				return err
			} else {
				if evidence.Metadata.JapaneseName != task.AfterName {
					return ErrActressSyncStale
				}
				if actor.JapaneseName != task.AfterName {
					if err := applyConfirmedJapaneseNameTx(tx, actor, task.AfterName); err != nil {
						return err
					}
					status = "applied"
				}
			}
			// Every terminal outcome retires this one-shot grant. A new reviewed
			// confirmation, not automatic refresh, is required for future work.
			return finish(status, reason, "consumed")
		})
	})
	if err != nil {
		return nil, err
	}
	return &receipt, nil
}

func finishOfflineNameTaskTx(tx *gorm.DB, task ActressSyncTask, lease ActressSyncLease, status, reason, bindingStatus string, now time.Time) error {
	// A mismatched/newer binding revision is not ours to consume or revoke.
	if err := tx.Model(&actressSyncBinding{}).Where("id = ? AND revision = ?", task.BindingID, task.BindingRevision).Update(colStatus, bindingStatus).Error; err != nil {
		return err
	}
	result := tx.Model(&ActressSyncTask{}).Where("id = ? AND status = 'applying' AND lease_generation = ? AND lease_owner = ?", task.ID, lease.Generation, lease.Owner).Updates(map[string]any{colStatus: status, syncColReason: reason, "finished_at": now.UTC().Round(0)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrActressSyncLease
	}
	return nil
}
