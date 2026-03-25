package scraper

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ScraperConstructor is a function that creates a scraper instance
type ScraperConstructor func(*config.Config, *database.DB) (models.Scraper, error)

// globalConstructorRegistry holds scraper constructors for init()-based registration
var globalConstructorRegistry = make(map[string]ScraperConstructor)

// RegisterScraper registers a scraper constructor for init()-based auto-registration.
// This is called from each scraper package's init() function.
// The constructor will be called by NewDefaultScraperRegistry with actual config and db.
func RegisterScraper(name string, constructor ScraperConstructor) {
	globalConstructorRegistry[name] = constructor
}

// GetScraperConstructors returns a copy of all registered scraper constructors.
// Primarily used by NewDefaultScraperRegistry.
func GetScraperConstructors() map[string]ScraperConstructor {
	result := make(map[string]ScraperConstructor, len(globalConstructorRegistry))
	for k, v := range globalConstructorRegistry {
		result[k] = v
	}
	return result
}

// ResetConstructors clears the constructor registry.
// Primarily used for test isolation.
func ResetConstructors() {
	globalConstructorRegistry = make(map[string]ScraperConstructor)
}