package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

func wrapDBErr(op, entity string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %s: %w", op, entity, err)
}

func isLocked(err error) bool {
	var sqliteErr *sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked
	}
	return err != nil && (strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "database table is locked"))
}

const defaultLockRetries = 10

func retryOnLocked(fn func() error) error {
	var err error
	for i := 0; i < defaultLockRetries; i++ {
		err = fn()
		if err == nil || !isLocked(err) {
			return err
		}
		time.Sleep(time.Duration(100*(i+1)) * time.Millisecond)
	}
	return err
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
