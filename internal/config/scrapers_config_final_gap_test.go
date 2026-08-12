package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestScraperConfigFinalGap_MaxRetriesYAMLError(t *testing.T) {
	input := `scrapers:
    r18dev:
        max_retries: not_a_number
`
	var cfg Config
	cfg.Scrapers.resolver = newEnabledResolver()
	err := yaml.Unmarshal([]byte(input), &cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_retries")
}
