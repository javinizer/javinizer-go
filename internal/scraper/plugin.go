package scraper

import (
	"fmt"
	"sort"
	"sync"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// ScraperConstructor is a function that creates a scraper instance.
// Parameters: settings, db, globalScrapersConfig
type ScraperConstructor func(config.ScraperSettings, *database.DB, *config.ScrapersConfig) (models.Scraper, error)

var (
	globalConstructorRegistry = make(map[string]ScraperConstructor)
	constructorMu             sync.RWMutex
)

// RegisterScraper registers a scraper constructor for init()-based auto-registration.
// This is called from each scraper package's init() function.
// The constructor will be called by NewDefaultScraperRegistry with actual config and db.
// Panics if constructor is nil - init-time failures are preferable to runtime crashes.
func RegisterScraper(name string, constructor ScraperConstructor) {
	if constructor == nil {
		panic(fmt.Sprintf("RegisterScraper: cannot register nil constructor for %q", name))
	}
	constructorMu.Lock()
	defer constructorMu.Unlock()
	globalConstructorRegistry[name] = constructor
}

// GetScraperConstructors returns a copy of all registered scraper constructors.
// Primarily used by NewDefaultScraperRegistry.
func GetScraperConstructors() map[string]ScraperConstructor {
	constructorMu.RLock()
	defer constructorMu.RUnlock()
	result := make(map[string]ScraperConstructor, len(globalConstructorRegistry))
	for k, v := range globalConstructorRegistry {
		result[k] = v
	}
	return result
}

// ResetConstructors clears the constructor registry.
// Primarily used for test isolation.
func ResetConstructors() {
	constructorMu.Lock()
	defer constructorMu.Unlock()
	globalConstructorRegistry = make(map[string]ScraperConstructor)
}

// DefaultSettings holds a scraper's default configuration and priority.
// Used by RegisterScraperDefaults for self-reported scraper priorities.
type DefaultSettings struct {
	Settings config.ScraperSettings
	Priority int
}

var (
	globalDefaultsRegistry = make(map[string]DefaultSettings)
	defaultsMu             sync.RWMutex
)

// RegisterScraperDefaults registers default settings and priority for a scraper.
// Called from each scraper package's init() function.
// Also registers with scraperutil for config.go to use via GetDefaultScraperSettings().
func RegisterScraperDefaults(name string, defaults DefaultSettings) {
	defaultsMu.Lock()
	defer defaultsMu.Unlock()
	globalDefaultsRegistry[name] = defaults
	// Also register with scraperutil so config.go can build defaults via GetDefaultScraperSettings()
	// Settings is stored as any to avoid import cycle with config package.
	scraperutil.RegisterDefaultScraperSettings(name, defaults.Settings, defaults.Priority)
}

// GetRegisteredDefaults returns a copy of all registered scraper defaults.
func GetRegisteredDefaults() map[string]DefaultSettings {
	defaultsMu.RLock()
	defer defaultsMu.RUnlock()
	result := make(map[string]DefaultSettings, len(globalDefaultsRegistry))
	for k, v := range globalDefaultsRegistry {
		result[k] = v
	}
	return result
}

// ResetDefaults clears the defaults registry.
// Primarily used for test isolation.
func ResetDefaults() {
	defaultsMu.Lock()
	defer defaultsMu.Unlock()
	globalDefaultsRegistry = make(map[string]DefaultSettings)
	scraperutil.ResetDefaults() // Also clear scraperutil's defaults registry
}

// Create instantiates a single scraper by name with the provided settings.
// This is the factory method that enables dynamic scraper instantiation with custom settings,
// rather than NewDefaultScraperRegistry which creates all scrapers at once.
//
// Parameters:
//   - name: The scraper name (e.g., "r18dev", "dmm")
//   - settings: The scraper configuration settings
//   - db: The database connection (can be nil for scrapers that don't need it)
//   - globalScrapersConfig: The global scrapers configuration (can be nil)
//
// Returns:
//   - A new Scraper instance configured with the provided settings
//   - error if the scraper name is not registered or instantiation fails
//
// Example usage:
//
//	settings := config.ScraperSettings{Enabled: true, Language: "en"}
//	scraper, err := scraper.Create("r18dev", settings, db, &cfg.Scrapers)
//	if err != nil {
//	    log.Fatalf("Failed to create r18dev scraper: %v", err)
//	}
func Create(
	name string,
	settings config.ScraperSettings,
	db *database.DB,
	globalScrapersConfig *config.ScrapersConfig,
) (models.Scraper, error) {
	constructorMu.RLock()
	constructor, exists := globalConstructorRegistry[name]
	constructorMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("scraper not found: %q (available: %v)", name, getRegisteredScraperNames())
	}

	if constructor == nil {
		return nil, fmt.Errorf("scraper %q has nil constructor", name)
	}

	// Pass dependencies to constructor for scrapers that need database access
	// or global scrapers configuration.
	scraper, err := constructor(settings, db, globalScrapersConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s scraper: %w", name, err)
	}

	return scraper, nil
}

// getRegisteredScraperNames returns sorted list of registered scraper names for error messages.
func getRegisteredScraperNames() []string {
	constructorMu.RLock()
	defer constructorMu.RUnlock()
	names := make([]string, 0, len(globalConstructorRegistry))
	for name := range globalConstructorRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
