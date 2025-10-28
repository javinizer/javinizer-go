package aggregator

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Aggregator combines metadata from multiple scrapers based on priority
type Aggregator struct {
	config                *config.Config
	genreReplacementRepo  *database.GenreReplacementRepository
	genreReplacementCache map[string]string
}

// New creates a new aggregator
func New(cfg *config.Config) *Aggregator {
	return &Aggregator{
		config:                cfg,
		genreReplacementCache: make(map[string]string),
	}
}

// NewWithDatabase creates a new aggregator with database support for genre replacements
func NewWithDatabase(cfg *config.Config, db *database.DB) *Aggregator {
	agg := &Aggregator{
		config:                cfg,
		genreReplacementRepo:  database.NewGenreReplacementRepository(db),
		genreReplacementCache: make(map[string]string),
	}

	// Load replacement cache
	agg.loadGenreReplacementCache()

	return agg
}

// loadGenreReplacementCache loads genre replacements into memory
func (a *Aggregator) loadGenreReplacementCache() {
	if a.genreReplacementRepo == nil {
		return
	}

	replacementMap, err := a.genreReplacementRepo.GetReplacementMap()
	if err == nil {
		a.genreReplacementCache = replacementMap
	}
}

// Aggregate combines multiple scraper results into a single Movie
func (a *Aggregator) Aggregate(results []*models.ScraperResult) (*models.Movie, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no scraper results to aggregate")
	}

	movie := &models.Movie{}

	// Build translations from all scraper results
	movie.Translations = a.buildTranslations(results)

	// Create a map of results by source name for quick lookup
	resultsBySource := make(map[string]*models.ScraperResult)
	for _, result := range results {
		resultsBySource[result.Source] = result
	}

	// Aggregate each field based on priority
	movie.ID = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.ID, func(r *models.ScraperResult) string {
		return r.ID
	})

	movie.ContentID = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.ContentID, func(r *models.ScraperResult) string {
		return r.ContentID
	})

	movie.Title = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.Title, func(r *models.ScraperResult) string {
		return r.Title
	})

	movie.OriginalTitle = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.OriginalTitle, func(r *models.ScraperResult) string {
		return r.OriginalTitle
	})

	movie.Description = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.Description, func(r *models.ScraperResult) string {
		return r.Description
	})

	movie.Director = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.Director, func(r *models.ScraperResult) string {
		return r.Director
	})

	movie.Maker = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.Maker, func(r *models.ScraperResult) string {
		return r.Maker
	})

	movie.Label = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.Label, func(r *models.ScraperResult) string {
		return r.Label
	})

	movie.Series = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.Series, func(r *models.ScraperResult) string {
		return r.Series
	})

	movie.PosterURL = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.PosterURL, func(r *models.ScraperResult) string {
		return r.PosterURL
	})

	movie.CoverURL = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.CoverURL, func(r *models.ScraperResult) string {
		return r.CoverURL
	})

	movie.TrailerURL = a.getFieldByPriority(resultsBySource, a.config.Metadata.Priority.TrailerURL, func(r *models.ScraperResult) string {
		return r.TrailerURL
	})

	// Aggregate runtime
	movie.Runtime = a.getIntFieldByPriority(resultsBySource, a.config.Metadata.Priority.Runtime, func(r *models.ScraperResult) int {
		return r.Runtime
	})

	// Aggregate release date
	movie.ReleaseDate = a.getTimeFieldByPriority(resultsBySource, a.config.Metadata.Priority.ReleaseDate, func(r *models.ScraperResult) *time.Time {
		return r.ReleaseDate
	})

	if movie.ReleaseDate != nil {
		movie.ReleaseYear = movie.ReleaseDate.Year()
	}

	// Aggregate rating
	ratingScore, ratingVotes := a.getRatingByPriority(resultsBySource, a.config.Metadata.Priority.Rating)
	movie.RatingScore = ratingScore
	movie.RatingVotes = ratingVotes

	// Aggregate actresses
	movie.Actresses = a.getActressesByPriority(resultsBySource, a.config.Metadata.Priority.Actress)

	// Aggregate genres
	genreNames := a.getGenresByPriority(resultsBySource, a.config.Metadata.Priority.Genre)
	movie.Genres = make([]models.Genre, 0, len(genreNames))
	for _, name := range genreNames {
		// Apply replacement if exists
		replacedName := a.applyGenreReplacement(name)

		// Filter out ignored genres
		if a.isGenreIgnored(replacedName) {
			continue
		}
		movie.Genres = append(movie.Genres, models.Genre{Name: replacedName})
	}

	// Aggregate screenshots
	movie.Screenshots = a.getScreenshotsByPriority(resultsBySource, a.config.Metadata.Priority.ScreenshotURL)

	// Set source metadata
	if len(results) > 0 {
		movie.SourceName = results[0].Source
		movie.SourceURL = results[0].SourceURL
	}

	// Set timestamps
	now := time.Now().UTC()
	movie.CreatedAt = now
	movie.UpdatedAt = now

	return movie, nil
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

// getActressesByPriority retrieves actresses based on priority and merges data from multiple sources
func (a *Aggregator) getActressesByPriority(
	results map[string]*models.ScraperResult,
	priority []string,
) []models.Actress {
	// Collect actresses from all sources, keyed by a unique identifier
	actressMap := make(map[string]*models.Actress)

	// Process sources in priority order
	for _, source := range priority {
		result, exists := results[source]
		if !exists || len(result.Actresses) == 0 {
			continue
		}

		for _, info := range result.Actresses {
			// Use JapaneseName as key for matching (most reliable across sources)
			key := info.JapaneseName
			if key == "" {
				// Fallback to FirstName + LastName if no Japanese name
				key = info.FirstName + " " + info.LastName
			}

			// If actress doesn't exist yet, create entry
			if _, found := actressMap[key]; !found {
				actressMap[key] = &models.Actress{
					DMMID:        info.DMMID,
					FirstName:    info.FirstName,
					LastName:     info.LastName,
					JapaneseName: info.JapaneseName,
					ThumbURL:     info.ThumbURL,
				}
			} else {
				// Merge data: fill in missing fields from this source
				existing := actressMap[key]
				if existing.DMMID == 0 && info.DMMID != 0 {
					existing.DMMID = info.DMMID
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
			}
		}
	}

	// Convert map to slice
	if len(actressMap) > 0 {
		actresses := make([]models.Actress, 0, len(actressMap))
		for _, actress := range actressMap {
			actresses = append(actresses, *actress)
		}
		return actresses
	}

	// If no actresses found and unknown actress text is set, add unknown
	if a.config.Metadata.NFO.UnknownActressText != "" {
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
			if len(result.ScreenshotURL) > 0 {
				return result.ScreenshotURL
			}
		}
	}
	return []string{}
}

// isGenreIgnored checks if a genre should be ignored
func (a *Aggregator) isGenreIgnored(genre string) bool {
	for _, ignored := range a.config.Metadata.IgnoreGenres {
		if genre == ignored {
			return true
		}
	}
	return false
}

// buildTranslations creates MovieTranslation records from scraper results
// Each result with a language code gets its own translation entry
func (a *Aggregator) buildTranslations(results []*models.ScraperResult) []models.MovieTranslation {
	translations := make([]models.MovieTranslation, 0, len(results))

	for _, result := range results {
		// Skip results without language metadata
		if result.Language == "" {
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

		translations = append(translations, translation)
	}

	return translations
}

// applyGenreReplacement applies genre replacement if one exists
func (a *Aggregator) applyGenreReplacement(original string) string {
	// Check cache first
	if replacement, exists := a.genreReplacementCache[original]; exists {
		return replacement
	}
	// Return original if no replacement found
	return original
}

// ReloadGenreReplacements reloads the genre replacement cache from database
func (a *Aggregator) ReloadGenreReplacements() {
	a.loadGenreReplacementCache()
}
