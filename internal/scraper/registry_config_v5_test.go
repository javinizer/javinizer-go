package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestScraperRegistryConfigFromApp_V5_Basic(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	names := []string{"r18dev", "dmm"}
	defaults := map[string]models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "en"},
		"dmm":    {Enabled: true, Language: "ja"},
	}
	result := ScraperRegistryConfigFromApp(cfg, names, defaults)

	assert.NotNil(t, result.Overrides)
	assert.Len(t, result.Overrides, 2)
	assert.Equal(t, "en", result.Overrides["r18dev"].Language)
	assert.Equal(t, "ja", result.Overrides["dmm"].Language)
	assert.Equal(t, cfg.Scrapers.Proxy, result.GlobalProxy)
	assert.Equal(t, cfg.Scrapers.FlareSolverr, result.FlareSolverr)
	assert.Equal(t, cfg.Scrapers.Browser, result.Browser)
	assert.Equal(t, cfg.Scrapers.TimeoutSeconds, result.TimeoutSeconds)
	assert.Equal(t, cfg.Scrapers.ScrapeActress, result.ScrapeActress)
}

func TestScraperRegistryConfigFromApp_V5_WithOverrides(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	r18Enabled := true
	dmmEnabled := false
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": {Enabled: r18Enabled},
		"dmm":    {Enabled: dmmEnabled},
	}
	names := []string{"r18dev", "dmm"}
	defaults := map[string]models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "en"},
		"dmm":    {Enabled: true, Language: "ja"},
	}

	result := ScraperRegistryConfigFromApp(cfg, names, defaults)
	assert.Len(t, result.Overrides, 2)
	assert.True(t, result.Overrides["r18dev"].Enabled)
	assert.False(t, result.Overrides["dmm"].Enabled)
	assert.Equal(t, "en", result.Overrides["r18dev"].Language)
	assert.Equal(t, "ja", result.Overrides["dmm"].Language)
}

func TestScraperRegistryConfigFromApp_V5_NilOverrides(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	enabled := true
	cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"r18dev": nil, // nil override falls back to module defaults
		"dmm":    {Enabled: enabled},
	}
	names := []string{"r18dev", "dmm"}
	defaults := map[string]models.ScraperSettings{
		"r18dev": {Enabled: true, Language: "en"},
		"dmm":    {Language: "ja"},
	}

	result := ScraperRegistryConfigFromApp(cfg, names, defaults)
	assert.Len(t, result.Overrides, 2)
	assert.True(t, result.Overrides["r18dev"].Enabled)
	assert.Equal(t, "en", result.Overrides["r18dev"].Language)
	assert.True(t, result.Overrides["dmm"].Enabled)
	assert.Equal(t, "ja", result.Overrides["dmm"].Language)
}
