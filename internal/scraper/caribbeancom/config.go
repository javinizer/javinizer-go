package caribbeancom

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for caribbeancom.
// The framework calls ScraperSettings.Validate(name) as a base check first
// (which trims and lowercases Language), so this function only checks
// scraper-specific constraints.
func validateScraperSettings(ss *models.ScraperSettings) error {
	switch ss.Language {
	case "", "ja", "en":
	default:
		return fmt.Errorf("caribbeancom: language must be 'ja' or 'en', got %q", ss.Language)
	}
	if err := config.ValidateHTTPBaseURL("caribbeancom.base_url", ss.BaseURL); err != nil {
		return err
	}
	return nil
}
