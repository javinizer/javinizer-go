package database

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDuplicateMovieReloadFailureRollsBackRecoveryWrites(t *testing.T) {
	db := newCreditTestDB(t)
	original := models.Movie{ContentID: "duplicate-reload", ID: "DUP-001", Title: "Original title", Genres: []models.Genre{{Name: "Original genre"}}}
	before, err := NewMovieRepository(db).Upsert(t.Context(), &original)
	require.NoError(t, err)

	pool, err := db.DB.DB()
	require.NoError(t, err)
	// Use the real SQLite primary-key violation, translated to the GORM error
	// handled by the duplicate-recovery helper. No insert result is fabricated.
	facade, err := gorm.Open(sqlite.Dialector{Conn: pool}, &gorm.Config{TranslateError: true, Logger: db.Config.Logger})
	require.NoError(t, err)
	repo := NewMovieRepository(&DB{DB: facade})
	reloadErr := errors.New("duplicate recovery reload unavailable")
	faults := 0
	const hook = "test:duplicate-recovery-reload-failure"
	require.NoError(t, facade.Callback().Query().Before("gorm:query").Register(hook, func(tx *gorm.DB) {
		if faults == 0 && tx.Statement.Table == "movies" && len(tx.Statement.Preloads) == 3 {
			faults++
			_ = tx.AddError(reloadErr)
		}
	}))
	t.Cleanup(func() { require.NoError(t, facade.Callback().Query().Remove(hook)) })

	incoming := models.Movie{ContentID: original.ContentID, ID: original.ID, Title: "Uncommitted replacement", Genres: []models.Genre{{Name: "Uncommitted genre"}}}
	var result *models.Movie
	err = facade.WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
		return repo.upserter.insertOrHandleDuplicateTx(tx, &incoming, &result)
	})
	require.ErrorIs(t, err, reloadErr)
	require.ErrorContains(t, err, "reload movie duplicate-reload")
	require.Equal(t, 1, faults, "failure must occur at the final preload after recovery writes")
	require.Nil(t, result, "an unreadable recovery result must not escape the transaction")

	after, err := NewMovieRepository(db).FindByID(t.Context(), original.ID)
	require.NoError(t, err)
	require.Equal(t, before, after, "movie fields and associations must remain unchanged")
	var rejectedGenres int64
	require.NoError(t, db.Model(&models.Genre{}).Where("name = ?", "Uncommitted genre").Count(&rejectedGenres).Error)
	require.Zero(t, rejectedGenres, "genre creation must roll back with the failed reload")

	// The same helper and database remain usable after the one-shot read fault.
	err = facade.WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
		return repo.upserter.insertOrHandleDuplicateTx(tx, &incoming, &result)
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, incoming.Title, result.Title)
	require.Len(t, result.Genres, 1)
	require.Equal(t, "Uncommitted genre", result.Genres[0].Name)
	require.Equal(t, before.CreatedAt, result.CreatedAt)
}
