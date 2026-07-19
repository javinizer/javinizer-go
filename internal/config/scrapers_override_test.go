package config

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverride_CreatesEntryWhenNoneExists(t *testing.T) {
	sc := &ScrapersConfig{}
	got := sc.Override("dmm")
	require.NotNil(t, got)
	assert.Same(t, got, sc.Overrides["dmm"])
}

func TestOverride_ReturnsExistingEntry(t *testing.T) {
	existing := &models.ScraperSettings{Enabled: true}
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"dmm": existing}}
	got := sc.Override("dmm")
	assert.Same(t, existing, got)
}

func TestOverride_NilsMapAndCreatesEntry(t *testing.T) {
	sc := &ScrapersConfig{Overrides: nil}
	got := sc.Override("javdb")
	require.NotNil(t, got)
	require.NotNil(t, sc.Overrides)
	assert.Same(t, got, sc.Overrides["javdb"])
}

func TestApplyOverride_NoOptsIsNoOp(t *testing.T) {
	sc := &ScrapersConfig{Overrides: nil}
	sc.ApplyOverride("dmm")
	assert.Nil(t, sc.Overrides)
}

func TestApplyOverride_WithOptsAppliesThem(t *testing.T) {
	sc := &ScrapersConfig{}
	sc.ApplyOverride("dmm", func(s *models.ScraperSettings) {
		s.Enabled = true
		s.Language = "ja"
	})
	require.NotNil(t, sc.Overrides["dmm"])
	assert.True(t, sc.Overrides["dmm"].Enabled)
	assert.Equal(t, "ja", sc.Overrides["dmm"].Language)
}

func TestUserOverride_EntryExists(t *testing.T) {
	existing := &models.ScraperSettings{Enabled: true}
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"dmm": existing}}
	got, ok := sc.UserOverride("dmm")
	assert.True(t, ok)
	assert.Same(t, existing, got)
}

func TestUserOverride_EntryAbsent(t *testing.T) {
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{}}
	got, ok := sc.UserOverride("dmm")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestUserOverride_NilMap(t *testing.T) {
	sc := &ScrapersConfig{Overrides: nil}
	got, ok := sc.UserOverride("dmm")
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestResolvedSettings_NilResolverReturnsUserOverrideClone(t *testing.T) {
	over := &models.ScraperSettings{Enabled: true, Language: "ja"}
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"dmm": over}}
	resolved := sc.ResolvedSettings("dmm")
	assert.True(t, resolved.Enabled)
	assert.Equal(t, "ja", resolved.Language)
	// Mutating the clone must not affect the override.
	resolved.Language = "en"
	assert.Equal(t, "ja", sc.Overrides["dmm"].Language)
}

func TestResolvedSettings_NilResolverAbsentOverrideReturnsEmpty(t *testing.T) {
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{}}
	resolved := sc.ResolvedSettings("dmm")
	assert.Equal(t, models.ScraperSettings{}, resolved)
}

func TestResolvedSettings_ResolverAbsentOverrideReturnsDefaultClone(t *testing.T) {
	resolver := &staticTestConfigResolver{
		registered: map[string]bool{"dmm": true},
		defaults:   map[string]models.ScraperSettings{"dmm": {Enabled: false, Language: "ja"}},
	}
	sc := &ScrapersConfig{}
	require.NoError(t, sc.Finalize(resolver))
	resolved := sc.ResolvedSettings("dmm")
	assert.False(t, resolved.Enabled)
	assert.Equal(t, "ja", resolved.Language)
}

func TestResolvedSettings_ResolverPresentOverrideMerges(t *testing.T) {
	resolver := &staticTestConfigResolver{
		registered: map[string]bool{"dmm": true},
		defaults:   map[string]models.ScraperSettings{"dmm": {Enabled: false, Language: "ja", UserAgent: "default-agent"}},
	}
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"dmm": {Enabled: true}}}
	require.NoError(t, sc.Finalize(resolver))
	resolved := sc.ResolvedSettings("dmm")
	assert.True(t, resolved.Enabled)
	assert.Equal(t, "ja", resolved.Language)
	assert.Equal(t, "default-agent", resolved.UserAgent)
}

func TestResolvedSettings_ResolverUnknownScraperReturnsEmpty(t *testing.T) {
	resolver := &staticTestConfigResolver{
		registered: map[string]bool{},
		defaults:   map[string]models.ScraperSettings{},
	}
	sc := &ScrapersConfig{}
	require.NoError(t, sc.Finalize(resolver))
	resolved := sc.ResolvedSettings("doesnotexist")
	assert.Equal(t, models.ScraperSettings{}, resolved)
}

func TestResolvedSettings_ResolverUnknownScraperButUserOverrideReturnsClone(t *testing.T) {
	resolver := &staticTestConfigResolver{
		registered: map[string]bool{},
		defaults:   map[string]models.ScraperSettings{},
	}
	over := &models.ScraperSettings{Enabled: true}
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"custom": over}}
	require.NoError(t, sc.Finalize(resolver))
	resolved := sc.ResolvedSettings("custom")
	assert.True(t, resolved.Enabled)
}

func TestIsScraperRegistered_WithResolver(t *testing.T) {
	resolver := &staticTestConfigResolver{registered: map[string]bool{"dmm": true}}
	sc := &ScrapersConfig{}
	require.NoError(t, sc.Finalize(resolver))
	assert.True(t, sc.IsScraperRegistered("dmm"))
	assert.False(t, sc.IsScraperRegistered("nope"))
}

func TestIsScraperRegistered_NilResolverPermissive(t *testing.T) {
	sc := &ScrapersConfig{}
	assert.True(t, sc.IsScraperRegistered("anything"))
}

func TestNormalize_RebuildsValidateFns(t *testing.T) {
	validateFn := func(ss *models.ScraperSettings) error { return nil }
	resolver := &validatorTestResolver{
		registered: map[string]bool{"r18dev": true},
		defaults:   map[string]models.ScraperSettings{"r18dev": {Enabled: true}},
		validateFn: validateFn,
	}
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"r18dev": {Enabled: true}}}
	require.NoError(t, sc.Finalize(resolver))
	require.NotNil(t, sc.getValidateFn("r18dev"))
	// Normalize again to exercise the rebuild (delete-then-repopulate) loop.
	sc.Normalize()
	require.NotNil(t, sc.getValidateFn("r18dev"))
}
