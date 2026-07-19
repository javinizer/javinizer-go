package config

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func loadScraperCfgForValidation(t *testing.T, src string, resolver models.ScraperConfigResolverInterface) *Config {
	t.Helper()
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.resolver = resolver
	require.NoError(t, yaml.Unmarshal([]byte(src), cfg))
	cfg.Scrapers.Normalize()
	return cfg
}

func TestValidateScraperOverrides_ResolverAware_OmittedEnabledDefaultTrueInvalidFails(t *testing.T) {
	cases := []struct {
		name  string
		yaml  string
		field string
	}{
		{"negative rate_limit", "scrapers:\n    r18dev:\n        rate_limit: -1\n", "rate_limit"},
		{"negative retry_count", "scrapers:\n    r18dev:\n        retry_count: -5\n", "retry_count"},
		{"negative timeout", "scrapers:\n    r18dev:\n        timeout: -1\n", "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadScraperCfgForValidation(t, tc.yaml, newEnabledResolver())
			err := ValidateScraperOverrides(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.field)
		})
	}
}

func TestValidateScraperOverrides_ResolverAware_OmittedEnabledDefaultTrueValidPasses(t *testing.T) {
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        rate_limit: 500\n", newEnabledResolver())
	assert.NoError(t, ValidateScraperOverrides(cfg))
}

func TestValidateScraperOverrides_ResolverAware_ExplicitFalseInvalidSkipped(t *testing.T) {
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        enabled: false\n        rate_limit: -1\n", newEnabledResolver())
	assert.NoError(t, ValidateScraperOverrides(cfg))
}

func TestValidateScraperOverrides_ResolverAware_ExplicitTrueInvalidFails(t *testing.T) {
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        enabled: true\n        rate_limit: -1\n", newEnabledResolver())
	err := ValidateScraperOverrides(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit")
}

func TestValidateScraperOverrides_ResolverAware_DefaultDisabledOmittedEnabledSkipped(t *testing.T) {
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    dmm:\n        rate_limit: -1\n", newEnabledResolver())
	assert.NoError(t, ValidateScraperOverrides(cfg))
}

func TestValidateScraperOverrides_ResolverAware_ScraperSpecificOmittedEnabledRunsValidateFn(t *testing.T) {
	sentinel := assertError("scraper-specific failure")
	resolver := &validateFnTestResolver{
		staticTestConfigResolver: staticTestConfigResolver{
			registered: map[string]bool{"r18dev": true},
			defaults:   map[string]models.ScraperSettings{"r18dev": {Enabled: true, Language: "en"}},
		},
		validateFns: map[string]func(*models.ScraperSettings) error{
			"r18dev": func(*models.ScraperSettings) error { return sentinel },
		},
	}
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        rate_limit: 500\n", resolver)
	err := ValidateScraperOverrides(cfg)
	require.Error(t, err)
	assert.Equal(t, sentinel, err)
}

func TestValidateScraperOverrides_ResolverAware_ValidateFnSeesResolvedSettings(t *testing.T) {
	var seen models.ScraperSettings
	resolver := &validateFnTestResolver{
		staticTestConfigResolver: staticTestConfigResolver{
			registered: map[string]bool{"r18dev": true},
			defaults:   map[string]models.ScraperSettings{"r18dev": {Enabled: true, Language: "en", UserAgent: "default-ua"}},
		},
		validateFns: map[string]func(*models.ScraperSettings) error{
			"r18dev": func(ss *models.ScraperSettings) error { seen = *ss; return nil },
		},
	}
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        rate_limit: 500\n", resolver)
	require.NoError(t, ValidateScraperOverrides(cfg))
	assert.True(t, seen.Enabled, "validateFn sees effective enabled (default true inherited)")
	assert.Equal(t, "en", seen.Language, "validateFn sees default-merged language")
	assert.Equal(t, "default-ua", seen.UserAgent, "validateFn sees default-merged user_agent")
	assert.Equal(t, 500, seen.RateLimit, "validateFn sees the override's explicit value")
}

func TestValidateScraperOverrides_ResolverAware_ValidateFnSkippedForExplicitFalse(t *testing.T) {
	called := false
	resolver := &validateFnTestResolver{
		staticTestConfigResolver: staticTestConfigResolver{
			registered: map[string]bool{"r18dev": true},
			defaults:   map[string]models.ScraperSettings{"r18dev": {Enabled: true, Language: "en"}},
		},
		validateFns: map[string]func(*models.ScraperSettings) error{
			"r18dev": func(*models.ScraperSettings) error { called = true; return nil },
		},
	}
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        enabled: false\n        rate_limit: -1\n", resolver)
	require.NoError(t, ValidateScraperOverrides(cfg))
	assert.False(t, called, "validateFn must not run for an explicitly-disabled scraper")
}

func TestValidateScraperOverrides_DoesNotMutateStoredLanguage(t *testing.T) {
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        language: \" EN \"\n", newEnabledResolver())
	require.NoError(t, ValidateScraperOverrides(cfg))
	assert.Equal(t, " EN ", cfg.Scrapers.Overrides["r18dev"].Language, "validation must not mutate stored overrides; canonicalization belongs to the normalize pipeline")
}

func TestClone_PreservesNilScraperOverrideEntries(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "en"},
		"dmm":    nil,
	}
	cp := cfg.Clone()
	require.NotNil(t, cp.Scrapers.Overrides)
	assert.NotNil(t, cp.Scrapers.Overrides["r18dev"])
	dmm, ok := cp.Scrapers.Overrides["dmm"]
	assert.True(t, ok, "nil scraper override entries must be preserved by Clone so fail-closed validation cannot be evaded")
	assert.Nil(t, dmm)
}

func TestPrepareForPersistence_NilScraperOverrideFails(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(NewTestScraperConfigResolverInterface())
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.Scrapers.Overrides["r18dev"] = nil
	_, err := PrepareForPersistence(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}

func TestPrepareRuntime_NilScraperOverrideFails(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(NewTestScraperConfigResolverInterface())
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.Scrapers.Overrides["r18dev"] = nil
	_, err := PrepareRuntime(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}

func TestConfigValidate_NilScraperOverrideFails(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Scrapers.Finalize(NewTestScraperConfigResolverInterface())
	cfg.Scrapers.Overrides["r18dev"] = nil
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}

func TestPrepareForPersistence_InvalidEffectiveScraperFails(t *testing.T) {
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        rate_limit: -1\n", newEnabledResolver())
	cfg.Database.Type = "sqlite"
	cfg.Database.DSN = ":memory:"
	cfg.Scrapers.Priority = []string{"r18dev"}
	_, err := PrepareForPersistence(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate_limit")
}

func TestPrepareForPersistence_ValidEffectiveScraperPasses(t *testing.T) {
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        rate_limit: 500\n", newEnabledResolver())
	cfg.Database.Type = "sqlite"
	cfg.Database.DSN = ":memory:"
	cfg.Scrapers.Priority = []string{"r18dev"}
	_, err := PrepareForPersistence(cfg)
	assert.NoError(t, err)
}

func TestPrepareForPersistence_ExplicitFalseInvalidScraperSkipped(t *testing.T) {
	cfg := loadScraperCfgForValidation(t, "scrapers:\n    r18dev:\n        enabled: false\n        rate_limit: -1\n", newEnabledResolver())
	cfg.Database.Type = "sqlite"
	cfg.Database.DSN = ":memory:"
	cfg.Scrapers.Priority = []string{"r18dev"}
	_, err := PrepareForPersistence(cfg)
	assert.NoError(t, err)
}
