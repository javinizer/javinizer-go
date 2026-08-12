package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestScrapersConfig_YAMLMaxRetriesDecodeError(t *testing.T) {
	yamlStr := "scrapers:\n  dmm:\n    max_retries: not_a_number\n"
	var cfg ScrapersConfig
	err := yaml.Unmarshal([]byte(yamlStr), &cfg)
	assert.Error(t, err)
}
