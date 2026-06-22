package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenreRepository(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := newGenreRepository(db)

	t.Run("FindOrCreate - creates new genre", func(t *testing.T) {
		genre, err := repo.FindOrCreate(context.TODO(), "Action")
		require.NoError(t, err)
		assert.Equal(t, "Action", genre.Name)
		assert.NotZero(t, genre.ID)
	})

	t.Run("FindOrCreate - finds existing genre", func(t *testing.T) {
		// Create first
		genre1, err := repo.FindOrCreate(context.TODO(), "Drama")
		require.NoError(t, err)

		// Try to create again
		genre2, err := repo.FindOrCreate(context.TODO(), "Drama")
		require.NoError(t, err)

		// Should be the same genre
		assert.Equal(t, genre1.ID, genre2.ID)
		assert.Equal(t, "Drama", genre2.Name)
	})

	t.Run("List all genres", func(t *testing.T) {
		// Create multiple genres
		genreNames := []string{"Comedy", "Romance", "Thriller", "Horror"}
		for _, name := range genreNames {
			_, err := repo.FindOrCreate(context.TODO(), name)
			require.NoError(t, err)
		}

		// List all
		genres, err := repo.List(context.TODO())
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(genres), len(genreNames))

		// Verify all genres are present
		genreMap := make(map[string]bool)
		for _, g := range genres {
			genreMap[g.Name] = true
		}

		for _, name := range genreNames {
			assert.True(t, genreMap[name], "Genre %s should exist", name)
		}
	})

	t.Run("List with empty database", func(t *testing.T) {
		cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

		db2, err := New(cfg)
		require.NoError(t, err)
		defer func() { _ = db2.Close() }()

		require.NoError(t, db2.RunMigrationsOnStartup(context.Background()))
		repo2 := newGenreRepository(db2)

		genres, err := repo2.List(context.TODO())
		require.NoError(t, err)
		assert.Len(t, genres, 0)
	})
}

func TestGenreReplacementRepository(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := NewGenreReplacementRepository(db)

	t.Run("Create genre replacement", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "Big Tits",
			Replacement: "Large Breasts",
		}

		err := repo.Create(context.TODO(), replacement)
		require.NoError(t, err)
		assert.NotZero(t, replacement.ID)
	})

	t.Run("FindByOriginal", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "Mature Woman",
			Replacement: "MILF",
		}

		err := repo.Create(context.TODO(), replacement)
		require.NoError(t, err)

		found, err := repo.FindByOriginal(context.TODO(), "Mature Woman")
		require.NoError(t, err)
		assert.Equal(t, "Mature Woman", found.Original)
		assert.Equal(t, "MILF", found.Replacement)
	})

	t.Run("FindByOriginal not found", func(t *testing.T) {
		_, err := repo.FindByOriginal(context.TODO(), "NonExistentGenre")
		assert.Error(t, err)
	})

	t.Run("Upsert - creates new", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "Creampie",
			Replacement: "Internal Finish",
		}

		err := repo.Upsert(context.TODO(), replacement)
		require.NoError(t, err)
		assert.NotZero(t, replacement.ID)
	})

	t.Run("Upsert - updates existing", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "Beautiful Girl",
			Replacement: "Pretty",
		}

		err := repo.Create(context.TODO(), replacement)
		require.NoError(t, err)
		originalID := replacement.ID

		// Update via upsert
		replacement2 := &models.GenreReplacement{
			Original:    "Beautiful Girl",
			Replacement: "Attractive",
		}

		err = repo.Upsert(context.TODO(), replacement2)
		require.NoError(t, err)
		assert.Equal(t, originalID, replacement2.ID)

		// Verify update
		found, err := repo.FindByOriginal(context.TODO(), "Beautiful Girl")
		require.NoError(t, err)
		assert.Equal(t, "Attractive", found.Replacement)
	})

	t.Run("List all replacements", func(t *testing.T) {
		// Create multiple replacements
		replacements := []*models.GenreReplacement{
			{Original: "Test1", Replacement: "Replaced1"},
			{Original: "Test2", Replacement: "Replaced2"},
			{Original: "Test3", Replacement: "Replaced3"},
		}

		for _, r := range replacements {
			err := repo.Create(context.TODO(), r)
			require.NoError(t, err)
		}

		// List all
		list, err := repo.List(context.TODO())
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(list), 3)
	})

	t.Run("Delete genre replacement", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "ToDelete",
			Replacement: "WillBeDeleted",
		}

		err := repo.Create(context.TODO(), replacement)
		require.NoError(t, err)

		// Delete
		err = repo.Delete(context.TODO(), "ToDelete")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByOriginal(context.TODO(), "ToDelete")
		assert.Error(t, err)
	})

	t.Run("Delete non-existent replacement", func(t *testing.T) {
		err := repo.Delete(context.TODO(), "DoesNotExist")
		assert.NoError(t, err, "Deleting non-existent replacement should not error")
	})

	t.Run("GetReplacementMap", func(t *testing.T) {
		// Create test replacements
		replacements := []*models.GenreReplacement{
			{Original: "Map1", Replacement: "MapValue1"},
			{Original: "Map2", Replacement: "MapValue2"},
			{Original: "Map3", Replacement: "MapValue3"},
		}

		for _, r := range replacements {
			err := repo.Upsert(context.TODO(), r)
			require.NoError(t, err)
		}

		// Get replacement map
		replMap, err := repo.GetReplacementMap(context.TODO())
		require.NoError(t, err)

		// Verify mappings
		assert.Equal(t, "MapValue1", replMap["Map1"])
		assert.Equal(t, "MapValue2", replMap["Map2"])
		assert.Equal(t, "MapValue3", replMap["Map3"])
	})

	t.Run("GetReplacementMap with empty database", func(t *testing.T) {
		cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

		db2, err := New(cfg)
		require.NoError(t, err)
		defer func() { _ = db2.Close() }()

		require.NoError(t, db2.RunMigrationsOnStartup(context.Background()))
		repo2 := NewGenreReplacementRepository(db2)

		replMap, err := repo2.GetReplacementMap(context.TODO())
		require.NoError(t, err)
		assert.Len(t, replMap, 0)
	})

	t.Run("Create and List and Update", func(t *testing.T) {
		// Create replacement
		replacement := &models.GenreReplacement{
			Original:    "OriginalGenre",
			Replacement: "ReplacedGenre",
		}
		err := repo.Create(context.TODO(), replacement)
		require.NoError(t, err)

		// List and verify we can update
		replacements, err := repo.List(context.TODO())
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(replacements), 1)

		// Update the first replacement using its ID
		for i := range replacements {
			if replacements[i].Original == "OriginalGenre" {
				replacements[i].Replacement = "Updated After List"
				err = repo.Upsert(context.TODO(), &replacements[i])
				require.NoError(t, err)
				break
			}
		}

		// Verify update persisted
		found, err := repo.FindByOriginal(context.TODO(), "OriginalGenre")
		require.NoError(t, err)
		assert.Equal(t, "Updated After List", found.Replacement)
	})

	t.Run("GetReplacementMap preserves all entries", func(t *testing.T) {
		// Create a fresh database to avoid test pollution
		cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}

		db2, err := New(cfg)
		require.NoError(t, err)
		defer func() { _ = db2.Close() }()

		require.NoError(t, db2.RunMigrationsOnStartup(context.Background()))
		repo2 := NewGenreReplacementRepository(db2)

		// Create multiple replacements
		replacements := []*models.GenreReplacement{
			{Original: "MapTest1", Replacement: "Replaced1"},
			{Original: "MapTest2", Replacement: "Replaced2"},
		}

		for _, r := range replacements {
			err := repo2.Create(context.TODO(), r)
			require.NoError(t, err)
		}

		// Get map
		replMap, err := repo2.GetReplacementMap(context.TODO())
		require.NoError(t, err)
		assert.Equal(t, 2, len(replMap))
		assert.Equal(t, "Replaced1", replMap["MapTest1"])
		assert.Equal(t, "Replaced2", replMap["MapTest2"])
	})
}
