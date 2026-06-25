package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRaceRetryCreate(t *testing.T) {
	t.Run("creates new record successfully", func(t *testing.T) {
		db := newDatabaseTestDB(t)

		genre := models.Genre{Name: "Action"}
		err := db.Transaction(func(tx *gorm.DB) error {
			return raceRetryCreate(tx, &genre, func(tx *gorm.DB) error {
				var existing models.Genre
				return tx.Where("name = ?", genre.Name).First(&existing).Error
			})
		})
		require.NoError(t, err)
		assert.NotZero(t, genre.ID)
		assert.Equal(t, "Action", genre.Name)
	})

	t.Run("returns error when create fails and find also fails", func(t *testing.T) {
		db := newDatabaseTestDB(t)

		genre := models.Genre{Name: "Comedy"}
		err := db.Transaction(func(tx *gorm.DB) error {
			require.NoError(t, tx.Exec("DROP TABLE genres").Error)
			return raceRetryCreate(tx, &genre, func(tx *gorm.DB) error {
				var found models.Genre
				return tx.Where("name = ?", genre.Name).First(&found).Error
			})
		})
		require.Error(t, err)
	})

	t.Run("retries on ErrDuplicatedKey using find callback", func(t *testing.T) {
		db := newDatabaseTestDB(t)

		existing := models.Genre{Name: "Drama"}
		require.NoError(t, db.DB.Create(&existing).Error)

		cbName := "test:inject_genre_duplicate"
		inserted := false
		require.NoError(t, db.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
			if inserted || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "genres" {
				return
			}
			dest, ok := tx.Statement.Dest.(*models.Genre)
			if !ok {
				return
			}
			if dest.Name == "Drama" {
				inserted = true
				_ = tx.AddError(gorm.ErrDuplicatedKey)
			}
		}))
		defer func() { _ = db.DB.Callback().Create().Remove(cbName) }()

		genre := models.Genre{Name: "Drama"}
		err := db.Transaction(func(tx *gorm.DB) error {
			return raceRetryCreate(tx, &genre, func(tx *gorm.DB) error {
				var found models.Genre
				if err := tx.Where("name = ?", genre.Name).First(&found).Error; err != nil {
					return err
				}
				genre.ID = found.ID
				return nil
			})
		})
		require.NoError(t, err)
		assert.Equal(t, existing.ID, genre.ID)
	})
}

func TestUpsertMovieCore(t *testing.T) {
	t.Run("saves movie with associations and translations", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-CORE-001")
		movie.Genres = []models.Genre{{Name: "Action"}, {Name: "Drama"}}
		movie.Actresses = []models.Actress{{DMMID: 88801, JapaneseName: "Core Actress"}}
		movie.Translations = []models.MovieTranslation{
			{Language: "en", Title: "English Title"},
		}

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations, nil, nil)
		})
		require.NoError(t, err)

		found, err := repo.FindByID(context.TODO(), "IPX-CORE-001")
		require.NoError(t, err)
		assert.Equal(t, "IPX-CORE-001", found.ID)
		assert.Len(t, found.Genres, 2)
		assert.Len(t, found.Actresses, 1)
		assert.Len(t, found.Translations, 1)
		assert.Equal(t, "English Title", found.Translations[0].Title)
	})

	t.Run("clears actress associations when list is empty", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-CLEAR-001")
		movie.Genres = []models.Genre{{Name: "Action"}}
		movie.Actresses = []models.Actress{{DMMID: 99901, JapaneseName: "ToRemove"}}
		require.NoError(t, repo.Create(context.TODO(), movie))

		found, err := repo.FindByID(context.TODO(), "IPX-CLEAR-001")
		require.NoError(t, err)
		require.Len(t, found.Actresses, 1)
		require.Len(t, found.Genres, 1)

		movie.CreatedAt = found.CreatedAt
		movie.Actresses = nil
		movie.Genres = []models.Genre{{Name: "Action"}}

		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations, nil, nil)
		})
		require.NoError(t, err)

		found, err = repo.FindByID(context.TODO(), "IPX-CLEAR-001")
		require.NoError(t, err)
		assert.Empty(t, found.Actresses, "actresses should be cleared when set to nil")
		assert.Len(t, found.Genres, 1, "genres should remain unchanged")
	})

	t.Run("clears genre associations when list is empty", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-CLEAR-002")
		movie.Genres = []models.Genre{{Name: "Comedy"}, {Name: "Drama"}}
		movie.Actresses = []models.Actress{{DMMID: 99902, JapaneseName: "KeepActress"}}
		require.NoError(t, repo.Create(context.TODO(), movie))

		found, err := repo.FindByID(context.TODO(), "IPX-CLEAR-002")
		require.NoError(t, err)
		require.Len(t, found.Genres, 2)

		movie.CreatedAt = found.CreatedAt
		movie.Genres = nil
		movie.Actresses = []models.Actress{{DMMID: 99902, JapaneseName: "KeepActress"}}

		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations, nil, nil)
		})
		require.NoError(t, err)

		found, err = repo.FindByID(context.TODO(), "IPX-CLEAR-002")
		require.NoError(t, err)
		assert.Empty(t, found.Genres, "genres should be cleared when set to nil")
		assert.Len(t, found.Actresses, 1, "actresses should remain unchanged")
	})

	t.Run("clears both associations when both lists are empty", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-CLEAR-003")
		movie.Genres = []models.Genre{{Name: "Horror"}}
		movie.Actresses = []models.Actress{{DMMID: 99903, JapaneseName: "ToRemove"}}
		require.NoError(t, repo.Create(context.TODO(), movie))

		found, err := repo.FindByID(context.TODO(), "IPX-CLEAR-003")
		require.NoError(t, err)
		require.Len(t, found.Genres, 1)
		require.Len(t, found.Actresses, 1)

		movie.CreatedAt = found.CreatedAt
		movie.Genres = nil
		movie.Actresses = nil

		err = db.Transaction(func(tx *gorm.DB) error {
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations, nil, nil)
		})
		require.NoError(t, err)

		found, err = repo.FindByID(context.TODO(), "IPX-CLEAR-003")
		require.NoError(t, err)
		assert.Empty(t, found.Genres, "genres should be cleared")
		assert.Empty(t, found.Actresses, "actresses should be cleared")
	})

	t.Run("updates existing movie with associations", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-CORE-002")
		movie.Genres = []models.Genre{{Name: "Comedy"}}
		require.NoError(t, repo.Create(context.TODO(), movie))

		existing, err := repo.FindByID(context.TODO(), "IPX-CORE-002")
		require.NoError(t, err)

		movie.CreatedAt = existing.CreatedAt
		movie.Title = "Updated via Core"
		movie.Genres = []models.Genre{{Name: "Thriller"}}
		movie.Actresses = []models.Actress{{DMMID: 88802, JapaneseName: "New Actress"}}
		movie.Translations = []models.MovieTranslation{
			{Language: "zh", Title: "Chinese Title"},
		}

		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations, nil, nil)
		})
		require.NoError(t, err)

		found, err := repo.FindByID(context.TODO(), "IPX-CORE-002")
		require.NoError(t, err)
		assert.Equal(t, "Updated via Core", found.Title)
		assert.Len(t, found.Genres, 1)
		assert.Equal(t, "Thriller", found.Genres[0].Name)
		assert.Len(t, found.Actresses, 1)
		assert.Len(t, found.Translations, 1)
	})

	t.Run("preserves existing translations when incoming list is empty", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-TRANS-PRES-001")
		movie.Actresses = []models.Actress{{DMMID: 88890, JapaneseName: "Pres Actress"}}
		movie.Translations = []models.MovieTranslation{
			{Language: "en", Title: "English Title"},
			{Language: "zh", Title: "Chinese Title"},
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

		existing, err := repo.FindByID(context.TODO(), "IPX-TRANS-PRES-001")
		require.NoError(t, err)
		movie.CreatedAt = existing.CreatedAt
		movie.Translations = nil
		movie.Actresses = []models.Actress{{DMMID: 88890, JapaneseName: "Pres Actress"}}
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			return upsertMovieCore(tx, db, movie, nil, nil, nil)
		})
		require.NoError(t, err)

		found, err := repo.FindByID(context.TODO(), "IPX-TRANS-PRES-001")
		require.NoError(t, err)
		assert.Len(t, found.Translations, 2, "existing translations should be preserved when incoming list is empty")
		assert.Equal(t, "English Title", found.Translations[0].Title)
		assert.Equal(t, "Chinese Title", found.Translations[1].Title)
	})

	t.Run("accumulates translations across languages on partial-overlap update", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-TRANS-STALE-001")
		movie.Actresses = []models.Actress{{DMMID: 88891, JapaneseName: "Stale Actress"}}
		movie.Translations = []models.MovieTranslation{
			{Language: "en", Title: "English Title"},
			{Language: "ja", Title: "Japanese Title"},
			{Language: "zh", Title: "Chinese Title"},
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

		existing, err := repo.FindByID(context.TODO(), "IPX-TRANS-STALE-001")
		require.NoError(t, err)
		movie.CreatedAt = existing.CreatedAt
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			translations := []models.MovieTranslation{
				{Language: "en", Title: "Updated English"},
			}
			return upsertMovieCore(tx, db, movie, translations, nil, nil)
		})
		require.NoError(t, err)

		found, err := repo.FindByID(context.TODO(), "IPX-TRANS-STALE-001")
		require.NoError(t, err)
		// Translations accumulate: the incoming "en" is upserted while "ja" and
		// "zh" are preserved (main's upsertMovieCore never deleted).
		assert.Len(t, found.Translations, 3, "all languages should accumulate, none deleted")
		byLang := make(map[string]string, len(found.Translations))
		for _, tr := range found.Translations {
			byLang[tr.Language] = tr.Title
		}
		assert.Equal(t, "Updated English", byLang["en"], "en should be upserted to the updated title")
		assert.Equal(t, "Japanese Title", byLang["ja"], "ja should be preserved (accumulate)")
		assert.Equal(t, "Chinese Title", byLang["zh"], "zh should be preserved (accumulate)")
	})

	t.Run("no-op when incoming translations match existing exactly", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-TRANS-NOOP-001")
		movie.Actresses = []models.Actress{{DMMID: 88892, JapaneseName: "Noop Actress"}}
		movie.Translations = []models.MovieTranslation{
			{Language: "en", Title: "English Title"},
			{Language: "zh", Title: "Chinese Title"},
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

		existing, err := repo.FindByID(context.TODO(), "IPX-TRANS-NOOP-001")
		require.NoError(t, err)
		movie.CreatedAt = existing.CreatedAt
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			translations := []models.MovieTranslation{
				{Language: "en", Title: "English Title"},
				{Language: "zh", Title: "Chinese Title"},
			}
			return upsertMovieCore(tx, db, movie, translations, nil, nil)
		})
		require.NoError(t, err)

		found, err := repo.FindByID(context.TODO(), "IPX-TRANS-NOOP-001")
		require.NoError(t, err)
		assert.Len(t, found.Translations, 2)
		assert.Equal(t, "English Title", found.Translations[0].Title)
		assert.Equal(t, "Chinese Title", found.Translations[1].Title)
	})

	t.Run("persists genre translation carry-fields with correct ID resolution", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)
		genreTransRepo := newGenreTranslationRepository(db)

		movie := createTestMovie("IPX-GTRANS-001")
		movie.Genres = []models.Genre{{Name: "Action"}, {Name: "Drama"}}
		genreTranslations := []models.GenreTranslationData{
			{GenreIndex: 0, Language: "en", Name: "Action (EN)", SourceName: "test"},
			{GenreIndex: 1, Language: "en", Name: "Drama (EN)", SourceName: "test"},
		}

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			return upsertMovieCore(tx, db, movie, nil, genreTranslations, nil)
		})
		require.NoError(t, err)

		found, err := repo.FindByID(context.TODO(), "IPX-GTRANS-001")
		require.NoError(t, err)
		require.Len(t, found.Genres, 2)

		for _, g := range found.Genres {
			trans, err := genreTransRepo.FindAllByGenre(context.TODO(), g.ID)
			require.NoError(t, err)
			require.Len(t, trans, 1, "expected exactly 1 translation for genre %s", g.Name)
			assert.Equal(t, "en", trans[0].Language)
			assert.Equal(t, g.ID, trans[0].GenreID)
			if g.Name == "Action" {
				assert.Equal(t, "Action (EN)", trans[0].Name)
			} else {
				assert.Equal(t, "Drama (EN)", trans[0].Name)
			}
		}
	})

	t.Run("persists actress translation carry-fields with correct ID resolution", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)
		actressTransRepo := newActressTranslationRepository(db)

		movie := createTestMovie("IPX-ATRANS-001")
		movie.Actresses = []models.Actress{{DMMID: 77001, JapaneseName: "Test Actress"}, {DMMID: 77002, JapaneseName: "Other Actress"}}
		actressTranslations := []models.ActressTranslationData{
			{ActressIndex: 0, Language: "en", DisplayName: "Test Actress (EN)", SourceName: "test"},
			{ActressIndex: 1, Language: "en", DisplayName: "Other Actress (EN)", SourceName: "test"},
		}

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			return upsertMovieCore(tx, db, movie, nil, nil, actressTranslations)
		})
		require.NoError(t, err)

		found, err := repo.FindByID(context.TODO(), "IPX-ATRANS-001")
		require.NoError(t, err)
		require.Len(t, found.Actresses, 2)

		for _, a := range found.Actresses {
			trans, err := actressTransRepo.FindAllByActress(context.TODO(), a.ID)
			require.NoError(t, err)
			require.Len(t, trans, 1, "expected exactly 1 translation for actress %s", a.JapaneseName)
			assert.Equal(t, "en", trans[0].Language)
			assert.Equal(t, a.ID, trans[0].ActressID)
			if a.JapaneseName == "Test Actress" {
				assert.Equal(t, "Test Actress (EN)", trans[0].DisplayName)
			} else {
				assert.Equal(t, "Other Actress (EN)", trans[0].DisplayName)
			}
		}
	})

	t.Run("genre translation ID resolution preserves original slice order", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)
		genreTransRepo := newGenreTranslationRepository(db)

		genre1 := models.Genre{Name: "Zebra"}
		genre2 := models.Genre{Name: "Alpha"}
		require.NoError(t, db.DB.Create(&genre1).Error)
		require.NoError(t, db.DB.Create(&genre2).Error)

		movie := createTestMovie("IPX-GORDER-001")
		movie.Genres = []models.Genre{{Name: "Zebra"}, {Name: "Alpha"}}
		genreTranslations := []models.GenreTranslationData{
			{GenreIndex: 0, Language: "en", Name: "Zebra (EN)", SourceName: "test"},
			{GenreIndex: 1, Language: "en", Name: "Alpha (EN)", SourceName: "test"},
		}

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			return upsertMovieCore(tx, db, movie, nil, genreTranslations, nil)
		})
		require.NoError(t, err)

		zebraTrans, err := genreTransRepo.FindByGenreAndLanguage(context.TODO(), genre1.ID, "en")
		require.NoError(t, err)
		assert.Equal(t, "Zebra (EN)", zebraTrans.Name, "GenreIndex 0 should map to Zebra, not Alpha")

		alphaTrans, err := genreTransRepo.FindByGenreAndLanguage(context.TODO(), genre2.ID, "en")
		require.NoError(t, err)
		assert.Equal(t, "Alpha (EN)", alphaTrans.Name, "GenreIndex 1 should map to Alpha, not Zebra")
	})

	t.Run("skips genre translation with out-of-range index", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-GOUT-001")
		movie.Genres = []models.Genre{{Name: "Action"}}
		genreTranslations := []models.GenreTranslationData{
			{GenreIndex: 5, Language: "en", Name: "Out of Range", SourceName: "test"},
		}

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			return upsertMovieCore(tx, db, movie, nil, genreTranslations, nil)
		})
		require.NoError(t, err)
	})

	t.Run("skips actress translation with out-of-range index", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-AOUT-001")
		movie.Actresses = []models.Actress{{DMMID: 77010, JapaneseName: "Solo"}}
		actressTranslations := []models.ActressTranslationData{
			{ActressIndex: -1, Language: "en", DisplayName: "Negative Index", SourceName: "test"},
		}

		err := db.Transaction(func(tx *gorm.DB) error {
			if err := repo.upserter.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			return upsertMovieCore(tx, db, movie, nil, nil, actressTranslations)
		})
		require.NoError(t, err)
	})

	t.Run("resolves actress ID via composite key when DMMID is zero", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)
		actressTransRepo := newActressTranslationRepository(db)

		movie := createTestMovie("IPX-ACOMPOSITE-001")
		movie.Actresses = []models.Actress{
			{FirstName: "Jane", LastName: "Doe", JapaneseName: "ジェーン"},
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

		found, err := repo.FindByID(context.TODO(), "IPX-ACOMPOSITE-001")
		require.NoError(t, err)
		require.Len(t, found.Actresses, 1)
		assert.NotZero(t, found.Actresses[0].ID, "actress ID should be resolved via composite key")

		trans, err := actressTransRepo.FindAllByActress(context.TODO(), found.Actresses[0].ID)
		require.NoError(t, err)
		require.Len(t, trans, 1)
		assert.Equal(t, "Jane Doe (EN)", trans[0].DisplayName)
	})
}

func TestWrapDBErr(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		assert.Nil(t, wrapDBErr("create", "genre", nil))
	})

	t.Run("wraps error with operation and entity", func(t *testing.T) {
		err := fmt.Errorf("duplicate key")
		result := wrapDBErr("create", "genre", err)
		require.Error(t, result)
		assert.Contains(t, result.Error(), "create genre")
		assert.ErrorIs(t, result, err)
	})
}

func TestIsLocked(t *testing.T) {
	t.Run("nil error returns false", func(t *testing.T) {
		assert.False(t, isLocked(nil))
	})

	t.Run("generic locked message returns true", func(t *testing.T) {
		assert.True(t, isLocked(fmt.Errorf("database is locked")))
	})

	t.Run("table locked message returns true", func(t *testing.T) {
		assert.True(t, isLocked(fmt.Errorf("database table is locked")))
	})

	t.Run("unrelated error returns false", func(t *testing.T) {
		assert.False(t, isLocked(fmt.Errorf("no such table")))
	})
}

func TestRetryOnLocked(t *testing.T) {
	t.Run("succeeds immediately", func(t *testing.T) {
		err := retryOnLocked(func() error { return nil })
		assert.NoError(t, err)
	})

	t.Run("retries on locked then succeeds", func(t *testing.T) {
		callCount := 0
		err := retryOnLocked(func() error {
			callCount++
			if callCount < 3 {
				return fmt.Errorf("database is locked")
			}
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 3, callCount)
	})

	t.Run("returns non-locked error immediately", func(t *testing.T) {
		expectedErr := fmt.Errorf("no such table")
		err := retryOnLocked(func() error { return expectedErr })
		assert.ErrorIs(t, err, expectedErr)
	})
}
