package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfigYAMLUsesRawScraperShapeWithoutMutation(t *testing.T) {
	t.Parallel()
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte("scrapers:\n  r18dev:\n    rate_limit: 0\n  dmm:\n    enabled: false\n    rate_limit: 250\n"), &cfg))
	require.NoError(t, cfg.Scrapers.Finalize(newEnabledResolver()))
	before := cfg.Clone()
	direct, err := yaml.Marshal(&cfg.Scrapers)
	require.NoError(t, err)
	full, err := yaml.Marshal(&cfg)
	require.NoError(t, err)
	var directShape map[string]any
	var fullShape map[string]any
	require.NoError(t, yaml.Unmarshal(direct, &directShape))
	require.NoError(t, yaml.Unmarshal(full, &fullShape))
	require.Equal(t, directShape, fullShape["scrapers"])
	r18 := directShape["r18dev"].(map[string]any)
	require.Equal(t, false, r18["enabled"])
	effective, err := json.Marshal(&cfg.Scrapers)
	require.NoError(t, err)
	var effectiveShape map[string]any
	require.NoError(t, json.Unmarshal(effective, &effectiveShape))
	require.Equal(t, true, effectiveShape["r18dev"].(map[string]any)["enabled"])
	require.Equal(t, 0, r18["rate_limit"])
	dmm := directShape["dmm"].(map[string]any)
	require.Equal(t, false, dmm["enabled"])
	require.Equal(t, before, cfg.Clone())
	var restored Config
	require.NoError(t, yaml.Unmarshal(full, &restored))
	restoredYAML, err := yaml.Marshal(&restored.Scrapers)
	require.NoError(t, err)
	require.Equal(t, string(direct), string(restoredYAML))
}
