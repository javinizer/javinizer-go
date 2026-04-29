package aggregator

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

// buildTranslations creates MovieTranslation records from scraper results
// Only includes a scraper's translation if that scraper contributed to at least
// one of the aggregated movie's fields (Title, OriginalTitle, or Description).
// This ensures buildTranslations only captures translations from scrapers that
// actually won the priority merge, preventing duplicate language entries.
func (a *Aggregator) buildTranslations(results []*models.ScraperResult, movie *models.Movie) []models.MovieTranslation {
	translations := make([]models.MovieTranslation, 0, len(results))

	for _, result := range results {
		// First, process any translations provided by the scraper (e.g., R18.dev provides both EN and JA)
		if len(result.Translations) > 0 {
			for _, trans := range result.Translations {
				// Check if this translation language is already added
				existingIdx := -1
				for i, existing := range translations {
					if existing.Language == trans.Language {
						existingIdx = i
						break
					}
				}

				if existingIdx >= 0 {
					// Merge with existing translation (prefer non-empty values)
					if trans.Title != "" && translations[existingIdx].Title == "" {
						translations[existingIdx].Title = trans.Title
					}
					if trans.OriginalTitle != "" && translations[existingIdx].OriginalTitle == "" {
						translations[existingIdx].OriginalTitle = trans.OriginalTitle
					}
					if trans.Description != "" && translations[existingIdx].Description == "" {
						translations[existingIdx].Description = trans.Description
					}
					if trans.Director != "" && translations[existingIdx].Director == "" {
						translations[existingIdx].Director = trans.Director
					}
					if trans.Maker != "" && translations[existingIdx].Maker == "" {
						translations[existingIdx].Maker = trans.Maker
					}
					if trans.Label != "" && translations[existingIdx].Label == "" {
						translations[existingIdx].Label = trans.Label
					}
					if trans.Series != "" && translations[existingIdx].Series == "" {
						translations[existingIdx].Series = trans.Series
					}
				} else {
					// Add new translation
					translations = append(translations, trans)
				}
			}
		}

		// Skip results without language metadata for the legacy path
		if result.Language == "" {
			continue
		}

		// Check if this scraper is a "winner" by comparing ALL its translation fields
		// to the aggregated movie. A scraper is a winner if ANY of its non-empty
		// translation fields match the corresponding aggregated movie field.
		isWinner := false
		if result.Title != "" && result.Title == movie.Title {
			isWinner = true
		}
		if result.OriginalTitle != "" && result.OriginalTitle == movie.OriginalTitle {
			isWinner = true
		}
		if result.Description != "" && result.Description == movie.Description {
			isWinner = true
		}
		if result.Director != "" && result.Director == movie.Director {
			isWinner = true
		}
		if result.Maker != "" && result.Maker == movie.Maker {
			isWinner = true
		}
		if result.Label != "" && result.Label == movie.Label {
			isWinner = true
		}
		if result.Series != "" && result.Series == movie.Series {
			isWinner = true
		}

		// Only include translation if scraper contributed at least one field to merged result
		if !isWinner {
			continue
		}

		translation := models.MovieTranslation{
			Language:      result.Language,
			Title:         result.Title,
			OriginalTitle: result.OriginalTitle, // Japanese/original language title
			Description:   result.Description,
			Director:      result.Director,
			Maker:         result.Maker,
			Label:         result.Label,
			Series:        result.Series,
			SourceName:    result.Source,
		}

		// Check if this language already exists (from scraper translations above)
		existingIdx := -1
		for i, existing := range translations {
			if existing.Language == result.Language {
				existingIdx = i
				break
			}
		}

		if existingIdx >= 0 {
			// Merge with existing translation (prefer non-empty values)
			if translation.Title != "" && translations[existingIdx].Title == "" {
				translations[existingIdx].Title = translation.Title
			}
			if translation.OriginalTitle != "" && translations[existingIdx].OriginalTitle == "" {
				translations[existingIdx].OriginalTitle = translation.OriginalTitle
			}
			if translation.Description != "" && translations[existingIdx].Description == "" {
				translations[existingIdx].Description = translation.Description
			}
			if translation.Director != "" && translations[existingIdx].Director == "" {
				translations[existingIdx].Director = translation.Director
			}
			if translation.Maker != "" && translations[existingIdx].Maker == "" {
				translations[existingIdx].Maker = translation.Maker
			}
			if translation.Label != "" && translations[existingIdx].Label == "" {
				translations[existingIdx].Label = translation.Label
			}
			if translation.Series != "" && translations[existingIdx].Series == "" {
				translations[existingIdx].Series = translation.Series
			}
		} else {
			translations = append(translations, translation)
		}
	}

	return translations
}
