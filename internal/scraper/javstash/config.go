package javstash

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for javstash.
func validateScraperSettings(ss *models.ScraperSettings) error {
	if !ss.Enabled {
		return nil
	}
	apiKey := strings.TrimSpace(ss.APIKey)
	if apiKey == "" {
		return fmt.Errorf("javstash: api_key is required (set in config)")
	}
	if err := config.ValidateScraperBaseURL("javstash.base_url", ss.BaseURL, []string{
		"javstash.org", "www.javstash.org",
	}); err != nil {
		return err
	}
	return nil
}
