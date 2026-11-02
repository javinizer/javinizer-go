package database_test

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// This exercises caller-configured repository composition, not the application's
// default TranslateError setting or a concurrent insertion. Repeated new names
// in one request cause a real SQLite uniqueness conflict followed by reload.
func TestMovieRepeatedNewGenreResolvesOneDurableIdentity(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "catalog.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	originalConfig := db.Config
	require.False(t, originalConfig.TranslateError)
	pool, err := db.DB.DB()
	require.NoError(t, err)
	facade, err := gorm.Open(sqlite.Dialector{Conn: pool}, &gorm.Config{TranslateError: true, Logger: originalConfig.Logger, NowFunc: originalConfig.NowFunc})
	require.NoError(t, err)
	require.NotSame(t, originalConfig, facade.Config)
	require.True(t, facade.Config.TranslateError)
	// The normal DB alone owns and closes the shared pool.
	repo := database.NewMovieRepository(&database.DB{DB: facade})
	ordinary := database.NewMovieRepository(db)
	existing, err := db.Repositories().GenreRepo.FindOrCreate(t.Context(), "Unrelated existing genre")
	require.NoError(t, err)
	names := []string{"New repeated genre", "New repeated genre", "New distinct genre"}
	request := func() *models.Movie {
		movie := &models.Movie{ContentID: "same-request-genres", ID: "GENRE-001", Title: "Repeated genres"}
		for _, name := range names {
			movie.Genres = append(movie.Genres, models.Genre{Name: name})
		}
		return movie
	}
	movie := request()
	saved, err := repo.UpsertWithTranslations(t.Context(), movie, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, saved)
	require.Len(t, movie.Genres, 3)
	for i, name := range names {
		require.Equal(t, name, movie.Genres[i].Name)
		require.NotZero(t, movie.Genres[i].ID)
	}
	require.NotZero(t, movie.Genres[0].ID)
	require.Equal(t, movie.Genres[0], movie.Genres[1])
	require.NotEqual(t, movie.Genres[0].ID, movie.Genres[2].ID)
	require.NotEqual(t, existing.ID, movie.Genres[0].ID)
	expected := []models.Genre{movie.Genres[0], movie.Genres[2]}
	require.ElementsMatch(t, expected, saved.Genres)
	assertDurable := func() {
		t.Helper()
		loaded, err := ordinary.FindByContentID(t.Context(), movie.ContentID)
		require.NoError(t, err)
		require.ElementsMatch(t, expected, loaded.Genres)
		all, err := db.Repositories().GenreRepo.List(t.Context())
		require.NoError(t, err)
		require.ElementsMatch(t, append([]models.Genre{*existing}, expected...), all)
		var joins []struct {
			MovieContentID string
			GenreID        uint
		}
		require.NoError(t, db.WithContext(t.Context()).Table("movie_genres").Order("genre_id").Find(&joins).Error)
		require.Len(t, joins, 2)
		ids := []uint{}
		for _, join := range joins {
			require.Equal(t, movie.ContentID, join.MovieContentID)
			ids = append(ids, join.GenreID)
		}
		require.ElementsMatch(t, []uint{expected[0].ID, expected[1].ID}, ids)
		require.Same(t, originalConfig, db.Config)
		require.False(t, db.Config.TranslateError)
	}
	assertDurable()
	// A fresh replay resolves the same identities without extra rows or joins.
	replay := request()
	again, err := repo.UpsertWithTranslations(t.Context(), replay, nil, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, expected, again.Genres)
	require.Equal(t, movie.Genres, replay.Genres)
	assertDurable()
	require.NoError(t, pool.PingContext(t.Context()))
}
