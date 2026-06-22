package tokyohot

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for tokyohot.
// The framework calls ScraperSettings.Validate(name) as a base check first
// (which trims and lowercases Language), so this function only checks
// scraper-specific constraints.
func validateScraperSettings(ss *models.ScraperSettings) error {
	switch ss.Language {
	case "", "en", "ja", "zh":
	default:
		return fmt.Errorf("tokyohot: language must be 'en', 'ja', or 'zh', got %q", ss.Language)
	}
	if err := config.ValidateHTTPBaseURL("tokyohot.base_url", ss.BaseURL); err != nil {
		return err
	}
	return nil
}
