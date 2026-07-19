package config

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scraperDefaultsForTest() map[string]models.ScraperSettings {
	return map[string]models.ScraperSettings{
		"r18dev":   {Enabled: true, Language: "en", UserAgent: DefaultUserAgent, RespectRetryAfter: boolPtr(true)},
		"dmm":      {Enabled: false},
		"javdb":    {Enabled: false, RateLimit: 1000, BaseURL: "https://javdb.com"},
		"libredmm": {Enabled: false, RateLimit: 1000, BaseURL: "https://www.libredmm.com"},
	}
}

func scraperNamesForTest() []string {
	return []string{"r18dev", "dmm", "javdb", "libredmm"}
}

func saveSparseScraperConfig(t *testing.T, cfg *Config) (*ConfigStorage, string) {
	t.Helper()
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	ctx, err := BuildSparseSaveContextWithScrapers(scraperNamesForTest(), scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))
	return cs, path
}

func readPersistedScraperYAML(t *testing.T, cs *ConfigStorage, path string) string {
	t.Helper()
	data, err := afero.ReadFile(cs.fs, path)
	require.NoError(t, err)
	return string(data)
}

func TestSaveSparseScraperDefaults_DefaultOnlyEnabledPruned(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true},
	}

	cs, path := saveSparseScraperConfig(t, cfg)
	got := readPersistedScraperYAML(t, cs, path)

	assert.NotContains(t, got, "r18dev:")
	assert.Contains(t, got, "config_version:")
}

func TestSaveSparseScraperDefaults_DefaultOnlyDisabledPruned(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"dmm": {Enabled: false},
	}

	cs, path := saveSparseScraperConfig(t, cfg)
	got := readPersistedScraperYAML(t, cs, path)

	assert.NotContains(t, got, "dmm:")
}

func TestSaveSparseScraperDefaults_DefaultTrueExplicitFalsePreserved(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: false},
	}

	cs, path := saveSparseScraperConfig(t, cfg)
	got := readPersistedScraperYAML(t, cs, path)

	assert.Contains(t, got, "r18dev:")
	assert.Contains(t, got, "enabled: false")
	assert.NotContains(t, got, "language")
}

func TestSaveSparseScraperDefaults_DefaultFalseExplicitTruePreserved(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"javdb": {Enabled: true},
	}

	cs, path := saveSparseScraperConfig(t, cfg)
	got := readPersistedScraperYAML(t, cs, path)

	assert.Contains(t, got, "javdb:")
	assert.Contains(t, got, "enabled: true")
	assert.NotContains(t, got, "rate_limit: 1000")
	assert.NotContains(t, got, "language")
}

func TestSaveSparseScraperDefaults_NonEnabledFieldOverridePreserved(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"javdb": {Enabled: true, RateLimit: 500},
	}

	cs, path := saveSparseScraperConfig(t, cfg)
	got := readPersistedScraperYAML(t, cs, path)

	assert.Contains(t, got, "javdb:")
	assert.Contains(t, got, "enabled: true")
	assert.Contains(t, got, "rate_limit: 500")
	assert.NotContains(t, got, "base_url")
}

func TestSaveSparseScraperDefaults_NonEnabledFieldMatchingDefaultPruned(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"javdb": {Enabled: true, RateLimit: 1000, BaseURL: "https://javdb.com"},
	}

	cs, path := saveSparseScraperConfig(t, cfg)
	got := readPersistedScraperYAML(t, cs, path)

	assert.Contains(t, got, "javdb:")
	assert.Contains(t, got, "enabled: true")
	assert.NotContains(t, got, "rate_limit")
	assert.NotContains(t, got, "base_url")
}

func TestSaveSparseScraperDefaults_UnregisteredScraperPreserved(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"customscraper": {Enabled: true, Language: "ja"},
	}

	cs, path := saveSparseScraperConfig(t, cfg)
	got := readPersistedScraperYAML(t, cs, path)

	assert.Contains(t, got, "customscraper:")
}

func TestSaveSparseScraperDefaults_ExistingDefaultOnlyBlockPrunedOnDisk(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"
	require.NoError(t, afero.WriteFile(cs.fs, path, []byte(strings.Join([]string{
		"config_version: 3",
		"scrapers:",
		"    r18dev:",
		"        enabled: true",
		"        language: en",
		"    request_timeout_seconds: 60",
	}, "\n")), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true},
	}
	cfg.Scrapers.RequestTimeoutSeconds = 60

	ctx, err := BuildSparseSaveContextWithScrapers(scraperNamesForTest(), scraperDefaultsForTest())
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got := readPersistedScraperYAML(t, cs, path)
	assert.NotContains(t, got, "r18dev:")
	assert.Contains(t, got, "request_timeout_seconds: 60")
}

func TestSaveSparseScraperDefaults_UnrelatedSaveRemainsMinimal(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Logging.Level = "debug"
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev":   {Enabled: true},
		"dmm":      {Enabled: false},
		"javdb":    {Enabled: true},
		"libredmm": {Enabled: false},
	}

	cs, path := saveSparseScraperConfig(t, cfg)
	got := readPersistedScraperYAML(t, cs, path)

	assert.Contains(t, got, "logging:")
	assert.Contains(t, got, "level: debug")
	assert.Contains(t, got, "javdb:")
	assert.NotContains(t, got, "r18dev:")
	assert.NotContains(t, got, "dmm:")
	assert.NotContains(t, got, "libredmm:")
}

func TestSaveSparseScraperDefaults_FutureDefaultChangeInherited(t *testing.T) {
	cs := newMemStorage()
	path := "/cfg/config.yaml"

	defaultsV1 := map[string]models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "en"},
	}
	ctxV1, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, defaultsV1)
	require.NoError(t, err)

	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true},
	}
	require.NoError(t, cs.SaveSparse(cfg, path, ctxV1))

	got := readPersistedScraperYAML(t, cs, path)
	assert.NotContains(t, got, "r18dev:")

	defaultsV2 := map[string]models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "ja"},
	}
	ctxV2, err := BuildSparseSaveContextWithScrapers([]string{"r18dev"}, defaultsV2)
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctxV2))
	gotV2 := readPersistedScraperYAML(t, cs, path)
	assert.NotContains(t, gotV2, "r18dev:")
}

func TestSaveSparseScraperDefaults_NamesOnlyPreservesDefaultOnlyBlocks(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true},
	}

	cs := newMemStorage()
	path := "/cfg/config.yaml"
	ctx, err := BuildSparseSaveContextWithNames([]string{"r18dev"})
	require.NoError(t, err)
	require.NoError(t, cs.SaveSparse(cfg, path, ctx))

	got := readPersistedScraperYAML(t, cs, path)
	assert.Contains(t, got, "r18dev:")
}

func TestResolveScraperOverridesForDiff_NoDefaultsReturnsOriginals(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	defaults := DefaultConfig(nil, nil)
	diffCfg, diffDefaults := resolveScraperOverridesForDiff(cfg, defaults, nil)
	assert.Same(t, cfg, diffCfg)
	assert.Same(t, defaults, diffDefaults)
}

func TestResolveScraperOverridesForDiff_NilCfgReturnsNil(t *testing.T) {
	diffCfg, diffDefaults := resolveScraperOverridesForDiff(nil, DefaultConfig(nil, nil), scraperDefaultsForTest())
	assert.Nil(t, diffCfg)
	assert.NotNil(t, diffDefaults)
}

func TestResolveScraperOverridesForDiff_DoesNotMutateOriginal(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: false},
	}
	original := cfg.Scrapers.Overrides["r18dev"].Clone()

	diffCfg, _ := resolveScraperOverridesForDiff(cfg, DefaultConfig(nil, nil), scraperDefaultsForTest())

	assert.Equal(t, original.Enabled, cfg.Scrapers.Overrides["r18dev"].Enabled)
	assert.Equal(t, "", cfg.Scrapers.Overrides["r18dev"].Language)
	resolved := diffCfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, resolved)
	assert.False(t, resolved.Enabled)
	assert.Equal(t, "en", resolved.Language)
	assert.Equal(t, DefaultUserAgent, resolved.UserAgent)
}

func TestResolveScraperOverridesForDiff_NilDefaultsOverridesSeeded(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: false},
	}
	defaults := DefaultConfig(nil, nil)
	defaults.Scrapers.Overrides = nil

	diffCfg, seeded := resolveScraperOverridesForDiff(cfg, defaults, scraperDefaultsForTest())

	require.NotNil(t, seeded.Scrapers.Overrides)
	assert.Nil(t, defaults.Scrapers.Overrides)
	assert.Equal(t, "", cfg.Scrapers.Overrides["r18dev"].Language)

	expected := scraperDefaultsForTest()["r18dev"]
	seededR18 := seeded.Scrapers.Overrides["r18dev"]
	require.NotNil(t, seededR18)
	assert.Equal(t, expected, *seededR18)

	resolvedR18 := diffCfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, resolvedR18)
	assert.False(t, resolvedR18.Enabled)
	assert.Equal(t, "en", resolvedR18.Language)
	assert.Equal(t, DefaultUserAgent, resolvedR18.UserAgent)
	require.NotNil(t, resolvedR18.RespectRetryAfter)
	assert.True(t, *resolvedR18.RespectRetryAfter)
}

func TestBuildSparseSaveContextWithScrapers_SeedsScraperDefaults(t *testing.T) {
	defaults := scraperDefaultsForTest()
	ctx, err := BuildSparseSaveContextWithScrapers(scraperNamesForTest(), defaults)
	require.NoError(t, err)
	assert.Equal(t, defaults, ctx.ScraperDefaults)
	assert.True(t, ctx.KnownScraperNames["r18dev"])
}

func TestBuildSparseSaveContextWithScrapers_NilDefaults(t *testing.T) {
	ctx, err := BuildSparseSaveContextWithScrapers(scraperNamesForTest(), nil)
	require.NoError(t, err)
	assert.Nil(t, ctx.ScraperDefaults)
	assert.True(t, ctx.KnownScraperNames["javdb"])
}
