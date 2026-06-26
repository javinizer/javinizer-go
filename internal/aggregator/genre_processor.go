package aggregator

import (
	"context"
	"errors"
	"regexp"
	"sync"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// genreProcessorInterface defines the contract for genre replacement and filtering.
// Extracted from Aggregator to isolate genre concerns with their own cache and mutex.
type genreProcessorInterface interface {
	// applyReplacement applies genre replacement if one exists in the cache.
	// Returns the replacement if found, or the original if no mapping exists.
	// When auto-add is enabled and a repo is available, unknown genres are
	// persisted as identity mappings.
	applyReplacement(original string) string

	// isIgnored checks whether a genre should be excluded based on compiled
	// regex patterns from the ignore_genres config or exact string matches.
	isIgnored(genre string) bool

	// Reload refreshes the genre replacement cache from the database.
	Reload(ctx context.Context)
}

// genreProcessor owns genre replacement and regex-based filtering.
// Each instance has its own cache, mutex, and repository — no shared mutable
// state with the parent Aggregator.
type genreProcessor struct {
	cfg                *MetadataConfig
	repo               genreLookup
	cache              map[string]string
	mu                 sync.RWMutex
	ignoreGenreRegexes []*regexp.Regexp
}

// NewGenreProcessor creates a genreProcessor from config and an optional repository.
// If cfg is nil, returns nil. If cfg.GenreReplacement.Enabled and repo is non-nil,
// the cache is loaded from the database. Regex patterns are always compiled from
// cfg.IgnoreGenres.
func NewGenreProcessor(cfg *MetadataConfig, repo genreLookup) *genreProcessor {
	if cfg == nil {
		return nil
	}
	gp := &genreProcessor{
		cfg:   cfg,
		repo:  repo,
		cache: make(map[string]string),
	}
	gp.mu.Lock()
	if cfg.GenreReplacement.Enabled && repo != nil {
		// Constructor context: there is no caller context available yet, so
		// we use context.Background(). The Reload method accepts a context
		// for callers that need cancellation support.
		gp.loadCacheLocked(context.Background())
	}
	gp.compileRegexes()
	gp.mu.Unlock()
	return gp
}

// applyReplacement applies genre replacement if one exists in the cache.
func (gp *genreProcessor) applyReplacement(original string) string {
	if gp == nil || gp.cfg == nil || !gp.cfg.GenreReplacement.Enabled {
		return original
	}

	// Check cache first with read lock
	gp.mu.RLock()
	replacement, exists := gp.cache[original]
	gp.mu.RUnlock()

	if exists {
		return replacement
	}

	// Auto-add genre if enabled and repository is available.
	if gp.cfg.GenreReplacement.AutoAdd && gp.repo != nil {
		genreReplacement := &models.GenreReplacement{
			Original:    original,
			Replacement: original,
		}

		// Re-check under write lock to avoid overwriting a concurrent insertion
		// that occurred between the RLock check above and this Lock.
		gp.mu.Lock()
		var shouldCreate bool
		if _, alreadyExists := gp.cache[original]; !alreadyExists {
			gp.cache[original] = original
			shouldCreate = true
		}
		gp.mu.Unlock()

		// Persist outside the lock — DB I/O must not block concurrent readers.
		// If Create fails the cache already has the identity mapping; Reload()
		// will rebuild from the DB and the entry may disappear, which is
		// acceptable for a best-effort auto-add of an identity mapping.
		if shouldCreate {
			if err := gp.repo.Create(context.Background(), genreReplacement); err != nil {
				if !errors.Is(err, database.ErrDuplicateKey) {
					logging.Warnf("genre auto-add failed for %q: %v", original, err)
				}
			}
		}
	}

	return original
}

// isIgnored checks whether a genre should be excluded.
func (gp *genreProcessor) isIgnored(genre string) bool {
	if gp == nil {
		return false
	}
	// Read regex slice under RLock to avoid racing with loadCache/compileRegexes.
	gp.mu.RLock()
	regexes := gp.ignoreGenreRegexes
	gp.mu.RUnlock()

	for _, re := range regexes {
		if re.MatchString(genre) {
			return true
		}
	}

	// Fall back to exact string matching for non-regex patterns
	if gp.cfg != nil {
		for _, ignored := range gp.cfg.IgnoreGenres {
			if genre == ignored {
				return true
			}
		}
	}

	return false
}

// Reload refreshes the genre replacement cache from the database.
func (gp *genreProcessor) Reload(ctx context.Context) {
	if gp == nil {
		return
	}
	gp.loadCache(ctx)
}

// loadCache loads genre replacements from the repository into memory.
// Acquires the write lock, so it is safe to call from Reload().
// Note: when called from the constructor (via loadCacheLocked), there is no
// caller context available, so context.Background() is used. Callers that
// need cancellation should use Reload(ctx) instead.
func (gp *genreProcessor) loadCache(ctx context.Context) {
	if gp.repo == nil {
		return
	}

	replacementMap, err := gp.repo.GetReplacementMap(ctx)
	if err == nil {
		gp.mu.Lock()
		gp.cache = replacementMap
		gp.compileRegexes()
		gp.mu.Unlock()
	}
}

func (gp *genreProcessor) loadCacheLocked(ctx context.Context) {
	if gp.repo == nil {
		return
	}

	replacementMap, err := gp.repo.GetReplacementMap(ctx)
	if err == nil {
		gp.cache = replacementMap
	}
}

// compileRegexes compiles regex patterns from the ignore_genres config.
// Patterns that look like regex (contain special chars) are compiled;
// plain strings are left for exact matching in isIgnored.
// Caller must hold gp.mu (or be in a single-goroutine construction path).
func (gp *genreProcessor) compileRegexes() {
	regexes := make([]*regexp.Regexp, 0)

	if gp.cfg != nil {
		for _, pattern := range gp.cfg.IgnoreGenres {
			if isRegexPattern(pattern) {
				compiled, err := regexp.Compile(pattern)
				if err == nil {
					regexes = append(regexes, compiled)
				} else {
					logging.Warnf("invalid ignore_genres regex %q: %v", pattern, err)
				}
			}
		}
	}

	gp.ignoreGenreRegexes = regexes
}
