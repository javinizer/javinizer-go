package database

import (
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

func wrapDBErr(op, entity string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %s: %w", op, entity, err)
}

func raceRetryCreate(tx *gorm.DB, entity interface{}, findExisting func(tx *gorm.DB) error) error {
	if err := tx.Create(entity).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			if findErr := findExisting(tx); findErr != nil {
				return err
			}
			return nil
		}
		return err
	}
	return nil
}

func upsertMovieCore(tx *gorm.DB, db *DB, movie *models.Movie, translations []models.MovieTranslation) error {
	if err := tx.Omit("Actresses", "Genres").Save(movie).Error; err != nil {
		return err
	}

	if err := tx.Model(movie).Association("Genres").Replace(movie.Genres); err != nil {
		return err
	}
	if err := tx.Model(movie).Association("Actresses").Replace(movie.Actresses); err != nil {
		return err
	}

	translationRepo := NewMovieTranslationRepository(db)
	for i := range translations {
		translations[i].MovieID = movie.ContentID
		if err := translationRepo.UpsertTx(tx, &translations[i]); err != nil {
			return err
		}
	}

	movie.Translations = translations
	return nil
}
