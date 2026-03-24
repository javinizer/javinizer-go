package movie

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// identifyDifferences compares NFO, scraped, and merged data to identify key differences
func identifyDifferences(nfoMovie, scrapedMovie, mergedMovie *models.Movie) []FieldDifference {
	diffs := []FieldDifference{}

	// Compare basic string fields
	if nfoMovie.Title != scrapedMovie.Title {
		diffs = append(diffs, FieldDifference{
			Field:        "title",
			NFOValue:     nfoMovie.Title,
			ScrapedValue: scrapedMovie.Title,
			MergedValue:  mergedMovie.Title,
		})
	}

	if nfoMovie.Description != scrapedMovie.Description {
		diffs = append(diffs, FieldDifference{
			Field:        "description",
			NFOValue:     nfoMovie.Description,
			ScrapedValue: scrapedMovie.Description,
			MergedValue:  mergedMovie.Description,
		})
	}

	if nfoMovie.Director != scrapedMovie.Director {
		diffs = append(diffs, FieldDifference{
			Field:        "director",
			NFOValue:     nfoMovie.Director,
			ScrapedValue: scrapedMovie.Director,
			MergedValue:  mergedMovie.Director,
		})
	}

	if nfoMovie.Maker != scrapedMovie.Maker {
		diffs = append(diffs, FieldDifference{
			Field:        "maker",
			NFOValue:     nfoMovie.Maker,
			ScrapedValue: scrapedMovie.Maker,
			MergedValue:  mergedMovie.Maker,
		})
	}

	// Compare numeric fields
	if nfoMovie.Runtime != scrapedMovie.Runtime {
		diffs = append(diffs, FieldDifference{
			Field:        "runtime",
			NFOValue:     nfoMovie.Runtime,
			ScrapedValue: scrapedMovie.Runtime,
			MergedValue:  mergedMovie.Runtime,
		})
	}

	// Compare array lengths as a proxy for content differences
	if len(nfoMovie.Actresses) != len(scrapedMovie.Actresses) {
		diffs = append(diffs, FieldDifference{
			Field:        "actresses",
			NFOValue:     fmt.Sprintf("%d actresses", len(nfoMovie.Actresses)),
			ScrapedValue: fmt.Sprintf("%d actresses", len(scrapedMovie.Actresses)),
			MergedValue:  fmt.Sprintf("%d actresses", len(mergedMovie.Actresses)),
		})
	}

	if len(nfoMovie.Genres) != len(scrapedMovie.Genres) {
		diffs = append(diffs, FieldDifference{
			Field:        "genres",
			NFOValue:     fmt.Sprintf("%d genres", len(nfoMovie.Genres)),
			ScrapedValue: fmt.Sprintf("%d genres", len(scrapedMovie.Genres)),
			MergedValue:  fmt.Sprintf("%d genres", len(mergedMovie.Genres)),
		})
	}

	return diffs
}
