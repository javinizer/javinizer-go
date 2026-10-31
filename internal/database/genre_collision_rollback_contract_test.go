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

// This is caller-configured repository composition, not the application's
// default error-translation setting or a simulated concurrent insertion.
// Both facades use the real, normally initialized and migrated SQLite pool.
func TestMovieGenreCollisionRollsBackWithTranslatedErrors(t *testing.T) {
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
	// The original DB owns the shared pool; closing the facade separately would
	// close the same pool twice. Its lifetime is bounded by this test's cleanup.
	translated := database.NewMovieRepository(&database.DB{DB: facade})
	ordinary := database.NewMovieRepository(db)
	existing, err := db.Repositories().GenreRepo.FindOrCreate(t.Context(), "Existing owner")
	require.NoError(t, err)
	_, err = ordinary.UpsertWithTranslations(t.Context(), &models.Movie{ContentID: "retained", ID: "RETAINED-001", Title: "Retained", Genres: []models.Genre{*existing}}, nil, nil)
	require.NoError(t, err)
	snapshot := func() map[string][]map[string]interface{} {
		t.Helper()
		out := make(map[string][]map[string]interface{})
		for _, table := range []string{"movies", "genres", "movie_genres", "actresses", "movie_actresses", "movie_credits", "movie_translations", "genre_translations", "actress_translations"} {
			var rows []map[string]interface{}
			require.NoError(t, db.WithContext(t.Context()).Table(table).Order("rowid").Find(&rows).Error)
			out[table] = rows
		}
		return out
	}
	before := snapshot()
	proposed := &models.Movie{ContentID: "collision", ID: "COLLISION-001", Title: "Proposed", Genres: []models.Genre{
		{Name: "Created before collision"},
		{ID: existing.ID, Name: "Different identity"},
	}}
	result, err := translated.UpsertWithTranslations(t.Context(), proposed, nil, nil)
	require.Nil(t, result)
	require.ErrorIs(t, err, gorm.ErrDuplicatedKey)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.EqualError(t, err, "ensure genres for movie collision: create duplicate key, then reload also failed: create=duplicated key not allowed, reload=record not found")
	// Input hydration is allowed. A generated ID proves the earlier genre insert
	// succeeded before the collision, while the durable snapshot proves rollback.
	require.NotZero(t, proposed.Genres[0].ID)
	require.NotEqual(t, existing.ID, proposed.Genres[0].ID)
	require.Equal(t, before, snapshot())
	require.Same(t, originalConfig, db.Config)
	require.False(t, db.Config.TranslateError)

	// Retry a fresh corrected request, without carrying rollback-generated IDs.
	retry := &models.Movie{ContentID: "collision", ID: "COLLISION-001", Title: "Proposed", Genres: []models.Genre{
		{Name: "Created before collision"}, {Name: "Different identity"},
	}}
	recovered, err := translated.UpsertWithTranslations(t.Context(), retry, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	require.Equal(t, "collision", recovered.ContentID)
	require.Len(t, recovered.Genres, 2)
	for _, genre := range recovered.Genres {
		require.NotEqual(t, existing.ID, genre.ID)
	}
	loaded, err := ordinary.FindByContentID(t.Context(), "collision")
	require.NoError(t, err)
	require.ElementsMatch(t, recovered.Genres, loaded.Genres)
	retained, err := ordinary.FindByContentID(t.Context(), "retained")
	require.NoError(t, err)
	require.Equal(t, []models.Genre{*existing}, retained.Genres)
	require.Same(t, originalConfig, db.Config)
	require.False(t, db.Config.TranslateError)
	require.NoError(t, pool.PingContext(t.Context()))
}
