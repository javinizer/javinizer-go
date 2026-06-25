package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
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

func raceRetryCreate(tx *gorm.DB, entity any, findExisting func(tx *gorm.DB) error) error {
	if err := tx.Create(entity).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			if findErr := findExisting(tx); findErr != nil {
				return fmt.Errorf("create duplicate key, then reload also failed: create=%w, reload=%w", err, findErr)
			}
			return nil
		}
		return err
	}
	return nil
}

// preparedMovie holds the results of business-logic preparation before GORM persistence.
// It carries the genre/actress ID-resolution state needed by persistTranslations.
type preparedMovie struct {
	movie               *models.Movie
	genreTranslations   []models.GenreTranslationData
	actressTranslations []models.ActressTranslationData
}

// prepareMovieForUpsert resolves genre and actress IDs from the database after associations
// have been replaced, so that translation carry-field logic can map GenreIndex/ActressIndex
// to concrete database IDs.
func prepareMovieForUpsert(tx *gorm.DB, movie *models.Movie, genreTranslations []models.GenreTranslationData, actressTranslations []models.ActressTranslationData) (*preparedMovie, error) {
	if len(genreTranslations) > 0 || len(actressTranslations) > 0 {
		if len(movie.Genres) > 0 {
			var dbGenres []models.Genre
			if err := tx.Model(movie).Association("Genres").Find(&dbGenres); err != nil {
				return nil, wrapDBErr("reload genres", movie.ContentID, err)
			}
			genreByName := make(map[string]uint, len(dbGenres))
			for _, g := range dbGenres {
				genreByName[g.Name] = g.ID
			}
			for i := range movie.Genres {
				if movie.Genres[i].ID == 0 {
					if id, ok := genreByName[movie.Genres[i].Name]; ok {
						movie.Genres[i].ID = id
					}
				}
			}
		}
		if len(movie.Actresses) > 0 {
			var dbActresses []models.Actress
			if err := tx.Model(movie).Association("Actresses").Find(&dbActresses); err != nil {
				return nil, wrapDBErr("reload actresses", movie.ContentID, err)
			}
			actressByDMMID := make(map[int]uint, len(dbActresses))
			actressByComposite := make(map[string]uint, len(dbActresses))
			for _, a := range dbActresses {
				if a.DMMID > 0 {
					actressByDMMID[a.DMMID] = a.ID
				}
				key := a.FirstName + "|" + a.LastName + "|" + a.JapaneseName
				actressByComposite[key] = a.ID
			}
			for i := range movie.Actresses {
				if movie.Actresses[i].ID == 0 {
					if movie.Actresses[i].DMMID > 0 {
						if id, ok := actressByDMMID[movie.Actresses[i].DMMID]; ok {
							movie.Actresses[i].ID = id
						}
					}
					if movie.Actresses[i].ID == 0 {
						key := movie.Actresses[i].FirstName + "|" + movie.Actresses[i].LastName + "|" + movie.Actresses[i].JapaneseName
						if id, ok := actressByComposite[key]; ok {
							movie.Actresses[i].ID = id
						}
					}
				}
			}
		}
	}

	return &preparedMovie{
		movie:               movie,
		genreTranslations:   genreTranslations,
		actressTranslations: actressTranslations,
	}, nil
}

// persistTranslations persists movie, genre, and actress translations for a prepared movie.
// Translations are managed via UpsertTx instead of Association().Replace() to preserve
// ID and CreatedAt on existing rows, and to maintain the SettingsHash field per translation.
// When the incoming translations list is empty, existing translations are preserved — unlike
// Genres/Actresses which are cleared. This is intentional: most scrapers do not return
// translations, and an empty list should not wipe translations provided by a different scraper.
func persistTranslations(tx *gorm.DB, db *DB, pm *preparedMovie, translations []models.MovieTranslation) error {
	movie := pm.movie

	translationRepo := newMovieTranslationRepository(db)
	for i := range translations {
		translations[i].MovieID = movie.ContentID
		if err := translationRepo.UpsertTx(tx, &translations[i]); err != nil {
			return err
		}
	}
	// Translations accumulate across languages: re-scraping after switching
	// metadata.translation.target_language upserts the new language alongside
	// any previously-persisted ones rather than deleting them. This mirrors
	// main's upsertMovieCore, which only upserted incoming translations and
	// never deleted, preserving a multilingual translation history.

	movie.Translations = translations

	// Persist genre translations (from Movie, populated by TranslateMovie).
	if len(pm.genreTranslations) > 0 {
		genreTranslationRepo := newGenreTranslationRepository(db)
		for _, gt := range pm.genreTranslations {
			if gt.GenreIndex < 0 || gt.GenreIndex >= len(movie.Genres) {
				continue
			}
			genreID := movie.Genres[gt.GenreIndex].ID
			if genreID == 0 {
				logging.Debugf("Translation: skipping genre translation for index %d — genre ID not resolved", gt.GenreIndex)
				continue
			}
			record := &models.GenreTranslation{
				GenreID:    genreID,
				Language:   gt.Language,
				Name:       gt.Name,
				SourceName: gt.SourceName,
			}
			if err := genreTranslationRepo.UpsertTx(tx, record); err != nil {
				return wrapDBErr("upsert genre translation", fmt.Sprintf("genre:%d/lang:%s", genreID, gt.Language), err)
			}
		}
	}

	// Persist actress translations (from Movie, populated by TranslateMovie).
	if len(pm.actressTranslations) > 0 {
		actressTranslationRepo := newActressTranslationRepository(db)
		for _, at := range pm.actressTranslations {
			if at.ActressIndex < 0 || at.ActressIndex >= len(movie.Actresses) {
				continue
			}
			actressID := movie.Actresses[at.ActressIndex].ID
			if actressID == 0 {
				logging.Debugf("Translation: skipping actress translation for index %d — actress ID not resolved", at.ActressIndex)
				continue
			}
			record := &models.ActressTranslation{
				ActressID:    actressID,
				Language:     at.Language,
				FirstName:    at.FirstName,
				LastName:     at.LastName,
				JapaneseName: at.JapaneseName,
				DisplayName:  at.DisplayName,
				SourceName:   at.SourceName,
			}
			if err := actressTranslationRepo.UpsertTx(tx, record); err != nil {
				return wrapDBErr("upsert actress translation", fmt.Sprintf("actress:%d/lang:%s", actressID, at.Language), err)
			}
		}
	}

	return nil
}

func upsertMovieCore(tx *gorm.DB, db *DB, movie *models.Movie, translations []models.MovieTranslation, genreTranslations []models.GenreTranslationData, actressTranslations []models.ActressTranslationData) error {
	// Step 1: GORM upsert the movie record (without associations)
	if err := tx.Omit("Actresses", "Genres", "Translations").Save(movie).Error; err != nil {
		return err
	}

	// Step 2: Handle associations (replace genres and actresses)
	if err := tx.Model(movie).Association("Genres").Replace(movie.Genres); err != nil {
		return err
	}
	if err := tx.Model(movie).Association("Actresses").Replace(movie.Actresses); err != nil {
		return err
	}

	// Step 3: Prepare — resolve genre/actress IDs for translation carry-field logic
	pm, err := prepareMovieForUpsert(tx, movie, genreTranslations, actressTranslations)
	if err != nil {
		return err
	}

	// Step 4: Persist translations (movie, genre, actress)
	return persistTranslations(tx, db, pm, translations)
}
