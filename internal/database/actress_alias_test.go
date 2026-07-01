package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActressAliasRepository(t *testing.T) {
	// Create in-memory database
	cfg := &Config{Type: "sqlite", DSN: "file::memory:?cache=shared"}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Run migrations
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	repo := NewActressAliasRepository(db)

	t.Run("Create and Find", func(t *testing.T) {
		alias := &models.ActressAlias{
			AliasName:     "Yui Hatano",
			CanonicalName: "Hatano Yui",
		}

		err := repo.Create(context.TODO(), alias)
		require.NoError(t, err)
		assert.NotZero(t, alias.ID)

		// Find by alias name
		found, err := repo.FindByAliasName(context.TODO(), "Yui Hatano")
		require.NoError(t, err)
		assert.Equal(t, "Hatano Yui", found.CanonicalName)
	})

	t.Run("Upsert", func(t *testing.T) {
		alias := &models.ActressAlias{
			AliasName:     "Tsubasa Amami",
			CanonicalName: "Amami Tsubasa",
		}

		// First upsert (create)
		err := repo.Upsert(context.TODO(), alias)
		require.NoError(t, err)
		originalID := alias.ID

		// Second upsert (update)
		alias.CanonicalName = "Tsubasa Amami (Updated)"
		err = repo.Upsert(context.TODO(), alias)
		require.NoError(t, err)
		assert.Equal(t, originalID, alias.ID, "ID should remain the same")

		// Verify update
		found, err := repo.FindByAliasName(context.TODO(), "Tsubasa Amami")
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
			err := repo.Create(context.TODO(), alias)
			require.NoError(t, err)
		}

		// Find all aliases for this canonical name
		found, err := repo.FindByCanonicalName(context.TODO(), "Amamiya Jun")
		require.NoError(t, err)
		assert.Len(t, found, 3)
	})

	t.Run("GetAliasMap", func(t *testing.T) {
		aliasMap, err := repo.GetAliasMap(context.TODO())
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

		err := repo.Create(context.TODO(), alias)
		require.NoError(t, err)

		// Delete
		err = repo.Delete(context.TODO(), "Test Actress")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByAliasName(context.TODO(), "Test Actress")
		assert.Error(t, err, "Should not find deleted alias")
	})

	t.Run("List", func(t *testing.T) {
		aliases, err := repo.List(context.TODO())
		require.NoError(t, err)
		assert.Greater(t, len(aliases), 0)
	})
}

func TestSeedDefaultActressAliases(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	SeedDefaultActressAliases(context.TODO(), repo)

	m, err := repo.GetAliasMap(context.TODO())
	require.NoError(t, err)
	assert.NotEmpty(t, m)
	// DOCP-392 rename mappings are present and resolve to the current name.
	assert.Equal(t, "新セリナ", m["青木桃"])
	assert.Equal(t, "新セリナ", m["朝日芹奈"])
	assert.Equal(t, "新セリナ", m["堤セリナ"])
	assert.Equal(t, "尾崎えりか", m["与田さくら"])
	assert.Equal(t, "日向ゆら", m["広瀬みつき"])
}

func TestSeedDefaultActressAliases_Idempotent(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	SeedDefaultActressAliases(context.TODO(), repo)
	list1, err := repo.List(context.TODO())
	require.NoError(t, err)

	SeedDefaultActressAliases(context.TODO(), repo)
	list2, err := repo.List(context.TODO())
	require.NoError(t, err)

	assert.Equal(t, len(list1), len(list2), "seeding twice should not duplicate entries")
}

// TestSeedDefaultActressAliases_PreservesUserCanonical verifies the seed is
// insert-only: a user's curated canonical for an alias is not overwritten.
func TestSeedDefaultActressAliases_PreservesUserCanonical(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	// User prefers the release-time name as canonical for this alias.
	require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{
		AliasName:     "青木桃",
		CanonicalName: "朝日芹奈",
	}))

	SeedDefaultActressAliases(context.TODO(), repo)

	found, err := repo.FindByAliasName(context.TODO(), "青木桃")
	require.NoError(t, err)
	assert.Equal(t, "朝日芹奈", found.CanonicalName, "user-curated canonical must not be overwritten by seed")
}

func TestGetAliasGroup(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	// Seed: 新セリナ canonical with three aliases; 尾崎えりか canonical with one.
	seed := []models.ActressAlias{
		{AliasName: "青木桃", CanonicalName: "新セリナ"},
		{AliasName: "朝日芹奈", CanonicalName: "新セリナ"},
		{AliasName: "堤セリナ", CanonicalName: "新セリナ"},
		{AliasName: "与田さくら", CanonicalName: "尾崎えりか"},
	}
	for _, a := range seed {
		require.NoError(t, repo.Create(context.TODO(), &a))
	}

	t.Run("alias input resolves to canonical-first group", func(t *testing.T) {
		g, err := repo.GetAliasGroup(context.TODO(), "朝日芹奈")
		require.NoError(t, err)
		assert.Equal(t, "新セリナ", g.Canonical)
		// Canonical first, then aliases in deterministic (alias_name) order.
		require.Len(t, g.Names, 4)
		assert.Equal(t, "新セリナ", g.Names[0])
		// Re-fetch and assert the order is stable across calls.
		g2, err := repo.GetAliasGroup(context.TODO(), "青木桃")
		require.NoError(t, err)
		assert.Equal(t, g.Names, g2.Names, "alias group order must be deterministic")
		assert.Subset(t, g.Names, []string{"青木桃", "朝日芹奈", "堤セリナ"})
	})

	t.Run("canonical input resolves to same group", func(t *testing.T) {
		g, err := repo.GetAliasGroup(context.TODO(), "新セリナ")
		require.NoError(t, err)
		assert.Equal(t, "新セリナ", g.Canonical)
		require.Len(t, g.Names, 4)
		assert.Equal(t, "新セリナ", g.Names[0])
	})

	t.Run("unknown name returns empty group", func(t *testing.T) {
		g, err := repo.GetAliasGroup(context.TODO(), "弥生みづき")
		require.NoError(t, err)
		assert.Empty(t, g.Canonical)
		assert.Empty(t, g.Names)
	})

	t.Run("empty name returns empty group", func(t *testing.T) {
		g, err := repo.GetAliasGroup(context.TODO(), "   ")
		require.NoError(t, err)
		assert.Empty(t, g.Canonical)
		assert.Empty(t, g.Names)
	})

	t.Run("single-alias canonical includes both names", func(t *testing.T) {
		g, err := repo.GetAliasGroup(context.TODO(), "尾崎えりか")
		require.NoError(t, err)
		assert.Equal(t, "尾崎えりか", g.Canonical)
		assert.Equal(t, []string{"尾崎えりか", "与田さくら"}, g.Names)
	})

	t.Run("name that is both an alias and a canonical prefers the canonical group", func(t *testing.T) {
		// Seed 日向ゆら as a canonical (alias 広瀬みつき points at it), then make
		// 日向ゆら ALSO an alias of a different performer to simulate the collision.
		require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{
			AliasName: "広瀬みつき", CanonicalName: "日向ゆら",
		}))
		require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{
			AliasName: "日向ゆら", CanonicalName: "別の女優",
		}))
		g, err := repo.GetAliasGroup(context.TODO(), "日向ゆら")
		require.NoError(t, err)
		// Canonical wins: group is 日向ゆら's own, not 別の女優's.
		assert.Equal(t, "日向ゆら", g.Canonical)
		assert.Contains(t, g.Names, "日向ゆら")
		assert.Contains(t, g.Names, "広瀬みつき")
		assert.NotContains(t, g.Names, "別の女優")
	})

	t.Run("alias equal to canonical is deduplicated", func(t *testing.T) {
		// A self-referential row (alias == canonical) exercises the dedup guard:
		// add(canonical) adds it to seen, then add(alias) must skip it.
		require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{
			AliasName: "新セリナ", CanonicalName: "新セリナ",
		}))
		g, err := repo.GetAliasGroup(context.TODO(), "新セリナ")
		require.NoError(t, err)
		assert.Equal(t, "新セリナ", g.Canonical)
		// 新セリナ appears exactly once despite being both canonical and an alias.
		count := 0
		for _, n := range g.Names {
			if n == "新セリナ" {
				count++
			}
		}
		assert.Equal(t, 1, count, "canonical must not be duplicated when it also appears as an alias")
	})

	t.Run("empty alias name is skipped", func(t *testing.T) {
		// A row with an empty AliasName exercises the n == "" guard in add().
		require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{
			AliasName: "", CanonicalName: "尾崎えりか",
		}))
		g, err := repo.GetAliasGroup(context.TODO(), "尾崎えりか")
		require.NoError(t, err)
		assert.Equal(t, "尾崎えりか", g.Canonical)
		assert.NotContains(t, g.Names, "", "empty alias names must not appear in the group")
	})
}
