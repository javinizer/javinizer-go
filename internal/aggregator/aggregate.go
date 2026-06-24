package aggregator

import (
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// Aggregate combines multiple scraper results into a single Movie
func (a *Aggregator) Aggregate(results []*models.ScraperResult) (*models.Movie, *AggregateResult, error) {
	if a == nil {
		return nil, nil, fmt.Errorf("Aggregate called on nil Aggregator")
	}
	return a.aggregateWithPriority(results, func(field string) []string {
		return a.resolvedPriorities[field]
	})
}

func (a *Aggregator) AggregateWithPriority(results []*models.ScraperResult, customPriority []string) (*models.Movie, *AggregateResult, error) {
	if a == nil {
		return nil, nil, fmt.Errorf("AggregateWithPriority called on nil Aggregator")
	}
	return a.aggregateWithPriority(results, func(field string) []string {
		return customPriority
	})
}

// stringFieldSpec defines a simple string field that can be assigned by priority.
type stringFieldSpec struct {
	fieldKey    string                             // key in fieldSources map
	priorityKey string                             // key for priorityFunc
	getter      func(*models.ScraperResult) string // value getter from ScraperResult
	setter      func(*models.Movie, string)        // value setter on Movie
}

// stringFieldSpecs lists all simple string fields assigned by priority.
var stringFieldSpecs = []stringFieldSpec{
	{"id", "ID", func(r *models.ScraperResult) string { return r.ID }, func(m *models.Movie, v string) { m.ID = v }},
	{"content_id", "ContentID", func(r *models.ScraperResult) string { return r.ContentID }, func(m *models.Movie, v string) { m.ContentID = v }},
	{"original_title", "OriginalTitle", func(r *models.ScraperResult) string { return r.OriginalTitle }, func(m *models.Movie, v string) { m.OriginalTitle = v }},
	{"description", "Description", func(r *models.ScraperResult) string { return r.Description }, func(m *models.Movie, v string) { m.Description = v }},
	{"director", "Director", func(r *models.ScraperResult) string { return r.Director }, func(m *models.Movie, v string) { m.Director = v }},
	{"maker", "Maker", func(r *models.ScraperResult) string { return r.Maker }, func(m *models.Movie, v string) { m.Maker = v }},
	{"label", "Label", func(r *models.ScraperResult) string { return r.Label }, func(m *models.Movie, v string) { m.Label = v }},
	{"series", "Series", func(r *models.ScraperResult) string { return r.Series }, func(m *models.Movie, v string) { m.Series = v }},
	{"poster_url", "PosterURL", func(r *models.ScraperResult) string { return r.PosterURL }, func(m *models.Movie, v string) { m.Poster.PosterURL = v }},
	{"cover_url", "CoverURL", func(r *models.ScraperResult) string { return r.CoverURL }, func(m *models.Movie, v string) { m.Poster.CoverURL = v }},
	{"trailer_url", "TrailerURL", func(r *models.ScraperResult) string { return r.TrailerURL }, func(m *models.Movie, v string) { m.TrailerURL = v }},
}

// assignStringFields assigns all simple string fields using a spec-driven loop.
func assignStringFields(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(field string) []string, fieldSources map[string]string) {
	assignString := func(fieldKey string, priority []string, getter func(*models.ScraperResult) string) string {
		for _, source := range priority {
			if result, exists := resultsBySource[source]; exists {
				if value := getter(result); value != "" {
					fieldSources[fieldKey] = source
					return value
				}
			}
		}
		return ""
	}

	for _, spec := range stringFieldSpecs {
		spec.setter(movie, assignString(spec.fieldKey, priorityFunc(spec.priorityKey), spec.getter))
	}
}

func (a *Aggregator) aggregateWithPriority(results []*models.ScraperResult, priorityFunc func(field string) []string) (*models.Movie, *AggregateResult, error) {
	if a == nil || a.cfg == nil || a.cfg.Metadata == nil {
		return nil, nil, fmt.Errorf("aggregateWithPriority called on Aggregator with nil config")
	}
	if len(results) == 0 {
		return nil, nil, fmt.Errorf("no scraper results to aggregate")
	}

	scraped := &models.Movie{}
	fieldSources := make(map[string]string)

	resultsBySource := make(map[string]*models.ScraperResult)
	for _, result := range results {
		resultsBySource[result.Source] = result
	}

	// Assign fields using named methods — each handles its own priority resolution
	assignStringFields(scraped, resultsBySource, priorityFunc, fieldSources)
	a.assignTitle(scraped, resultsBySource, priorityFunc, fieldSources)
	a.assignShouldCropPoster(scraped, resultsBySource, priorityFunc, fieldSources)
	a.assignRuntime(scraped, resultsBySource, priorityFunc, fieldSources)
	a.assignReleaseDate(scraped, resultsBySource, priorityFunc, fieldSources)
	a.assignRating(scraped, resultsBySource, priorityFunc, fieldSources)
	a.assignActresses(scraped, resultsBySource, priorityFunc, fieldSources)
	a.assignGenres(scraped, resultsBySource, priorityFunc, fieldSources)
	a.assignScreenshots(scraped, resultsBySource, priorityFunc, fieldSources)

	// Derived fields
	if scraped.ReleaseDate != nil {
		scraped.ReleaseYear = scraped.ReleaseDate.Year()
	}

	a.assignSourceMetadata(scraped, resultsBySource, results, priorityFunc)

	scraped.Translations = a.buildTranslations(results, scraped)

	if a.wordProcessor != nil {
		a.wordProcessor.applyToMovie(scraped)
	}

	if len(a.cfg.Metadata.RequiredFields) > 0 {
		if err := validateRequiredFieldsScraped(scraped, a.cfg.Metadata.RequiredFields); err != nil {
			return nil, nil, fmt.Errorf("required field validation failed: %w", err)
		}
	}

	now := time.Now().UTC()
	scraped.CreatedAt = now
	scraped.UpdatedAt = now

	result := &AggregateResult{
		FieldSources:       fieldSources,
		ResolvedPriorities: a.resolvedPriorities,
	}

	return scraped, result, nil
}

// assignTitle assigns the title field and records both title and display_title sources.
func (a *Aggregator) assignTitle(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(string) []string, fieldSources map[string]string) {
	priority := priorityFunc("Title")
	titleSource := ""
	for _, source := range priority {
		if result, exists := resultsBySource[source]; exists {
			if result.Title != "" {
				fieldSources["title"] = source
				movie.Title = result.Title
				titleSource = source
				break
			}
		}
	}
	if titleSource != "" {
		fieldSources["display_title"] = titleSource
	}
}

// assignShouldCropPoster assigns the ShouldCropPoster flag derived from the PosterURL source.
func (a *Aggregator) assignShouldCropPoster(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(string) []string, fieldSources map[string]string) {
	priority := priorityFunc("PosterURL")
	for _, source := range priority {
		if result, exists := resultsBySource[source]; exists && result.PosterURL != "" {
			movie.Poster.ShouldCropPoster = result.ShouldCropPoster
			if result.ShouldCropPoster {
				fieldSources["should_crop_poster"] = source
			}
			break
		}
	}
}

// assignRuntime assigns the runtime field from the first source with a positive value.
func (a *Aggregator) assignRuntime(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(string) []string, fieldSources map[string]string) {
	priority := priorityFunc("Runtime")
	for _, source := range priority {
		if result, exists := resultsBySource[source]; exists {
			if v := result.Runtime; v > 0 {
				fieldSources["runtime"] = source
				movie.Runtime = v
				break
			}
		}
	}
}

// assignReleaseDate assigns the release date from the first source with a non-nil date.
func (a *Aggregator) assignReleaseDate(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(string) []string, fieldSources map[string]string) {
	priority := priorityFunc("ReleaseDate")
	for _, source := range priority {
		if result, exists := resultsBySource[source]; exists {
			if v := result.ReleaseDate; v != nil {
				fieldSources["release_date"] = source
				movie.ReleaseDate = v
				break
			}
		}
	}
}

// assignRating assigns the rating score and votes, with out-of-range validation.
func (a *Aggregator) assignRating(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(string) []string, fieldSources map[string]string) {
	priority := priorityFunc("Rating")
	ratingScore, ratingVotes, ratingSource, ratingWarning := a.getRatingByPriorityWithSource(resultsBySource, priority)
	movie.RatingScore = ratingScore
	movie.RatingVotes = ratingVotes
	if ratingSource != "" {
		fieldSources["rating_score"] = ratingSource
		fieldSources["rating_votes"] = ratingSource
	}
	// ratingWarning names the source(s) whose out-of-range score was skipped.
	// getRatingByPriorityWithSource skips corrupt scores before returning, so
	// any stored rating is already in range; the warning is the surviving
	// diagnostic for the skipped sources (restores the source name that the
	// old single-rating warning lost — cycle-1 NIT-11).
	if ratingWarning != "" {
		movie.RatingWarning = ratingWarning
	}
}

// assignActresses assigns the actress list using alias-aware merging.
func (a *Aggregator) assignActresses(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(string) []string, fieldSources map[string]string) {
	priority := priorityFunc("Actress")
	movie.Actresses = a.getActressesByPriorityWithSource(resultsBySource, priority, fieldSources)
}

// assignGenres assigns genres with replacement and filtering applied.
func (a *Aggregator) assignGenres(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(string) []string, fieldSources map[string]string) {
	priority := priorityFunc("Genre")
	genreNames, genreSource := a.getGenresByPriorityWithSource(resultsBySource, priority)
	movie.Genres = make([]models.Genre, 0, len(genreNames))
	for _, name := range genreNames {
		// Apply configured word replacements to each genre token before genre
		// replacement + ignore-check. The old genre loop (deleted genre.go)
		// did `name = a.applyWordReplacement(name)` first; the rewrite dropped
		// it and wordProcessor.applyToMovie does not touch movie.Genres, so
		// user word maps no longer normalized genre tokens.
		replacedName := name
		if a.wordProcessor != nil {
			replacedName = a.wordProcessor.Apply(replacedName)
		}
		if a.genreProcessor != nil {
			replacedName = a.genreProcessor.applyReplacement(replacedName)
		}
		ignored := false
		if a.genreProcessor != nil {
			ignored = a.genreProcessor.isIgnored(replacedName)
		}
		if ignored {
			continue
		}
		movie.Genres = append(movie.Genres, models.Genre{Name: replacedName})
	}
	if genreSource != "" {
		fieldSources["genres"] = genreSource
	}
}

// assignScreenshots assigns screenshot URLs from the highest-priority source.
func (a *Aggregator) assignScreenshots(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, priorityFunc func(string) []string, fieldSources map[string]string) {
	priority := priorityFunc("ScreenshotURL")
	movie.Screenshots = a.getScreenshotsByPriorityWithSource(resultsBySource, priority, fieldSources)
}

// assignSourceMetadata sets SourceName and SourceURL from the first available result.
func (a *Aggregator) assignSourceMetadata(movie *models.Movie, resultsBySource map[string]*models.ScraperResult, results []*models.ScraperResult, priorityFunc func(string) []string) {
	sourcePriority := priorityFunc("title")
	for _, source := range sourcePriority {
		if result, exists := resultsBySource[source]; exists {
			movie.SourceName = result.Source
			movie.SourceURL = result.SourceURL
			return
		}
	}
	if len(results) > 0 {
		movie.SourceName = results[0].Source
		movie.SourceURL = results[0].SourceURL
	}
}

func (a *Aggregator) getRatingByPriorityWithSource(
	results map[string]*models.ScraperResult,
	priority []string,
) (float64, int, string, string) {
	var skipped []string
	for _, source := range priority {
		if result, exists := results[source]; exists {
			if result.Rating != nil && (result.Rating.Score > 0 || result.Rating.Votes > 0) {
				score := result.Rating.Score
				// Skip corrupt/out-of-range scores and keep scanning for the
				// first *valid* priority source. The old getRatingByPriority
				// did `if !isRatingScoreValid(score) { warn; continue }` —
				// without this, a scraper returning a 0–100 percentage or
				// garbage is persisted into movie/DB/NFO instead of being
				// defensively discarded and replaced by the next source.
				if score > 0 && (score < ratingMinValid || score > ratingMaxValid) {
					skipped = append(skipped, fmt.Sprintf("%s(%.2f)", source, score))
					continue
				}
				return score, result.Rating.Votes, source, skippedWarning(skipped)
			}
		}
	}
	return 0, 0, "", skippedWarning(skipped)
}

// skippedWarning builds a diagnostic naming the sources whose out-of-range
// rating was skipped. Empty when nothing was skipped. The source name is
// included so users can see which scraper returned the corrupt score (the
// pre-refactor warning named only the stored rating, not the source).
func skippedWarning(skipped []string) string {
	if len(skipped) == 0 {
		return ""
	}
	return fmt.Sprintf("skipped out-of-range rating(s): %s (valid range [%.1f, %.1f])", strings.Join(skipped, ", "), ratingMinValid, ratingMaxValid)
}

func isUnknownActress(info models.ActressInfo, nameKey string, unknownText string) bool {
	if unknownText == "" {
		return false
	}
	if nameKey == unknownText {
		return true
	}
	if models.NormalizeActressNameKey(info.JapaneseName) == unknownText {
		return true
	}
	if models.NormalizeActressNameKey(info.FirstName) == unknownText {
		return true
	}
	if models.NormalizeActressNameKey(info.LastName) == unknownText {
		return true
	}
	return false
}

func resolveNameKey(japaneseName, firstName, lastName string) string {
	if k := models.NormalizeActressNameKey(japaneseName); k != "" {
		return k
	}
	if k := models.NormalizeActressNameKey(firstName + " " + lastName); k != "" {
		return k
	}
	return models.NormalizeActressNameKey(lastName + " " + firstName)
}

func (a *Aggregator) getActressesByPriorityWithSource(
	results map[string]*models.ScraperResult,
	priority []string,
	fieldSources map[string]string,
) []models.Actress {
	// Map ScraperResult → actressSource at the call boundary
	sources := make([]actressSource, 0, len(priority))
	for _, src := range priority {
		if r, ok := results[src]; ok {
			sources = append(sources, actressSource{
				Source:    src,
				Actresses: r.Actresses,
			})
		}
	}

	// Build narrow options from config
	skipUnknown := false
	unknownText := ""
	if a.cfg != nil {
		skipUnknown = !a.cfg.Metadata.NFO.IsUnknownActressFallback()
		unknownText = a.cfg.Metadata.NFO.UnknownActressText
	}

	opts := actressMergeOptions{
		Priority:      priority,
		SkipUnknown:   skipUnknown,
		UnknownText:   unknownText,
		AliasResolver: a.aliasResolver,
	}

	result := a.actressMerger.Merge(sources, opts)

	// Record field source (side-effect belongs to Aggregator, not ActressMerger)
	if len(result) > 0 && fieldSources != nil {
		for _, src := range priority {
			if r, ok := results[src]; ok && len(r.Actresses) > 0 {
				fieldSources["actresses"] = src
				break
			}
		}
	}

	return result
}

func (a *Aggregator) getGenresByPriorityWithSource(
	results map[string]*models.ScraperResult,
	priority []string,
) ([]string, string) {
	for _, source := range priority {
		if result, exists := results[source]; exists {
			if len(result.Genres) > 0 {
				return result.Genres, source
			}
		}
	}
	return []string{}, ""
}

const (
	ratingMinValid = 0.1
	ratingMaxValid = 10.0
)

func (a *Aggregator) getScreenshotsByPriorityWithSource(
	results map[string]*models.ScraperResult,
	priority []string,
	fieldSources map[string]string,
) []string {
	for _, source := range priority {
		if result, exists := results[source]; exists {
			screenshotCount := len(result.ScreenshotURL)
			if screenshotCount > 0 {
				logging.Debugf("Screenshots: Using %s (%d screenshots)", source, screenshotCount)
				if fieldSources != nil {
					fieldSources["screenshot_urls"] = source
				}
				return result.ScreenshotURL
			}
			logging.Debugf("Screenshots: %s has 0 screenshots, checking next priority", source)
		}
	}
	logging.Debugf("Screenshots: All sources returned empty screenshots")
	return []string{}
}
