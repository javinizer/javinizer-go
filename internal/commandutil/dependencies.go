package commandutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/dmm"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
)

// Dependencies contains all external dependencies that CLI commands need.
// This struct enables dependency injection for testing.
type Dependencies struct {
	Config          *config.Config
	DB              *database.DB
	ScraperRegistry *models.ScraperRegistry
}

// NewDependencies creates a new Dependencies instance from a config.
// It initializes the database connection and scraper registry.
func NewDependencies(cfg *config.Config) (*Dependencies, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

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
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Initialize scraper registry
	registry := models.NewScraperRegistry()

	// Register scrapers based on config (same as runScrape)
	contentIDRepo := database.NewContentIDMappingRepository(db)
	registry.Register(r18dev.New(cfg))
	registry.Register(dmm.New(cfg, contentIDRepo))

	return &Dependencies{
		Config:          cfg,
		DB:              db,
		ScraperRegistry: registry,
	}, nil
}

// Close closes all resources held by the Dependencies.
// Should be called when done using the Dependencies.
func (d *Dependencies) Close() error {
	if d.DB != nil {
		return d.DB.Close()
	}
	return nil
}
