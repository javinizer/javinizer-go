package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Audit P1 (phase 3): explicit rate_limit: 0 means "no delay" and differs
// from omitted (conservative 1s default) — the YAML decoder must record the
// presence, or the explicit-zero contract is unreachable dead code.
func TestScraperRateLimitPresence_YAMLZeroRecorded(t *testing.T) {
	input := `
scrapers:
    r18dev:
        rate_limit: 0
`
	var cfg Config
	cfg.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))
	override := cfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.Equal(t, 0, override.RateLimit)
	assert.True(t, override.RateLimitIsExplicit(), "rate_limit: 0 must be recorded as explicit")
}

// Omitted rate_limit must NOT be marked explicit.
func TestScraperRateLimitPresence_YAMLOmittedNotExplicit(t *testing.T) {
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
	assert.Equal(t, 500, override.RateLimit)
	assert.True(t, override.RateLimitIsExplicit(), "500 is non-zero and explicit")

	input2 := `
scrapers:
    r18dev:
        enabled: true
`
	var cfg2 Config
	cfg2.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, yaml.Unmarshal([]byte(input2), &cfg2))
	override2 := cfg2.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override2)
	assert.False(t, override2.RateLimitIsExplicit(), "omitted rate_limit must not be explicit")
}

// JSON path parity.
func TestScraperRateLimitPresence_JSON(t *testing.T) {
	var cfg Config
	require.NoError(t, cfg.Scrapers.UnmarshalJSON([]byte(`{"r18dev":{"rate_limit":0}}`)))
	override := cfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.True(t, override.RateLimitIsExplicit())

	var cfg2 Config
	require.NoError(t, cfg2.Scrapers.UnmarshalJSON([]byte(`{"r18dev":{"enabled":true}}`)))
	assert.False(t, cfg2.Scrapers.Overrides["r18dev"].RateLimitIsExplicit())
}
