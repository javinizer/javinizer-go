package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompletenessConfigStruct(t *testing.T) {
	t.Run("MetadataConfig contains Completeness field", func(t *testing.T) {
		cfg := MetadataConfig{}
		assert.Equal(t, false, cfg.Completeness.Enabled, "Completeness.Enabled should default to false")
	})

	t.Run("CompletenessTierDefinition has Weight and Fields", func(t *testing.T) {
		def := CompletenessTierDefinition{
			Weight: 50,
			Fields: []string{"title", "poster_url"},
		}
		assert.Equal(t, 50, def.Weight)
		assert.Equal(t, []string{"title", "poster_url"}, def.Fields)
	})

	t.Run("CompletenessTierConfig has Essential, Important, NiceToHave", func(t *testing.T) {
		tiers := CompletenessTierConfig{
			Essential:  CompletenessTierDefinition{Weight: 50, Fields: []string{"title"}},
			Important:  CompletenessTierDefinition{Weight: 35, Fields: []string{"description"}},
			NiceToHave: CompletenessTierDefinition{Weight: 15, Fields: []string{"label"}},
		}
		assert.Equal(t, 50, tiers.Essential.Weight)
		assert.Equal(t, 35, tiers.Important.Weight)
		assert.Equal(t, 15, tiers.NiceToHave.Weight)
	})

	t.Run("CompletenessConfig has Enabled and Tiers", func(t *testing.T) {
		cc := CompletenessConfig{
			Enabled: true,
			Tiers: CompletenessTierConfig{
				Essential:  CompletenessTierDefinition{Weight: 50, Fields: []string{}},
				Important:  CompletenessTierDefinition{Weight: 35, Fields: []string{}},
				NiceToHave: CompletenessTierDefinition{Weight: 15, Fields: []string{}},
			},
		}
		assert.True(t, cc.Enabled)
		assert.Equal(t, 50, cc.Tiers.Essential.Weight)
	})
}

func TestDefaultConfigCompleteness(t *testing.T) {
	t.Run("DefaultConfig includes Completeness with default tier assignments", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.False(t, cfg.Metadata.Completeness.Enabled, "Completeness should be disabled by default")
		assert.Equal(t, 50, cfg.Metadata.Completeness.Tiers.Essential.Weight)
		assert.Equal(t, 35, cfg.Metadata.Completeness.Tiers.Important.Weight)
		assert.Equal(t, 15, cfg.Metadata.Completeness.Tiers.NiceToHave.Weight)
	})

	t.Run("Default tier weights sum to 100", func(t *testing.T) {
		cfg := DefaultConfig()
		total := cfg.Metadata.Completeness.Tiers.Essential.Weight +
			cfg.Metadata.Completeness.Tiers.Important.Weight +
			cfg.Metadata.Completeness.Tiers.NiceToHave.Weight
		assert.Equal(t, 100, total, "default tier weights must sum to 100")
	})

	t.Run("Default essential fields match expected list", func(t *testing.T) {
		cfg := DefaultConfig()
		expected := []string{"title", "poster_url", "cover_url", "actresses", "genres"}
		assert.Equal(t, expected, cfg.Metadata.Completeness.Tiers.Essential.Fields)
	})

	t.Run("Default important fields match expected list", func(t *testing.T) {
		cfg := DefaultConfig()
		expected := []string{"description", "maker", "release_date", "director", "runtime", "trailer_url", "screenshot_urls"}
		assert.Equal(t, expected, cfg.Metadata.Completeness.Tiers.Important.Fields)
	})

	t.Run("Default nice-to-have fields match expected list", func(t *testing.T) {
		cfg := DefaultConfig()
		expected := []string{"label", "series", "rating_score", "original_title", "translations"}
		assert.Equal(t, expected, cfg.Metadata.Completeness.Tiers.NiceToHave.Fields)
	})
}

func TestCompletenessConfigJSONTags(t *testing.T) {
	t.Run("CompletenessConfig serializes with snake_case JSON keys", func(t *testing.T) {
		cc := CompletenessConfig{
			Enabled: true,
			Tiers: CompletenessTierConfig{
				Essential: CompletenessTierDefinition{
					Weight: 50,
					Fields: []string{"title"},
				},
			},
		}
		data, err := cc.MarshalJSON()
		assert.NoError(t, err)
		jsonStr := string(data)
		assert.Contains(t, jsonStr, `"enabled"`)
		assert.Contains(t, jsonStr, `"tiers"`)
		assert.Contains(t, jsonStr, `"essential"`)
		assert.Contains(t, jsonStr, `"nice_to_have"`)
		assert.Contains(t, jsonStr, `"weight"`)
		assert.Contains(t, jsonStr, `"fields"`)
	})
}
