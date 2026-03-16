package commandutil

import (
	"fmt"
	"os"
	"path/filepath"

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

// DependenciesInterface abstracts Dependencies for test injection.
// Allows CLI commands to accept either real Dependencies or test mocks.
// Added in Epic 6 Story 6.1 to enable dependency injection for testability.
type DependenciesInterface interface {
	GetConfig() *config.Config
	GetDB() *database.DB
	GetScraperRegistry() *models.ScraperRegistry
	Close() error
}

// Dependencies contains all external dependencies that CLI commands need.
// This struct enables dependency injection for testing.
// Implements DependenciesInterface.
type Dependencies struct {
	Config          *config.Config
	DB              *database.DB
	ScraperRegistry *models.ScraperRegistry
}

// DependenciesOptions allows optional dependency injection for testing.
// Fields left nil will be initialized with real implementations.
// Added in Epic 6 Story 6.1 to support testable CLI commands.
type DependenciesOptions struct {
	DB              *database.DB            // Optional: injected database (for tests)
	ScraperRegistry *models.ScraperRegistry // Optional: injected registry (for tests)
}

// NewDependencies creates a new Dependencies instance from a config.
// It initializes the database connection and scraper registry.
// This is the production constructor - for testable constructor see NewDependenciesWithOptions.
func NewDependencies(cfg *config.Config) (*Dependencies, error) {
	return NewDependenciesWithOptions(cfg, nil)
}

// NewDependenciesWithOptions creates a new Dependencies instance with optional dependency injection.
// If opts is nil or opts fields are nil, real implementations are created.
// If opts fields are non-nil, injected dependencies are used (for testing).
// Added in Epic 6 Story 6.1 to enable testable CLI commands.
func NewDependenciesWithOptions(cfg *config.Config, opts *DependenciesOptions) (*Dependencies, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	deps := &Dependencies{
		Config: cfg,
	}

	// Use injected DB or create real one
	if opts != nil && opts.DB != nil {
		deps.DB = opts.DB
	} else {
		// Ensure database directory exists before opening database
		// This prevents "unable to open database file" errors on clean installs
		dbDir := filepath.Dir(cfg.Database.DSN)
		if err := os.MkdirAll(dbDir, 0777); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}

		// Initialize database
		db, err := database.New(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize database: %w", err)
		}

		// Run migrations
		if err := db.AutoMigrate(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}

		deps.DB = db
	}

	// Use injected registry or create real one
	if opts != nil && opts.ScraperRegistry != nil {
		deps.ScraperRegistry = opts.ScraperRegistry
	} else {
		// Initialize scraper registry
		registry := models.NewScraperRegistry()

		// Register scrapers based on config (same as runScrape)
		contentIDRepo := database.NewContentIDMappingRepository(deps.DB)
		registry.Register(r18dev.New(cfg))
		registry.Register(dmm.New(cfg, contentIDRepo))
		registry.Register(libredmm.New(cfg))
		registry.Register(mgstage.New(cfg))
		registry.Register(javdb.New(cfg))
		registry.Register(javbus.New(cfg))
		registry.Register(jav321.New(cfg))
		registry.Register(tokyohot.New(cfg))
		registry.Register(aventertainment.New(cfg))
		registry.Register(dlgetchu.New(cfg))
		registry.Register(caribbeancom.New(cfg))
		registry.Register(fc2.New(cfg))
		javLibraryProxy := config.ResolveScraperProxy(cfg.Scrapers.Proxy, cfg.Scrapers.JavLibrary.Proxy)
		javlib, err := javlibrary.New(&cfg.Scrapers.JavLibrary, javLibraryProxy, cfg.Scrapers.UserAgent)
		if err != nil {
			logging.Warnf("Failed to initialize JavLibrary scraper: %v", err)
		} else {
			registry.Register(javlib)
		}

		deps.ScraperRegistry = registry
	}

	return deps, nil
}

// GetConfig returns the config from dependencies (implements DependenciesInterface).
func (d *Dependencies) GetConfig() *config.Config {
	return d.Config
}

// GetDB returns the database from dependencies (implements DependenciesInterface).
func (d *Dependencies) GetDB() *database.DB {
	return d.DB
}

// GetScraperRegistry returns the scraper registry from dependencies (implements DependenciesInterface).
func (d *Dependencies) GetScraperRegistry() *models.ScraperRegistry {
	return d.ScraperRegistry
}

// Close closes all resources held by the Dependencies (implements DependenciesInterface).
// Should be called when done using the Dependencies.
func (d *Dependencies) Close() error {
	if d.DB != nil {
		return d.DB.Close()
	}
	return nil
}
