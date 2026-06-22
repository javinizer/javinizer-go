package aggregator

import (
	"context"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/models"
)

// aliasResolverInterface defines the contract for actress alias resolution.
// Extracted from Aggregator to isolate alias concerns with their own cache and mutex.
type aliasResolverInterface interface {
	// Resolve converts an actress's name using the alias database.
	// It checks JapaneseName, FirstName LastName, and LastName FirstName
	// combinations against the alias cache.
	Resolve(actress *models.Actress)

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

	// Check cache with read lock
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	// Try Japanese name first
	if actress.JapaneseName != "" {
		if canonical, found := ar.cache[actress.JapaneseName]; found {
			actress.JapaneseName = canonical
			return
		}
	}

	// Try FirstName LastName combination
	if actress.FirstName != "" && actress.LastName != "" {
		fullName := actress.FirstName + " " + actress.LastName
		if canonical, found := ar.cache[fullName]; found {
			if len(canonical) > 0 {
				first, last := models.SplitFullName(canonical)
				if last != "" && !strings.Contains(last, " ") {
					actress.LastName = first
					actress.FirstName = last
				} else {
					actress.JapaneseName = canonical
				}
			}
			return
		}

		// Try LastName FirstName combination
		reverseName := actress.LastName + " " + actress.FirstName
		if canonical, found := ar.cache[reverseName]; found {
			if len(canonical) > 0 {
				first, last := models.SplitFullName(canonical)
				if last != "" && !strings.Contains(last, " ") {
					actress.LastName = first
					actress.FirstName = last
				} else {
					actress.JapaneseName = canonical
				}
			}
			return
		}
	}
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
	}
}
