package commandutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
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
		// Initialize scraper registry using centralized function
		registry, err := scraper.NewDefaultScraperRegistry(cfg, deps.DB)
		if err != nil {
			_ = deps.DB.Close()
			return nil, fmt.Errorf("failed to initialize scraper registry: %w", err)
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
