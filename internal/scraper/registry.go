// Package scraper provides utilities for scraper registration and management.
package scraper

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// NewDefaultScraperRegistry creates a new scraper registry with all default scrapers.
// This is the single source of truth for scraper registration across all modes (API, TUI, CLI).
//
// Parameters:
//   - cfg: The application configuration
//   - db: The database connection (for ContentIDMappingRepository)
//
// Returns:
//   - *models.ScraperRegistry: The configured registry
//   - error: Any error encountered during scraper initialization
//
// The registry uses GetScraperConstructors() to discover all registered scrapers via init().
// JavLibrary is handled specially (removed from constructors, initialized separately) due to
// its unique proxy and initialization requirements.
//
// Note: Language validation for JavLibrary is performed in Config.Validate() to ensure
// consistent behavior across all initialization paths. Invalid languages are rejected
// at config load time rather than causing partial failures during startup.
func NewDefaultScraperRegistry(cfg *config.Config, db *database.DB) (*models.ScraperRegistry, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	registry := models.NewScraperRegistry()

	// Get all registered scraper constructors from init() registrations
	constructors := GetScraperConstructors()

	// Handle JavLibrary scraper specially (language validation is done in config)
	// This scraper requires special proxy resolution and is removed from the generic constructor map
	if javlibConstructor, ok := constructors["javlibrary"]; ok {
		delete(constructors, "javlibrary")
		javlib, err := javlibConstructor(cfg, db)
		if err != nil {
			logging.Warnf("Failed to initialize JavLibrary scraper: %v", err)
		} else {
			registry.Register(javlib)
		}
	}

	// Initialize all other scrapers via their registered constructors
	for name, constructor := range constructors {
		scraper, err := constructor(cfg, db)
		if err != nil {
			logging.Warnf("Failed to initialize %s scraper: %v", name, err)
			continue
		}
		registry.Register(scraper)
	}

	logging.Infof("Registered %d scrapers", len(registry.GetAll()))

	return registry, nil
}
