package aventertainment

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for aventertainment.
// The framework calls ScraperSettings.Validate(name) as a base check first
// (which trims and lowercases Language), so this function only checks
// scraper-specific constraints.
func validateScraperSettings(ss *models.ScraperSettings) error {
	switch ss.Language {
	case "", "en", "ja":
	default:
		return fmt.Errorf("aventertainment: language must be 'en' or 'ja', got %q", ss.Language)
	}
	if err := config.ValidateHTTPBaseURL("aventertainment.base_url", ss.BaseURL); err != nil {
		return err
	}
	return nil
}
