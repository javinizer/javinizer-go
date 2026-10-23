package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestNumericPresenceRegistryConfig(t *testing.T) {
	for _, fields := range []string{"rate_limit: 0\n        retry_count: 0\n        timeout: 0", "request_delay: 0\n        max_retries: 0\n        timeout: 0"} {
		cfg := loadConfigFromYAML(t, "scrapers:\n    r18dev:\n        "+fields+"\n")
		defaults := map[string]models.ScraperSettings{"r18dev": {Enabled: true, RateLimit: 500, RetryCount: 3, Timeout: 30}}
		result := ScraperRegistryConfigFromApp(cfg, []string{"r18dev"}, defaults)
		got := result.Overrides["r18dev"]
		assert.Zero(t, got.RateLimit)
		assert.Zero(t, got.RetryCount)
		assert.Zero(t, got.Timeout)
		assert.True(t, got.Enabled)
		assert.True(t, got.HasExplicitRetryCount())
	}
}
