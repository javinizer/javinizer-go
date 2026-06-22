package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromAppConfig_Scrape(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		assert.Nil(t, ConfigFromAppConfig(nil))
	})

	t.Run("extracts scrape config", func(t *testing.T) {
		cfg := &config.Config{
			Scrapers: config.ScrapersConfig{
				Priority:      []string{"r18dev", "javdb"},
				UserAgent:     "test-agent",
				Referer:       "https://example.com",
				ScrapeActress: true,
			},
			Metadata: config.MetadataConfig{
				Translation: config.TranslationConfig{
					Enabled:        true,
					TargetLanguage: "ja",
				},
				ActressDatabase: config.ActressDatabaseConfig{
					Enabled: true,
				},
			},
		}
		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.Equal(t, []string{"r18dev", "javdb"}, result.ScrapersPriority)
		assert.True(t, result.TranslationEnabled)
		assert.Equal(t, "ja", result.TranslationTargetLang)
		assert.True(t, result.ScrapeActress)
	})

	t.Run("translation disabled omits hash", func(t *testing.T) {
		cfg := &config.Config{
			Metadata: config.MetadataConfig{
				Translation: config.TranslationConfig{
					Enabled: false,
				},
			},
		}
		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.Empty(t, result.TranslationSettingsHash)
	})
}
