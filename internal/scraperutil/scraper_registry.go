// Package scraperutil provides the scraper registry infrastructure and module system used
// by both individual scraper plugin implementations and the scrape engine.
//
// The ScraperRegistry (defined in scraper_registry.go) is the central type — it holds both
// constructor registrations (populated by plugins at init time) and initialized scraper
// instances (populated at startup). It is the single source of truth for which scrapers
// exist, their priorities, defaults, and runtime instances.
//
// Scraper plugins (in scraper/) self-register via the module pattern (module.go) during init().
// The scrape engine (in scrape/) queries the registry at runtime to discover available scrapers.
//
// Key types:
//   - ScraperRegistry — dual-purpose registry (registrations + instances) with thread-safe access
//   - ScraperRegistration — metadata for one scraper: constructor, defaults, priority, name
//   - ScraperModule — interface for scraper plugin packages to implement
package scraperutil

import (
	"fmt"
	"sort"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ScraperOptionsProvider groups display metadata with scraper-specific option entries.
type ScraperOptionsProvider struct {
	DisplayTitle string
	Options      []models.ScraperOption
}

// DefaultSettings holds a scraper's default settings along with its priority.
type DefaultSettings struct {
	Settings models.ScraperSettings
	Priority int
}

var (
	_ models.ScraperConfigResolverInterface = (*ScraperRegistry)(nil)
	_ ScraperCatalogInterface               = (*ScraperRegistry)(nil)
	_ ScraperInstancesInterface             = (*ScraperRegistry)(nil)
	_ ScraperRegistrar                      = (*ScraperRegistry)(nil)
	_ ScraperListerInterface                = (*ScraperRegistry)(nil)
)

// ScraperDeps holds typed dependencies for scraper construction.
// Passed to ScraperConstructor instead of separate any-typed parameters.
type ScraperDeps struct {
	Settings      models.ScraperSettings
	GlobalProxy   *models.ProxyConfig
	FlareSolverr  models.FlareSolverrConfig
	ContentIDRepo models.ContentIDMappingRepositoryInterface
	R18DevDump    models.R18DevDumpLookup

	TimeoutSeconds int
	ScrapeActress  bool
	Browser        models.BrowserConfig
}

// ScraperConstructor creates a Scraper from typed dependencies.
type ScraperConstructor func(deps ScraperDeps) (models.Scraper, error)

// ScraperRegistration holds the metadata for one scraper in the registry.
type ScraperRegistration struct {
	Name        string
	Description string
	Constructor ScraperConstructor
	Defaults    models.ScraperSettings
	Priority    int
	Options     []models.ScraperOption

	// ValidateFn performs scraper-specific validation on the settings.
	// The framework calls ScraperSettings.Validate(name) as a base check first,
	// so ValidateFn closures do NOT need to call ss.Validate(name) themselves.
	ValidateFn func(*models.ScraperSettings) error
}

// ScraperRegistry is the central registry for scraper registration metadata and
// runtime instances. It is the single source of truth for which scrapers exist,
// their priorities, defaults, and runtime instances.
//
// It provides two logical method groups:
//   - Registration/Catalog: Get, GetAll, Names, Priorities, IsRegistered, GetOptions,
//     GetScraperConstructor, GetScraperConstructors, GetAllDefaults, GetDefaults, GetValidateFn
//   - Instance queries: GetInstance, GetAllInstances, GetEnabledInstances,
//     GetInstancesByPriority, GetInstancesByPriorityForInput
//
// Per Go convention, callers that need a narrow surface should define their own
// interface at the call site rather than depending on the full ScraperRegistry.
//
// Internally it composes two sub-structs:
//   - catalog: write-once registration metadata (populated by plugins at init time)
//   - store: runtime scraper instances (populated at startup)
type ScraperRegistry struct {
	catalog *registrationCatalog
	store   *instanceStore
}

// NewScraperRegistry creates a new ScraperRegistry with both sub-structs initialized.
func NewScraperRegistry() *ScraperRegistry {
	catalog := newregistrationCatalog()
	return &ScraperRegistry{
		catalog: catalog,
		store:   newInstanceStore(),
	}
}

// --- Registration concern (delegates to catalog) ---

// Register adds a scraper registration to the registry.
func (r *ScraperRegistry) Register(reg ScraperRegistration) {
	r.catalog.Register(reg)
}

// Get returns the registration metadata for the named scraper.
func (r *ScraperRegistry) Get(name string) (ScraperRegistration, bool) {
	return r.catalog.Get(name)
}

// GetAll returns a copy of the registry map.
// Options and Defaults are cloned to prevent mutations from affecting the original.
func (r *ScraperRegistry) GetAll() map[string]ScraperRegistration {
	return r.catalog.GetAll()
}

// Names returns the names of all registered scrapers.
func (r *ScraperRegistry) Names() []string {
	return r.catalog.Names()
}

// Priorities returns the names of all registered scrapers ordered by descending priority, with ties broken alphabetically.
func (r *ScraperRegistry) Priorities() []string {
	return r.catalog.Priorities()
}

// IsRegistered reports whether a scraper with the given name is registered.
func (r *ScraperRegistry) IsRegistered(name string) bool {
	return r.catalog.IsRegistered(name)
}

// GetOptions returns scraper options for the named scraper from this registry.
func (r *ScraperRegistry) GetOptions(name string) (ScraperOptionsProvider, bool) {
	return r.catalog.GetOptions(name)
}

// GetScraperConstructors returns a map of scraper constructors from this registry.
func (r *ScraperRegistry) GetScraperConstructors() map[string]ScraperConstructor {
	return r.catalog.GetScraperConstructors()
}

// GetScraperConstructor returns the constructor for the named scraper from this registry.
func (r *ScraperRegistry) GetScraperConstructor(name string) (ScraperConstructor, bool) {
	return r.catalog.GetScraperConstructor(name)
}

// --- Instance concern (delegates to store) ---

// RegisterInstance adds a scraper instance to the registry.
// Passing nil is a safe no-op.
func (r *ScraperRegistry) RegisterInstance(scraper models.Scraper) {
	r.store.RegisterInstance(scraper)
}

// GetInstance retrieves a scraper instance by name.
// Returns (nil, false) if the name is not found.
func (r *ScraperRegistry) GetInstance(name string) (models.Scraper, bool) {
	return r.store.GetInstance(name)
}

// GetAllInstances returns all registered scraper instances in sorted key order
// for deterministic iteration. Nil entries are skipped.
func (r *ScraperRegistry) GetAllInstances() []models.Scraper {
	return r.store.GetAllInstances()
}

// GetEnabledInstances returns all enabled scraper instances in sorted key order
// for deterministic iteration.
func (r *ScraperRegistry) GetEnabledInstances() []models.Scraper {
	return r.store.GetEnabledInstances()
}

// GetInstancesByPriority returns enabled scraper instances in the specified priority order.
// If the priority list is empty or nil, returns all enabled instances in sorted key order.
// Only returns instances that are both in the priority list AND enabled.
func (r *ScraperRegistry) GetInstancesByPriority(priority []string) []models.Scraper {
	return r.store.GetInstancesByPriority(priority)
}

// GetInstancesByPriorityForInput returns enabled instances in priority order, but moves
// instances with matching query resolvers to the front for the provided input.
//
// If no instance resolver matches, the original GetInstancesByPriority ordering is
// returned unchanged.
func (r *ScraperRegistry) GetInstancesByPriorityForInput(priority []string, input string) []models.Scraper {
	return r.store.GetInstancesByPriorityForInput(priority, input)
}

// --- Config concern (operates on catalog directly) ---

// GetAllDefaults returns a map of default scraper settings from this registry.
// Values are models.ScraperSettings (satisfying models.ScraperConfigResolverInterface).
func (r *ScraperRegistry) GetAllDefaults() map[string]models.ScraperSettings {
	result := make(map[string]models.ScraperSettings)
	for name, reg := range r.catalog.GetAll() {
		result[name] = reg.Defaults
	}
	return result
}

// GetValidateFn returns a scraper-specific validation function for the named scraper.
// Returns nil if no ValidateFn is registered for the scraper.
func (r *ScraperRegistry) GetValidateFn(name string) func(*models.ScraperSettings) error {
	reg, ok := r.catalog.Get(name)
	if !ok {
		return nil
	}
	return reg.ValidateFn
}

// GetDefaults returns a map of default settings from this registry.
func (r *ScraperRegistry) GetDefaults() map[string]DefaultSettings {
	result := make(map[string]DefaultSettings)
	for name, reg := range r.catalog.GetAll() {
		result[name] = DefaultSettings{
			Settings: reg.Defaults,
			Priority: reg.Priority,
		}
	}
	return result
}

// --- Facade-level methods ---

// InitInstances populates the instances map from the registration metadata.
// Iterates over registered scrapers (constructor metadata), calls each constructor
// with the corresponding ScraperDeps, and stores the resulting Scraper instances.
//
// Lock ordering: catalog before store. InitInstances acquires catalog first
// (via GetAll), then store (via RegisterInstance). Never acquire in reverse order.
//
// depsMap maps scraper name to its typed dependencies. It must not be nil.
//
// InitInstances must be called before any concurrent registry access.
// It populates the instances map from the registration metadata and is
// safe only during single-threaded bootstrap.
func (r *ScraperRegistry) InitInstances(depsMap map[string]ScraperDeps) error {
	if depsMap == nil {
		return fmt.Errorf("depsMap cannot be nil")
	}

	for name, registration := range r.catalog.GetAll() {
		if registration.Constructor == nil {
			logging.Warnf("Nil constructor for %s, skipping", name)
			continue
		}
		deps, ok := depsMap[name]
		if !ok {
			logging.Warnf("No configuration found for %s scraper, skipping", name)
			continue
		}
		scraper, err := registration.Constructor(deps)
		if err != nil {
			logging.Warnf("Failed to initialize %s scraper: %v", name, err)
			continue
		}
		if scraper == nil {
			logging.Warnf("Constructor for %s returned nil, skipping", name)
			continue
		}
		r.store.RegisterInstance(scraper)
	}

	logging.Infof("Registered %d scrapers", len(r.store.GetAllInstances()))
	return nil
}

// CollectDownloadProxyResolvers collects scrapers from the registry that implement
// the DownloadProxyResolver interface, ordered by scraper priority.
// Uses type assertion to find resolvers — scrapers that don't implement
// DownloadProxyResolver are skipped.
func (r *ScraperRegistry) CollectDownloadProxyResolvers(priority []string) []models.DownloadProxyResolver {
	if r == nil {
		return nil
	}

	resolvers := make([]models.DownloadProxyResolver, 0)
	seen := make(map[string]struct{})
	add := func(scraper models.Scraper) {
		if scraper == nil {
			return
		}
		name := scraper.Name()
		if _, ok := seen[name]; ok {
			return
		}
		resolver, ok := scraper.(models.DownloadProxyResolver)
		if ok {
			resolvers = append(resolvers, resolver)
		}
		seen[name] = struct{}{}
	}

	for _, name := range priority {
		scraper, exists := r.GetInstance(name)
		if exists {
			add(scraper)
		}
	}

	remaining := make([]string, 0)
	for _, scraper := range r.GetAllInstances() {
		if scraper == nil {
			continue
		}
		if _, ok := seen[scraper.Name()]; ok {
			continue
		}
		remaining = append(remaining, scraper.Name())
	}
	sort.Strings(remaining)
	for _, name := range remaining {
		scraper, exists := r.GetInstance(name)
		if exists {
			add(scraper)
		}
	}

	return resolvers
}
