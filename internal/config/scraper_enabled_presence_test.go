package config

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func enabledDefaultsForTest() map[string]models.ScraperSettings {
	return map[string]models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "en"},
		"dmm":    {Enabled: false},
	}
}

func newEnabledResolver() models.ScraperConfigResolverInterface {
	return &staticTestConfigResolver{
		registered: map[string]bool{"r18dev": true, "dmm": true},
		defaults:   enabledDefaultsForTest(),
	}
}

func TestScraperSettingsYAMLDecode_OmittedEnabledInheritsTrueDefault(t *testing.T) {
	t.Parallel()

	input := `
scrapers:
    r18dev:
        rate_limit: 500
`
	var cfg Config
	cfg.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))

	override := cfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.False(t, override.Enabled)

	resolved := cfg.Scrapers.ResolvedSettings("r18dev")
	assert.True(t, resolved.Enabled, "omitted enabled must inherit default true")
	assert.Equal(t, 500, resolved.RateLimit)
}

func TestScraperSettingsYAMLDecode_ExplicitFalseRemainsFalse(t *testing.T) {
	t.Parallel()

	input := `
scrapers:
    r18dev:
        enabled: false
        rate_limit: 500
`
	var cfg Config
	cfg.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))

	override := cfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.False(t, override.Enabled)

	resolved := cfg.Scrapers.ResolvedSettings("r18dev")
	assert.False(t, resolved.Enabled, "explicit enabled:false must remain false")
	assert.Equal(t, 500, resolved.RateLimit)
}

func TestScraperSettingsYAMLDecode_OmittedEnabledInheritsFalseDefault(t *testing.T) {
	t.Parallel()

	input := `
scrapers:
    dmm:
        rate_limit: 250
`
	var cfg Config
	cfg.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))

	resolved := cfg.Scrapers.ResolvedSettings("dmm")
	assert.False(t, resolved.Enabled, "omitted enabled must inherit default false")
	assert.Equal(t, 250, resolved.RateLimit)
}

func TestScraperSettingsJSONDecode_OmittedEnabledInheritsTrueDefault(t *testing.T) {
	t.Parallel()

	input := `{"r18dev":{"rate_limit":500}}`
	var sc ScrapersConfig
	sc.resolver = newEnabledResolver()
	require.NoError(t, json.Unmarshal([]byte(input), &sc))

	override := sc.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.False(t, override.Enabled)

	require.NoError(t, sc.Finalize(newEnabledResolver()))
	resolved := sc.ResolvedSettings("r18dev")
	assert.True(t, resolved.Enabled, "omitted enabled must inherit default true")
	assert.Equal(t, 500, resolved.RateLimit)
}

func TestScraperSettingsJSONDecode_ExplicitFalseRemainsFalse(t *testing.T) {
	t.Parallel()

	input := `{"r18dev":{"enabled":false,"rate_limit":500}}`
	var sc ScrapersConfig
	sc.resolver = newEnabledResolver()
	require.NoError(t, json.Unmarshal([]byte(input), &sc))

	override := sc.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.False(t, override.Enabled)

	require.NoError(t, sc.Finalize(newEnabledResolver()))
	resolved := sc.ResolvedSettings("r18dev")
	assert.False(t, resolved.Enabled, "explicit enabled:false must remain false")
}

func TestScraperSettingsJSONDecode_OmittedEnabledInheritsFalseDefault(t *testing.T) {
	t.Parallel()

	input := `{"dmm":{"rate_limit":250}}`
	var sc ScrapersConfig
	sc.resolver = newEnabledResolver()
	require.NoError(t, json.Unmarshal([]byte(input), &sc))

	require.NoError(t, sc.Finalize(newEnabledResolver()))
	resolved := sc.ResolvedSettings("dmm")
	assert.False(t, resolved.Enabled, "omitted enabled must inherit default false")
	assert.Equal(t, 250, resolved.RateLimit)
}
