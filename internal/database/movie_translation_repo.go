package database

import (
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type MovieTranslationRepository struct {
	db *DB
}

func NewMovieTranslationRepository(db *DB) *MovieTranslationRepository {
	return &MovieTranslationRepository{db: db}
}

func translationEntityID(movieID, language string) string {
	return fmt.Sprintf("translation %s/%s", movieID, language)
}

func (r *MovieTranslationRepository) Upsert(translation *models.MovieTranslation) error {
	return r.UpsertTx(r.db.DB, translation)
}

func (r *MovieTranslationRepository) UpsertTx(tx *gorm.DB, translation *models.MovieTranslation) error {
	var existing models.MovieTranslation
	err := tx.First(&existing, "movie_id = ? AND language = ?", translation.MovieID, translation.Language).Error
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return wrapDBErr("find", translationEntityID(translation.MovieID, translation.Language), err)
		}
		if err := tx.Create(translation).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				if loadErr := tx.First(&existing, "movie_id = ? AND language = ?", translation.MovieID, translation.Language).Error; loadErr == nil {
					translation.ID = existing.ID
					translation.CreatedAt = existing.CreatedAt
					if saveErr := tx.Save(translation).Error; saveErr != nil {
						return wrapDBErr("update", translationEntityID(translation.MovieID, translation.Language), saveErr)
					}
					return nil
				}
			}
			return wrapDBErr("create", translationEntityID(translation.MovieID, translation.Language), err)
		}
		return nil
	}

	translation.ID = existing.ID
	translation.CreatedAt = existing.CreatedAt
	if err := tx.Save(translation).Error; err != nil {
		return wrapDBErr("update", translationEntityID(translation.MovieID, translation.Language), err)
	}
	return nil
}

func (r *MovieTranslationRepository) FindByMovieAndLanguage(movieID, language string) (*models.MovieTranslation, error) {
	var translation models.MovieTranslation
	err := r.db.First(&translation, "movie_id = ? AND language = ?", movieID, language).Error
	if err != nil {
		return nil, wrapDBErr("find", translationEntityID(movieID, language), err)
	}
	return &translation, nil
}

func (r *MovieTranslationRepository) FindAllByMovie(movieID string) ([]models.MovieTranslation, error) {
	var translations []models.MovieTranslation
	err := r.db.Where("movie_id = ?", movieID).Find(&translations).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("translations for movie %s", movieID), err)
	}
	return translations, nil
}

func (r *MovieTranslationRepository) Delete(movieID, language string) error {
	if err := r.db.Delete(&models.MovieTranslation{}, "movie_id = ? AND language = ?", movieID, language).Error; err != nil {
		return wrapDBErr("delete", translationEntityID(movieID, language), err)
	}
	return nil
}
