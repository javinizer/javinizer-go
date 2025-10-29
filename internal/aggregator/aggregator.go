package aggregator

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// Aggregator combines metadata from multiple scrapers based on priority
type Aggregator struct {
	config                *config.Config
	templateEngine        *template.Engine
	genreReplacementRepo  *database.GenreReplacementRepository
	genreReplacementCache map[string]string
	genreCacheMutex       sync.RWMutex        // Protects genreReplacementCache from concurrent access
	actressAliasRepo      *database.ActressAliasRepository
	actressAliasCache     map[string]string // Maps alias name to canonical name
	aliasCacheMutex       sync.RWMutex      // Protects actressAliasCache from concurrent access
	resolvedPriorities    map[string][]string // Cached resolved priorities for each field
	ignoreGenreRegexes    []*regexp.Regexp    // Compiled regex patterns for genre filtering
}

// New creates a new aggregator
func New(cfg *config.Config) *Aggregator {
	agg := &Aggregator{
		config:                cfg,
		templateEngine:        template.NewEngine(),
		genreReplacementCache: make(map[string]string),
		actressAliasCache:     make(map[string]string),
	}
	agg.resolvePriorities()
	agg.compileGenreRegexes()
	return agg
}

// NewWithDatabase creates a new aggregator with database support for genre replacements and actress aliases
func NewWithDatabase(cfg *config.Config, db *database.DB) *Aggregator {
	agg := &Aggregator{
		config:                cfg,
		templateEngine:        template.NewEngine(),
		genreReplacementRepo:  database.NewGenreReplacementRepository(db),
		genreReplacementCache: make(map[string]string),
		actressAliasRepo:      database.NewActressAliasRepository(db),
		actressAliasCache:     make(map[string]string),
	}

	// Load replacement cache
	agg.loadGenreReplacementCache()

	// Load actress alias cache
	agg.loadActressAliasCache()

	// Resolve priorities at initialization
	agg.resolvePriorities()

	// Compile genre filter regexes
	agg.compileGenreRegexes()

	return agg
}

// loadGenreReplacementCache loads genre replacements into memory
func (a *Aggregator) loadGenreReplacementCache() {
	if a.genreReplacementRepo == nil {
		return
	}

	replacementMap, err := a.genreReplacementRepo.GetReplacementMap()
	if err == nil {
		a.genreCacheMutex.Lock()
		a.genreReplacementCache = replacementMap
		a.genreCacheMutex.Unlock()
	}
}

// loadActressAliasCache loads actress aliases into memory
func (a *Aggregator) loadActressAliasCache() {
	if a.actressAliasRepo == nil {
		return
	}

	aliasMap, err := a.actressAliasRepo.GetAliasMap()
	if err == nil {
		a.aliasCacheMutex.Lock()
		a.actressAliasCache = aliasMap
		a.aliasCacheMutex.Unlock()
	}
}

// applyActressAlias converts actress names using the alias database
// It checks Japanese name, FirstName LastName, and LastName FirstName combinations
func (a *Aggregator) applyActressAlias(actress *models.Actress) {
	// Check cache first with read lock
	a.aliasCacheMutex.RLock()
	defer a.aliasCacheMutex.RUnlock()

	// Try Japanese name first
	if actress.JapaneseName != "" {
		if canonical, found := a.actressAliasCache[actress.JapaneseName]; found {
			actress.JapaneseName = canonical
			return
		}
	}

	// Try FirstName LastName combination
	if actress.FirstName != "" && actress.LastName != "" {
		fullName := actress.FirstName + " " + actress.LastName
		if canonical, found := a.actressAliasCache[fullName]; found {
			// Parse canonical name back into first/last if it contains space
			// Otherwise, assume it's a Japanese name
			if len(canonical) > 0 {
				parts := splitActressName(canonical)
				if len(parts) == 2 {
					// Canonical form is typically "FamilyName GivenName" (Japanese convention)
					// Assign so FullName() returns LastName + " " + FirstName = canonical
					actress.LastName = parts[0]  // Family name
					actress.FirstName = parts[1] // Given name
				} else {
					// Canonical is a single name (likely Japanese)
					actress.JapaneseName = canonical
				}
			}
			return
		}

		// Try LastName FirstName combination
		reverseName := actress.LastName + " " + actress.FirstName
		if canonical, found := a.actressAliasCache[reverseName]; found {
			if len(canonical) > 0 {
				parts := splitActressName(canonical)
				if len(parts) == 2 {
					// Canonical form is typically "FamilyName GivenName" (Japanese convention)
					// Assign so FullName() returns LastName + " " + FirstName = canonical
					actress.LastName = parts[0]  // Family name
					actress.FirstName = parts[1] // Given name
				} else {
					actress.JapaneseName = canonical
				}
			}
			return
		}
	}
}

// splitActressName splits a full name into parts (e.g., "Yui Hatano" -> ["Yui", "Hatano"])
func splitActressName(fullName string) []string {
	// Simple split by space - could be enhanced for more complex names
	var parts []string
	currentPart := ""
	for _, char := range fullName {
		if char == ' ' {
			if currentPart != "" {
				parts = append(parts, currentPart)
				currentPart = ""
			}
		} else {
			currentPart += string(char)
		}
	}
	if currentPart != "" {
		parts = append(parts, currentPart)
	}
	return parts
}

// compileGenreRegexes compiles regex patterns from ignore_genres config
// Patterns that look like regex (contain special chars) are compiled
// Plain strings are left for exact matching
func (a *Aggregator) compileGenreRegexes() {
	a.ignoreGenreRegexes = make([]*regexp.Regexp, 0)

	for _, pattern := range a.config.Metadata.IgnoreGenres {
		// Check if pattern looks like a regex (contains regex metacharacters)
		if isRegexPattern(pattern) {
			compiled, err := regexp.Compile(pattern)
			if err == nil {
				a.ignoreGenreRegexes = append(a.ignoreGenreRegexes, compiled)
			}
			// If compilation fails, silently skip (will fall through to exact match)
		}
	}
}

// isRegexPattern checks if a string looks like a regex pattern
func isRegexPattern(s string) bool {
	// Check for common regex metacharacters
	regexChars := []string{"^", "$", ".*", ".+", "\\", "[", "]", "(", ")", "|", "?", "*", "+"}
	for _, char := range regexChars {
		if len(s) > 0 && (s[0] == '^' || s[len(s)-1] == '$') {
			return true
		}
		if len(s) >= len(char) {
			for i := 0; i <= len(s)-len(char); i++ {
				if s[i:i+len(char)] == char {
					return true
				}
			}
		}
	}
	return false
}

// resolvePriorities resolves all field priorities at initialization time
// If a field has an empty priority list or is missing, it falls back to global priority
func (a *Aggregator) resolvePriorities() {
	a.resolvedPriorities = make(map[string][]string)

	globalPriority := a.config.Scrapers.Priority
	if len(globalPriority) == 0 {
		// Fallback to a sensible default if global priority is empty
		globalPriority = []string{"r18dev", "dmm"}
	}

	// List of all metadata fields that need priority resolution
	fields := []string{
		"ID", "ContentID", "Title", "OriginalTitle", "Description",
		"Director", "Maker", "Label", "Series", "PosterURL", "CoverURL",
		"TrailerURL", "Runtime", "ReleaseDate", "Rating", "Actress",
		"Genre", "ScreenshotURL",
	}

	for _, field := range fields {
		fieldPriority := getFieldPriorityFromConfig(a.config, field)

		// If field priority is empty or nil, use global priority
		if len(fieldPriority) == 0 {
			a.resolvedPriorities[field] = copySlice(globalPriority)
		} else {
			a.resolvedPriorities[field] = copySlice(fieldPriority)
		}
	}
}

// getFieldPriorityFromConfig extracts the priority list for a field from config
// Returns nil if the field is not configured
func getFieldPriorityFromConfig(cfg *config.Config, fieldKey string) []string {
	// Use reflection-free approach by checking each field explicitly
	switch fieldKey {
	case "ID":
		return cfg.Metadata.Priority.ID
	case "ContentID":
		return cfg.Metadata.Priority.ContentID
	case "Title":
		return cfg.Metadata.Priority.Title
	case "OriginalTitle":
		return cfg.Metadata.Priority.OriginalTitle
	case "Description":
		return cfg.Metadata.Priority.Description
	case "Director":
		return cfg.Metadata.Priority.Director
	case "Maker":
		return cfg.Metadata.Priority.Maker
	case "Label":
		return cfg.Metadata.Priority.Label
	case "Series":
		return cfg.Metadata.Priority.Series
	case "PosterURL":
		return cfg.Metadata.Priority.PosterURL
	case "CoverURL":
		return cfg.Metadata.Priority.CoverURL
	case "TrailerURL":
		return cfg.Metadata.Priority.TrailerURL
	case "Runtime":
		return cfg.Metadata.Priority.Runtime
	case "ReleaseDate":
		return cfg.Metadata.Priority.ReleaseDate
	case "Rating":
		return cfg.Metadata.Priority.Rating
	case "Actress":
		return cfg.Metadata.Priority.Actress
	case "Genre":
		return cfg.Metadata.Priority.Genre
	case "ScreenshotURL":
		return cfg.Metadata.Priority.ScreenshotURL
	default:
		return nil
	}
}

// copySlice creates a copy of a string slice
func copySlice(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
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

	// Aggregate each field based on resolved priority
	movie.ID = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["ID"], func(r *models.ScraperResult) string {
		return r.ID
	})

	movie.ContentID = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["ContentID"], func(r *models.ScraperResult) string {
		return r.ContentID
	})

	movie.Title = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["Title"], func(r *models.ScraperResult) string {
		return r.Title
	})

	movie.OriginalTitle = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["OriginalTitle"], func(r *models.ScraperResult) string {
		return r.OriginalTitle
	})

	movie.Description = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["Description"], func(r *models.ScraperResult) string {
		return r.Description
	})

	movie.Director = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["Director"], func(r *models.ScraperResult) string {
		return r.Director
	})

	movie.Maker = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["Maker"], func(r *models.ScraperResult) string {
		return r.Maker
	})

	movie.Label = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["Label"], func(r *models.ScraperResult) string {
		return r.Label
	})

	movie.Series = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["Series"], func(r *models.ScraperResult) string {
		return r.Series
	})

	movie.PosterURL = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["PosterURL"], func(r *models.ScraperResult) string {
		return r.PosterURL
	})

	movie.CoverURL = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["CoverURL"], func(r *models.ScraperResult) string {
		return r.CoverURL
	})

	movie.TrailerURL = a.getFieldByPriority(resultsBySource, a.resolvedPriorities["TrailerURL"], func(r *models.ScraperResult) string {
		return r.TrailerURL
	})

	// Aggregate runtime
	movie.Runtime = a.getIntFieldByPriority(resultsBySource, a.resolvedPriorities["Runtime"], func(r *models.ScraperResult) int {
		return r.Runtime
	})

	// Aggregate release date
	movie.ReleaseDate = a.getTimeFieldByPriority(resultsBySource, a.resolvedPriorities["ReleaseDate"], func(r *models.ScraperResult) *time.Time {
		return r.ReleaseDate
	})

	if movie.ReleaseDate != nil {
		movie.ReleaseYear = movie.ReleaseDate.Year()
	}

	// Aggregate rating
	ratingScore, ratingVotes := a.getRatingByPriority(resultsBySource, a.resolvedPriorities["Rating"])
	movie.RatingScore = ratingScore
	movie.RatingVotes = ratingVotes

	// Aggregate actresses
	movie.Actresses = a.getActressesByPriority(resultsBySource, a.resolvedPriorities["Actress"])

	// Aggregate genres
	genreNames := a.getGenresByPriority(resultsBySource, a.resolvedPriorities["Genre"])
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
	movie.Screenshots = a.getScreenshotsByPriority(resultsBySource, a.resolvedPriorities["ScreenshotURL"])

	// Set source metadata
	if len(results) > 0 {
		movie.SourceName = results[0].Source
		movie.SourceURL = results[0].SourceURL
	}

	// Generate display name from template if configured
	if a.config.Metadata.NFO.DisplayName != "" {
		ctx := template.NewContextFromMovie(movie)
		displayName, err := a.templateEngine.Execute(a.config.Metadata.NFO.DisplayName, ctx)
		if err == nil {
			movie.DisplayName = displayName
		}
		// Silently ignore template errors - display name is optional
	}

	// Validate required fields if configured
	if len(a.config.Metadata.RequiredFields) > 0 {
		if err := a.validateRequiredFields(movie); err != nil {
			return nil, fmt.Errorf("required field validation failed: %w", err)
		}
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

	// Convert map to slice and apply alias conversion if enabled
	if len(actressMap) > 0 {
		actresses := make([]models.Actress, 0, len(actressMap))
		for _, actress := range actressMap {
			// Apply alias conversion if enabled
			if a.config.Metadata.ActressDatabase.ConvertAlias {
				a.applyActressAlias(actress)
			}
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
// Supports both exact string matching and regex patterns
func (a *Aggregator) isGenreIgnored(genre string) bool {
	// First, check compiled regex patterns
	for _, re := range a.ignoreGenreRegexes {
		if re.MatchString(genre) {
			return true
		}
	}

	// Fall back to exact string matching for non-regex patterns
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
	// Check cache first with read lock
	a.genreCacheMutex.RLock()
	replacement, exists := a.genreReplacementCache[original]
	a.genreCacheMutex.RUnlock()

	if exists {
		return replacement
	}

	// Auto-add genre if enabled and repository is available
	if a.config.Metadata.GenreReplacement.AutoAdd && a.genreReplacementRepo != nil {
		// Create identity mapping (genre maps to itself)
		genreReplacement := &models.GenreReplacement{
			Original:    original,
			Replacement: original,
		}

		// Try to create the replacement (will fail silently if already exists due to race condition)
		if err := a.genreReplacementRepo.Create(genreReplacement); err == nil {
			// Successfully added, update cache with write lock
			a.genreCacheMutex.Lock()
			a.genreReplacementCache[original] = original
			a.genreCacheMutex.Unlock()
		}
		// If create failed due to unique constraint (race condition), ignore the error
		// The genre is already in the database from another goroutine
	}

	// Return original if no replacement found
	return original
}

// ReloadGenreReplacements reloads the genre replacement cache from database
func (a *Aggregator) ReloadGenreReplacements() {
	a.loadGenreReplacementCache()
}

// validateRequiredFields checks if all required fields are present and non-empty
func (a *Aggregator) validateRequiredFields(movie *models.Movie) error {
	missingFields := []string{}

	for _, fieldName := range a.config.Metadata.RequiredFields {
		// Normalize field name to lowercase for case-insensitive comparison
		fieldLower := strings.ToLower(fieldName)

		// Check each field
		switch fieldLower {
		case "id":
			if movie.ID == "" {
				missingFields = append(missingFields, "ID")
			}
		case "contentid", "content_id":
			if movie.ContentID == "" {
				missingFields = append(missingFields, "ContentID")
			}
		case "title":
			if movie.Title == "" {
				missingFields = append(missingFields, "Title")
			}
		case "originaltitle", "original_title":
			if movie.OriginalTitle == "" {
				missingFields = append(missingFields, "OriginalTitle")
			}
		case "description", "plot":
			if movie.Description == "" {
				missingFields = append(missingFields, "Description")
			}
		case "director":
			if movie.Director == "" {
				missingFields = append(missingFields, "Director")
			}
		case "maker", "studio":
			if movie.Maker == "" {
				missingFields = append(missingFields, "Maker")
			}
		case "label":
			if movie.Label == "" {
				missingFields = append(missingFields, "Label")
			}
		case "series", "set":
			if movie.Series == "" {
				missingFields = append(missingFields, "Series")
			}
		case "releasedate", "release_date", "premiered":
			if movie.ReleaseDate == nil {
				missingFields = append(missingFields, "ReleaseDate")
			}
		case "runtime":
			if movie.Runtime == 0 {
				missingFields = append(missingFields, "Runtime")
			}
		case "coverurl", "cover_url", "cover":
			if movie.CoverURL == "" {
				missingFields = append(missingFields, "CoverURL")
			}
		case "posterurl", "poster_url", "poster":
			if movie.PosterURL == "" {
				missingFields = append(missingFields, "PosterURL")
			}
		case "trailerurl", "trailer_url", "trailer":
			if movie.TrailerURL == "" {
				missingFields = append(missingFields, "TrailerURL")
			}
		case "screenshots", "screenshot_url", "screenshoturl":
			if len(movie.Screenshots) == 0 {
				missingFields = append(missingFields, "Screenshots")
			}
		case "actresses", "actress":
			if len(movie.Actresses) == 0 {
				missingFields = append(missingFields, "Actresses")
			}
		case "genres", "genre":
			if len(movie.Genres) == 0 {
				missingFields = append(missingFields, "Genres")
			}
		case "rating", "ratingscore", "rating_score":
			if movie.RatingScore == 0 {
				missingFields = append(missingFields, "RatingScore")
			}
		default:
			// Unknown field name - log warning but don't fail
			// This allows for forward compatibility
			continue
		}
	}

	if len(missingFields) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missingFields, ", "))
	}

	return nil
}
