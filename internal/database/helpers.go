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
	if err := tx.Omit("Actresses", "Genres", "Translations").Save(movie).Error; err != nil {
		return err
	}

	if err := tx.Model(movie).Association("Genres").Replace(movie.Genres); err != nil {
		return err
	}
	if err := tx.Model(movie).Association("Actresses").Replace(movie.Actresses); err != nil {
		return err
	}

	// Translations are managed via UpsertTx instead of Association().Replace() to preserve
	// ID and CreatedAt on existing rows, and to maintain the SettingsHash field per translation.
	// When the incoming translations list is empty, existing translations are preserved — unlike
	// Genres/Actresses which are cleared. This is intentional: most scrapers do not return
	// translations, and an empty list should not wipe translations provided by a different scraper.
	translationRepo := NewMovieTranslationRepository(db)
	incomingLangs := make(map[string]bool, len(translations))
	for i := range translations {
		translations[i].MovieID = movie.ContentID
		incomingLangs[translations[i].Language] = true
		if err := translationRepo.UpsertTx(tx, &translations[i]); err != nil {
			return err
		}
	}

	if len(translations) > 0 {
		var existingTranslations []models.MovieTranslation
		if err := tx.Where("movie_id = ?", movie.ContentID).Find(&existingTranslations).Error; err != nil {
			return wrapDBErr("find stale translations", movie.ContentID, err)
		}
		for _, et := range existingTranslations {
			if !incomingLangs[et.Language] {
				if err := tx.Delete(&et).Error; err != nil {
					return wrapDBErr("delete stale translation", translationEntityID(movie.ContentID, et.Language), err)
				}
			}
		}
	}

	movie.Translations = translations
	return nil
}
