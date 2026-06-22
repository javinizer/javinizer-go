package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigFromAppConfig_ScrapersPriorityDefensiveCopy(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm", "javlibrary"},
		},
	}

	result := ConfigFromAppConfig(cfg)

	// Verify initial values are copied correctly
	assert.Equal(t, []string{"r18dev", "dmm", "javlibrary"}, result.ScrapersPriority)

	// Mutate the original slice — the returned config must NOT be affected
	cfg.Scrapers.Priority[0] = "mutated"

	assert.Equal(t, []string{"r18dev", "dmm", "javlibrary"}, result.ScrapersPriority,
		"ConfigFromAppConfig should return a defensive copy of ScrapersPriority; mutating the source must not corrupt the result")
}

func TestConfigFromAppConfig_NilInput(t *testing.T) {
	result := ConfigFromAppConfig(nil)
	assert.Nil(t, result)
}

func TestConfigFromAppConfig_NilScrapersPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: nil,
		},
	}

	result := ConfigFromAppConfig(cfg)
	assert.Nil(t, result.ScrapersPriority)
}

func TestConfigFromAppConfig_EmptyScrapersPriority(t *testing.T) {
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{},
		},
	}

	result := ConfigFromAppConfig(cfg)
	assert.Empty(t, result.ScrapersPriority)
	cfg.Scrapers.Priority = append(cfg.Scrapers.Priority, "new")
	assert.Empty(t, result.ScrapersPriority)
}
