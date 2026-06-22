package r18dev

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for r18dev.
// The framework calls ScraperSettings.Validate(name) as a base check first
// (which trims and lowercases Language), so this function only checks
// scraper-specific constraints.
func validateScraperSettings(ss *models.ScraperSettings) error {
	switch ss.Language {
	case "", "en", "ja":
	default:
		return fmt.Errorf("r18dev: language must be 'en' or 'ja', got %q", ss.Language)
	}
	return nil
}
