package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type ActressTranslationRepository struct {
	db *DB
}

func newActressTranslationRepository(db *DB) *ActressTranslationRepository {
	return &ActressTranslationRepository{db: db}
}

func actressTranslationEntityID(actressID uint, language string) string {
	return fmt.Sprintf("actress translation %d/%s", actressID, language)
}

func (r *ActressTranslationRepository) Upsert(ctx context.Context, translation *models.ActressTranslation) error {
	return r.UpsertTx(r.db.WithContext(ctx), translation)
}

func (r *ActressTranslationRepository) UpsertTx(tx *gorm.DB, translation *models.ActressTranslation) error {
	var existing models.ActressTranslation
	err := tx.First(&existing, "actress_id = ? AND language = ?", translation.ActressID, translation.Language).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return wrapDBErr("find", actressTranslationEntityID(translation.ActressID, translation.Language), err)
		}
		if err := tx.Create(translation).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				loadErr := tx.First(&existing, "actress_id = ? AND language = ?", translation.ActressID, translation.Language).Error
				if loadErr == nil {
					translation.ID = existing.ID
					translation.CreatedAt = existing.CreatedAt
					if saveErr := tx.Save(translation).Error; saveErr != nil {
						return wrapDBErr("update", actressTranslationEntityID(translation.ActressID, translation.Language), saveErr)
					}
					return nil
				}
				return fmt.Errorf("create duplicate key, then reload also failed: create=%w, reload=%w", err, loadErr)
			}
			return wrapDBErr("create", actressTranslationEntityID(translation.ActressID, translation.Language), err)
		}
		return nil
	}
	translation.ID = existing.ID
	translation.CreatedAt = existing.CreatedAt
	if err := tx.Save(translation).Error; err != nil {
		return wrapDBErr("update", actressTranslationEntityID(translation.ActressID, translation.Language), err)
	}
	return nil
}

func (r *ActressTranslationRepository) FindByActressAndLanguage(ctx context.Context, actressID uint, language string) (*models.ActressTranslation, error) {
	var translation models.ActressTranslation
	err := r.db.WithContext(ctx).First(&translation, "actress_id = ? AND language = ?", actressID, language).Error
	if err != nil {
		return nil, wrapDBErr("find", actressTranslationEntityID(actressID, language), err)
	}
	return &translation, nil
}

func (r *ActressTranslationRepository) FindAllByActress(ctx context.Context, actressID uint) ([]models.ActressTranslation, error) {
	var translations []models.ActressTranslation
	err := r.db.WithContext(ctx).Where("actress_id = ?", actressID).Find(&translations).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress translations for actress %d", actressID), err)
	}
	return translations, nil
}

func (r *ActressTranslationRepository) FindByActressIDsAndLanguage(ctx context.Context, actressIDs []uint, language string) (map[uint][]models.ActressTranslation, error) {
	if len(actressIDs) == 0 {
		return make(map[uint][]models.ActressTranslation), nil
	}
	var translations []models.ActressTranslation
	if err := r.db.WithContext(ctx).Where("actress_id IN ? AND language = ?", actressIDs, language).Find(&translations).Error; err != nil {
		return nil, wrapDBErr("find", "actress translations batch", err)
	}
	result := make(map[uint][]models.ActressTranslation, len(actressIDs))
	for _, t := range translations {
		result[t.ActressID] = append(result[t.ActressID], t)
	}
	return result, nil
}

func (r *ActressTranslationRepository) Delete(ctx context.Context, actressID uint, language string) error {
	if err := r.db.WithContext(ctx).Delete(&models.ActressTranslation{}, "actress_id = ? AND language = ?", actressID, language).Error; err != nil {
		return wrapDBErr("delete", actressTranslationEntityID(actressID, language), err)
	}
	return nil
}
