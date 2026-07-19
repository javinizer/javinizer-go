package config

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestUnmarshalYAML_TimeoutSecondsDecodeError(t *testing.T) {
	var sc ScrapersConfig
	err := yaml.Unmarshal([]byte("timeout_seconds: not-an-int\n"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout_seconds")
}

func TestUnmarshalYAML_RequestTimeoutSecondsDecodeError(t *testing.T) {
	var sc ScrapersConfig
	err := yaml.Unmarshal([]byte("request_timeout_seconds: not-an-int\n"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request_timeout_seconds")
}

func TestUnmarshalYAML_PriorityEmptyScalar(t *testing.T) {
	var sc ScrapersConfig
	require.NoError(t, yaml.Unmarshal([]byte("priority: \"\"\n"), &sc))
	assert.Nil(t, sc.Priority)
}

func TestUnmarshalYAML_PriorityDecodeError(t *testing.T) {
	var sc ScrapersConfig
	err := yaml.Unmarshal([]byte("priority:\n  - foo: bar\n"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "priority")
}

func TestUnmarshalYAML_FlareSolverrDecodeError(t *testing.T) {
	var sc ScrapersConfig
	err := yaml.Unmarshal([]byte("flaresolverr:\n  enabled: \"definitely-not-a-bool\"\n"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "flaresolverr")
}

func TestUnmarshalYAML_ScrapeActressDecodeError(t *testing.T) {
	var sc ScrapersConfig
	err := yaml.Unmarshal([]byte("scrape_actress: not-a-bool\n"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scrape_actress")
}

func TestUnmarshalYAML_BrowserDecodeError(t *testing.T) {
	var sc ScrapersConfig
	err := yaml.Unmarshal([]byte("browser:\n  timeout: not-an-int\n"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browser")
}

func TestUnmarshalYAML_ScraperEntryEmptyMapping(t *testing.T) {
	var sc ScrapersConfig
	require.NoError(t, yaml.Unmarshal([]byte("r18dev: {}\n"), &sc))
	assert.Nil(t, sc.Overrides["r18dev"])
}

func TestUnmarshalYAML_ScraperEntryNull(t *testing.T) {
	var sc ScrapersConfig
	require.NoError(t, yaml.Unmarshal([]byte("r18dev: null\n"), &sc))
	assert.Nil(t, sc.Overrides["r18dev"])
}

func TestUnmarshalYAML_UnknownScraperNameWithResolver(t *testing.T) {
	resolver := &staticTestConfigResolver{registered: map[string]bool{"r18dev": true}}
	var sc ScrapersConfig
	sc.resolver = resolver
	err := yaml.Unmarshal([]byte("mystery_scraper:\n  enabled: true\n"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown scraper")
}

func TestUnmarshalYAML_ScraperEntryDecodeError(t *testing.T) {
	var sc ScrapersConfig
	err := yaml.Unmarshal([]byte("r18dev:\n  timeout: not-an-int\n"), &sc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode config for scraper")
}

func TestApplyYAMLAliases_NonMappingNoOp(t *testing.T) {
	sc := &ScrapersConfig{}
	ss := &models.ScraperSettings{}
	sc.applyYAMLAliases(&yaml.Node{Kind: yaml.ScalarNode, Value: "x"}, ss)
	assert.Equal(t, 0, ss.RateLimit)
}

func TestValidateYAMLScraperKeys_NonMappingNoOp(t *testing.T) {
	sc := &ScrapersConfig{}
	assert.NoError(t, sc.validateYAMLScraperKeys("r18dev", &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}))
}

func TestScraperYAMLHasEnabledKey(t *testing.T) {
	assert.False(t, scraperYAMLHasEnabledKey(nil))
	assert.False(t, scraperYAMLHasEnabledKey(&yaml.Node{Kind: yaml.ScalarNode, Value: "x"}))
	assert.True(t, scraperYAMLHasEnabledKey(&yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Value: "enabled"},
			{Value: "true"},
		},
	}))
	assert.False(t, scraperYAMLHasEnabledKey(&yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			{Value: "rate_limit"},
			{Value: "500"},
		},
	}))
}
