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

// Codex: the deprecated request_delay alias must record explicit presence —
// request_delay: 0 means "no delay" and must survive MergeDefaultsFrom
// defaults, in both the YAML and JSON decoders.
func TestScraperRateLimitPresence_YAMLAliasZeroRecorded(t *testing.T) {
	input := `
scrapers:
    r18dev:
        request_delay: 0
`
	var cfg Config
	cfg.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))
	override := cfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.Equal(t, 0, override.RateLimit)
	assert.True(t, override.RateLimitIsExplicit(), "request_delay: 0 must be recorded as explicit")

	// Non-zero alias still fills the canonical field.
	var cfg2 Config
	cfg2.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, yaml.Unmarshal([]byte("scrapers:\n    r18dev:\n        request_delay: 750\n"), &cfg2))
	override2 := cfg2.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override2)
	assert.Equal(t, 750, override2.RateLimit)
	assert.True(t, override2.RateLimitIsExplicit())

	// Canonical key keeps precedence over the alias (round-7 contract).
	var cfg3 Config
	cfg3.Scrapers.resolver = newEnabledResolver()
	require.NoError(t, yaml.Unmarshal([]byte("scrapers:\n    r18dev:\n        rate_limit: 0\n        request_delay: 750\n"), &cfg3))
	override3 := cfg3.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override3)
	assert.Equal(t, 0, override3.RateLimit, "canonical rate_limit: 0 beats request_delay: 750")
	assert.True(t, override3.RateLimitIsExplicit())
}

func TestScraperRateLimitPresence_JSONAliasZeroRecorded(t *testing.T) {
	var cfg Config
	require.NoError(t, cfg.Scrapers.UnmarshalJSON([]byte(`{"r18dev":{"request_delay":0}}`)))
	override := cfg.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override)
	assert.Equal(t, 0, override.RateLimit)
	assert.True(t, override.RateLimitIsExplicit(), "request_delay: 0 must be recorded as explicit")

	var cfg2 Config
	require.NoError(t, cfg2.Scrapers.UnmarshalJSON([]byte(`{"r18dev":{"request_delay":750}}`)))
	override2 := cfg2.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override2)
	assert.Equal(t, 750, override2.RateLimit)
	assert.True(t, override2.RateLimitIsExplicit())

	// Canonical key keeps precedence over the alias.
	var cfg3 Config
	require.NoError(t, cfg3.Scrapers.UnmarshalJSON([]byte(`{"r18dev":{"rate_limit":0,"request_delay":750}}`)))
	override3 := cfg3.Scrapers.Overrides["r18dev"]
	require.NotNil(t, override3)
	assert.Equal(t, 0, override3.RateLimit, "canonical rate_limit: 0 beats request_delay: 750")
	assert.True(t, override3.RateLimitIsExplicit())
}
