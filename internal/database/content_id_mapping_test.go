package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentIDMappingRepository(t *testing.T) {
	// Create in-memory database
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

	// Run migrations
	err = db.AutoMigrate()
	require.NoError(t, err)

	repo := NewContentIDMappingRepository(db)

	t.Run("Create and FindBySearchID", func(t *testing.T) {
		mapping := &models.ContentIDMapping{
			SearchID:  "IPX-535",
			ContentID: "ipx00535",
			Source:    "r18dev",
		}

		err := repo.Create(mapping)
		require.NoError(t, err)
		assert.NotZero(t, mapping.ID)

		// Find by search ID (should be case-insensitive)
		found, err := repo.FindBySearchID("ipx-535")
		require.NoError(t, err)
		assert.Equal(t, "IPX-535", found.SearchID)
		assert.Equal(t, "ipx00535", found.ContentID)
		assert.Equal(t, "r18dev", found.Source)
	})

	t.Run("Create normalizes search ID to uppercase", func(t *testing.T) {
		mapping := &models.ContentIDMapping{
			SearchID:  "abc-123",
			ContentID: "abc00123",
			Source:    "dmm",
		}

		err := repo.Create(mapping)
		require.NoError(t, err)

		// Search ID should be normalized to uppercase
		found, err := repo.FindBySearchID("ABC-123")
		require.NoError(t, err)
		assert.Equal(t, "ABC-123", found.SearchID)
	})

	t.Run("Create upserts on duplicate search ID", func(t *testing.T) {
		mapping := &models.ContentIDMapping{
			SearchID:  "MDB-087",
			ContentID: "61mdb087",
			Source:    "dmm",
		}

		// First create
		err := repo.Create(mapping)
		require.NoError(t, err)
		originalID := mapping.ID

		// Second create with same search ID but different content ID (should update)
		mapping2 := &models.ContentIDMapping{
			SearchID:  "MDB-087",
			ContentID: "updated_mdb087",
			Source:    "dmm_updated",
		}
		err = repo.Create(mapping2)
		require.NoError(t, err)

		// Verify update
		found, err := repo.FindBySearchID("MDB-087")
		require.NoError(t, err)
		assert.Equal(t, "updated_mdb087", found.ContentID)
		assert.Equal(t, "dmm_updated", found.Source)
		assert.Equal(t, originalID, found.ID, "ID should not change on upsert")
	})

	t.Run("FindBySearchID with non-existent ID", func(t *testing.T) {
		_, err := repo.FindBySearchID("NONEXISTENT-999")
		assert.Error(t, err, "Should return error for non-existent mapping")
	})

	t.Run("Delete", func(t *testing.T) {
		mapping := &models.ContentIDMapping{
			SearchID:  "DELETE-TEST",
			ContentID: "delete123",
			Source:    "test",
		}

		err := repo.Create(mapping)
		require.NoError(t, err)

		// Delete
		err = repo.Delete("delete-test")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindBySearchID("DELETE-TEST")
		assert.Error(t, err, "Should not find deleted mapping")
	})

	t.Run("Delete non-existent mapping", func(t *testing.T) {
		// Should not error on deleting non-existent mapping
		err := repo.Delete("NONEXISTENT-999")
		assert.NoError(t, err)
	})

	t.Run("GetAll", func(t *testing.T) {
		// Create multiple mappings
		mappings := []*models.ContentIDMapping{
			{SearchID: "TEST-001", ContentID: "test001", Source: "dmm"},
			{SearchID: "TEST-002", ContentID: "test002", Source: "r18dev"},
			{SearchID: "TEST-003", ContentID: "test003", Source: "dmm"},
		}

		for _, m := range mappings {
			err := repo.Create(m)
			require.NoError(t, err)
		}

		// Get all mappings
		all, err := repo.GetAll()
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(all), 3, "Should have at least 3 mappings")

		// Verify specific mappings exist
		var found001, found002, found003 bool
		for _, m := range all {
			switch m.SearchID {
			case "TEST-001":
				found001 = true
				assert.Equal(t, "test001", m.ContentID)
			case "TEST-002":
				found002 = true
				assert.Equal(t, "test002", m.ContentID)
			case "TEST-003":
				found003 = true
				assert.Equal(t, "test003", m.ContentID)
			}
		}
		assert.True(t, found001, "TEST-001 should exist")
		assert.True(t, found002, "TEST-002 should exist")
		assert.True(t, found003, "TEST-003 should exist")
	})

	t.Run("GetAll with empty database", func(t *testing.T) {
		// Create fresh database
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

		err = db2.AutoMigrate()
		require.NoError(t, err)

		repo2 := NewContentIDMappingRepository(db2)

		all, err := repo2.GetAll()
		require.NoError(t, err)
		assert.Len(t, all, 0)
	})
}
