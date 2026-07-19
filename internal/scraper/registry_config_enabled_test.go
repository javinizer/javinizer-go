package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func testEnabledDefaults() map[string]models.ScraperSettings {
	return map[string]models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "en"},
		"dmm":    {Enabled: false},
	}
}

func loadConfigFromYAML(t *testing.T, src string) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	require.NoError(t, yaml.Unmarshal([]byte(src), cfg))
	return cfg
}

func TestScraperRegistryConfigFromApp_OmittedEnabledInheritsTrueDefault(t *testing.T) {
	cfg := loadConfigFromYAML(t, "scrapers:\n    r18dev:\n        rate_limit: 500\n")

	result := ScraperRegistryConfigFromApp(cfg, []string{"r18dev"}, testEnabledDefaults())
	got := result.Overrides["r18dev"]
	assert.True(t, got.Enabled, "omitted enabled must inherit default true")
	assert.Equal(t, 500, got.RateLimit)
	assert.Equal(t, "en", got.Language)
}

func TestScraperRegistryConfigFromApp_ExplicitFalseRemainsFalse(t *testing.T) {
	cfg := loadConfigFromYAML(t, "scrapers:\n    r18dev:\n        enabled: false\n        rate_limit: 500\n")

	result := ScraperRegistryConfigFromApp(cfg, []string{"r18dev"}, testEnabledDefaults())
	got := result.Overrides["r18dev"]
	assert.False(t, got.Enabled, "explicit enabled:false must remain false")
	assert.Equal(t, 500, got.RateLimit)
}

func TestScraperRegistryConfigFromApp_OmittedEnabledInheritsFalseDefault(t *testing.T) {
	cfg := loadConfigFromYAML(t, "scrapers:\n    dmm:\n        rate_limit: 250\n")

	result := ScraperRegistryConfigFromApp(cfg, []string{"dmm"}, testEnabledDefaults())
	got := result.Overrides["dmm"]
	assert.False(t, got.Enabled, "omitted enabled must inherit default false")
	assert.Equal(t, 250, got.RateLimit)
}

func TestScraperRegistryConfigFromApp_ProgrammaticExplicitFalsePreserved(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: false, RateLimit: 500},
	}

	result := ScraperRegistryConfigFromApp(cfg, []string{"r18dev"}, testEnabledDefaults())
	got := result.Overrides["r18dev"]
	assert.False(t, got.Enabled, "programmatic explicit false must remain false against true default")
	assert.Equal(t, 500, got.RateLimit)
}

func TestScraperRegistryConfigFromApp_ProgrammaticExplicitTruePreserved(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"dmm": {Enabled: true, RateLimit: 250},
	}

	result := ScraperRegistryConfigFromApp(cfg, []string{"dmm"}, testEnabledDefaults())
	got := result.Overrides["dmm"]
	assert.True(t, got.Enabled, "programmatic explicit true must remain true against false default")
	assert.Equal(t, 250, got.RateLimit)
}
