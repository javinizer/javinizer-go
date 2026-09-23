package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// ActressTranslationRepository persists localized name translations for actresses.
type ActressTranslationRepository struct {
	db *DB
}

func newActressTranslationRepository(db *DB) *ActressTranslationRepository {
	return &ActressTranslationRepository{db: db}
}

func normalizeActressTranslationLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func actressTranslationEntityID(actressID uint, language string) string {
	return fmt.Sprintf("actress translation %d/%s", actressID, normalizeActressTranslationLanguage(language))
}

// Upsert inserts the actress translation or updates the existing record matched by actress and language.
func (r *ActressTranslationRepository) Upsert(ctx context.Context, translation *models.ActressTranslation) error {
	return r.UpsertTx(r.db.WithContext(ctx), translation)
}

// UpsertTx inserts or updates the actress translation within the provided transaction.
func (r *ActressTranslationRepository) UpsertTx(tx *gorm.DB, translation *models.ActressTranslation) error {
	language := normalizeActressTranslationLanguage(translation.Language)
	if language == "" {
		return wrapDBErr("upsert", actressTranslationEntityID(translation.ActressID, translation.Language), ErrInvalidLookup)
	}
	translation.Language = language
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

// FindByActressAndLanguage returns the translation for the given actress in the given language.
func (r *ActressTranslationRepository) FindByActressAndLanguage(ctx context.Context, actressID uint, language string) (*models.ActressTranslation, error) {
	language = normalizeActressTranslationLanguage(language)
	var translation models.ActressTranslation
	err := r.db.WithContext(ctx).First(&translation, "actress_id = ? AND language = ?", actressID, language).Error
	if err != nil {
		return nil, wrapDBErr("find", actressTranslationEntityID(actressID, language), err)
	}
	translation.Language = normalizeActressTranslationLanguage(translation.Language)
	return &translation, nil
}

// FindAllByActress returns every translation stored for the given actress.
func (r *ActressTranslationRepository) FindAllByActress(ctx context.Context, actressID uint) ([]models.ActressTranslation, error) {
	var translations []models.ActressTranslation
	err := r.db.WithContext(ctx).Where("actress_id = ?", actressID).Find(&translations).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress translations for actress %d", actressID), err)
	}
	for i := range translations {
		translations[i].Language = normalizeActressTranslationLanguage(translations[i].Language)
	}
	return translations, nil
}

// FindByActressIDsAndLanguage returns translations grouped by actress ID for the given actress IDs and language.
func (r *ActressTranslationRepository) FindByActressIDsAndLanguage(ctx context.Context, actressIDs []uint, language string) (map[uint][]models.ActressTranslation, error) {
	if len(actressIDs) == 0 {
		return make(map[uint][]models.ActressTranslation), nil
	}
	language = normalizeActressTranslationLanguage(language)
	var translations []models.ActressTranslation
	if err := r.db.WithContext(ctx).Where("actress_id IN ? AND language = ?", actressIDs, language).Find(&translations).Error; err != nil {
		return nil, wrapDBErr("find", "actress translations batch", err)
	}
	result := make(map[uint][]models.ActressTranslation, len(actressIDs))
	for _, t := range translations {
		t.Language = normalizeActressTranslationLanguage(t.Language)
		result[t.ActressID] = append(result[t.ActressID], t)
	}
	return result, nil
}

// Delete removes the translation for the given actress in the given language.
func (r *ActressTranslationRepository) Delete(ctx context.Context, actressID uint, language string) error {
	language = normalizeActressTranslationLanguage(language)
	if err := r.db.WithContext(ctx).Delete(&models.ActressTranslation{}, "actress_id = ? AND language = ?", actressID, language).Error; err != nil {
		return wrapDBErr("delete", actressTranslationEntityID(actressID, language), err)
	}
	return nil
}
