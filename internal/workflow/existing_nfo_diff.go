package workflow

import "github.com/javinizer/javinizer-go/internal/models"

// IdentifyDifferences is the exported wrapper around identifyDifferences, computing
// per-field differences between the NFO, scraped, and merged movies.
func IdentifyDifferences(nfoMovie, scrapedMovie, mergedMovie *models.Movie) []FieldDifference {
	return identifyDifferences(nfoMovie, scrapedMovie, mergedMovie)
}
