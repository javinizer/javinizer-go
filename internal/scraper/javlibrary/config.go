package javlibrary

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for javlibrary.
// The framework calls ScraperSettings.Validate(name) as a base check first
// (which trims and lowercases Language), so this function only checks
// scraper-specific constraints.
func validateScraperSettings(ss *models.ScraperSettings) error {
	switch ss.Language {
	case "", "en", "ja", "cn", "tw":
	default:
		return fmt.Errorf("javlibrary: language must be 'en', 'ja', 'cn', or 'tw', got %q", ss.Language)
	}
	if err := config.ValidateScraperBaseURL("javlibrary.base_url", ss.BaseURL, []string{
		"www.javlibrary.com", "javlibrary.com", "www.javlibrary.org", "javlibrary.org",
		"www.javlibrary.jp", "javlibrary.jp",
	}); err != nil {
		return err
	}
	return nil
}
