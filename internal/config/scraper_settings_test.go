package config

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestScraperSettings_MarshalYAML(t *testing.T) {
	t.Parallel()

	t.Run("nil_pointer_serialization", func(t *testing.T) {
		// Value receivers preserve numeric presence for settings stored by
		// value (including map elements). Nil pointers use the encoders'
		// standard null handling, not a direct method call on a nil value.
		var s *models.ScraperSettings
		result, err := yaml.Marshal(s)
		assert.NoError(t, err)
		assert.Equal(t, "null\n", string(result))
		result, err = json.Marshal(s)
		assert.NoError(t, err)
		assert.Equal(t, "null", string(result))
	})

	t.Run("with_typed_fields", func(t *testing.T) {
		scrapeActress := true
		s := &models.ScraperSettings{
			Enabled:                true,
			Language:               "en",
			Timeout:                30,
			RateLimit:              1000,
			RetryCount:             3,
			UserAgent:              "test-agent",
			BaseURL:                "https://example.com",
			UseFlareSolverr:        true,
			UseBrowser:             true,
			ScrapeActress:          &scrapeActress,
			Cookies:                map[string]string{"session": "abc123"},
			PlaceholderThresholdKB: 15,
			APIKey:                 "test-key",
		}

		result, err := s.MarshalYAML()
		assert.NoError(t, err)
		assert.NotNil(t, result)

		resultMap := result.(map[string]any)
		assert.Equal(t, true, resultMap["enabled"])
		assert.Equal(t, "en", resultMap["language"])
		assert.Equal(t, 30, resultMap["timeout"])
		assert.Equal(t, 15, resultMap["placeholder_threshold"])
		assert.Equal(t, "test-key", resultMap["api_key"])
		assert.Equal(t, true, resultMap["use_browser"])
		assert.Equal(t, true, resultMap["scrape_actress"])
	})
}

func TestScraperSettings_MarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		s := &models.ScraperSettings{
			Enabled:   true,
			Language:  "en",
			Timeout:   30,
			UserAgent: "test-agent",
		}

		data, err := s.MarshalJSON()
		assert.NoError(t, err)
		assert.Contains(t, string(data), `"enabled":true`)
		assert.Contains(t, string(data), `"language":"en"`)
	})
}

// NOTE: Tests for GetBaseURL, SetScrapeActress, ShouldScrapeActress, ShouldUseBrowser
// have been removed — these methods were deleted from models.ScraperSettings (dead code).
// The authoritative implementations live on models.ScraperSettings and are tested there.
