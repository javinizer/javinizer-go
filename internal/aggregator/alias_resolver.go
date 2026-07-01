package aggregator

import (
	"context"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// aliasResolverInterface defines the contract for actress alias resolution.
// Extracted from Aggregator to isolate alias concerns with their own cache and mutex.
type aliasResolverInterface interface {
	// Resolve converts an actress's name using the alias database.
	// It checks JapaneseName, FirstName LastName, and LastName FirstName
	// combinations against the alias cache.
	Resolve(actress *models.Actress)

	// CanonicalName returns the canonical name for the given name components
	// without mutating them. Used for cross-source deduplication so that two
	// scrapers crediting the same person under different alias names collapse
	// into one entry. Unlike Resolve, this is gated only by
	// ActressDatabase.Enabled (not ConvertAlias): removing duplicates is
	// always correct, while renaming the display name is opt-in.
	// Returns "" when resolution is disabled or no alias is found.
	CanonicalName(japaneseName, firstName, lastName string) string

	// Reload refreshes the actress alias cache from the database.
	Reload(ctx context.Context)
}

// aliasResolver owns actress alias resolution logic.
// Each instance has its own cache, mutex, and repository — no shared
// mutable state with the parent Aggregator.
type aliasResolver struct {
	cfg   *MetadataConfig
	repo  aliasLookup
	cache map[string]string // Maps alias name to canonical name
	mu    sync.RWMutex
}

// NewAliasResolver creates an AliasResolver from config and an optional repository.
// If cfg is nil, returns nil. If cfg.ActressDatabase.Enabled and repo is non-nil,
// the cache is loaded from the database.
func NewAliasResolver(cfg *MetadataConfig, repo aliasLookup) *aliasResolver {
	if cfg == nil {
		return nil
	}
	ar := &aliasResolver{
		cfg:   cfg,
		repo:  repo,
		cache: make(map[string]string),
	}
	if cfg.ActressDatabase.Enabled && repo != nil {
		// Constructor context: there is no caller context available yet, so
		// we use context.Background(). The Reload method accepts a context
		// for callers that need cancellation support.
		ar.loadCache(context.Background())
	}
	return ar
}

// Resolve converts an actress's name using the alias database.
// It checks Japanese name first, then FirstName LastName, then LastName FirstName.
//
// Name ordering convention: canonical names in the alias cache follow the
// "LastName FirstName" ordering (Japanese convention, family name first).
// When a canonical name is split into two parts, parts[0] is assigned to
// LastName and parts[1] to FirstName. For names with 3+ parts, the full
// canonical string is stored as JapaneseName instead.
func (ar *aliasResolver) Resolve(actress *models.Actress) {
	if ar == nil || actress == nil {
		return
	}

	// Skip if alias conversion is not enabled
	if !ar.cfg.ActressDatabase.Enabled || !ar.cfg.ActressDatabase.ConvertAlias {
		return
	}

	ar.mu.RLock()
	defer ar.mu.RUnlock()

	// Japanese-name matches map directly to the canonical JapaneseName (no
	// First/Last split). Romanized matches split the canonical into LastName /
	// FirstName when it is two words, else fall back to JapaneseName. We check
	// the Japanese path first so its precedence and write behavior are
	// preserved — lookupLocked itself is precedence-agnostic about the result.
	if actress.JapaneseName != "" {
		if canonical, found := ar.cache[actress.JapaneseName]; found {
			actress.JapaneseName = canonical
			return
		}
	}

	if actress.FirstName != "" && actress.LastName != "" {
		canonical, found := ar.lookupLocked("", actress.FirstName, actress.LastName)
		if !found || canonical == "" {
			return
		}
		first, last := models.SplitFullName(canonical)
		if last != "" && !strings.Contains(last, " ") {
			actress.LastName = first
			actress.FirstName = last
		} else {
			actress.JapaneseName = canonical
		}
	}
}

// CanonicalName returns the canonical name for the given components without
// mutating any caller state. Gated only by ActressDatabase.Enabled so that
// deduplication works even when ConvertAlias (display rename) is disabled.
// Returns "" when disabled or no alias maps the input.
func (ar *aliasResolver) CanonicalName(japaneseName, firstName, lastName string) string {
	if ar == nil || !ar.cfg.ActressDatabase.Enabled {
		return ""
	}

	ar.mu.RLock()
	defer ar.mu.RUnlock()

	if canonical, found := ar.lookupLocked(japaneseName, firstName, lastName); found {
		return canonical
	}
	return ""
}

// lookupLocked performs the alias cache lookup shared by Resolve and
// CanonicalName. Caller must hold ar.mu (read or write).
// Precedence: JapaneseName, then "FirstName LastName", then "LastName FirstName".
func (ar *aliasResolver) lookupLocked(japaneseName, firstName, lastName string) (string, bool) {
	if japaneseName != "" {
		if canonical, found := ar.cache[japaneseName]; found {
			return canonical, true
		}
	}

	if firstName != "" && lastName != "" {
		if canonical, found := ar.cache[firstName+" "+lastName]; found {
			return canonical, true
		}
		if canonical, found := ar.cache[lastName+" "+firstName]; found {
			return canonical, true
		}
	}

	return "", false
}

// Reload refreshes the actress alias cache from the database.
func (ar *aliasResolver) Reload(ctx context.Context) {
	if ar == nil {
		return
	}
	ar.loadCache(ctx)
}

// loadCache loads actress aliases from the repository into memory.
// Note: when called from the constructor, there is no caller context available,
// so context.Background() is used. Callers that need cancellation should use
// Reload(ctx) instead, which delegates to this method with the provided context.
func (ar *aliasResolver) loadCache(ctx context.Context) {
	if ar.repo == nil {
		return
	}

	aliasMap, err := ar.repo.GetAliasMap(ctx)
	if err == nil {
		ar.mu.Lock()
		ar.cache = aliasMap
		ar.mu.Unlock()
	} else {
		logging.Warnf("aliasResolver: failed to load actress aliases: %v", err)
	}
}
