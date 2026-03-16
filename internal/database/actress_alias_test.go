package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressAliasRepository(t *testing.T) {
	// Create in-memory database
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  "file::memory:?cache=shared",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Run migrations
	err = db.AutoMigrate()
	require.NoError(t, err)

	repo := NewActressAliasRepository(db)

	t.Run("Create and Find", func(t *testing.T) {
		alias := &models.ActressAlias{
			AliasName:     "Yui Hatano",
			CanonicalName: "Hatano Yui",
		}

		err := repo.Create(alias)
		require.NoError(t, err)
		assert.NotZero(t, alias.ID)

		// Find by alias name
		found, err := repo.FindByAliasName("Yui Hatano")
		require.NoError(t, err)
		assert.Equal(t, "Hatano Yui", found.CanonicalName)
	})

	t.Run("Upsert", func(t *testing.T) {
		alias := &models.ActressAlias{
			AliasName:     "Tsubasa Amami",
			CanonicalName: "Amami Tsubasa",
		}

		// First upsert (create)
		err := repo.Upsert(alias)
		require.NoError(t, err)
		originalID := alias.ID

		// Second upsert (update)
		alias.CanonicalName = "Tsubasa Amami (Updated)"
		err = repo.Upsert(alias)
		require.NoError(t, err)
		assert.Equal(t, originalID, alias.ID, "ID should remain the same")

		// Verify update
		found, err := repo.FindByAliasName("Tsubasa Amami")
		require.NoError(t, err)
		assert.Equal(t, "Tsubasa Amami (Updated)", found.CanonicalName)
	})

	t.Run("FindByCanonicalName", func(t *testing.T) {
		// Create multiple aliases for the same canonical name
		aliases := []*models.ActressAlias{
			{AliasName: "Jun Amamiya", CanonicalName: "Amamiya Jun"},
			{AliasName: "Amamiya Jun", CanonicalName: "Amamiya Jun"},
			{AliasName: "天宮じゅん", CanonicalName: "Amamiya Jun"},
		}

		for _, alias := range aliases {
			err := repo.Create(alias)
			require.NoError(t, err)
		}

		// Find all aliases for this canonical name
		found, err := repo.FindByCanonicalName("Amamiya Jun")
		require.NoError(t, err)
		assert.Len(t, found, 3)
	})

	t.Run("GetAliasMap", func(t *testing.T) {
		aliasMap, err := repo.GetAliasMap()
		require.NoError(t, err)

		// Should contain all aliases created in previous tests
		assert.GreaterOrEqual(t, len(aliasMap), 3)
		assert.Equal(t, "Hatano Yui", aliasMap["Yui Hatano"])
		assert.Equal(t, "Amamiya Jun", aliasMap["Jun Amamiya"])
	})

	t.Run("Delete", func(t *testing.T) {
		alias := &models.ActressAlias{
			AliasName:     "Test Actress",
			CanonicalName: "Actress Test",
		}

		err := repo.Create(alias)
		require.NoError(t, err)

		// Delete
		err = repo.Delete("Test Actress")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByAliasName("Test Actress")
		assert.Error(t, err, "Should not find deleted alias")
	})

	t.Run("List", func(t *testing.T) {
		aliases, err := repo.List()
		require.NoError(t, err)
		assert.Greater(t, len(aliases), 0)
	})
}
