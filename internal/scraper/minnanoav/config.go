package minnanoav

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

func validateScraperSettings(ss *models.ScraperSettings) error {
	if err := config.ValidateHTTPBaseURL("minnanoav.base_url", ss.BaseURL); err != nil {
		return err
	}
	return nil
}
