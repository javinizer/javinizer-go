package mgstage

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

// validateScraperSettings performs scraper-specific validation for mgstage.
// mgstage has no scraper-specific constraints beyond the base checks.
func validateScraperSettings(ss *models.ScraperSettings) error {
	return nil
}
