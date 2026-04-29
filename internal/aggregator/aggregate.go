package aggregator

import (
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// Aggregate combines multiple scraper results into a single Movie
func (a *Aggregator) Aggregate(results []*models.ScraperResult) (*models.Movie, string, error) {
	return a.aggregateWithPriority(results, func(field string) []string {
		return a.resolvedPriorities[field]
	})
}

func (a *Aggregator) AggregateWithPriority(results []*models.ScraperResult, customPriority []string) (*models.Movie, string, error) {
	return a.aggregateWithPriority(results, func(field string) []string {
		return customPriority
	})
}

// aggregateWithPriority contains the shared aggregation logic used by both Aggregate and AggregateWithPriority.
// The priorityFunc parameter returns the priority list for a given field name.
func (a *Aggregator) aggregateWithPriority(results []*models.ScraperResult, priorityFunc func(field string) []string) (*models.Movie, string, error) {
	if len(results) == 0 {
		return nil, "", fmt.Errorf("no scraper results to aggregate")
	}

	movie := &models.Movie{}

	resultsBySource := make(map[string]*models.ScraperResult)
	for _, result := range results {
		resultsBySource[result.Source] = result
	}

	movie.ID = a.getFieldByPriority(resultsBySource, priorityFunc("ID"), func(r *models.ScraperResult) string {
		return r.ID
	})

	movie.ContentID = a.getFieldByPriority(resultsBySource, priorityFunc("ContentID"), func(r *models.ScraperResult) string {
		return r.ContentID
	})

	movie.Title = a.getFieldByPriority(resultsBySource, priorityFunc("Title"), func(r *models.ScraperResult) string {
		return r.Title
	})

	movie.OriginalTitle = a.getFieldByPriority(resultsBySource, priorityFunc("OriginalTitle"), func(r *models.ScraperResult) string {
		return r.OriginalTitle
	})

	movie.Description = a.getFieldByPriority(resultsBySource, priorityFunc("Description"), func(r *models.ScraperResult) string {
		return r.Description
	})

	movie.Director = a.getFieldByPriority(resultsBySource, priorityFunc("Director"), func(r *models.ScraperResult) string {
		return r.Director
	})

	movie.Maker = a.getFieldByPriority(resultsBySource, priorityFunc("Maker"), func(r *models.ScraperResult) string {
		return r.Maker
	})

	movie.Label = a.getFieldByPriority(resultsBySource, priorityFunc("Label"), func(r *models.ScraperResult) string {
		return r.Label
	})

	movie.Series = a.getFieldByPriority(resultsBySource, priorityFunc("Series"), func(r *models.ScraperResult) string {
		return r.Series
	})

	movie.PosterURL = a.getFieldByPriority(resultsBySource, priorityFunc("PosterURL"), func(r *models.ScraperResult) string {
		return r.PosterURL
	})

	movie.CoverURL = a.getFieldByPriority(resultsBySource, priorityFunc("CoverURL"), func(r *models.ScraperResult) string {
		return r.CoverURL
	})

	for _, source := range priorityFunc("PosterURL") {
		if result, exists := resultsBySource[source]; exists && result.PosterURL != "" {
			movie.ShouldCropPoster = result.ShouldCropPoster
			break
		}
	}

	movie.TrailerURL = a.getFieldByPriority(resultsBySource, priorityFunc("TrailerURL"), func(r *models.ScraperResult) string {
		return r.TrailerURL
	})

	movie.Runtime = a.getIntFieldByPriority(resultsBySource, priorityFunc("Runtime"), func(r *models.ScraperResult) int {
		return r.Runtime
	})

	movie.ReleaseDate = a.getTimeFieldByPriority(resultsBySource, priorityFunc("ReleaseDate"), func(r *models.ScraperResult) *time.Time {
		return r.ReleaseDate
	})

	if movie.ReleaseDate != nil {
		movie.ReleaseYear = movie.ReleaseDate.Year()
	}

	ratingScore, ratingVotes := a.getRatingByPriority(resultsBySource, priorityFunc("Rating"))
	movie.RatingScore = ratingScore
	movie.RatingVotes = ratingVotes

	movie.Actresses = a.getActressesByPriority(resultsBySource, priorityFunc("Actress"))

	genreNames := a.getGenresByPriority(resultsBySource, priorityFunc("Genre"))
	movie.Genres = make([]models.Genre, 0, len(genreNames))
	for _, name := range genreNames {
		replacedName := a.applyGenreReplacement(name)

		if a.isGenreIgnored(replacedName) {
			continue
		}
		movie.Genres = append(movie.Genres, models.Genre{Name: replacedName})
	}

	movie.Screenshots = a.getScreenshotsByPriority(resultsBySource, priorityFunc("ScreenshotURL"))

	if len(results) > 0 {
		movie.SourceName = results[0].Source
		movie.SourceURL = results[0].SourceURL
	}

	movie.Translations = a.buildTranslations(results, movie)

	preTranslationTitle := movie.Title
	preTranslationMaker := movie.Maker
	preTranslationContentID := movie.ContentID

	translationWarning := a.ApplyConfiguredTranslation(movie)

	if preTranslationTitle != movie.Title || preTranslationMaker != movie.Maker || preTranslationContentID != movie.ContentID {
		logging.Debugf("Aggregation: translation modified primary fields - Title: %q->%q, Maker: %q->%q, ContentID: %q->%q",
			preTranslationTitle, movie.Title, preTranslationMaker, movie.Maker, preTranslationContentID, movie.ContentID)
	}

	if a.config.Metadata.NFO.DisplayTitle != "" {
		ctx := template.NewContextFromMovie(movie)
		displayTitle, err := a.templateEngine.Execute(a.config.Metadata.NFO.DisplayTitle, ctx)
		if err == nil && displayTitle != "" {
			movie.DisplayTitle = displayTitle
		}
	}
	if movie.DisplayTitle == "" && movie.Title != "" {
		movie.DisplayTitle = movie.Title
	}

	if len(a.config.Metadata.RequiredFields) > 0 {
		if err := validateRequiredFields(movie, a.config.Metadata.RequiredFields); err != nil {
			return nil, "", fmt.Errorf("required field validation failed: %w", err)
		}
	}

	now := time.Now().UTC()
	movie.CreatedAt = now
	movie.UpdatedAt = now

	return movie, translationWarning, nil
}

// getFieldByPriority retrieves a string field based on priority
func (a *Aggregator) getFieldByPriority(
	results map[string]*models.ScraperResult,
	priority []string,
	getter func(*models.ScraperResult) string,
) string {
	for _, source := range priority {
		if result, exists := results[source]; exists {
			if value := getter(result); value != "" {
				return value
			}
		}
	}
	return ""
}

// getIntFieldByPriority retrieves an integer field based on priority
func (a *Aggregator) getIntFieldByPriority(
	results map[string]*models.ScraperResult,
	priority []string,
	getter func(*models.ScraperResult) int,
) int {
	for _, source := range priority {
		if result, exists := results[source]; exists {
			if value := getter(result); value > 0 {
				return value
			}
		}
	}
	return 0
}

// getTimeFieldByPriority retrieves a time field based on priority
func (a *Aggregator) getTimeFieldByPriority(
	results map[string]*models.ScraperResult,
	priority []string,
	getter func(*models.ScraperResult) *time.Time,
) *time.Time {
	for _, source := range priority {
		if result, exists := results[source]; exists {
			if value := getter(result); value != nil {
				return value
			}
		}
	}
	return nil
}

// getRatingByPriority retrieves rating based on priority
func (a *Aggregator) getRatingByPriority(
	results map[string]*models.ScraperResult,
	priority []string,
) (float64, int) {
	for _, source := range priority {
		if result, exists := results[source]; exists {
			if result.Rating != nil && (result.Rating.Score > 0 || result.Rating.Votes > 0) {
				return result.Rating.Score, result.Rating.Votes
			}
		}
	}
	return 0, 0
}

func normalizeNameKey(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(name))), " ")
}

func isUnknownActress(info models.ActressInfo, nameKey string, unknownText string) bool {
	if unknownText == "" {
		return false
	}
	if nameKey == unknownText {
		return true
	}
	if normalizeNameKey(info.JapaneseName) == unknownText {
		return true
	}
	if normalizeNameKey(info.FirstName) == unknownText {
		return true
	}
	if normalizeNameKey(info.LastName) == unknownText {
		return true
	}
	return false
}

func resolveNameKey(japaneseName, firstName, lastName string) string {
	if k := normalizeNameKey(japaneseName); k != "" {
		return k
	}
	if k := normalizeNameKey(firstName + " " + lastName); k != "" {
		return k
	}
	return normalizeNameKey(lastName + " " + firstName)
}

// getActressesByPriority retrieves actresses based on priority and merges data from multiple sources
func (a *Aggregator) getActressesByPriority(
	results map[string]*models.ScraperResult,
	priority []string,
) []models.Actress {
	// Collect actresses from all sources, keyed by DMMID (most reliable identifier)
	actressByDMMID := make(map[int]*models.Actress)
	actressByName := make(map[string]*models.Actress)

	unknownText := ""
	skipUnknown := false
	if a.config != nil {
		skipUnknown = !a.config.Metadata.NFO.IsUnknownActressFallback()
		if skipUnknown {
			unknownText = strings.ToLower(strings.TrimSpace(a.config.Metadata.NFO.UnknownActressText))
		}
	}

	hadAnyActressFromScrapers := false

	for _, source := range priority {
		result, exists := results[source]
		if !exists || len(result.Actresses) == 0 {
			continue
		}

		for _, info := range result.Actresses {
			hadAnyActressFromScrapers = true

			nameKey := resolveNameKey(info.JapaneseName, info.FirstName, info.LastName)

			if skipUnknown && unknownText != "" && isUnknownActress(info, nameKey, unknownText) {
				continue
			}

			var existing *models.Actress
			var foundInDMMIDMap bool

			if info.DMMID != 0 {
				existing, foundInDMMIDMap = actressByDMMID[info.DMMID]
			}

			if existing == nil && nameKey != "" {
				for _, actress := range actressByDMMID {
					actressNameKey := resolveNameKey(actress.JapaneseName, actress.FirstName, actress.LastName)
					if actressNameKey == nameKey {
						existing = actress
						foundInDMMIDMap = true
						break
					}
				}

				if existing == nil {
					existing = actressByName[nameKey]
				}
			}

			// If actress exists, merge fields
			if existing != nil {
				if existing.DMMID <= 0 && info.DMMID != 0 {
					oldDMMID := existing.DMMID
					existing.DMMID = info.DMMID
					// Move from placeholder/non-DMMID entries to real DMMID key.
					if foundInDMMIDMap && oldDMMID != info.DMMID {
						delete(actressByDMMID, oldDMMID)
					}
					if !foundInDMMIDMap && nameKey != "" {
						delete(actressByName, nameKey)
					}
					actressByDMMID[info.DMMID] = existing
				}
				if existing.FirstName == "" && info.FirstName != "" {
					existing.FirstName = info.FirstName
				}
				if existing.LastName == "" && info.LastName != "" {
					existing.LastName = info.LastName
				}
				if existing.JapaneseName == "" && info.JapaneseName != "" {
					existing.JapaneseName = info.JapaneseName
				}
				if existing.ThumbURL == "" && info.ThumbURL != "" {
					existing.ThumbURL = info.ThumbURL
				}
			} else {
				// New actress - add to appropriate map
				actress := &models.Actress{
					DMMID:        info.DMMID,
					FirstName:    info.FirstName,
					LastName:     info.LastName,
					JapaneseName: info.JapaneseName,
					ThumbURL:     info.ThumbURL,
				}

				if info.DMMID != 0 {
					actressByDMMID[info.DMMID] = actress
				} else if nameKey != "" {
					actressByName[nameKey] = actress
				}
				// Skip actresses with no DMMID and no name
			}
		}
	}

	// Merge both maps and convert to slice
	totalActresses := len(actressByDMMID) + len(actressByName)
	if totalActresses > 0 {
		actresses := make([]models.Actress, 0, totalActresses)

		// Add actresses with DMMID first (primary source)
		for _, actress := range actressByDMMID {
			// Apply alias conversion if enabled
			if a.config.Metadata.ActressDatabase.Enabled && a.config.Metadata.ActressDatabase.ConvertAlias {
				a.applyActressAlias(actress)
			}
			actresses = append(actresses, *actress)
		}

		// Add actresses without DMMID (fallback)
		for _, actress := range actressByName {
			// Apply alias conversion if enabled
			if a.config.Metadata.ActressDatabase.Enabled && a.config.Metadata.ActressDatabase.ConvertAlias {
				a.applyActressAlias(actress)
			}
			actresses = append(actresses, *actress)
		}

		return actresses
	}

	// If no actresses found and unknown actress text is set, add unknown
	if !hadAnyActressFromScrapers && a.config.Metadata.NFO.IsUnknownActressFallback() && a.config.Metadata.NFO.UnknownActressText != "" {
		return []models.Actress{
			{
				FirstName:    a.config.Metadata.NFO.UnknownActressText,
				JapaneseName: a.config.Metadata.NFO.UnknownActressText,
			},
		}
	}

	return []models.Actress{}
}

// getGenresByPriority retrieves genres based on priority
func (a *Aggregator) getGenresByPriority(
	results map[string]*models.ScraperResult,
	priority []string,
) []string {
	for _, source := range priority {
		if result, exists := results[source]; exists {
			if len(result.Genres) > 0 {
				return result.Genres
			}
		}
	}
	return []string{}
}

// getScreenshotsByPriority retrieves screenshots based on priority
func (a *Aggregator) getScreenshotsByPriority(
	results map[string]*models.ScraperResult,
	priority []string,
) []string {
	for _, source := range priority {
		if result, exists := results[source]; exists {
			screenshotCount := len(result.ScreenshotURL)
			if screenshotCount > 0 {
				logging.Debugf("Screenshots: Using %s (%d screenshots)", source, screenshotCount)
				return result.ScreenshotURL
			}
			logging.Debugf("Screenshots: %s has 0 screenshots, checking next priority", source)
		}
	}
	logging.Debugf("Screenshots: All sources returned empty screenshots")
	return []string{}
}
