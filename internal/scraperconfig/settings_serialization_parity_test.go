package scraperconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestScraperSettingsJSONYAMLPreserveTheSamePresence(t *testing.T) {
	const input = `
fixture:
  enabled: false
  timeout: 0
  language: ja
  scrape_actress: false
  respect_retry_after: false
  placeholder_threshold: 4
  extra_placeholder_hashes: [fixture-hash]
`
	const want = `{
	"fixture": {
		"enabled": false, "language": "ja", "timeout": 0, "user_agent": "",
		"use_flaresolverr": false, "use_browser": false, "scrape_actress": false,
		"respect_retry_after": false, "placeholder_threshold": 4,
		"extra_placeholder_hashes": ["fixture-hash"]
	}
}`
	var settings map[string]ScraperSettings
	require.NoError(t, yaml.Unmarshal([]byte(input), &settings))
	jsonBytes, err := json.Marshal(settings)
	require.NoError(t, err)
	require.JSONEq(t, want, string(jsonBytes))

	yamlBytes, err := yaml.Marshal(settings)
	require.NoError(t, err)
	var yamlShape map[string]any
	require.NoError(t, yaml.Unmarshal(yamlBytes, &yamlShape))
	yamlAsJSON, err := json.Marshal(yamlShape)
	require.NoError(t, err)
	require.JSONEq(t, want, string(yamlAsJSON))
}
