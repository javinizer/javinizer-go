package api

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// Test helpers for creating mock repositories

// mockScraperWithResults implements Scraper and returns predefined results
// For security testing, it echoes back the ID in the result to verify sanitization
type mockScraperWithResults struct {
	name    string
	enabled bool
	result  *models.ScraperResult
	err     error
}

func (m *mockScraperWithResults) Name() string {
	return m.name
}

func (m *mockScraperWithResults) Search(id string) (*models.ScraperResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Clone the result and set the ID to the searched ID
	// This allows security tests to verify that malicious input is sanitized
	result := *m.result
	result.ID = id // Echo back the input ID for security testing
	return &result, nil
}

func (m *mockScraperWithResults) GetURL(id string) (string, error) {
	return "", nil
}

func (m *mockScraperWithResults) IsEnabled() bool {
	return m.enabled
}

// newMockMovieRepo creates a test movie repository with in-memory database
func newMockMovieRepo() *database.MovieRepository {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(cfg)
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(); err != nil {
		panic(err)
	}
	return database.NewMovieRepository(db)
}

// newMockActressRepo creates a test actress repository with in-memory database
func newMockActressRepo() *database.ActressRepository {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(cfg)
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(); err != nil {
		panic(err)
	}
	return database.NewActressRepository(db)
}

// createTestDeps creates minimal ServerDependencies for testing
func createTestDeps(t *testing.T, cfg *config.Config, configFile string) *ServerDependencies {
	t.Helper()

	// Initialize in-memory database with a unique name per test to avoid cross-test pollution
	// Using file:TESTNAME:?mode=memory&cache=shared&_busy_timeout=5000 ensures:
	// 1. Isolation between different tests (each gets its own DB)
	// 2. Shared cache within a test (concurrent goroutines see same data)
	// 3. Busy timeout (5s) reduces "database is locked" errors in concurrent scenarios
	// Note: WAL mode is not compatible with in-memory databases
	dbName := t.Name()
	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  "file:" + dbName + ":?mode=memory&cache=shared&_busy_timeout=5000",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(dbCfg)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	// Initialize repositories
	movieRepo := database.NewMovieRepository(db)
	actressRepo := database.NewActressRepository(db)

	// Initialize scraper registry
	registry := models.NewScraperRegistry()

	// Initialize aggregator
	agg := aggregator.NewWithDatabase(cfg, db)

	// Initialize matcher
	mat, err := matcher.NewMatcher(&cfg.Matching)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	// Initialize job queue
	jobQueue := worker.NewJobQueue()

	deps := &ServerDependencies{
		ConfigFile:  configFile,
		Registry:    registry,
		DB:          db,
		Aggregator:  agg,
		MovieRepo:   movieRepo,
		ActressRepo: actressRepo,
		Matcher:     mat,
		JobQueue:    jobQueue,
	}
	// Initialize atomic config pointer
	deps.SetConfig(cfg)

	return deps
}
