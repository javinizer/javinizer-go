package aggregator

import (
	"slices"

	"github.com/javinizer/javinizer-go/internal/models"
)

// translationFields returns fields in the same order as translationPriorityKeys.
func translationFields(t *models.MovieTranslation) [7]*string {
	return [7]*string{&t.Title, &t.OriginalTitle, &t.Description, &t.Director, &t.Maker, &t.Label, &t.Series}
}

var translationPriorityKeys = [7]string{"Title", "OriginalTitle", "Description", "Director", "Maker", "Label", "Series"}

// buildTranslations preserves first-seen language order and explicit translations
// before legacy fields from the same scraper. Each field independently selects
// the highest-priority source available for that language. An exclusive override
// therefore also excludes unlisted sources from translated output.
func (a *Aggregator) buildTranslations(results []*models.ScraperResult, scraped *models.Movie, priorityFunc func(string) []string) []models.MovieTranslation {
	translations := make([]models.MovieTranslation, 0, len(results))
	indices := make(map[string]int)
	ranks := make(map[string][7]int)
	merge := func(trans models.MovieTranslation, source string) {
		for field, value := range translationFields(&trans) {
			rank := slices.Index(priorityFunc(translationPriorityKeys[field]), source)
			if *value == "" || rank < 0 {
				continue
			}
			idx, exists := indices[trans.Language]
			if !exists {
				entry := trans
				for _, value := range translationFields(&entry) {
					*value = ""
				}
				idx = len(translations)
				indices[trans.Language] = idx
				translations = append(translations, entry)
			}
			target := translationFields(&translations[idx])[field]
			languageRanks := ranks[trans.Language]
			if *target == "" || rank < languageRanks[field] {
				*target = *value
				languageRanks[field] = rank
				ranks[trans.Language] = languageRanks
			}
		}
	}

	for _, result := range results {
		// Preserve genuinely alternative languages supplied by a scraper.
		for _, trans := range result.Translations {
			merge(trans, result.Source)
		}
		if result.Language == "" {
			continue
		}
		// Check if this scraper is a "winner" by comparing ALL its translation fields
		// to the aggregated movie. A scraper is a winner if ANY of its non-empty
		// translation fields match the corresponding aggregated movie field.
		isWinner := false
		if result.Title != "" && result.Title == scraped.Title {
			isWinner = true
		}
		if result.OriginalTitle != "" && result.OriginalTitle == scraped.OriginalTitle {
			isWinner = true
		}
		if result.Description != "" && result.Description == scraped.Description {
			isWinner = true
		}
		if result.Director != "" && result.Director == scraped.Director {
			isWinner = true
		}
		if result.Maker != "" && result.Maker == scraped.Maker {
			isWinner = true
		}
		if result.Label != "" && result.Label == scraped.Label {
			isWinner = true
		}
		if result.Series != "" && result.Series == scraped.Series {
			isWinner = true
		}

		// Only include translation if scraper contributed at least one field to merged result
		if !isWinner {
			continue
		}

		merge(models.MovieTranslation{
			Language:      result.Language,
			Title:         result.Title,
			OriginalTitle: result.OriginalTitle,
			Description:   result.Description,
			Director:      result.Director,
			Maker:         result.Maker,
			Label:         result.Label,
			Series:        result.Series,
			SourceName:    result.Source,
		}, result.Source)
	}
	return translations
}
