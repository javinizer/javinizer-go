package scraperutil

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

// ScraperCatalogInterface is the read-only interface on ScraperRegistry for
// listing available scrapers and their registration metadata. Callers that
// only need to query catalog data (names, priorities, defaults, options,
// constructors) should accept this interface instead of the full ScraperRegistry.
//
// Per CONTEXT.md: "Narrows the ScraperRegistry for callers that only need
// registration metadata."
type ScraperCatalogInterface interface {
	// Get retrieves a scraper registration by name.
	Get(name string) (ScraperRegistration, bool)

	// GetAll returns a copy of the registry map.
	// Options and Defaults are cloned to prevent mutations from affecting the original.
	GetAll() map[string]ScraperRegistration

	// Names returns all registered scraper names in arbitrary order.
	Names() []string

	// Priorities returns scraper names sorted by priority (highest first),
	// with ties broken alphabetically by name.
	Priorities() []string

	// IsRegistered reports whether a scraper with the given name is registered.
	IsRegistered(name string) bool

	// GetOptions returns scraper options for the named scraper from this registry.
	GetOptions(name string) (ScraperOptionsProvider, bool)

	// GetScraperConstructors returns a map of scraper constructors from this registry.
	GetScraperConstructors() map[string]ScraperConstructor

	// GetScraperConstructor returns the constructor for the named scraper from this registry.
	GetScraperConstructor(name string) (ScraperConstructor, bool)

	// GetAllDefaults returns a map of default scraper settings from this registry.
	GetAllDefaults() map[string]models.ScraperSettings

	// GetValidateFn returns a scraper-specific validation function for the named scraper.
	// Returns nil if no ValidateFn is registered for the scraper.
	GetValidateFn(name string) func(*models.ScraperSettings) error

	// GetDefaults returns a map of default settings from this registry.
	GetDefaults() map[string]DefaultSettings
}

// ScraperInstancesInterface is the interface on ScraperRegistry for querying
// runtime scraper instances. Callers that only need to discover, resolve, or
// iterate scraper instances should accept this interface instead of the full
// ScraperRegistry.
//
// Per CONTEXT.md: "Narrows the ScraperRegistry for callers that only need
// instance queries."
type ScraperInstancesInterface interface {
	// RegisterInstance adds a scraper instance to the registry.
	// Passing nil is a safe no-op.
	RegisterInstance(scraper models.Scraper)

	// GetInstance retrieves a scraper instance by name.
	// Returns (nil, false) if the name is not found.
	GetInstance(name string) (models.Scraper, bool)

	// GetAllInstances returns all registered scraper instances in sorted key order
	// for deterministic iteration. Nil entries are skipped.
	GetAllInstances() []models.Scraper

	// GetEnabledInstances returns all enabled scraper instances in sorted key order
	// for deterministic iteration.
	GetEnabledInstances() []models.Scraper

	// GetInstancesByPriority returns enabled scraper instances in the specified priority order.
	// If the priority list is empty or nil, returns all enabled instances in sorted key order.
	// Only returns instances that are both in the priority list AND enabled.
	GetInstancesByPriority(priority []string) []models.Scraper

	// GetInstancesByPriorityForInput returns enabled instances in priority order, but moves
	// instances with matching query resolvers to the front for the provided input.
	GetInstancesByPriorityForInput(priority []string, input string) []models.Scraper
}

// ScraperRegistrar is the narrow interface for scraper plugin modules.
// Plugin modules only need the Register method to add their scraper metadata
// to the registry. Accepting this 1-method interface instead of the full
// ScraperRegistry reduces the coupling surface for plugin packages.
type ScraperRegistrar interface {
	// Register adds a scraper registration to the registry.
	Register(reg ScraperRegistration)
}

// ScraperListerInterface is the narrow interface the API layer requires from the
// scraper registry. It defines only the catalog and options methods needed
// by API handlers to list scrapers, check registration, and retrieve options.
//
// Defined in scraperutil (the canonical package) so that all consumers
// (API, TUI, health checks) share a single definition rather than each
// defining their own local copy.
type ScraperListerInterface interface {
	// GetOptions returns scraper options for the named scraper.
	GetOptions(name string) (ScraperOptionsProvider, bool)

	// GetAllInstances returns all registered scraper instances in sorted key order.
	GetAllInstances() []models.Scraper

	// GetEnabledInstances returns all enabled scraper instances in sorted key order.
	GetEnabledInstances() []models.Scraper
}
