package dmm

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for dmm.
func validateScraperSettings(ss *models.ScraperSettings) error {
	// Constrain dmm.base_url to DMM/FANZA hosts. A generic HTTP check would let
	// a user-set base_url steer egress to an arbitrary host; the source allow-list
	// keeps outbound scraper traffic within DMM/FANZA domains.
	if err := config.ValidateScraperBaseURL("dmm.base_url", ss.BaseURL, []string{
		"www.dmm.co.jp", "dmm.co.jp", "video.dmm.co.jp", "www.dmm.com", "dmm.com",
		"pics.dmm.co.jp", "www.libredmm.com", "libredmm.com",
	}); err != nil {
		return err
	}
	return nil
}
