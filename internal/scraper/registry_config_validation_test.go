package scraper

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestScraperRegistryConfigFromApp_OmittedEnabledInvalidNotReverted(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		get  func(models.ScraperSettings) int
		want int
	}{
		{
			name: "negative rate_limit",
			yaml: "scrapers:\n    r18dev:\n        rate_limit: -1\n",
			get:  func(s models.ScraperSettings) int { return s.RateLimit },
			want: -1,
		},
		{
			name: "negative retry_count",
			yaml: "scrapers:\n    r18dev:\n        retry_count: -5\n",
			get:  func(s models.ScraperSettings) int { return s.RetryCount },
			want: -5,
		},
		{
			name: "negative timeout",
			yaml: "scrapers:\n    r18dev:\n        timeout: -1\n",
			get:  func(s models.ScraperSettings) int { return s.Timeout },
			want: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := loadConfigFromYAML(t, tc.yaml)
			result := ScraperRegistryConfigFromApp(cfg, []string{"r18dev"}, testEnabledDefaults())
			got := result.Overrides["r18dev"]

			assert.True(t, got.Enabled, "omitted enabled must inherit default true; the factory must not revert to the raw (false) override")
			assert.Equal(t, tc.want, tc.get(got), "invalid value must pass through unchanged — the factory performs no validation/sanitization")
			assert.Equal(t, "en", got.Language, "MergeDefaultsFrom still resolves non-enabled default fields")
		})
	}
}

func TestScraperRegistryConfigFromApp_SparseOverrideOmittingEnabledValidActivated(t *testing.T) {
	cfg := loadConfigFromYAML(t, "scrapers:\n    r18dev:\n        rate_limit: 500\n")

	result := ScraperRegistryConfigFromApp(cfg, []string{"r18dev"}, testEnabledDefaults())
	got := result.Overrides["r18dev"]
	assert.True(t, got.Enabled, "a valid sparse override omitting enabled must inherit the default and be activated")
	assert.Equal(t, 500, got.RateLimit)
	assert.Equal(t, "en", got.Language)
}

func TestScraperRegistryConfigFromApp_ExplicitFalseInvalidPassedThrough(t *testing.T) {
	cfg := loadConfigFromYAML(t, "scrapers:\n    r18dev:\n        enabled: false\n        rate_limit: -1\n")

	result := ScraperRegistryConfigFromApp(cfg, []string{"r18dev"}, testEnabledDefaults())
	got := result.Overrides["r18dev"]
	assert.False(t, got.Enabled, "explicit enabled:false is preserved")
	assert.Equal(t, -1, got.RateLimit, "invalid value passes through unchanged")
}

func TestScraperRegistryConfigFromApp_ExplicitTrueInvalidPassedThrough(t *testing.T) {
	cfg := loadConfigFromYAML(t, "scrapers:\n    r18dev:\n        enabled: true\n        rate_limit: -1\n")

	result := ScraperRegistryConfigFromApp(cfg, []string{"r18dev"}, testEnabledDefaults())
	got := result.Overrides["r18dev"]
	assert.True(t, got.Enabled, "explicit enabled:true is preserved")
	assert.Equal(t, -1, got.RateLimit, "invalid value passes through unchanged")
}

func TestScraperRegistryConfigFromApp_DefaultDisabledScraperOmittedEnabledStaysDisabled(t *testing.T) {
	cfg := loadConfigFromYAML(t, "scrapers:\n    dmm:\n        rate_limit: -1\n")

	result := ScraperRegistryConfigFromApp(cfg, []string{"dmm"}, testEnabledDefaults())
	got := result.Overrides["dmm"]
	assert.False(t, got.Enabled, "a default-disabled scraper with an omitted enabled stays disabled")
	assert.Equal(t, -1, got.RateLimit, "invalid value passes through unchanged")
}
