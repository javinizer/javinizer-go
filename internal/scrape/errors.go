package scrape

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

func classifyFailures(failures []models.ScraperError) models.ScraperErrorKind {
	if len(failures) == 0 {
		return models.ScraperErrorKindUnknown
	}
	precedence := []models.ScraperErrorKind{
		models.ScraperErrorKindNotFound,
		models.ScraperErrorKindBlocked,
		models.ScraperErrorKindRateLimited,
		models.ScraperErrorKindUnavailable,
		models.ScraperErrorKindUnknown,
	}
	present := make(map[models.ScraperErrorKind]bool, len(failures))
	for _, f := range failures {
		k := f.Kind
		if k == "" {
			k = models.ScraperErrorKindUnknown
		}
		present[k] = true
	}
	for _, k := range precedence {
		if present[k] {
			return k
		}
	}
	return models.ScraperErrorKindUnknown
}

func buildNoResultsError(failures []models.ScraperError) string {
	if len(failures) == 0 {
		return "No results from any scraper"
	}
	errMsg := "No results from any scraper: "
	errs := make([]string, 0, len(failures))
	for _, f := range failures {
		msg := strings.TrimSpace(f.Message)
		if msg == "" && f.Cause != nil {
			msg = f.Cause.Error()
		}
		if msg != "" {
			errs = append(errs, fmt.Sprintf("%s: %s", f.Scraper, msg))
		} else {
			errs = append(errs, f.Scraper+": no result")
		}
	}
	return errMsg + strings.Join(errs, "; ")
}
