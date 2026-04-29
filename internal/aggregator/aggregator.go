package aggregator

import (
	"regexp"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// AggregatorInterface abstracts aggregator operations for dependency injection.
// Allows CLI commands and API endpoints to accept either real Aggregator or test mocks.
// Added in Epic 8 Story 8.2 to enable testable aggregation logic.
type AggregatorInterface interface {
	Aggregate(results []*models.ScraperResult) (*models.Movie, string, error)

	AggregateWithPriority(results []*models.ScraperResult, customPriority []string) (*models.Movie, string, error)

	GetResolvedPriorities() map[string][]string
}

// AggregatorOptions allows optional dependency injection for testing.
// Fields left nil will be initialized with real implementations or skipped entirely.
// Added in Epic 8 Story 8.2 to support testable aggregator initialization.
type AggregatorOptions struct {
	// GenreReplacementRepo is an optional genre replacement repository for tests.
	// If nil, loadGenreReplacementCache() is skipped (empty cache).
	// If non-nil, genre replacements are loaded from the repository during initialization.
	GenreReplacementRepo database.GenreReplacementRepositoryInterface

	// ActressAliasRepo is an optional actress alias repository for tests.
	// If nil, loadActressAliasCache() is skipped (empty cache).
	// If non-nil, actress aliases are loaded from the repository during initialization.
	ActressAliasRepo database.ActressAliasRepositoryInterface

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
	genreReplacementRepo  database.GenreReplacementRepositoryInterface
	genreReplacementCache map[string]string
	genreCacheMutex       sync.RWMutex // Protects genreReplacementCache from concurrent access
	actressAliasRepo      database.ActressAliasRepositoryInterface
	actressAliasCache     map[string]string   // Maps alias name to canonical name
	aliasCacheMutex       sync.RWMutex        // Protects actressAliasCache from concurrent access
	resolvedPriorities    map[string][]string // Cached resolved priorities for each field
	ignoreGenreRegexes    []*regexp.Regexp    // Compiled regex patterns for genre filtering
}

func (a *Aggregator) Config() *config.Config {
	if a == nil {
		return nil
	}
	return a.config
}

func (a *Aggregator) TemplateEngine() *template.Engine {
	if a == nil {
		return nil
	}
	return a.templateEngine
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
	return strings.Fields(fullName)
}
