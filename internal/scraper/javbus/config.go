package javbus

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for javbus.
// The framework calls ScraperSettings.Validate(name) as a base check first
// (which trims and lowercases Language), so this function only checks
// scraper-specific constraints.
func validateScraperSettings(ss *models.ScraperSettings) error {
	switch ss.Language {
	case "", "en", "ja", "zh":
	default:
		return fmt.Errorf("javbus: language must be 'en', 'ja', or 'zh', got %q", ss.Language)
	}
	if err := config.ValidateScraperBaseURL("javbus.base_url", ss.BaseURL, []string{
		"www.javbus.com", "javbus.com", "www.javbus.org", "javbus.org",
		"www.javbus0.com", "javbus0.com",
	}); err != nil {
		return err
	}
	return nil
}
