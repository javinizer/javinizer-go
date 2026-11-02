package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

const candidateDismissalExists = "EXISTS (SELECT 1 FROM actress_candidate_dismissals d WHERE d.actress_id = actresses.id)"

// ListDismissedCandidates lists unverified identities dismissed from review.
// Dismissal never changes identity matching, cast visibility or scrape evidence.
func (r *ActressRepository) ListDismissedCandidates(ctx context.Context, limit, offset int) ([]models.Actress, error) {
	if limit <= 0 {
		limit = 50
	}
	var candidates []models.Actress
	err := r.GetDB().WithContext(ctx).Where("verified = ?", false).
		Where(candidateDismissalExists).Order("updated_at DESC, id ASC").
		Limit(limit).Offset(offset).Find(&candidates).Error
	if err != nil {
		return nil, wrapDBErr("list", "dismissed candidate actresses", err)
	}
	return candidates, nil
}

// CountDismissedCandidates uses the same disposition filter as its list.
func (r *ActressRepository) CountDismissedCandidates(ctx context.Context) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.Actress{}).
		Where("verified = ?", false).Where(candidateDismissalExists).Count(&count).Error
	return count, wrapDBErr("count", "dismissed candidate actresses", err)
}

// SetCandidateDismissed changes only the review-queue disposition. Repeated
// requests are idempotent, including the original dismissal timestamp. Both
// identity verification and the write run in one retried transaction, sharing
// the serialization boundary with PromoteCandidate; neither can win on a stale
// read after the other commits. No actress timestamp or render state is touched.
func (r *ActressRepository) SetCandidateDismissed(ctx context.Context, id uint, dismissed bool) error {
	if id == 0 {
		return ErrInvalidLookup
	}
	return retryOnLocked(func() error {
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var candidate models.Actress
			if err := tx.First(&candidate, id).Error; err != nil {
				return wrapDBErr("find", fmt.Sprintf("candidate %d", id), err)
			}
			if candidate.Verified {
				return ErrCandidateAlreadyVerified
			}
			if dismissed {
				return tx.Exec(`INSERT INTO actress_candidate_dismissals (actress_id, dismissed_at)
VALUES (?, ?) ON CONFLICT(actress_id) DO NOTHING`, id, time.Now().UTC()).Error
			}
			return deleteCandidateDismissalTx(tx, id)
		})
	})
}

func candidateDismissedTx(tx *gorm.DB, id uint) (bool, error) {
	var count int64
	err := tx.Table("actress_candidate_dismissals").Where("actress_id = ?", id).Count(&count).Error
	return count > 0, err
}

// Explicit cleanup also supports databases where SQLite foreign keys are off.
func deleteCandidateDismissalTx(tx *gorm.DB, id uint) error {
	return tx.Exec("DELETE FROM actress_candidate_dismissals WHERE actress_id = ?", id).Error
}
