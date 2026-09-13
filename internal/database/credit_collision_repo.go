package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

// CreditCollisionRepository persists and queries credit collision records
// across their lifecycle: recording, deduplication, resolution, and transfer.
type CreditCollisionRepository struct {
	*BaseRepository[models.CreditCollision, uint]
}

// NewCreditCollisionRepository constructs a CreditCollisionRepository backed
// by the given DB.
func NewCreditCollisionRepository(db *DB) *CreditCollisionRepository {
	return &CreditCollisionRepository{
		BaseRepository: NewBaseRepository[models.CreditCollision, uint](
			db, "credit collision",
			func(c models.CreditCollision) string { return fmt.Sprintf("%d/%s", c.CreditID, c.Field) },
			WithNewEntity[models.CreditCollision, uint](func() models.CreditCollision { return models.CreditCollision{} }),
		),
	}
}

// RecordTx records a collision within the given transaction, deduplicating
// onto an existing row for the same credit/field/reported-value triple by
// incrementing its occurrence count and merging sources.
func (r *CreditCollisionRepository) RecordTx(tx *gorm.DB, collision *models.CreditCollision, source string) error {
	var existing models.CreditCollision
	err := tx.First(&existing,
		"credit_id = ? AND field = ? AND reported_value = ?",
		collision.CreditID, collision.Field, collision.ReportedValue).Error
	if err == nil {
		existing.Occurrences++
		existing.AddSource(source)
		existing.LastSeenAt = time.Now().UTC()
		if collision.CanonicalValue != "" {
			existing.CanonicalValue = collision.CanonicalValue
		}
		if err := tx.Model(&models.CreditCollision{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
			"occurrences":     existing.Occurrences,
			"sources_seen":    existing.SourcesSeen,
			"last_seen_at":    existing.LastSeenAt,
			"canonical_value": existing.CanonicalValue,
			colUpdatedAt:      time.Now().UTC(),
		}).Error; err != nil {
			return wrapDBErr("update collision", fmt.Sprintf("collision %d", existing.ID), err)
		}
		*collision = existing
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return wrapDBErr("find collision", fmt.Sprintf("credit %d/%s", collision.CreditID, collision.Field), err)
	}
	collision.Occurrences = 1
	collision.Status = models.CollisionStatusOpen
	collision.AddSource(source)
	collision.LastSeenAt = time.Now().UTC()
	if err := tx.Create(collision).Error; err != nil {
		return wrapDBErr("create collision", fmt.Sprintf("credit %d/%s", collision.CreditID, collision.Field), err)
	}
	return nil
}

// ListOpenByMovie returns open collisions for the given movie.
func (r *CreditCollisionRepository) ListOpenByMovie(ctx context.Context, movieContentID string) ([]models.CreditCollision, error) {
	var collisions []models.CreditCollision
	err := r.GetDB().WithContext(ctx).
		Where("movie_content_id = ? AND status = ?", movieContentID, models.CollisionStatusOpen).
		Order("id ASC").
		Find(&collisions).Error
	if err != nil {
		return nil, wrapDBErr("list", fmt.Sprintf("open collisions for movie %s", movieContentID), err)
	}
	return collisions, nil
}

// ListOpenByMovieTx is the transaction-scoped form of ListOpenByMovie.
func (r *CreditCollisionRepository) ListOpenByMovieTx(tx *gorm.DB, movieContentID string) ([]models.CreditCollision, error) {
	var collisions []models.CreditCollision
	err := tx.Where("movie_content_id = ? AND status = ?", movieContentID, models.CollisionStatusOpen).
		Order("id ASC").
		Find(&collisions).Error
	if err != nil {
		return nil, wrapDBErr("list", fmt.Sprintf("open collisions for movie %s", movieContentID), err)
	}
	return collisions, nil
}

// HasOpenForMovie reports whether the movie has any open collision.
func (r *CreditCollisionRepository) HasOpenForMovie(ctx context.Context, movieContentID string) (bool, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.CreditCollision{}).
		Where("movie_content_id = ? AND status = ?", movieContentID, models.CollisionStatusOpen).
		Count(&count).Error
	if err != nil {
		return false, wrapDBErr("count", fmt.Sprintf("open collisions for movie %s", movieContentID), err)
	}
	return count > 0, nil
}

// ListOpenByActress returns open collisions across all credits of an actress.
func (r *CreditCollisionRepository) ListOpenByActress(ctx context.Context, actressID uint) ([]models.CreditCollision, error) {
	var collisions []models.CreditCollision
	err := r.GetDB().WithContext(ctx).
		Joins("JOIN movie_credits ON movie_credits.id = credit_collisions.credit_id").
		Where("movie_credits.actress_id = ? AND credit_collisions.status = ?", actressID, models.CollisionStatusOpen).
		Order("credit_collisions.id ASC").
		Find(&collisions).Error
	if err != nil {
		return nil, wrapDBErr("list", fmt.Sprintf("open collisions for actress %d", actressID), err)
	}
	return collisions, nil
}

// Resolve closes a collision with the given resolution outcome.
func (r *CreditCollisionRepository) Resolve(ctx context.Context, collisionID uint, resolution string) error {
	updates := map[string]interface{}{
		colStatus:     models.CollisionStatusResolved,
		colResolution: resolution,
		colUpdatedAt:  time.Now().UTC(),
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.CreditCollision{}).Where("id = ?", collisionID).Updates(updates).Error; err != nil {
		return wrapDBErr("resolve", fmt.Sprintf("collision %d", collisionID), err)
	}
	return nil
}

// ResolveTx is the transaction-scoped form of Resolve.
func (r *CreditCollisionRepository) ResolveTx(tx *gorm.DB, collisionID uint, resolution string) error {
	updates := map[string]interface{}{
		colStatus:     models.CollisionStatusResolved,
		colResolution: resolution,
		colUpdatedAt:  time.Now().UTC(),
	}
	if err := tx.Model(&models.CreditCollision{}).Where("id = ?", collisionID).Updates(updates).Error; err != nil {
		return wrapDBErr("resolve", fmt.Sprintf("collision %d", collisionID), err)
	}
	return nil
}

// Reopen re-opens a resolved collision and marks it user-pinned so
// automated reconciliation cannot re-close it.
func (r *CreditCollisionRepository) Reopen(ctx context.Context, collisionID uint) error {
	updates := map[string]interface{}{
		colStatus:     models.CollisionStatusOpen,
		colResolution: "",
		"user_pinned": true,
		colUpdatedAt:  time.Now().UTC(),
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.CreditCollision{}).Where("id = ?", collisionID).Updates(updates).Error; err != nil {
		return wrapDBErr("reopen", fmt.Sprintf("collision %d", collisionID), err)
	}
	return nil
}

// TransferTx moves all collisions from one credit to another.
func (r *CreditCollisionRepository) TransferTx(tx *gorm.DB, fromCreditID, toCreditID uint, toMovieContentID string) error {
	return tx.Model(&models.CreditCollision{}).
		Where("credit_id = ?", fromCreditID).
		Updates(map[string]interface{}{
			"credit_id":        toCreditID,
			"movie_content_id": toMovieContentID,
			colUpdatedAt:       time.Now().UTC(),
		}).Error
}

// CloseByCreditTx closes open collisions attached to a credit.
func (r *CreditCollisionRepository) CloseByCreditTx(tx *gorm.DB, creditID uint, resolution string) error {
	return tx.Model(&models.CreditCollision{}).
		Where("credit_id = ? AND status = ?", creditID, models.CollisionStatusOpen).
		Updates(map[string]interface{}{
			colStatus:     models.CollisionStatusResolved,
			colResolution: resolution,
			colUpdatedAt:  time.Now().UTC(),
		}).Error
}

// CreditCollisionRepository implements the credit identity lifecycle contract.
// CreditCollisionRepository implements the credit identity lifecycle contract.
func (r *CreditCollisionRepository) CloseByCredit(ctx context.Context, creditID uint, resolution string) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.CloseByCreditTx(tx, creditID, resolution)
	})
}

// CountOpenByMovieBatch returns per-movie open-collision counts for the
// given movie IDs.
func (r *CreditCollisionRepository) CountOpenByMovieBatch(ctx context.Context, movieIDs []string) (map[string]int64, error) {
	type row struct {
		MovieContentID string
		Count          int64
	}
	var rows []row
	err := r.GetDB().WithContext(ctx).Model(&models.CreditCollision{}).
		Select("movie_content_id, COUNT(*) as count").
		Where("movie_content_id IN ? AND status = ?", movieIDs, models.CollisionStatusOpen).
		Group("movie_content_id").
		Scan(&rows).Error
	if err != nil {
		return nil, wrapDBErr("count", "open collisions batch", err)
	}
	result := make(map[string]int64, len(rows))
	for _, r := range rows {
		result[r.MovieContentID] = r.Count
	}
	return result, nil
}
