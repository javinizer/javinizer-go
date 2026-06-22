package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type movieTranslationRepository struct {
	db *DB
}

func newMovieTranslationRepository(db *DB) *movieTranslationRepository {
	return &movieTranslationRepository{db: db}
}

func translationEntityID(movieID, language string) string {
	return fmt.Sprintf("translation %s/%s", movieID, language)
}

func (r *movieTranslationRepository) Upsert(ctx context.Context, translation *models.MovieTranslation) error {
	return r.UpsertTx(r.db.WithContext(ctx), translation)
}

func (r *movieTranslationRepository) UpsertTx(tx *gorm.DB, translation *models.MovieTranslation) error {
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

func (r *movieTranslationRepository) FindByMovieAndLanguage(ctx context.Context, movieID, language string) (*models.MovieTranslation, error) {
	var translation models.MovieTranslation
	err := r.db.WithContext(ctx).First(&translation, "movie_id = ? AND language = ?", movieID, language).Error
	if err != nil {
		return nil, wrapDBErr("find", translationEntityID(movieID, language), err)
	}
	return &translation, nil
}

func (r *movieTranslationRepository) FindAllByMovie(ctx context.Context, movieID string) ([]models.MovieTranslation, error) {
	var translations []models.MovieTranslation
	err := r.db.WithContext(ctx).Where("movie_id = ?", movieID).Find(&translations).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("translations for movie %s", movieID), err)
	}
	return translations, nil
}

func (r *movieTranslationRepository) Delete(ctx context.Context, movieID, language string) error {
	if err := r.db.WithContext(ctx).Delete(&models.MovieTranslation{}, "movie_id = ? AND language = ?", movieID, language).Error; err != nil {
		return wrapDBErr("delete", translationEntityID(movieID, language), err)
	}
	return nil
}
