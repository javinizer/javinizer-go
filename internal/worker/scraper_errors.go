package worker

import (
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

type scraperErrorKind int

const (
	scraperErrorOther scraperErrorKind = iota
	scraperErrorNotFound
	scraperErrorUnavailable
	scraperErrorRateLimited
	scraperErrorBlocked
)

type scraperFailure struct {
	Scraper string
	Err     error
}

func buildScraperNoResultsError(failures []scraperFailure) string {
	if len(failures) == 0 {
		return "Movie lookup failed: no scraper results"
	}

	details := make([]string, 0, len(failures))
	hasAvailabilityIssues := false
	allNotFound := true

	for _, failure := range failures {
		kind, _, _ := classifyScraperError(failure.Err)
		if kind != scraperErrorNotFound {
			allNotFound = false
		}
		if kind == scraperErrorUnavailable || kind == scraperErrorRateLimited || kind == scraperErrorBlocked {
			hasAvailabilityIssues = true
		}
		details = append(details, formatScraperFailure(failure.Scraper, failure.Err))
	}

	prefix := "Movie lookup failed across scrapers"
	if allNotFound {
		prefix = "Movie not found on configured scrapers"
	} else if hasAvailabilityIssues {
		prefix = "Movie lookup failed due to source availability issues"
	}

	return fmt.Sprintf("%s: %s", prefix, strings.Join(details, "; "))
}

func formatScraperFailure(scraperName string, err error) string {
	if err == nil {
		return fmt.Sprintf("%s: scraper failed with unknown error", scraperName)
	}

	if scraperErr, ok := models.AsScraperError(err); ok {
		raw := strings.TrimSpace(scraperErr.Error())
		if raw != "" {
			return fmt.Sprintf("%s: %s", scraperName, raw)
		}
	}

	kind, statusCode, raw := classifyScraperError(err)

	switch kind {
	case scraperErrorNotFound:
		return fmt.Sprintf("%s: movie not found on source (details: %s)", scraperName, raw)
	case scraperErrorRateLimited:
		if statusCode > 0 {
			return fmt.Sprintf("%s: source rate-limited request (HTTP %d; details: %s)", scraperName, statusCode, raw)
		}
		return fmt.Sprintf("%s: source rate-limited request (details: %s)", scraperName, raw)
	case scraperErrorBlocked:
		if statusCode > 0 {
			return fmt.Sprintf("%s: source blocked access (HTTP %d; geo/IP/challenge restrictions possible; details: %s)", scraperName, statusCode, raw)
		}
		return fmt.Sprintf("%s: source blocked access (geo/IP/challenge restrictions possible; details: %s)", scraperName, raw)
	case scraperErrorUnavailable:
		if statusCode == 502 {
			return fmt.Sprintf("%s: source temporarily unavailable (HTTP 502 Bad Gateway; host may be down; details: %s)", scraperName, raw)
		}
		if statusCode > 0 {
			return fmt.Sprintf("%s: source temporarily unavailable (HTTP %d; details: %s)", scraperName, statusCode, raw)
		}
		return fmt.Sprintf("%s: source temporarily unavailable (details: %s)", scraperName, raw)
	default:
		if statusCode > 0 {
			return fmt.Sprintf("%s: scraper request failed (HTTP %d; details: %s)", scraperName, statusCode, raw)
		}
		return fmt.Sprintf("%s: scraper request failed (details: %s)", scraperName, raw)
	}
}

func classifyScraperError(err error) (scraperErrorKind, int, string) {
	if err == nil {
		return scraperErrorOther, 0, ""
	}

	var scraperErr *models.ScraperError
	if errors.As(err, &scraperErr) && scraperErr != nil {
		switch scraperErr.Kind {
		case models.ScraperErrorKindNotFound:
			return scraperErrorNotFound, scraperErr.StatusCode, scraperErr.Error()
		case models.ScraperErrorKindUnavailable:
			return scraperErrorUnavailable, scraperErr.StatusCode, scraperErr.Error()
		case models.ScraperErrorKindRateLimited:
			return scraperErrorRateLimited, scraperErr.StatusCode, scraperErr.Error()
		case models.ScraperErrorKindBlocked:
			return scraperErrorBlocked, scraperErr.StatusCode, scraperErr.Error()
		default:
			if scraperErr.StatusCode > 0 {
				switch {
				case scraperErr.StatusCode == 404:
					return scraperErrorNotFound, scraperErr.StatusCode, scraperErr.Error()
				case scraperErr.StatusCode == 429:
					return scraperErrorRateLimited, scraperErr.StatusCode, scraperErr.Error()
				case scraperErr.StatusCode == 403 || scraperErr.StatusCode == 451:
					return scraperErrorBlocked, scraperErr.StatusCode, scraperErr.Error()
				case scraperErr.StatusCode >= 500 && scraperErr.StatusCode <= 599:
					return scraperErrorUnavailable, scraperErr.StatusCode, scraperErr.Error()
				}
			}
			return scraperErrorOther, scraperErr.StatusCode, scraperErr.Error()
		}
	}

	var httpErr *models.ScraperHTTPError
	if errors.As(err, &httpErr) && httpErr != nil {
		statusCode := httpErr.StatusCode
		raw := httpErr.Error()
		switch {
		case statusCode == 404:
			return scraperErrorNotFound, statusCode, raw
		case statusCode == 429:
			return scraperErrorRateLimited, statusCode, raw
		case statusCode == 403 || statusCode == 451:
			return scraperErrorBlocked, statusCode, raw
		case statusCode >= 500 && statusCode <= 599:
			return scraperErrorUnavailable, statusCode, raw
		default:
			return scraperErrorOther, statusCode, raw
		}
	}

	raw := strings.TrimSpace(err.Error())
	return scraperErrorOther, 0, raw
}
