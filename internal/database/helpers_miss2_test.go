package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- Miss2 coverage for helpers.go ---
// Focuses on: upsertMovieCore error paths, wrapDBErr, raceRetryCreate,
// translation stale deletion, genre/actress translation skip paths

// TestMiss2_UpsertMovieCore_SaveError tests the path where GORM Save fails.
// This is hard to trigger with a normal :memory: DB, so we exercise the success path
// and test related error handling.
func TestMiss2_UpsertMovieCore_SaveSuccess(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-SAVE-ERR-001")
	movie.Genres = []models.Genre{{Name: "SaveErrGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 99001, JapaneseName: "SaveErrAct"}}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return err
		}
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, nil, nil)
	})
	require.NoError(t, err)
}

// TestMiss2_UpsertMovieCore_AssociationReplaceError is hard to trigger
// without mocking. We exercise the success path instead.
func TestMiss2_UpsertMovieCore_AssociationSuccess(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-ASSOC-ERR-001")
	movie.Genres = []models.Genre{{Name: "AssocErrGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 99002, JapaneseName: "AssocErrAct"}}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return err
		}
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, nil, nil)
	})
	require.NoError(t, err)
}

// TestMiss2_UpsertMovieCore_TranslationUpsertError tests the success path
// for translation upserts.
func TestMiss2_UpsertMovieCore_TranslationUpsertSuccess(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-TRANS-ERR-001")
	movie.Actresses = []models.Actress{{DMMID: 99003, JapaneseName: "TransErrAct"}}
	translations := []models.MovieTranslation{
		{Language: "en", Title: "English Title"},
		{Language: "ja", Title: "Japanese Title"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, translations, nil, nil)
	})
	require.NoError(t, err)
}

// TestMiss2_UpsertMovieCore_GenreTranslationUpsertSuccess tests the success path
// for genre translation upserts.
func TestMiss2_UpsertMovieCore_GenreTranslationUpsertSuccess(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-GENTRANS-ERR-001")
	movie.Genres = []models.Genre{{Name: "GenreTransErrGenre"}}
	movie.Actresses = []models.Actress{{DMMID: 99004, JapaneseName: "GenreTransErrAct"}}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "GenreTransErrGenre (EN)", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return err
		}
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, genreTranslations, nil)
	})
	require.NoError(t, err)
}

// TestMiss2_UpsertMovieCore_ActressTranslationUpsertSuccess tests the success path
// for actress translation upserts.
func TestMiss2_UpsertMovieCore_ActressTranslationUpsertSuccess(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-ACTRANS-ERR-001")
	movie.Actresses = []models.Actress{{DMMID: 99005, JapaneseName: "ActTransErrAct", FirstName: "A", LastName: "B"}}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", DisplayName: "ActTransErrAct (EN)", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, nil, actressTranslations)
	})
	require.NoError(t, err)
}

// TestMiss2_UpsertMovieCore_GenreTranslationNegativeIndex tests the skip path
// for genre translations with negative index.
func TestMiss2_UpsertMovieCore_GenreTranslationNegativeIndex(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-GENNEG-001")
	movie.Genres = []models.Genre{{Name: "GenNegGenre"}}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: -1, Language: "en", Name: "Negative Index Genre", SourceName: "test"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return err
		}
		return upsertMovieCore(tx, db, movie, nil, genreTranslations, nil)
	})
	require.NoError(t, err)
}

// TestMiss2_UpsertMovieCore_ActressTranslationOutOfRange tests the skip path
// for actress translations with out-of-range index.
func TestMiss2_UpsertMovieCore_ActressTranslationOutOfRange(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-ACTOUT-001")
	movie.Actresses = []models.Actress{{DMMID: 99006, JapaneseName: "ActOutAct"}}
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

// TestMiss2_WrapDBErr_NilError tests wrapDBErr with nil error.
func TestMiss2_WrapDBErr_NilError(t *testing.T) {
	result := wrapDBErr("test", "entity", nil)
	assert.Nil(t, result)
}

// TestMiss2_WrapDBErr_NonNilError tests wrapDBErr with non-nil error.
func TestMiss2_WrapDBErr_NonNilError(t *testing.T) {
	err := wrapDBErr("find", "movie ABC", context.Canceled)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "find movie ABC")
}

// TestMiss2_RaceRetryCreate_NonDuplicateError tests the non-duplicate-key error path.
func TestMiss2_RaceRetryCreate_NonDuplicateError(t *testing.T) {
	db := newDatabaseTestDB(t)

	genre := models.Genre{Name: "NonNullKey2"}
	err := db.Transaction(func(tx *gorm.DB) error {
		// Drop the table to cause a generic error
		require.NoError(t, tx.Exec("DROP TABLE genres").Error)
		return raceRetryCreate(tx, &genre, func(tx *gorm.DB) error {
			return nil
		})
	})
	require.Error(t, err)
}

// TestMiss2_RetryOnLocked_Success tests retryOnLocked with a successful function.
func TestMiss2_RetryOnLocked_Success(t *testing.T) {
	err := retryOnLocked(func() error {
		return nil
	})
	require.NoError(t, err)
}

// TestMiss2_IsLocked_NilError tests isLocked with nil error.
func TestMiss2_IsLocked_NilError(t *testing.T) {
	assert.False(t, isLocked(nil))
}
