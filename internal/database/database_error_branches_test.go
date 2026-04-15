package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovieRepositoryUpsert_MoviesTableMissingReturnsError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	require.NoError(t, db.DB.Exec("DROP TABLE movies").Error)
	_, err := repo.Upsert(createTestMovie("IPX-ERR-MOVIES"))
	require.Error(t, err)
}

func TestMovieRepositoryUpsert_CreatePathAssociationErrors(t *testing.T) {
	t.Run("missing movie_genres table", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		require.NoError(t, db.DB.Exec("DROP TABLE movie_genres").Error)
		movie := createTestMovie("IPX-ERR-GENRE")
		movie.Genres = []models.Genre{{Name: "Drama"}}

		_, err := repo.Upsert(movie)
		require.Error(t, err)
	})

	t.Run("missing movie_actresses table", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		require.NoError(t, db.DB.Exec("DROP TABLE movie_actresses").Error)
		movie := createTestMovie("IPX-ERR-ACTRESS")
		movie.Actresses = []models.Actress{{DMMID: 91001, JapaneseName: "Branch Actress"}}

		_, err := repo.Upsert(movie)
		require.Error(t, err)
	})
}

func TestMovieRepositoryUpsert_UpdatePathTranslationError(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := createTestMovie("IPX-ERR-TRANS-UPD")
	_, err := repo.Upsert(movie)
	require.NoError(t, err)

	require.NoError(t, db.DB.Exec("DROP TABLE movie_translations").Error)

	movie.Title = "Updated with translation"
	movie.Translations = []models.MovieTranslation{
		{Language: "en", Title: "English"},
	}
	_, err = repo.Upsert(movie)
	require.Error(t, err)
}

func TestMovieRepositoryDelete_ErrorBranches(t *testing.T) {
	t.Run("missing movie_actresses table", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-DEL-ERR-1")
		_, err := repo.Upsert(movie)
		require.NoError(t, err)
		require.NoError(t, db.DB.Exec("DROP TABLE movie_actresses").Error)

		err = repo.Delete(movie.ID)
		require.Error(t, err)
	})

	t.Run("missing movie_translations table", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-DEL-ERR-2")
		_, err := repo.Upsert(movie)
		require.NoError(t, err)
		require.NoError(t, db.DB.Exec("DROP TABLE movie_translations").Error)

		err = repo.Delete(movie.ID)
		require.Error(t, err)
	})

	t.Run("missing movie_tags table", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		movie := createTestMovie("IPX-DEL-ERR-3")
		_, err := repo.Upsert(movie)
		require.NoError(t, err)
		require.NoError(t, db.DB.Exec("DROP TABLE movie_tags").Error)

		err = repo.Delete(movie.ID)
		require.Error(t, err)
	})

	t.Run("returns nil for empty content_id row", func(t *testing.T) {
		db := newDatabaseTestDB(t)
		repo := NewMovieRepository(db)

		require.NoError(t, db.DB.Exec(
			"INSERT INTO movies (content_id, id, created_at, updated_at) VALUES ('', ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			"IPX-DEL-EMPTY-CID",
		).Error)

		err := repo.Delete("IPX-DEL-EMPTY-CID")
		require.NoError(t, err)

		var count int64
		require.NoError(t, db.DB.Table("movies").Where("id = ?", "IPX-DEL-EMPTY-CID").Count(&count).Error)
		assert.Equal(t, int64(1), count)
	})
}
