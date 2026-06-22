package jav321

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for jav321.
func validateScraperSettings(ss *models.ScraperSettings) error {
	if err := config.ValidateHTTPBaseURL("jav321.base_url", ss.BaseURL); err != nil {
		return err
	}
	return nil
}
