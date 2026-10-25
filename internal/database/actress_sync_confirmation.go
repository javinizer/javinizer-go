package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

func syncClockValid(now time.Time) bool {
	return !now.IsZero() && now.Year() >= 1 && now.Year() <= 9999
}
func validSyncHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func syncOpaque(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	_, err := actresscache.SourceKey("request", value)
	return err == nil
}
func syncDigest(value any) string {
	// The only callers pass fixed structs of supported JSON scalars.
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func syncActorToken(actor *models.Actress) string {
	// Keep the fingerprint schema to JSON-safe strings rather than marshaling
	// model time/custom fields whose serialization could fail. This is a state
	// precondition, never a biological identity or authorization credential.
	return syncDigest([]string{strconv.FormatUint(uint64(actor.ID), 10), strconv.Itoa(actor.DMMID),
		actor.FirstName, actor.LastName, actor.JapaneseName, actor.ThumbURL, actor.Aliases,
		strconv.FormatBool(actor.Verified), actor.Origin, actor.NameKey, strconv.FormatBool(actor.AmbiguityQuarantined),
		actor.CreatedAt.UTC().Format(time.RFC3339Nano)})
}

func syncEligibleActorTx(tx *gorm.DB, id uint) (*models.Actress, error) {
	var actor models.Actress
	if err := tx.First(&actor, id).Error; err != nil {
		return nil, err
	}
	if !actor.Verified || actor.AmbiguityQuarantined || actor.Origin != ActressOriginImport {
		return nil, ErrActressSyncProtected
	}
	provenance, err := importNameProvenanceTx(tx, id)
	if err != nil {
		return nil, err
	}
	var protected bool
	if err := tx.Raw("SELECT protected FROM actress_sync_name_guards WHERE actress_id = ?", id).Scan(&protected).Error; err != nil {
		return nil, err
	}
	if provenance != importNamesManaged || protected {
		return nil, ErrActressSyncProtected
	}
	return &actor, nil
}

func syncNameEvidence(snapshot *actresscache.Snapshot, key string, now time.Time, age time.Duration) (actresscache.Evidence, error) {
	if snapshot == nil || snapshot.Digest() == "" || len(key) > 512 || !syncClockValid(now) || age <= 0 {
		return actresscache.Evidence{}, ErrActressSyncStale
	}
	evidence, ok := snapshot.Get(key)
	if !ok || evidence.Source != "r18dev" || strings.TrimSpace(evidence.Metadata.JapaneseName) == "" || len(evidence.Metadata.JapaneseName) > 4096 {
		return actresscache.Evidence{}, ErrActressSyncStale
	}
	if evidence.Provenance.ObservedAt.After(now) || evidence.Provenance.ObservedAt.Before(now.Add(-age)) {
		return actresscache.Evidence{}, ErrActressSyncStale
	}
	return evidence, nil
}

// ReviewOfflineNameSync prepares a bounded explicit actor/source association batch.
// It performs no name matching and grants no permission. A future UI must show
// these exact before/after names and identities and request human confirmation.
func (r *ActressRepository) ReviewOfflineNameSync(ctx context.Context, choices []ActressSyncChoice, snapshot *actresscache.Snapshot, options actresssync.Options) (*ActressSyncReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(choices) > actresssync.MaxSelection {
		return nil, ErrActressSyncConflict
	}
	ids := make([]uint, len(choices))
	seen := map[string]bool{}
	for i, c := range choices {
		ids[i] = c.ActressID
		if len(c.SourceKey) == 0 || len(c.SourceKey) > 512 || seen[c.SourceKey] {
			return nil, ErrActressSyncConflict
		}
		seen[c.SourceKey] = true
	}
	if err := options.ValidateSelection(ids); err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.Digest() == "" {
		return nil, ErrActressSyncStale
	}
	review := &ActressSyncReview{SnapshotDigest: snapshot.Digest(), MaxAge: options.MaxAge}
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, c := range choices {
			actor, err := syncEligibleActorTx(tx, c.ActressID)
			if err != nil {
				return err
			}
			evidence, err := syncNameEvidence(snapshot, c.SourceKey, options.Now, options.MaxAge)
			if err != nil {
				return err
			}
			review.Proposals = append(review.Proposals, ActressSyncProposal{c.ActressID, c.SourceKey, syncActorToken(actor), actor.JapaneseName, evidence.Metadata.JapaneseName})
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return review, nil
}

// SubmitConfirmedOfflineNameSync records one explicitly confirmed batch and its
// one-shot source bindings atomically. Callers must establish human confirmation;
// neither an import nor ordinary scrape may call this method automatically.
// A repeated request key with the exact payload returns its original job even
// after completion; a different payload conflicts. Binding is not field ownership.
func (r *ActressRepository) SubmitConfirmedOfflineNameSync(ctx context.Context, requestKey string, review ActressSyncReview, snapshot *actresscache.Snapshot, now time.Time) (*ActressSyncJob, error) {
	if !syncOpaque(requestKey) || !syncClockValid(now) || !validSyncHash(review.SnapshotDigest) || review.MaxAge <= 0 || len(review.Proposals) == 0 || len(review.Proposals) > actresssync.MaxSelection {
		return nil, ErrActressSyncConflict
	}
	ids := make([]uint, len(review.Proposals))
	seen := map[string]bool{}
	for i, p := range review.Proposals {
		ids[i] = p.ActressID
		if len(p.SourceKey) == 0 || len(p.SourceKey) > 512 || seen[p.SourceKey] || !validSyncHash(p.ActorToken) || len(p.BeforeName) > 4096 || len(p.AfterName) > 4096 {
			return nil, ErrActressSyncConflict
		}
		seen[p.SourceKey] = true
	}
	if err := (actresssync.Options{Now: now, MaxAge: review.MaxAge, MaxActors: actresssync.MaxSelection}).ValidateSelection(ids); err != nil {
		return nil, err
	}
	hash := syncDigest(review)
	jobID := "name-" + syncDigest(requestKey)
	var committed *ActressSyncJob
	err := retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Acquire SQLite's write admission before read/decision work. Idempotent
			// duplicate requests do not mutate rows; conflicting concurrent writers retry.
			if err := tx.Exec("UPDATE actress_sync_jobs SET cancelled = cancelled WHERE request_key = ?", requestKey).Error; err != nil {
				return err
			}
			var existing ActressSyncJob
			err := tx.Where("request_key = ?", requestKey).Take(&existing).Error
			if err == nil {
				if existing.RequestHash != hash {
					return ErrActressSyncConflict
				}
				committed, err = offlineNameSyncJobTx(tx, existing.ID)
				return err
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if snapshot == nil || snapshot.Digest() != review.SnapshotDigest {
				return ErrActressSyncStale
			}
			job := ActressSyncJob{ID: jobID, RequestKey: requestKey, RequestHash: hash, SnapshotDigest: review.SnapshotDigest, MaxAgeNS: int64(review.MaxAge), CreatedAt: now.UTC().Round(0)}
			if err := tx.Omit("Tasks").Create(&job).Error; err != nil {
				return err
			}
			for _, p := range review.Proposals {
				actor, err := syncEligibleActorTx(tx, p.ActressID)
				if err != nil {
					return err
				}
				evidence, err := syncNameEvidence(snapshot, p.SourceKey, now, review.MaxAge)
				if err != nil {
					return err
				}
				if syncActorToken(actor) != p.ActorToken || actor.JapaneseName != p.BeforeName || evidence.Metadata.JapaneseName != p.AfterName {
					return ErrActressSyncStale
				}
				var binding actressSyncBinding
				err = tx.Where("source_key = ?", p.SourceKey).Take(&binding).Error
				if err == nil {
					if binding.ActressID != p.ActressID || binding.Revision == math.MaxInt64 {
						return ErrActressSyncConflict
					}
					// A new explicit confirmation replaces only the same actor/source mapping.
					if err := tx.Model(&ActressSyncTask{}).Where("binding_id = ? AND status IN ?", binding.ID, []string{syncTaskPending, syncTaskLeased}).Updates(map[string]any{colStatus: "invalidated", syncColReason: "confirmation_replaced"}).Error; err != nil {
						return err
					}
					binding.Revision++
				} else if errors.Is(err, gorm.ErrRecordNotFound) {
					binding = actressSyncBinding{ActressID: p.ActressID, SourceKey: p.SourceKey, Revision: 1}
				} else {
					return err
				}
				binding.SnapshotDigest = review.SnapshotDigest
				binding.ActorToken = p.ActorToken
				binding.JapaneseName = p.AfterName
				binding.Status = "active"
				if err := tx.Save(&binding).Error; err != nil {
					return err
				}
				task := ActressSyncTask{JobID: jobID, ActressID: p.ActressID, BindingID: binding.ID, BindingRevision: binding.Revision, SourceKey: p.SourceKey, SnapshotDigest: review.SnapshotDigest, ActorToken: p.ActorToken, BeforeName: p.BeforeName, AfterName: p.AfterName, Status: syncTaskPending}
				if err := tx.Create(&task).Error; err != nil {
					return fmt.Errorf("submit actress name task: %w", err)
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			committed, err = offlineNameSyncJobTx(tx, jobID)
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	return committed, nil
}

// OfflineNameSyncJob reads one durable job and its bounded ordered task history.
func (r *ActressRepository) OfflineNameSyncJob(ctx context.Context, id string) (*ActressSyncJob, error) {
	var job *ActressSyncJob
	err := r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		job, err = offlineNameSyncJobTx(tx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func offlineNameSyncJobTx(tx *gorm.DB, id string) (*ActressSyncJob, error) {
	var job ActressSyncJob
	if err := tx.First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("job_id = ?", id).Order("id").Limit(actresssync.MaxSelection + 1).Find(&job.Tasks).Error; err != nil {
		return nil, err
	}
	if len(job.Tasks) > actresssync.MaxSelection {
		return nil, ErrActressSyncConflict
	}
	return &job, nil
}
