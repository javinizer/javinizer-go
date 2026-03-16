package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenreRepository(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewGenreRepository(db)

	t.Run("FindOrCreate - creates new genre", func(t *testing.T) {
		genre, err := repo.FindOrCreate("Action")
		require.NoError(t, err)
		assert.Equal(t, "Action", genre.Name)
		assert.NotZero(t, genre.ID)
	})

	t.Run("FindOrCreate - finds existing genre", func(t *testing.T) {
		// Create first
		genre1, err := repo.FindOrCreate("Drama")
		require.NoError(t, err)

		// Try to create again
		genre2, err := repo.FindOrCreate("Drama")
		require.NoError(t, err)

		// Should be the same genre
		assert.Equal(t, genre1.ID, genre2.ID)
		assert.Equal(t, "Drama", genre2.Name)
	})

	t.Run("List all genres", func(t *testing.T) {
		// Create multiple genres
		genreNames := []string{"Comedy", "Romance", "Thriller", "Horror"}
		for _, name := range genreNames {
			_, err := repo.FindOrCreate(name)
			require.NoError(t, err)
		}

		// List all
		genres, err := repo.List()
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
		cfg := &config.Config{
			Database: config.DatabaseConfig{
				Type: "sqlite",
				DSN:  ":memory:",
			},
			Logging: config.LoggingConfig{
				Level: "error",
			},
		}

		db2, err := New(cfg)
		require.NoError(t, err)
		defer func() { _ = db2.Close() }()

		require.NoError(t, db2.AutoMigrate())
		repo2 := NewGenreRepository(db2)

		genres, err := repo2.List()
		require.NoError(t, err)
		assert.Len(t, genres, 0)
	})
}

func TestGenreReplacementRepository(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewGenreReplacementRepository(db)

	t.Run("Create genre replacement", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "Big Tits",
			Replacement: "Large Breasts",
		}

		err := repo.Create(replacement)
		require.NoError(t, err)
		assert.NotZero(t, replacement.ID)
	})

	t.Run("FindByOriginal", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "Mature Woman",
			Replacement: "MILF",
		}

		err := repo.Create(replacement)
		require.NoError(t, err)

		found, err := repo.FindByOriginal("Mature Woman")
		require.NoError(t, err)
		assert.Equal(t, "Mature Woman", found.Original)
		assert.Equal(t, "MILF", found.Replacement)
	})

	t.Run("FindByOriginal not found", func(t *testing.T) {
		_, err := repo.FindByOriginal("NonExistentGenre")
		assert.Error(t, err)
	})

	t.Run("Upsert - creates new", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "Creampie",
			Replacement: "Internal Finish",
		}

		err := repo.Upsert(replacement)
		require.NoError(t, err)
		assert.NotZero(t, replacement.ID)
	})

	t.Run("Upsert - updates existing", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "Beautiful Girl",
			Replacement: "Pretty",
		}

		err := repo.Create(replacement)
		require.NoError(t, err)
		originalID := replacement.ID

		// Update via upsert
		replacement2 := &models.GenreReplacement{
			Original:    "Beautiful Girl",
			Replacement: "Attractive",
		}

		err = repo.Upsert(replacement2)
		require.NoError(t, err)
		assert.Equal(t, originalID, replacement2.ID)

		// Verify update
		found, err := repo.FindByOriginal("Beautiful Girl")
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
			err := repo.Create(r)
			require.NoError(t, err)
		}

		// List all
		list, err := repo.List()
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(list), 3)
	})

	t.Run("Delete genre replacement", func(t *testing.T) {
		replacement := &models.GenreReplacement{
			Original:    "ToDelete",
			Replacement: "WillBeDeleted",
		}

		err := repo.Create(replacement)
		require.NoError(t, err)

		// Delete
		err = repo.Delete("ToDelete")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByOriginal("ToDelete")
		assert.Error(t, err)
	})

	t.Run("Delete non-existent replacement", func(t *testing.T) {
		err := repo.Delete("DoesNotExist")
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
			err := repo.Upsert(r)
			require.NoError(t, err)
		}

		// Get replacement map
		replMap, err := repo.GetReplacementMap()
		require.NoError(t, err)

		// Verify mappings
		assert.Equal(t, "MapValue1", replMap["Map1"])
		assert.Equal(t, "MapValue2", replMap["Map2"])
		assert.Equal(t, "MapValue3", replMap["Map3"])
	})

	t.Run("GetReplacementMap with empty database", func(t *testing.T) {
		cfg := &config.Config{
			Database: config.DatabaseConfig{
				Type: "sqlite",
				DSN:  ":memory:",
			},
			Logging: config.LoggingConfig{
				Level: "error",
			},
		}

		db2, err := New(cfg)
		require.NoError(t, err)
		defer func() { _ = db2.Close() }()

		require.NoError(t, db2.AutoMigrate())
		repo2 := NewGenreReplacementRepository(db2)

		replMap, err := repo2.GetReplacementMap()
		require.NoError(t, err)
		assert.Len(t, replMap, 0)
	})
}
