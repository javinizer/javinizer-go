package aggregator

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/translation"
)

// AggregatorInterface abstracts aggregator operations for dependency injection.
// Allows CLI commands and API endpoints to accept either real Aggregator or test mocks.
// Added in Epic 8 Story 8.2 to enable testable aggregation logic.
type AggregatorInterface interface {
	// Aggregate merges multiple scraper results into a single Movie based on field-level priorities
	Aggregate(results []*models.ScraperResult) (*models.Movie, error)

	// GetResolvedPriorities returns the cached field-level priority map (for debugging)
	GetResolvedPriorities() map[string][]string
}

// AggregatorOptions allows optional dependency injection for testing.
// Fields left nil will be initialized with real implementations or skipped entirely.
// Added in Epic 8 Story 8.2 to support testable aggregator initialization.
type AggregatorOptions struct {
	// GenreReplacementRepo is an optional genre replacement repository for tests.
	// If nil, loadGenreReplacementCache() is skipped (empty cache).
	// If non-nil, genre replacements are loaded from the repository during initialization.
	GenreReplacementRepo *database.GenreReplacementRepository

	// ActressAliasRepo is an optional actress alias repository for tests.
	// If nil, loadActressAliasCache() is skipped (empty cache).
	// If non-nil, actress aliases are loaded from the repository during initialization.
	ActressAliasRepo *database.ActressAliasRepository

	// TemplateEngine is an optional template engine for tests.
	// If nil, a real template.NewEngine() is created.
	// If non-nil, the injected template engine is used.
	TemplateEngine *template.Engine

	// GenreCache is an optional pre-populated genre replacement cache for tests.
	// If non-nil, this cache is used directly without loading from database.
	// Takes precedence over GenreReplacementRepo if both are provided.
	GenreCache map[string]string

	// ActressCache is an optional pre-populated actress alias cache for tests.
	// If non-nil, this cache is used directly without loading from database.
	// Takes precedence over ActressAliasRepo if both are provided.
	ActressCache map[string]string

	// Scrapers is an optional list of scrapers for dependency injection.
	// If non-nil, the scrapers are used in order as their priority for aggregation.
	// If nil, scraperutil.GetPriorities() is used for backward compatibility.
	Scrapers []models.Scraper
}

// Compile-time verification that Aggregator implements AggregatorInterface
var _ AggregatorInterface = (*Aggregator)(nil)

// Aggregator combines metadata from multiple scrapers based on priority
type Aggregator struct {
	config                *config.Config
	scrapers              []models.Scraper // Injected scrapers for priority (nil = use scraperutil)
	templateEngine        *template.Engine
	genreReplacementRepo  *database.GenreReplacementRepository
	genreReplacementCache map[string]string
	genreCacheMutex       sync.RWMutex // Protects genreReplacementCache from concurrent access
	actressAliasRepo      *database.ActressAliasRepository
	actressAliasCache     map[string]string   // Maps alias name to canonical name
	aliasCacheMutex       sync.RWMutex        // Protects actressAliasCache from concurrent access
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

// NewWithOptions creates a new aggregator with optional dependency injection.
// If opts is nil or opts fields are nil, real implementations are created or database loading is skipped.
// If opts fields are non-nil, injected dependencies are used (for testing).
// Added in Epic 8 Story 8.2 to enable testable aggregator initialization.
//
// Production usage: Use NewWithDatabase() instead
// Test usage: aggregator.NewWithOptions(cfg, &AggregatorOptions{GenreCache: mockCache})
func NewWithOptions(cfg *config.Config, opts *AggregatorOptions) *Aggregator {
	if cfg == nil {
		return nil // Defensive: prevent nil config
	}

	agg := &Aggregator{
		config:                cfg,
		scrapers:              nil, // Default empty, populated below if provided
		genreReplacementCache: make(map[string]string),
		actressAliasCache:     make(map[string]string),
	}

	// Store injected scrapers if provided (for priority ordering)
	if opts != nil && opts.Scrapers != nil {
		agg.scrapers = opts.Scrapers
	}

	// Use injected template engine or create real one
	if opts != nil && opts.TemplateEngine != nil {
		agg.templateEngine = opts.TemplateEngine
	} else {
		agg.templateEngine = template.NewEngine()
	}

	// Use injected genre replacement repository or skip
	if opts != nil && opts.GenreReplacementRepo != nil {
		agg.genreReplacementRepo = opts.GenreReplacementRepo
	}

	// Use injected actress alias repository or skip
	if opts != nil && opts.ActressAliasRepo != nil {
		agg.actressAliasRepo = opts.ActressAliasRepo
	}

	// Use pre-populated genre cache if provided (for tests)
	if opts != nil && opts.GenreCache != nil {
		agg.genreCacheMutex.Lock()
		agg.genreReplacementCache = opts.GenreCache
		agg.genreCacheMutex.Unlock()
	} else if agg.genreReplacementRepo != nil && agg.config.Metadata.GenreReplacement.Enabled {
		// Load from database if repository is available
		agg.loadGenreReplacementCache()
	}

	// Use pre-populated actress cache if provided (for tests)
	if opts != nil && opts.ActressCache != nil {
		agg.aliasCacheMutex.Lock()
		agg.actressAliasCache = opts.ActressCache
		agg.aliasCacheMutex.Unlock()
	} else if agg.actressAliasRepo != nil && agg.config.Metadata.ActressDatabase.Enabled {
		// Load from database if repository is available
		agg.loadActressAliasCache()
	}

	// Resolve field-level priorities (always required)
	agg.resolvePriorities()

	// Compile genre filter regexes (always required)
	agg.compileGenreRegexes()

	return agg
}

// NewWithDatabase creates a new aggregator with database support for genre replacements and actress aliases.
// This is the production constructor - for testable constructor see NewWithOptions.
// Refactored in Epic 8 Story 8.2 to wrap NewWithOptions for backward compatibility.
func NewWithDatabase(cfg *config.Config, db *database.DB) *Aggregator {
	return NewWithOptions(cfg, &AggregatorOptions{
		GenreReplacementRepo: database.NewGenreReplacementRepository(db),
		ActressAliasRepo:     database.NewActressAliasRepository(db),
		TemplateEngine:       nil, // Use default template.NewEngine()
	})
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

// isRegexPattern checks if a string contains regex metacharacters
// Only returns true for patterns with clear regex intent
// Avoids false positives on literal dots in names like "S1.No1Style"
func isRegexPattern(s string) bool {
	if s == "" {
		return false
	}
	// Check for anchor characters (highest confidence indicators)
	if s[0] == '^' || s[len(s)-1] == '$' {
		return true
	}
	// Check for quantifier patterns (*, +, ?) that follow other characters
	// This catches patterns like "test*", "test+", "test?" which are clearly regex
	if len(s) >= 2 {
		for i := 0; i < len(s)-1; i++ {
			// Check if next character is a quantifier
			if s[i+1] == '*' || s[i+1] == '+' || s[i+1] == '?' {
				return true
			}
		}
	}
	// Check for other unambiguous regex metacharacters
	// Note: we explicitly exclude lone dots (.) as they're common in genre names
	meta := []rune{'\\', '[', ']', '(', ')', '|', '{', '}'}
	for _, r := range s {
		for _, m := range meta {
			if r == m {
				return true
			}
		}
	}
	return false
}

// resolvePriorities resolves all field priorities at initialization time
// With simplified priorities, all fields use the same global priority derived from scraper registrations
func (a *Aggregator) resolvePriorities() {
	a.resolvedPriorities = make(map[string][]string)

	var globalPriority []string

	// If scrapers are injected, use their names in order as priority
	if len(a.scrapers) > 0 {
		globalPriority = make([]string, 0, len(a.scrapers))
		for _, s := range a.scrapers {
			globalPriority = append(globalPriority, s.Name())
		}
	} else {
		// Fall back to scraperutil for backward compatibility
		globalPriority = getFieldPriorityFromConfig(a.config, "")
	}

	// List of all metadata fields that need priority resolution
	fields := []string{
		"ID", "ContentID", "Title", "OriginalTitle", "Description",
		"Director", "Maker", "Label", "Series", "PosterURL", "CoverURL",
		"TrailerURL", "Runtime", "ReleaseDate", "Rating", "Actress",
		"Genre", "ScreenshotURL",
	}

	for _, field := range fields {
		// All fields now use the same global priority
		a.resolvedPriorities[field] = copySlice(globalPriority)
	}
}

// GetResolvedPriorities returns the cached field-level priority map (for debugging)
// Implements AggregatorInterface
func (a *Aggregator) GetResolvedPriorities() map[string][]string {
	return a.resolvedPriorities
}

// getFieldPriorityFromConfig returns the scraper priority list.
// With simplified priorities, this always returns one global priority.
// User can override via cfg.Metadata.Priority.Priority; otherwise falls back to cfg.Scrapers.Priority.
func getFieldPriorityFromConfig(cfg *config.Config, fieldKey string) []string {
	if cfg == nil {
		if priorities := scraperutil.GetPriorities(); len(priorities) > 0 {
			return priorities
		}
		return nil
	}

	// If user explicitly set priority, use it
	if len(cfg.Metadata.Priority.Priority) > 0 {
		return cfg.Metadata.Priority.Priority
	}

	// Fall back to configured global scraper priority.
	// This keeps default aggregation behavior aligned with scrape execution order.
	return cfg.Scrapers.Priority
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
	return a.aggregateWithPriority(results, func(field string) []string {
		return a.resolvedPriorities[field]
	})
}

// AggregateWithPriority aggregates results using a custom scraper priority order
// This is used for manual scraping where users specify which scrapers to use and in what order
func (a *Aggregator) AggregateWithPriority(results []*models.ScraperResult, customPriority []string) (*models.Movie, error) {
	return a.aggregateWithPriority(results, func(field string) []string {
		return customPriority
	})
}

// aggregateWithPriority contains the shared aggregation logic used by both Aggregate and AggregateWithPriority.
// The priorityFunc parameter returns the priority list for a given field name.
func (a *Aggregator) aggregateWithPriority(results []*models.ScraperResult, priorityFunc func(field string) []string) (*models.Movie, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no scraper results to aggregate")
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

	a.ApplyConfiguredTranslation(movie)

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
			return nil, fmt.Errorf("required field validation failed: %w", err)
		}
	}

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
	// Collect actresses from all sources, keyed by DMMID (most reliable identifier)
	actressByDMMID := make(map[int]*models.Actress)
	// Track actresses without DMMID separately (keyed by name)
	actressByName := make(map[string]*models.Actress)

	// Process sources in priority order
	for _, source := range priority {
		result, exists := results[source]
		if !exists || len(result.Actresses) == 0 {
			continue
		}

		for _, info := range result.Actresses {
			// Determine the name-based key for this actress
			nameKey := info.JapaneseName
			if nameKey == "" {
				nameKey = info.FirstName + " " + info.LastName
			}

			// Check if we already have this actress by DMMID or by name
			var existing *models.Actress
			var foundInDMMIDMap bool

			// Primary match: by DMMID (most reliable)
			if info.DMMID != 0 {
				existing, foundInDMMIDMap = actressByDMMID[info.DMMID]
			}

			// Secondary match: by name (for cases where one source has DMMID and other doesn't)
			if existing == nil && nameKey != "" {
				// Check DMMID map first (in case we need to upgrade a name-only entry)
				for _, actress := range actressByDMMID {
					actressNameKey := actress.JapaneseName
					if actressNameKey == "" {
						actressNameKey = actress.FirstName + " " + actress.LastName
					}
					if actressNameKey == nameKey {
						existing = actress
						foundInDMMIDMap = true
						break
					}
				}

				// Check name map if not found in DMMID map
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

func (a *Aggregator) ApplyConfiguredTranslation(movie *models.Movie) {
	if a == nil || movie == nil || a.config == nil {
		logging.Debugf("Translation: skipped (nil aggregator, movie, or config)")
		return
	}

	translationCfg := a.config.Metadata.Translation
	if !translationCfg.Enabled {
		logging.Debugf("Translation: skipped (disabled)")
		return
	}

	settingsHash := translationCfg.SettingsHash()
	logging.Debugf("Translation: starting (provider=%s, source=%s, target=%s, hash=%s)", translationCfg.Provider, translationCfg.SourceLanguage, translationCfg.TargetLanguage, settingsHash)

	timeout := translationCfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	service := translation.New(translationCfg)
	translatedRecord, err := service.TranslateMovie(ctx, movie, settingsHash)
	if err != nil {
		id := movie.ID
		if id == "" {
			id = movie.ContentID
		}
		logging.Warnf("[%s] Metadata translation failed: %v", id, err)
		return
	}
	if translatedRecord == nil {
		logging.Debugf("Translation: returned nil record (no fields to translate or source==target)")
		return
	}

	logging.Debugf("Translation: appending %s translation (title=%q, hash=%s)", translatedRecord.Language, translatedRecord.Title, translatedRecord.SettingsHash)

	movie.Translations = mergeOrAppendTranslation(
		movie.Translations,
		*translatedRecord,
		translationCfg.OverwriteExistingTarget,
	)

	logging.Debugf("Translation: movie now has %d translation(s)", len(movie.Translations))
}

// applyGenreReplacement applies genre replacement if one exists
func (a *Aggregator) applyGenreReplacement(original string) string {
	// Feature toggle: bypass DB-backed genre replacement entirely when disabled.
	if a == nil || a.config == nil || !a.config.Metadata.GenreReplacement.Enabled {
		return original
	}

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
