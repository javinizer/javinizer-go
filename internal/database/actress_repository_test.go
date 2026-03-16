package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressRepository(t *testing.T) {
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
	repo := NewActressRepository(db)

	t.Run("Create actress", func(t *testing.T) {
		actress := &models.Actress{
			DMMID:        12345,
			FirstName:    "Yui",
			LastName:     "Hatano",
			JapaneseName: "波多野結衣",
			ThumbURL:     "http://example.com/thumb.jpg",
		}

		err := repo.Create(actress)
		require.NoError(t, err)
		assert.NotZero(t, actress.ID)
	})

	t.Run("Update actress", func(t *testing.T) {
		actress := &models.Actress{
			DMMID:        54321,
			FirstName:    "Original",
			LastName:     "Name",
			JapaneseName: "オリジナル",
		}

		err := repo.Create(actress)
		require.NoError(t, err)

		// Update
		actress.FirstName = "Updated"
		actress.LastName = "Updated Name"
		err = repo.Update(actress)
		require.NoError(t, err)

		// Verify update (we'll need to search by DMMID or Japanese name)
		found, err := repo.FindByJapaneseName("オリジナル")
		require.NoError(t, err)
		assert.Equal(t, "Updated", found.FirstName)
		assert.Equal(t, "Updated Name", found.LastName)
	})

	t.Run("FindByJapaneseName", func(t *testing.T) {
		actress := &models.Actress{
			DMMID:        99999,
			JapaneseName: "テスト女優",
			FirstName:    "Test",
			LastName:     "Actress",
		}

		err := repo.Create(actress)
		require.NoError(t, err)

		found, err := repo.FindByJapaneseName("テスト女優")
		require.NoError(t, err)
		assert.Equal(t, "テスト女優", found.JapaneseName)
		assert.Equal(t, "Test", found.FirstName)
	})

	t.Run("FindByJapaneseName not found", func(t *testing.T) {
		_, err := repo.FindByJapaneseName("存在しない女優")
		assert.Error(t, err)
	})

	t.Run("FindOrCreate - finds existing", func(t *testing.T) {
		existing := &models.Actress{
			DMMID:        88888,
			JapaneseName: "既存女優",
			FirstName:    "Existing",
			LastName:     "Actress",
		}

		err := repo.Create(existing)
		require.NoError(t, err)

		// Try to FindOrCreate with same Japanese name
		actress := &models.Actress{
			JapaneseName: "既存女優",
			FirstName:    "Different",
			LastName:     "Name",
		}

		err = repo.FindOrCreate(actress)
		require.NoError(t, err)

		// Should have found existing actress
		assert.Equal(t, existing.ID, actress.ID)
		assert.Equal(t, "Existing", actress.FirstName) // Should keep original data
	})

	t.Run("FindOrCreate - creates new", func(t *testing.T) {
		actress := &models.Actress{
			DMMID:        77777,
			JapaneseName: "新しい女優",
			FirstName:    "New",
			LastName:     "Actress",
		}

		err := repo.FindOrCreate(actress)
		require.NoError(t, err)
		assert.NotZero(t, actress.ID)
	})

	t.Run("List with pagination", func(t *testing.T) {
		// Create multiple actresses
		for i := 1; i <= 5; i++ {
			actress := &models.Actress{
				DMMID:        10000 + i,
				JapaneseName: "女優" + string(rune('A'+i)),
				FirstName:    "First" + string(rune('A'+i)),
				LastName:     "Last" + string(rune('A'+i)),
			}
			err := repo.Create(actress)
			require.NoError(t, err)
		}

		// Get first 3
		actresses, err := repo.List(3, 0)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(actresses), 3)

		// Get next batch
		actresses, err = repo.List(3, 3)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(actresses), 0)
	})

	t.Run("Search by name", func(t *testing.T) {
		// Create test actresses
		actresses := []*models.Actress{
			{DMMID: 20001, JapaneseName: "山田花子", FirstName: "Hanako", LastName: "Yamada"},
			{DMMID: 20002, JapaneseName: "佐藤美咲", FirstName: "Misaki", LastName: "Sato"},
			{DMMID: 20003, JapaneseName: "田中優子", FirstName: "Yuko", LastName: "Tanaka"},
		}

		for _, a := range actresses {
			err := repo.Create(a)
			require.NoError(t, err)
		}

		// Search by first name
		results, err := repo.Search("Hanako")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 1)

		found := false
		for _, r := range results {
			if r.FirstName == "Hanako" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find actress by first name")

		// Search by last name
		results, err = repo.Search("Sato")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 1)

		found = false
		for _, r := range results {
			if r.LastName == "Sato" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find actress by last name")

		// Search by Japanese name
		results, err = repo.Search("田中")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(results), 1)

		found = false
		for _, r := range results {
			if r.JapaneseName == "田中優子" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should find actress by Japanese name")
	})

	t.Run("Search with empty query", func(t *testing.T) {
		results, err := repo.Search("")
		require.NoError(t, err)
		// Should return all actresses (limited to 100)
		assert.Greater(t, len(results), 0)
	})

	t.Run("Search no results", func(t *testing.T) {
		results, err := repo.Search("NonExistentActress12345")
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})
}

func TestActress_FullName(t *testing.T) {
	t.Run("Full name with first and last", func(t *testing.T) {
		actress := &models.Actress{
			FirstName: "Yui",
			LastName:  "Hatano",
		}
		assert.Equal(t, "Hatano Yui", actress.FullName())
	})

	t.Run("Full name with only first name", func(t *testing.T) {
		actress := &models.Actress{
			FirstName: "Yui",
		}
		assert.Equal(t, "Yui", actress.FullName())
	})

	t.Run("Full name falls back to Japanese name", func(t *testing.T) {
		actress := &models.Actress{
			JapaneseName: "波多野結衣",
		}
		assert.Equal(t, "波多野結衣", actress.FullName())
	})

	t.Run("Full name with Japanese and English names", func(t *testing.T) {
		actress := &models.Actress{
			FirstName:    "Yui",
			LastName:     "Hatano",
			JapaneseName: "波多野結衣",
		}
		// Should prefer English name
		assert.Equal(t, "Hatano Yui", actress.FullName())
	})
}
