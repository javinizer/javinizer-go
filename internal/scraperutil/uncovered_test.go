package scraperutil

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationCatalog_Names(t *testing.T) {
	cat := newregistrationCatalog()
	cat.Register(ScraperRegistration{Name: "r18dev", Priority: 1})
	cat.Register(ScraperRegistration{Name: "javdb", Priority: 2})

	names := cat.Names()
	assert.Len(t, names, 2)
	assert.Contains(t, names, "r18dev")
	assert.Contains(t, names, "javdb")
}

func TestRegistrationCatalog_NamesEmpty(t *testing.T) {
	cat := newregistrationCatalog()
	names := cat.Names()
	assert.Empty(t, names)
}

func TestRegistrationCatalog_Priorities(t *testing.T) {
	cat := newregistrationCatalog()
	cat.Register(ScraperRegistration{Name: "r18dev", Priority: 1})
	cat.Register(ScraperRegistration{Name: "javdb", Priority: 3})
	cat.Register(ScraperRegistration{Name: "dmm", Priority: 2})

	priorities := cat.Priorities()
	require.Len(t, priorities, 3)
	// Highest priority first
	assert.Equal(t, "javdb", priorities[0])
	assert.Equal(t, "dmm", priorities[1])
	assert.Equal(t, "r18dev", priorities[2])
}

func TestRegistrationCatalog_PrioritiesTiebreaker(t *testing.T) {
	cat := newregistrationCatalog()
	cat.Register(ScraperRegistration{Name: "beta", Priority: 2})
	cat.Register(ScraperRegistration{Name: "alpha", Priority: 2})

	priorities := cat.Priorities()
	require.Len(t, priorities, 2)
	assert.Equal(t, "alpha", priorities[0])
	assert.Equal(t, "beta", priorities[1])
}

func TestRegistrationCatalog_PrioritiesEmpty(t *testing.T) {
	cat := newregistrationCatalog()
	priorities := cat.Priorities()
	assert.Nil(t, priorities)
}

func TestRegistrationCatalog_IsRegistered(t *testing.T) {
	cat := newregistrationCatalog()
	cat.Register(ScraperRegistration{Name: "r18dev", Priority: 1})

	assert.True(t, cat.IsRegistered("r18dev"))
	assert.False(t, cat.IsRegistered("nonexistent"))
}

func TestRegistrationCatalog_GetAll_ClonesOptions(t *testing.T) {
	cat := newregistrationCatalog()
	cat.Register(ScraperRegistration{
		Name:    "test",
		Options: []models.ScraperOption{{Key: "k1"}},
	})

	all := cat.GetAll()
	require.Contains(t, all, "test")
	// Mutating the clone should not affect original
	all["test"].Options[0].Key = "mutated"
	orig, ok := cat.Get("test")
	require.True(t, ok)
	assert.Equal(t, "k1", orig.Options[0].Key)
}

func TestScraperRegistry_Uncovered(t *testing.T) {
	reg := NewScraperRegistry()

	t.Run("Get returns false for unregistered", func(t *testing.T) {
		_, ok := reg.Get("nonexistent")
		assert.False(t, ok)
	})

	t.Run("Names returns empty for no registrations", func(t *testing.T) {
		names := reg.Names()
		assert.Empty(t, names)
	})

	t.Run("Priorities returns empty for no registrations", func(t *testing.T) {
		priorities := reg.Priorities()
		assert.Empty(t, priorities)
	})

	t.Run("IsRegistered returns false for unregistered", func(t *testing.T) {
		assert.False(t, reg.IsRegistered("nonexistent"))
	})

	t.Run("Names returns registered names", func(t *testing.T) {
		reg.Register(ScraperRegistration{Name: "test1", Priority: 1})
		reg.Register(ScraperRegistration{Name: "test2", Priority: 2})
		names := reg.Names()
		assert.Len(t, names, 2)
		assert.Contains(t, names, "test1")
		assert.Contains(t, names, "test2")
	})

	t.Run("Priorities returns sorted names", func(t *testing.T) {
		priorities := reg.Priorities()
		require.Len(t, priorities, 2)
		assert.Equal(t, "test2", priorities[0])
		assert.Equal(t, "test1", priorities[1])
	})

	t.Run("IsRegistered returns true for registered", func(t *testing.T) {
		assert.True(t, reg.IsRegistered("test1"))
	})

	t.Run("Get returns registration", func(t *testing.T) {
		r, ok := reg.Get("test1")
		require.True(t, ok)
		assert.Equal(t, "test1", r.Name)
	})

	t.Run("GetAllDefaults", func(t *testing.T) {
		settings := reg.GetAllDefaults()
		assert.NotNil(t, settings)
	})

	t.Run("GetAllDefaults", func(t *testing.T) {
		defaults := reg.GetAllDefaults()
		assert.NotNil(t, defaults)
	})

}

func TestHelpers_IntPtr(t *testing.T) {
	result := IntPtr(42)
	require.NotNil(t, result)
	assert.Equal(t, 42, *result)
}
