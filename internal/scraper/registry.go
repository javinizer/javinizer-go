// Package scraper provides utilities for scraper registration and management.
package scraper

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/aventertainment"
	"github.com/javinizer/javinizer-go/internal/scraper/caribbeancom"
	"github.com/javinizer/javinizer-go/internal/scraper/dlgetchu"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraper/fc2"
	"github.com/javinizer/javinizer-go/internal/scraper/jav321"
	"github.com/javinizer/javinizer-go/internal/scraper/javbus"
	"github.com/javinizer/javinizer-go/internal/scraper/javdb"
	"github.com/javinizer/javinizer-go/internal/scraper/javlibrary"
	"github.com/javinizer/javinizer-go/internal/scraper/libredmm"
	"github.com/javinizer/javinizer-go/internal/scraper/mgstage"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	"github.com/javinizer/javinizer-go/internal/scraper/tokyohot"
)

// ScraperInfo holds registration information for a scraper.
type ScraperInfo struct {
	Name        string
	Registrar   func(*config.Config, database.ContentIDMappingRepositoryInterface) (models.Scraper, error)
	IsEnabled   bool
	ErrorAction string // "warn" or "fail"
}

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
// The registry includes all scrapers from config:
//   - r18dev, dmm, libredmm, mgstage, javdb, javbus, jav321, tokyohot
//   - aventertainment, dlgetchu, caribbeancom, fc2
//   - javlibrary (always registered; language validation is done in Config.Validate())
//
// Note: Language validation for JavLibrary is performed in Config.Validate() to ensure
// consistent behavior across all initialization paths. Invalid languages are rejected
// at config load time rather than causing partial failures during startup.
func NewDefaultScraperRegistry(cfg *config.Config, db *database.DB) (*models.ScraperRegistry, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	registry := models.NewScraperRegistry()

	// Create ContentIDMappingRepository for DMM scraper
	contentIDRepo := database.NewContentIDMappingRepository(db)

	// Register all scrapers in a table-driven manner for maintainability
	scraperConfigs := []struct {
		name      string
		shouldAdd bool
		addFunc   func()
	}{
		{"r18dev", true, func() {
			registry.Register(r18dev.New(cfg))
		}},
		{"dmm", true, func() {
			registry.Register(dmm.New(cfg, contentIDRepo))
		}},
		{"libredmm", true, func() {
			registry.Register(libredmm.New(cfg))
		}},
		{"mgstage", true, func() {
			registry.Register(mgstage.New(cfg))
		}},
		{"javdb", true, func() {
			registry.Register(javdb.New(cfg))
		}},
		{"javbus", true, func() {
			registry.Register(javbus.New(cfg))
		}},
		{"jav321", true, func() {
			registry.Register(jav321.New(cfg))
		}},
		{"tokyohot", true, func() {
			registry.Register(tokyohot.New(cfg))
		}},
		{"aventertainment", true, func() {
			registry.Register(aventertainment.New(cfg))
		}},
		{"dlgetchu", true, func() {
			registry.Register(dlgetchu.New(cfg))
		}},
		{"caribbeancom", true, func() {
			registry.Register(caribbeancom.New(cfg))
		}},
		{"fc2", true, func() {
			registry.Register(fc2.New(cfg))
		}},
	}

	// Register all scrapers with enabled status
	for _, sc := range scraperConfigs {
		if sc.shouldAdd {
			sc.addFunc()
		}
	}

	// Handle JavLibrary scraper (language validation is done in config, but other errors possible)
	javLibraryProxy := config.ResolveScraperProxy(cfg.Scrapers.Proxy, cfg.Scrapers.JavLibrary.Proxy)
	javlib, err := javlibrary.New(&cfg.Scrapers.JavLibrary, javLibraryProxy, cfg.Scrapers.UserAgent)
	if err != nil {
		logging.Warnf("Failed to initialize JavLibrary scraper: %v", err)
	} else {
		registry.Register(javlib)
	}

	logging.Infof("Registered %d scrapers", len(registry.GetAll()))

	return registry, nil
}
