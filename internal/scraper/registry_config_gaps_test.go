package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestScraperRegistryConfigFromApp_V5_UnknownNameNoDefault(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	names := []string{"ghost_scraper"}
	defaults := map[string]models.ScraperSettings{}
	result := ScraperRegistryConfigFromApp(cfg, names, defaults)
	assert.Len(t, result.Overrides, 1)
	assert.Equal(t, models.ScraperSettings{}, result.Overrides["ghost_scraper"])
}
