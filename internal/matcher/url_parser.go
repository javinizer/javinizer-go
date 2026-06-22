package matcher

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// URLScraperLister is the narrow interface the URL parser requires from the
// scraper registry. It needs only the ability to iterate all scraper instances
// to check URL handling capability. Defined here per Go convention: consume
// interfaces, produce structs.
type URLScraperLister interface {
	GetAllInstances() []models.Scraper
}

// parsedInput represents the result of parsing user input
type parsedInput struct {
	ID                 string   // Extracted movie ID
	ScraperHint        string   // Suggested scraper ("dmm", "r18dev", or "")
	IsURL              bool     // true if input was a URL
	CompatibleScrapers []string // List of scrapers that can handle this URL (if IsURL)
}

// isNilInterface checks whether an interface value is nil or wraps a nil
// underlying pointer. This handles Go's nil-interface pitfall where a typed
// nil pointer (e.g., (*ScraperRegistry)(nil)) wrapped in an interface is
// non-nil by interface comparison but will panic on method calls.
func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// ParseInput determines if input is a URL or ID and extracts the movie ID.
// The parser is agnostic about URL patterns - it delegates URL detection to scrapers
// that implement the Scraper interface. If no scraper handles the URL, the input
// is treated as a plain movie ID.
//
// When input is a URL, the function also returns the list of all compatible scrapers
// that can handle the URL, avoiding redundant registry iteration in callers.
func ParseInput(input string, registry URLScraperLister) (*parsedInput, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("input cannot be empty")
	}

	// Query scrapers to see if any can handle this URL
	// Collect all compatible scrapers to avoid redundant iteration in callers
	var compatibleScrapers []string
	var matchedID string
	var matchedScraper string
	var matched bool

	// Track if any scraper claimed to handle the URL but failed extraction
	var claimedButFailed bool
	var firstFailedScraper string
	var firstFailedErr error

	if !isNilInterface(registry) {
		for _, scraper := range registry.GetAllInstances() {
			if scraper == nil || !scraper.IsEnabled() {
				continue
			}
			handler, ok := scraper.(models.URLHandler)
			if !ok || !handler.CanHandleURL(input) {
				continue
			}
			compatibleScrapers = append(compatibleScrapers, scraper.Name())
			if !matched {
				id, err := handler.ExtractIDFromURL(input)
				if err == nil {
					matchedID = id
					matchedScraper = scraper.Name()
					matched = true
				} else if !claimedButFailed {
					claimedButFailed = true
					firstFailedScraper = scraper.Name()
					firstFailedErr = err
				}
			}
		}
	}

	// If at least one scraper matched, return as URL
	if matched {
		return &parsedInput{
			ID:                 matchedID,
			ScraperHint:        matchedScraper,
			IsURL:              true,
			CompatibleScrapers: compatibleScrapers,
		}, nil
	}

	// If no scraper extracted ID but some claimed to handle, return error
	if claimedButFailed {
		return nil, fmt.Errorf("URL matched scraper %q but extraction failed: %w",
			firstFailedScraper, firstFailedErr)
	}

	// No scraper handles this URL - treat as plain movie ID
	return &parsedInput{
		ID:                 input,
		ScraperHint:        "",
		IsURL:              false,
		CompatibleScrapers: nil,
	}, nil
}

// filterScrapersForURL filters a list of scrapers to only those compatible with a parsed URL.
// This helper is used by API endpoints to optimize scraper selection when URL is detected.
//
// Parameters:
//   - userScrapers: User's selected scrapers (can be empty to use all compatible)
//   - parsed: Result from ParseInput containing CompatibleScrapers
//
// Returns filtered scrapers or all compatible scrapers if userScrapers is empty.
// If no compatible scrapers exist, returns empty slice (caller should handle this case).
func filterScrapersForURL(userScrapers []string, parsed *parsedInput) []string {
	if parsed == nil || !parsed.IsURL || len(parsed.CompatibleScrapers) == 0 {
		return userScrapers
	}

	// If user didn't specify scrapers, use all compatible ones
	if len(userScrapers) == 0 {
		return parsed.CompatibleScrapers
	}

	// Filter user's scrapers to only URL-compatible ones
	var filtered []string
	for _, userScraper := range userScrapers {
		for _, compatibleScraper := range parsed.CompatibleScrapers {
			if userScraper == compatibleScraper {
				filtered = append(filtered, userScraper)
				break
			}
		}
	}

	// If no user scrapers are compatible, fall back to all compatible scrapers
	if len(filtered) == 0 {
		return parsed.CompatibleScrapers
	}

	return filtered
}

// reorderWithPriority moves the priority scraper to the front of the list.
// This is useful when multiple compatible scrapers exist for a URL - the hinted
// scraper should be tried first for best performance.
//
// Parameters:
//   - scrapers: List of scraper names
//   - priority: Scraper name to move to front
//
// Returns reordered list with priority scraper first.
// If scrapers is empty, returns a single-item list with just the priority scraper.
func reorderWithPriority(scrapers []string, priority string) []string {
	if priority == "" {
		return scrapers
	}

	// If scrapers is empty, return just the priority
	if len(scrapers) == 0 {
		return []string{priority}
	}

	result := []string{priority}
	for _, s := range scrapers {
		if s != priority {
			result = append(result, s)
		}
	}
	return result
}

// CalculateOptimalScrapers determines the optimal scraper list for a given input.
// This consolidates the scraper selection logic used by both /scrape and /rescrape endpoints,
// ensuring consistent behavior and preventing logic drift.
//
// The function applies two optimizations when a URL is detected:
// 1. FILTER: Reduces scraper list to only URL-compatible scrapers
// 2. REORDER: Places hinted scraper first for best performance
//
// Parameters:
//   - requestScrapers: User's explicitly selected scrapers (can be empty)
//   - configPriority: Default scraper priority from configuration
//   - parsed: Result from ParseInput containing URL detection info (can be nil)
//
// Returns the optimized scraper list to use for scraping.
func CalculateOptimalScrapers(
	requestScrapers []string,
	configPriority []string,
	parsed *parsedInput,
) []string {
	// Step 1: Start with user's selection or config default
	scrapersToUse := configPriority
	if len(requestScrapers) > 0 {
		scrapersToUse = requestScrapers
	}

	// Step 2: If parsed is nil or not a URL, return current selection
	if parsed == nil || !parsed.IsURL || len(parsed.CompatibleScrapers) == 0 {
		return scrapersToUse
	}

	// Step 3: Filter to compatible scrapers
	filteredScrapers := filterScrapersForURL(scrapersToUse, parsed)
	if len(filteredScrapers) > 0 {
		scrapersToUse = filteredScrapers

		// Step 4: If user didn't specify custom scrapers, use config priority for hint
		if len(requestScrapers) == 0 && len(configPriority) > 0 {
			// Find highest priority scraper that is compatible with the URL
			for _, prioScraper := range configPriority {
				for _, compat := range parsed.CompatibleScrapers {
					if prioScraper == compat {
						// Reorder with the highest priority compatible scraper as hint
						scrapersToUse = reorderWithPriority(scrapersToUse, prioScraper)
						return scrapersToUse
					}
				}
			}
		}
	}

	return scrapersToUse
}
