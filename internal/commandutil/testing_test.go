package commandutil

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB creates an in-memory SQLite database with GORM migrations.
// It returns a configured *gorm.DB ready for testing.
//
// Usage:
//
//	db := setupTestDB(t)
//	// Use db for testing...
//	// Cleanup happens automatically when test ends (in-memory)
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	dbName := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=10000&_fk=1", dbPath)
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	require.NoError(t, err, "Failed to open test database")

	// Limit connection pool to ensure migrations are visible to all queries
	sqlDB, err := db.DB()
	require.NoError(t, err, "Failed to get underlying sql.DB")
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// Run all migrations
	wrappedDB := &database.DB{DB: db}
	err = wrappedDB.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err, "Failed to run migrations")

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

// ConfigOption is a functional option for customizing test configuration.
type ConfigOption func(*config.Config)

// WithScraperPriority sets the global scraper priority order.
func WithScraperPriority(priority []string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Scrapers.Priority = priority
	}
}

// WithDatabaseDSN sets the database DSN.
func WithDatabaseDSN(dsn string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Database.DSN = dsn
	}
}

// WithOutputFolder sets the output folder format template.
func WithOutputFolder(format string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Output.Template.FolderFormat = format
	}
}

// WithOutputFile sets the output file format template.
func WithOutputFile(format string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Output.Template.FileFormat = format
	}
}

// WithDownloadCover sets whether cover download is enabled.
func WithDownloadCover(enabled bool) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Output.Download.DownloadCover = enabled
	}
}

// createTestConfig generates a test configuration file in a temp directory.
// It returns both the config file path and the loaded config object.
//
// Usage:
//
//	configPath, cfg := createTestConfig(t,
//	    WithScraperPriority([]string{"r18dev"}),
//	    WithOutputFolder("<ID> - <TITLE>"),
//	)
func createTestConfig(t *testing.T, options ...ConfigOption) (string, *config.Config) {
	t.Helper()

	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Start with default config
	cfg := config.DefaultConfig(nil, nil)

	// Override database path to use temp directory (prevents mutating real workspace DB)
	cfg.Database.DSN = filepath.Join(tmpDir, "test.db")

	// Apply options (can override the temp DB path if needed)
	for _, opt := range options {
		opt(cfg)
	}

	// Save config to file
	err := config.Save(cfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	return configPath, cfg
}
