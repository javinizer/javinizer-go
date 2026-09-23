package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestMigration016RollbackPreservesCurrentAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	movie := models.Movie{ContentID: "rollback-current", ID: "rollback-current"}
	actress := models.Actress{FirstName: "Current"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)

	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 15)
	require.NoError(t, err)
	var count int
	require.NoError(t, sqlDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM movie_actresses WHERE movie_content_id = ? AND actress_id = ?", movie.ContentID, actress.ID).Scan(&count))
	require.Equal(t, 1, count)
}
