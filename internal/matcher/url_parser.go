package matcher

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ParsedInput represents the result of parsing user input
type ParsedInput struct {
	ID                 string   // Extracted movie ID
	ScraperHint        string   // Suggested scraper ("dmm", "r18dev", or "")
	IsURL              bool     // true if input was a URL
	CompatibleScrapers []string // List of scrapers that can handle this URL (if IsURL)
}

// ParseInput determines if input is a URL or ID and extracts the movie ID.
// The parser is agnostic about URL patterns - it delegates URL detection to scrapers
// that implement the URLHandler interface. If no scraper handles the URL, the input
// is treated as a plain movie ID.
//
// When input is a URL, the function also returns the list of all compatible scrapers
// that can handle the URL, avoiding redundant registry iteration in callers.
func ParseInput(input string, registry *models.ScraperRegistry) (*ParsedInput, error) {
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

	if registry != nil {
		for _, scraper := range registry.GetAll() {
			// Defensive nil check - registry should never contain nil scrapers, but be safe
			if scraper == nil {
				continue
			}
			if handler, ok := scraper.(models.URLHandler); ok && scraper.IsEnabled() {
				if handler.CanHandleURL(input) {
					compatibleScrapers = append(compatibleScrapers, scraper.Name())
					// Extract ID from first match, but continue collecting all compatible scrapers
					if !matched {
						id, err := handler.ExtractIDFromURL(input)
						if err == nil {
							matchedID = id
							matchedScraper = scraper.Name()
							matched = true
						}
					}
				}
			}
		}
	}

	// If at least one scraper matched, return as URL
	if matched {
		return &ParsedInput{
			ID:                 matchedID,
			ScraperHint:        matchedScraper,
			IsURL:              true,
			CompatibleScrapers: compatibleScrapers,
		}, nil
	}

	// No scraper handles this URL - treat as plain movie ID
	return &ParsedInput{
		ID:                 input,
		ScraperHint:        "",
		IsURL:              false,
		CompatibleScrapers: nil,
	}, nil
}

// FilterScrapersForURL filters a list of scrapers to only those compatible with a parsed URL.
// This helper is used by API endpoints to optimize scraper selection when URL is detected.
//
// Parameters:
//   - userScrapers: User's selected scrapers (can be empty to use all compatible)
//   - parsed: Result from ParseInput containing CompatibleScrapers
//
// Returns filtered scrapers or all compatible scrapers if userScrapers is empty.
// If no compatible scrapers exist, returns empty slice (caller should handle this case).
func FilterScrapersForURL(userScrapers []string, parsed *ParsedInput) []string {
	if !parsed.IsURL || len(parsed.CompatibleScrapers) == 0 {
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

// ReorderWithPriority moves the priority scraper to the front of the list.
// This is useful when multiple compatible scrapers exist for a URL - the hinted
// scraper should be tried first for best performance.
//
// Parameters:
//   - scrapers: List of scraper names
//   - priority: Scraper name to move to front
//
// Returns reordered list with priority scraper first.
// If scrapers is empty, returns a single-item list with just the priority scraper.
func ReorderWithPriority(scrapers []string, priority string) []string {
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
	parsed *ParsedInput,
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
	filteredScrapers := FilterScrapersForURL(scrapersToUse, parsed)
	if len(filteredScrapers) > 0 {
		scrapersToUse = filteredScrapers

		// Step 4: If user didn't specify custom scrapers, prioritize hinted scraper
		if len(requestScrapers) == 0 && parsed.ScraperHint != "" {
			scrapersToUse = ReorderWithPriority(scrapersToUse, parsed.ScraperHint)
		}
	}

	return scrapersToUse
}
