package database

import (
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
			if err := repo.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			if err := repo.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations)
		})
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-CORE-001")
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
		require.NoError(t, repo.Create(movie))

		found, err := repo.FindByID("IPX-CLEAR-001")
		require.NoError(t, err)
		require.Len(t, found.Actresses, 1)
		require.Len(t, found.Genres, 1)

		movie.CreatedAt = found.CreatedAt
		movie.Actresses = nil
		movie.Genres = []models.Genre{{Name: "Action"}}

		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations)
		})
		require.NoError(t, err)

		found, err = repo.FindByID("IPX-CLEAR-001")
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
		require.NoError(t, repo.Create(movie))

		found, err := repo.FindByID("IPX-CLEAR-002")
		require.NoError(t, err)
		require.Len(t, found.Genres, 2)

		movie.CreatedAt = found.CreatedAt
		movie.Genres = nil
		movie.Actresses = []models.Actress{{DMMID: 99902, JapaneseName: "KeepActress"}}

		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations)
		})
		require.NoError(t, err)

		found, err = repo.FindByID("IPX-CLEAR-002")
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
		require.NoError(t, repo.Create(movie))

		found, err := repo.FindByID("IPX-CLEAR-003")
		require.NoError(t, err)
		require.Len(t, found.Genres, 1)
		require.Len(t, found.Actresses, 1)

		movie.CreatedAt = found.CreatedAt
		movie.Genres = nil
		movie.Actresses = nil

		err = db.Transaction(func(tx *gorm.DB) error {
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations)
		})
		require.NoError(t, err)

		found, err = repo.FindByID("IPX-CLEAR-003")
		require.NoError(t, err)
		assert.Empty(t, found.Genres, "genres should be cleared")
		assert.Empty(t, found.Actresses, "actresses should be cleared")
	})

	t.Run("updates existing movie with associations", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-CORE-002")
		movie.Genres = []models.Genre{{Name: "Comedy"}}
		require.NoError(t, repo.Create(movie))

		existing, err := repo.FindByID("IPX-CORE-002")
		require.NoError(t, err)

		movie.CreatedAt = existing.CreatedAt
		movie.Title = "Updated via Core"
		movie.Genres = []models.Genre{{Name: "Thriller"}}
		movie.Actresses = []models.Actress{{DMMID: 88802, JapaneseName: "New Actress"}}
		movie.Translations = []models.MovieTranslation{
			{Language: "zh", Title: "Chinese Title"},
		}

		err = db.Transaction(func(tx *gorm.DB) error {
			if err := repo.ensureGenresExistTx(tx, movie.Genres); err != nil {
				return err
			}
			if err := repo.ensureActressesExistTx(tx, movie.Actresses); err != nil {
				return err
			}
			translations := movie.Translations
			movie.Translations = nil
			return upsertMovieCore(tx, db, movie, translations)
		})
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-CORE-002")
		require.NoError(t, err)
		assert.Equal(t, "Updated via Core", found.Title)
		assert.Len(t, found.Genres, 1)
		assert.Equal(t, "Thriller", found.Genres[0].Name)
		assert.Len(t, found.Actresses, 1)
		assert.Len(t, found.Translations, 1)
	})
}
