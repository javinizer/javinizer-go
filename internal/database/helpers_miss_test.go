package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestUpsertMovieCore_GenreIDNotResolvedAfterReload tests the path where
// genre ID is not resolved after reloading from the database (ID==0 after lookup).
// This hits the genreByName lookup miss branch.
func TestUpsertMovieCore_GenreIDNotResolvedAfterReload(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-GENRE-MISS-001")
	movie.Genres = []models.Genre{{Name: "UniqueGenre"}}
	// Also add genre translations to trigger the reload+ID-resolution path
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "UniqueGenre (EN)", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, genreTranslations, nil)
	})
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-GENRE-MISS-001")
	require.NoError(t, err)
	assert.Len(t, found.Genres, 1)
}

// TestUpsertMovieCore_ActressIDResolvedViaCompositeKey tests the path where
// actress ID is resolved via the composite key (FirstName|LastName|JapaneseName)
// when DMMID is 0 or doesn't match.
func TestUpsertMovieCore_ActressIDResolvedViaCompositeKey(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-COMPOSITE-002")
	// Actress with no DMMID — forces composite key resolution
	movie.Actresses = []models.Actress{
		{FirstName: "Jane", LastName: "Doe", JapaneseName: "ジェーン・ドー"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", DisplayName: "Jane Doe (EN)", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, nil, actressTranslations)
	})
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-COMPOSITE-002")
	require.NoError(t, err)
	require.Len(t, found.Actresses, 1)
	assert.NotZero(t, found.Actresses[0].ID)
}

// TestUpsertMovieCore_ActressIDResolvedViaDMMID tests the path where
// actress ID is resolved via DMMID lookup.
func TestUpsertMovieCore_ActressIDResolvedViaDMMID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-DMMID-002")
	movie.Actresses = []models.Actress{
		{DMMID: 55501, JapaneseName: "DMMID Actress"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", DisplayName: "DMMID Actress (EN)", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, nil, actressTranslations)
	})
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-DMMID-002")
	require.NoError(t, err)
	require.Len(t, found.Actresses, 1)
	assert.NotZero(t, found.Actresses[0].ID)
}

// TestUpsertMovieCore_GenreTranslationSkippedWhenGenreIDZero tests the path
// where genre translation is skipped because the genre ID was not resolved.
func TestUpsertMovieCore_GenreTranslationSkippedWhenGenreIDZero(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	// Create a movie with genres but provide genre translations referencing
	// an index where the genre ID couldn't be resolved.
	// We use a normal flow but the genre ID resolution will work, so we
	// test with an out-of-range index which also triggers the skip path.
	movie := createTestMovie("IPX-GENRE-SKIP-001")
	movie.Genres = []models.Genre{{Name: "Action"}}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: -1, Language: "en", Name: "Negative Index", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, genreTranslations, nil)
	})
	require.NoError(t, err)
}

// TestUpsertMovieCore_ActressTranslationSkippedWhenActressIDZero tests the path
// where actress translation is skipped because the actress ID was not resolved.
func TestUpsertMovieCore_ActressTranslationSkippedWhenActressIDZero(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-ACTRESS-SKIP-001")
	movie.Actresses = []models.Actress{{DMMID: 77801, JapaneseName: "Skip Actress"}}
	// Out-of-range index triggers the skip path
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 99, Language: "en", DisplayName: "Out of Range", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, nil, actressTranslations)
	})
	require.NoError(t, err)
}

// TestUpsertMovieCore_TranslationStaleDeletion tests that translations accumulate
// across languages: updating with a single language upserts it while preserving
// previously-persisted translations for other languages. This mirrors main's
// upsertMovieCore, which only upserted incoming translations and never deleted
// (re-scraping after switching target_language must not lose prior languages).
func TestUpsertMovieCore_TranslationStaleDeletion(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-STALE-DEL-001")
	movie.Actresses = []models.Actress{{DMMID: 88901, JapaneseName: "Stale Actress"}}
	// First: create with three translations
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "English"},
		{Language: "fr", Title: "French"},
		{Language: "de", Title: "German"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		translations := movie.Translations
		movie.Translations = nil
		return upsertMovieCore(tx, db, movie, translations, nil, nil)
	})
	require.NoError(t, err)

	// Now update with only "en" — "fr" and "de" must be preserved (accumulate),
	// and "en" upserted to the updated title.
	existing, err := repo.FindByID(context.TODO(), "IPX-STALE-DEL-001")
	require.NoError(t, err)
	movie.CreatedAt = existing.CreatedAt
	movie.Actresses = []models.Actress{{DMMID: 88901, JapaneseName: "Stale Actress"}}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		translations := []models.MovieTranslation{
			{Language: "en", Title: "English Updated"},
		}
		return upsertMovieCore(tx, db, movie, translations, nil, nil)
	})
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-STALE-DEL-001")
	require.NoError(t, err)
	// All three languages persist; none are deleted as "stale".
	assert.Len(t, found.Translations, 3)
	byLang := make(map[string]string, len(found.Translations))
	for _, tr := range found.Translations {
		byLang[tr.Language] = tr.Title
	}
	assert.Equal(t, "English Updated", byLang["en"], "en should be upserted to the updated title")
	assert.Equal(t, "French", byLang["fr"], "fr should be preserved (accumulate, not delete)")
	assert.Equal(t, "German", byLang["de"], "de should be preserved (accumulate, not delete)")
}

// TestRaceRetryCreate_NonDuplicateKeyError tests the path where Create fails
// with an error that is NOT ErrDuplicatedKey.
func TestRaceRetryCreate_NonDuplicateKeyError(t *testing.T) {
	db := newDatabaseTestDB(t)

	genre := models.Genre{Name: "NonNullKey"}
	err := db.Transaction(func(tx *gorm.DB) error {
		// Drop the table to cause a generic error (not duplicate key)
		require.NoError(t, tx.Exec("DROP TABLE genres").Error)
		return raceRetryCreate(tx, &genre, func(tx *gorm.DB) error {
			return nil
		})
	})
	require.Error(t, err)
	// Should not be a "duplicate key" error
	assert.NotContains(t, err.Error(), "duplicate key")
}

// TestUpsertMovieCore_BothGenreAndActressTranslations tests the path where
// both genre and actress translations are provided simultaneously,
// hitting the genre+actress ID resolution code.
func TestUpsertMovieCore_BothGenreAndActressTranslations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-BOTH-TRANS-001")
	movie.Genres = []models.Genre{{Name: "Action"}, {Name: "Drama"}}
	movie.Actresses = []models.Actress{
		{DMMID: 66001, JapaneseName: "BothTrans Actress"},
	}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Action (EN)", SourceName: "test"},
		{GenreIndex: 1, Language: "en", Name: "Drama (EN)", SourceName: "test"},
	}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", DisplayName: "BothTrans Actress (EN)", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return err
		}
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, genreTranslations, actressTranslations)
	})
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), "IPX-BOTH-TRANS-001")
	require.NoError(t, err)
	assert.Len(t, found.Genres, 2)
	assert.Len(t, found.Actresses, 1)
}
