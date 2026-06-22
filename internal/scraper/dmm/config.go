package dmm

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for dmm.
func validateScraperSettings(ss *models.ScraperSettings) error {
	if err := config.ValidateHTTPBaseURL("dmm.base_url", ss.BaseURL); err != nil {
		return err
	}
	return nil
}
