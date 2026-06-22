package config

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Finalize / Normalize deep tests
// ---------------------------------------------------------------------------

func TestFinalize_NilResolver(t *testing.T) {
	sc := &ScrapersConfig{}
	err := sc.Finalize(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil resolver")
}

func TestFinalize_PopulatesDefaults(t *testing.T) {
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"r18dev": {}}}
	resolver := NewTestScraperConfigResolverInterface()
	require.NoError(t, sc.Finalize(resolver))
	assert.NotNil(t, sc.Overrides["r18dev"])
}

func TestFinalize_MergesDefaults(t *testing.T) {
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "ja"},
	}}
	resolver := &staticTestConfigResolver{
		registered: map[string]bool{"r18dev": true},
		defaults:   map[string]models.ScraperSettings{"r18dev": {Enabled: true, UserAgent: "Javinizer"}},
	}
	require.NoError(t, sc.Finalize(resolver))
	assert.Equal(t, "ja", sc.Overrides["r18dev"].Language)         // preserved
	assert.Equal(t, "Javinizer", sc.Overrides["r18dev"].UserAgent) // filled from defaults
}

func TestNormalize_WithoutFinalize(t *testing.T) {
	sc := &ScrapersConfig{}
	// Normalize is a no-op when resolver is nil (void method).
	sc.Normalize()
	assert.Empty(t, sc.Overrides)
}

func TestNormalize_Idempotent_Deep(t *testing.T) {
	resolver := NewTestScraperConfigResolverInterface()
	sc := &ScrapersConfig{}
	require.NoError(t, sc.Finalize(resolver))
	count1 := len(sc.Overrides)
	sc.Normalize()
	assert.Equal(t, count1, len(sc.Overrides))
}

func TestFinalize_ValidatorDispatch(t *testing.T) {
	validateCalled := false
	resolver := &validatorTestResolver{
		registered: map[string]bool{"r18dev": true},
		defaults:   map[string]models.ScraperSettings{"r18dev": {Enabled: true}},
		validateFn: func(ss *models.ScraperSettings) error {
			validateCalled = true
			return nil
		},
	}
	sc := &ScrapersConfig{Overrides: map[string]*models.ScraperSettings{"r18dev": {Enabled: true}}}
	require.NoError(t, sc.Finalize(resolver))
	fn := sc.getValidateFn("r18dev")
	require.NotNil(t, fn)
	require.NoError(t, fn(sc.Overrides["r18dev"]))
	assert.True(t, validateCalled)
}

// ---------------------------------------------------------------------------
// UnmarshalYAML deep tests
// ---------------------------------------------------------------------------

func TestUnmarshalYAML_Deep_NilNode(t *testing.T) {
	sc := &ScrapersConfig{}
	require.NoError(t, sc.UnmarshalYAML(nil))
	assert.NotNil(t, sc.Overrides)
}

func TestUnmarshalYAML_Deep_ViaYAMLParse(t *testing.T) {
	input := `
user_agent: "TestAgent"
timeout_seconds: 60
priority:
  - r18dev
  - dmm
r18dev:
  enabled: true
  language: ja
`
	var sc ScrapersConfig
	require.NoError(t, yaml.Unmarshal([]byte(input), &sc))
	assert.Equal(t, "TestAgent", sc.UserAgent)
	assert.Equal(t, 60, sc.TimeoutSeconds)
	assert.Equal(t, []string{"r18dev", "dmm"}, sc.Priority)
	assert.NotNil(t, sc.Overrides["r18dev"])
	assert.True(t, sc.Overrides["r18dev"].Enabled)
	assert.Equal(t, "ja", sc.Overrides["r18dev"].Language)
}

func TestUnmarshalYAML_Deep_InvalidProxyYAML(t *testing.T) {
	input := `
proxy:
  enabled: "not_a_bool"
`
	var sc ScrapersConfig
	err := yaml.Unmarshal([]byte(input), &sc)
	assert.Error(t, err)
}

func TestUnmarshalYAML_Deep_UnknownScraperField(t *testing.T) {
	resolver := NewTestScraperConfigResolverInterface()
	input := `
r18dev:
  enabled: true
  nonexistent_field: "oops"
`
	var sc ScrapersConfig
	sc.resolver = resolver
	err := yaml.Unmarshal([]byte(input), &sc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown field")
}

func TestUnmarshalYAML_Deep_Aliases(t *testing.T) {
	input := `
r18dev:
  enabled: true
  request_delay: 1500
  max_retries: 3
`
	var sc ScrapersConfig
	require.NoError(t, yaml.Unmarshal([]byte(input), &sc))
	assert.Equal(t, 1500, sc.Overrides["r18dev"].RateLimit)
	assert.Equal(t, 3, sc.Overrides["r18dev"].RetryCount)
}

// ---------------------------------------------------------------------------
// UnmarshalJSON deep tests
// ---------------------------------------------------------------------------

func TestUnmarshalJSON_Deep_BasicFields(t *testing.T) {
	input := `{"user_agent":"TestAgent","timeout_seconds":60,"priority":["r18dev"]}`
	var sc ScrapersConfig
	require.NoError(t, json.Unmarshal([]byte(input), &sc))
	assert.Equal(t, "TestAgent", sc.UserAgent)
	assert.Equal(t, 60, sc.TimeoutSeconds)
	assert.Equal(t, []string{"r18dev"}, sc.Priority)
}

func TestUnmarshalJSON_Deep_InvalidJSON(t *testing.T) {
	input := `{invalid`
	var sc ScrapersConfig
	assert.Error(t, json.Unmarshal([]byte(input), &sc))
}

func TestUnmarshalJSON_Deep_ScraperEntry(t *testing.T) {
	resolver := NewTestScraperConfigResolverInterface()
	input := `{"r18dev":{"enabled":true,"language":"ja"}}`
	var sc ScrapersConfig
	sc.resolver = resolver
	require.NoError(t, json.Unmarshal([]byte(input), &sc))
	assert.True(t, sc.Overrides["r18dev"].Enabled)
	assert.Equal(t, "ja", sc.Overrides["r18dev"].Language)
}

func TestUnmarshalJSON_Deep_Aliases(t *testing.T) {
	input := `{"r18dev":{"enabled":true,"request_delay":1500,"max_retries":3}}`
	var sc ScrapersConfig
	require.NoError(t, json.Unmarshal([]byte(input), &sc))
	assert.Equal(t, 1500, sc.Overrides["r18dev"].RateLimit)
	assert.Equal(t, 3, sc.Overrides["r18dev"].RetryCount)
}

// ---------------------------------------------------------------------------
// MarshalJSON/YAML deep tests
// ---------------------------------------------------------------------------

func TestMarshalJSON_Deep_Roundtrip(t *testing.T) {
	sc := ScrapersConfig{
		UserAgent:      "TestAgent",
		TimeoutSeconds: 60,
		Priority:       []string{"r18dev"},
		Overrides: map[string]*models.ScraperSettings{
			"r18dev": {Enabled: true, Language: "ja"},
		},
	}
	data, err := json.Marshal(&sc)
	require.NoError(t, err)

	var sc2 ScrapersConfig
	require.NoError(t, json.Unmarshal(data, &sc2))
	assert.Equal(t, sc.UserAgent, sc2.UserAgent)
	assert.Equal(t, sc.TimeoutSeconds, sc2.TimeoutSeconds)
}

func TestMarshalYAML_Deep_Roundtrip(t *testing.T) {
	sc := ScrapersConfig{
		UserAgent:      "TestAgent",
		TimeoutSeconds: 60,
		Priority:       []string{"r18dev"},
		Overrides: map[string]*models.ScraperSettings{
			"r18dev": {Enabled: true, Language: "ja"},
		},
	}
	data, err := yaml.Marshal(&sc)
	require.NoError(t, err)

	var sc2 ScrapersConfig
	require.NoError(t, yaml.Unmarshal(data, &sc2))
	assert.Equal(t, sc.UserAgent, sc2.UserAgent)
	assert.Equal(t, sc.TimeoutSeconds, sc2.TimeoutSeconds)
}

// ---------------------------------------------------------------------------
// validatorTestResolver
// ---------------------------------------------------------------------------

type validatorTestResolver struct {
	registered map[string]bool
	defaults   map[string]models.ScraperSettings
	validateFn func(*models.ScraperSettings) error
}

func (r *validatorTestResolver) IsRegistered(name string) bool {
	return r.registered[name]
}

func (r *validatorTestResolver) GetAllDefaults() map[string]models.ScraperSettings {
	result := make(map[string]models.ScraperSettings, len(r.defaults))
	for k, v := range r.defaults {
		result[k] = v
	}
	return result
}

func (r *validatorTestResolver) GetValidateFn(name string) func(*models.ScraperSettings) error {
	return r.validateFn
}

func TestUnmarshalJSON_NonAliasPath_RejectsUnknownFields(t *testing.T) {
	t.Parallel()

	t.Run("unknown field in non-alias path is rejected", func(t *testing.T) {
		jsonInput := `{"r18dev": {"enabled": true, "nonexistent_field": "oops"}}`
		var cfg ScrapersConfig
		err := json.Unmarshal([]byte(jsonInput), &cfg)
		require.Error(t, err, "unknown field should be rejected in non-alias path")
		assert.Contains(t, err.Error(), "unknown field")
	})

	t.Run("known field in non-alias path is accepted", func(t *testing.T) {
		jsonInput := `{"r18dev": {"enabled": true, "language": "en"}}`
		var cfg ScrapersConfig
		err := json.Unmarshal([]byte(jsonInput), &cfg)
		assert.NoError(t, err)
	})
}
