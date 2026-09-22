package tag_test

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/tag"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func executeTagCommand(t *testing.T, configPath string, args ...string) string {
	t.Helper()
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", configPath, "config file")
	root.AddCommand(tag.NewCommand())
	root.SetArgs(append([]string{"tag"}, args...))
	stdout, _ := testutil.CaptureOutput(t, func() { require.NoError(t, root.Execute()) })
	return stdout
}

func TestTagCommandDisplayIDCanonicalLifecycle(t *testing.T) {
	configPath, dbPath := setupTagTestDB(t)
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "error"})
	require.NoError(t, err)
	movie := models.Movie{ContentID: "opaque-cli-content", ID: "IPX-535"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Close())

	executeTagCommand(t, configPath, "add", movie.ID, "Favorite", "Collection")
	output := executeTagCommand(t, configPath, "list", movie.ID)
	require.Contains(t, output, "Favorite")
	require.Contains(t, output, "Collection")

	db, err = database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "error"})
	require.NoError(t, err)
	var keys []string
	require.NoError(t, db.Model(&models.MovieTag{}).Order("tag").Pluck("movie_id", &keys).Error)
	require.Equal(t, []string{movie.ContentID, movie.ContentID}, keys)
	var persisted models.Movie
	require.NoError(t, db.First(&persisted, "content_id = ?", movie.ContentID).Error)
	require.True(t, persisted.RenderDirty)
	require.EqualValues(t, 2, persisted.RenderGeneration)
	require.NoError(t, db.Close())

	executeTagCommand(t, configPath, "remove", movie.ID, "Favorite")
	executeTagCommand(t, configPath, "remove", movie.ID)
	db, err = database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "error"})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := database.NewMovieTagRepository(db)
	tags, err := repo.GetTagsForMovie(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Empty(t, tags)
	require.NoError(t, db.First(&persisted, "content_id = ?", movie.ContentID).Error)
	require.EqualValues(t, 4, persisted.RenderGeneration)
}
