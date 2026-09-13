package database

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

// MovieCreditRepository persists and queries per-movie credit records.
type MovieCreditRepository struct {
	*BaseRepository[models.MovieCredit, uint]
}

// NewMovieCreditRepository constructs a repository backed by the given database handle.
// NewMovieCreditRepository constructs a MovieCreditRepository backed by the given DB.
func NewMovieCreditRepository(db *DB) *MovieCreditRepository {
	return &MovieCreditRepository{
		BaseRepository: NewBaseRepository[models.MovieCredit, uint](
			db, "movie credit",
			func(c models.MovieCredit) string { return fmt.Sprintf("%s/%d", c.MovieContentID, c.ActressID) },
			WithNewEntity[models.MovieCredit, uint](func() models.MovieCredit { return models.MovieCredit{} }),
		),
	}
}

// ListByMovie returns ordered credits (with nested identity) for a movie.
func (r *MovieCreditRepository) ListByMovie(ctx context.Context, movieContentID string) ([]models.MovieCredit, error) {
	var credits []models.MovieCredit
	err := r.GetDB().WithContext(ctx).
		Preload("Actress").
		Where("movie_content_id = ?", movieContentID).
		Order("order_index ASC, id ASC").
		Find(&credits).Error
	if err != nil {
		return nil, wrapDBErr("list", fmt.Sprintf("credits for movie %s", movieContentID), err)
	}
	return credits, nil
}

// ListByMovieTx is the transaction-scoped form of ListByMovie.
func (r *MovieCreditRepository) ListByMovieTx(tx *gorm.DB, movieContentID string) ([]models.MovieCredit, error) {
	var credits []models.MovieCredit
	err := tx.Preload("Actress").
		Where("movie_content_id = ?", movieContentID).
		Order("order_index ASC, id ASC").
		Find(&credits).Error
	if err != nil {
		return nil, wrapDBErr("list", fmt.Sprintf("credits for movie %s", movieContentID), err)
	}
	return credits, nil
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// FindByCreditID loads a single credit by its primary key with the nested identity.
func (r *MovieCreditRepository) FindByCreditID(ctx context.Context, creditID uint) (*models.MovieCredit, error) {
	var credit models.MovieCredit
	err := r.GetDB().WithContext(ctx).Preload("Actress").First(&credit, creditID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie credit %d: %w", creditID, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie credit %d", creditID), err)
	}
	return &credit, nil
}

// FindByMovieAndActress loads a single credit by its movie/actress pair.
func (r *MovieCreditRepository) FindByMovieAndActress(ctx context.Context, movieContentID string, actressID uint) (*models.MovieCredit, error) {
	var credit models.MovieCredit
	err := r.GetDB().WithContext(ctx).
		Preload("Actress").
		First(&credit, "movie_content_id = ? AND actress_id = ?", movieContentID, actressID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find movie credit %s/%d: %w", movieContentID, actressID, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("movie credit %s/%d", movieContentID, actressID), err)
	}
	return &credit, nil
}

// UpsertTx upserts a credit within the given transaction, adopting user
// carried fields on collision per the D12 rules.
func (r *MovieCreditRepository) UpsertTx(tx *gorm.DB, credit *models.MovieCredit) error {
	var existing models.MovieCredit
	err := tx.First(&existing, "movie_content_id = ? AND actress_id = ?", credit.MovieContentID, credit.ActressID).Error
	if err == nil {
		credit.ID = existing.ID
		credit.CreatedAt = existing.CreatedAt
		if existing.Suppressed {
			credit.ID = existing.ID
			credit.CreatedAt = existing.CreatedAt
			credit.Suppressed = true
			return nil
		}
		updateCols := []string{"credited_name", "credited_japanese_name", "reported_thumb_url", "source", "updated_at"}
		if existing.Origin != string(models.CreditOriginUser) {
			updateCols = append(updateCols, "origin")
		}
		if !existing.OrderPinned {
			updateCols = append(updateCols, "order_index")
		}
		if err := tx.Model(&existing).Select(updateCols).Updates(credit).Error; err != nil {
			return wrapDBErr("update", fmt.Sprintf("movie credit %s/%d", credit.MovieContentID, credit.ActressID), err)
		}
		credit.ID = existing.ID
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return wrapDBErr("find", fmt.Sprintf("movie credit %s/%d", credit.MovieContentID, credit.ActressID), err)
	}
	return raceRetryCreate(tx, credit, func(tx *gorm.DB) error {
		var found models.MovieCredit
		if ferr := tx.First(&found, "movie_content_id = ? AND actress_id = ?", credit.MovieContentID, credit.ActressID).Error; ferr != nil {
			return ferr
		}
		credit.ID = found.ID
		credit.CreatedAt = found.CreatedAt
		return nil
	})
}

// DeleteTx deletes a credit by its movie/actress pair.
func (r *MovieCreditRepository) DeleteTx(tx *gorm.DB, movieContentID string, actressID uint) error {
	if err := tx.Where("movie_content_id = ? AND actress_id = ?", movieContentID, actressID).
		Delete(&models.MovieCredit{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("movie credit %s/%d", movieContentID, actressID), err)
	}
	return nil
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// DeleteByIDTx deletes a credit by its primary key within the given transaction.
func (r *MovieCreditRepository) DeleteByIDTx(tx *gorm.DB, id uint) error {
	if err := tx.Delete(&models.MovieCredit{}, id).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("movie credit %d", id), err)
	}
	return nil
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// UpdateOverride sets a per-movie display override on a credit and marks it user-owned.
func (r *MovieCreditRepository) UpdateOverride(ctx context.Context, creditID uint, overrideName string, userOverride bool) error {
	updates := map[string]interface{}{
		colOverrideName: overrideName,
		colUserOverride: userOverride,
		colOrigin:       string(models.CreditOriginUser),
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.MovieCredit{}).Where("id = ?", creditID).Updates(updates).Error; err != nil {
		return wrapDBErr("update override", fmt.Sprintf("movie credit %d", creditID), err)
	}
	return nil
}

// UpdateSuppressed toggles the user-removal tombstone on a credit.
func (r *MovieCreditRepository) UpdateSuppressed(ctx context.Context, creditID uint, suppressed bool) error {
	updates := map[string]interface{}{
		colSuppressed: suppressed,
		colOrigin:     string(models.CreditOriginUser),
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.MovieCredit{}).Where("id = ?", creditID).Updates(updates).Error; err != nil {
		return wrapDBErr("update suppressed", fmt.Sprintf("movie credit %d", creditID), err)
	}
	return nil
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// UpdateOrderPinned sets a user-owned cast position on a credit.
func (r *MovieCreditRepository) UpdateOrderPinned(ctx context.Context, creditID uint, orderIndex int, pinned bool) error {
	updates := map[string]interface{}{
		colOrderIndex:  orderIndex,
		colOrderPinned: pinned,
		colOrigin:      string(models.CreditOriginUser),
	}
	if err := r.GetDB().WithContext(ctx).Model(&models.MovieCredit{}).Where("id = ?", creditID).Updates(updates).Error; err != nil {
		return wrapDBErr("update order", fmt.Sprintf("movie credit %d", creditID), err)
	}
	return nil
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// SetDisplayForceCanonical forces or clears the canonical display preference on a credit.
func (r *MovieCreditRepository) SetDisplayForceCanonical(ctx context.Context, creditID uint, forced bool) error {
	if err := r.GetDB().WithContext(ctx).Model(&models.MovieCredit{}).Where("id = ?", creditID).
		Update("display_force_canonical", forced).Error; err != nil {
		return wrapDBErr("update force canonical", fmt.Sprintf("movie credit %d", creditID), err)
	}
	return nil
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// ReassignCredit moves a credit to a different identity within one transaction,
// applying the D12 collision field rules.
func (r *MovieCreditRepository) ReassignCredit(ctx context.Context, credit *models.MovieCredit, targetActressID uint) error {
	return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return reassignCreditTx(tx, credit, targetActressID)
	})
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// MarkMovieDirty marks a movie render-dirty with a generation bump.
func (r *MovieCreditRepository) MarkMovieDirty(ctx context.Context, movieContentID string) error {
	if err := r.GetDB().WithContext(ctx).Exec(
		"UPDATE movies SET render_dirty = 1, render_generation = render_generation + 1, updated_at = CURRENT_TIMESTAMP WHERE content_id = ?",
		movieContentID,
	).Error; err != nil {
		return wrapDBErr("mark dirty", fmt.Sprintf("movie %s", movieContentID), err)
	}
	return nil
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// CountByActress returns the number of credits held by an actress.
func (r *MovieCreditRepository) CountByActress(ctx context.Context, actressID uint) (int64, error) {
	var count int64
	if err := r.GetDB().WithContext(ctx).Model(&models.MovieCredit{}).
		Where("actress_id = ?", actressID).Count(&count).Error; err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("credits for actress %d", actressID), err)
	}
	return count, nil
}

// MovieCreditRepository implements the credit identity lifecycle contract.
// ListByActress returns all credits held by an actress, newest first.
func (r *MovieCreditRepository) ListByActress(ctx context.Context, actressID uint) ([]models.MovieCredit, error) {
	var credits []models.MovieCredit
	err := r.GetDB().WithContext(ctx).
		Where("actress_id = ?", actressID).
		Order("updated_at DESC").
		Find(&credits).Error
	if err != nil {
		return nil, wrapDBErr("list", fmt.Sprintf("credits for actress %d", actressID), err)
	}
	return credits, nil
}
