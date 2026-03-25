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
func NewDefaultScraperRegistry(cfg *config.Config, db *database.DB) (*models.ScraperRegistry, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	registry := models.NewScraperRegistry()

	// Get all registered scraper constructors from init() registrations
	constructors := GetScraperConstructors()

	// Initialize all scrapers via their registered constructors
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
