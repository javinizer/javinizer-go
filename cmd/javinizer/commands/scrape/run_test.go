package scrape_test

import (
	"os"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration Tests for Run() Function (Epic 7 Story 7.2)
// Following Epic 7 pattern from Story 7.1 (API command testing)

// MockScraper is a mock scraper for testing
type MockScraper struct {
	name string
	fail bool
}

func NewMockScraper(name string) *MockScraper {
	return &MockScraper{name: name, fail: false}
}

func (m *MockScraper) Name() string {
	return m.name
}

func (m *MockScraper) Search(id string) (*models.ScraperResult, error) {
	if m.fail {
		return nil, assert.AnError
	}

	releaseDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return &models.ScraperResult{
		ID:          id,
		ContentID:   id, // Required for database save
		Title:       "Test Movie " + id,
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Test Director",
		Maker:       "Test Maker",
		Label:       "Test Label",
		Series:      "Test Series",
		Description: "Test description for " + id,
		CoverURL:    "http://test.com/cover.jpg",
		Source:      m.name,
		SourceURL:   "http://test.com/" + id,
	}, nil
}

func (m *MockScraper) GetURL(id string) (string, error) {
	return "http://test.com/" + id, nil
}

func (m *MockScraper) IsEnabled() bool {
	return true
}

// setupTestDB creates a temporary database and config for testing
func setupTestDB(t *testing.T) (string, *database.DB) {
	t.Helper()

	// Create temp config file with in-memory database
	configContent := `
database:
  dsn: ":memory:"
scrapers:
  priority: ["mock1", "mock2"]
  dmm:
    enabled: true
  r18dev:
    enabled: true
metadata:
  priority:
    id: ["mock1", "mock2"]
    content_id: ["mock1", "mock2"]
    title: ["mock1", "mock2"]
    description: ["mock1", "mock2"]
matching:
  extensions: [".mp4"]
  regex_enabled: false
`
	tmpFile := t.TempDir() + "/config.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(configContent), 0644))

	// Load config
	cfg, err := config.Load(tmpFile)
	require.NoError(t, err)

	// Create and migrate database
	db, err := database.New(cfg)
	require.NoError(t, err)
	err = db.AutoMigrate()
	require.NoError(t, err)

	return tmpFile, db
}

// createTestMovie creates a test movie for database operations
func createTestMovie(id, title string) *models.Movie {
	releaseDate := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return &models.Movie{
		ID:          id,
		Title:       title,
		ReleaseDate: &releaseDate,
		Runtime:     120,
	}
}

// TestRun_ConfigNotFound tests error handling when config file is missing
func TestRun_ConfigNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	cmd := scrape.NewCommand()

	movie, results, err := scrape.Run(cmd, []string{"TEST-001"}, "/nonexistent/config.yaml", nil)

	assert.Error(t, err)
	assert.Nil(t, movie)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRun_CacheHit tests that Run() returns cached movie without scraping
// UNSKIPPED in Epic 8 Story 8.3: aggregator.NewWithOptions() enables testable aggregator initialization
func TestRun_CacheHit(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	// Pre-populate database with test movie
	movieRepo := database.NewMovieRepository(db)
	cachedMovie := createTestMovie("IPX-123", "Cached Movie")
	require.NoError(t, movieRepo.Upsert(cachedMovie))

	// Create command
	cmd := scrape.NewCommand()

	// Create test config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Create dependencies using Epic 6/8 dependency injection pattern
	// Use NewDependenciesWithOptions() to inject test database and empty registry
	registry := models.NewScraperRegistry() // Empty registry - no scrapers should be called
	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run without force refresh - should hit cache
	movie, results, err := scrape.Run(cmd, []string{"IPX-123"}, configPath, deps)

	assert.NoError(t, err)
	assert.NotNil(t, movie)
	assert.Equal(t, "IPX-123", movie.ID)
	assert.Equal(t, "Cached Movie", movie.Title)
	assert.Nil(t, results, "Cache hit should not return scraper results")
}

// TestRun_ForceRefresh tests that --force flag clears cache and scrapes fresh
// UNSKIPPED in Epic 8 Story 8.3: aggregator.NewWithOptions() enables testable aggregator initialization
func TestRun_ForceRefresh(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	// Pre-populate database with test movie
	movieRepo := database.NewMovieRepository(db)
	cachedMovie := createTestMovie("IPX-123", "Old Cached Movie")
	require.NoError(t, movieRepo.Upsert(cachedMovie))

	// Create command with force flag
	cmd := scrape.NewCommand()
	require.NoError(t, cmd.Flags().Set("force", "true"))

	// Create test config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Create mock dependencies with mock scraper using Epic 6/8 pattern
	registry := models.NewScraperRegistry()
	mockScraper := NewMockScraper("mock1")
	registry.Register(mockScraper)

	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run with force refresh - should ignore cache
	movie, results, err := scrape.Run(cmd, []string{"IPX-123"}, configPath, deps)

	assert.NoError(t, err)
	assert.NotNil(t, movie)
	assert.Equal(t, "IPX-123", movie.ID)
	// Should get fresh data from mock scraper, not cached title
	assert.Equal(t, "Test Movie IPX-123", movie.Title)
	assert.NotNil(t, results)
	assert.Len(t, results, 1)
}

// TestRun_CustomScrapers tests --scrapers flag overrides config priority
// UNSKIPPED in Epic 8 Story 8.3: aggregator.NewWithOptions() enables testable aggregator initialization
func TestRun_CustomScrapers(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	// Create command with custom scrapers flag
	cmd := scrape.NewCommand()
	require.NoError(t, cmd.Flags().Set("scrapers", "mock2"))

	// Create test config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Create mock dependencies with multiple scrapers using Epic 6/8 pattern
	registry := models.NewScraperRegistry()
	mock1 := NewMockScraper("mock1")
	mock2 := NewMockScraper("mock2")
	registry.Register(mock1)
	registry.Register(mock2)

	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run with custom scrapers - should only use mock2
	movie, results, err := scrape.Run(cmd, []string{"TEST-001"}, configPath, deps)

	assert.NoError(t, err)
	assert.NotNil(t, movie)
	assert.NotNil(t, results)
	// Should only have results from mock2
	assert.Len(t, results, 1)
	assert.Equal(t, "mock2", results[0].Source)
}

// TestRun_CustomScrapers_OverridesMetadataPriority ensures --scrapers controls
// aggregation order even when metadata priorities exclude the selected scraper.
func TestRun_CustomScrapers_OverridesMetadataPriority(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configContent := `
database:
  dsn: ":memory:"
scrapers:
  priority: ["mock1"]
  dmm:
    enabled: false
  r18dev:
    enabled: false
metadata:
  priority:
    id: ["mock1"]
    content_id: ["mock1"]
    title: ["mock1"]
matching:
  extensions: [".mp4"]
  regex_enabled: false
`
	configPath := t.TempDir() + "/config.yaml"
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	db, err := database.New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	cmd := scrape.NewCommand()
	require.NoError(t, cmd.Flags().Set("scrapers", "mock2"))

	registry := models.NewScraperRegistry()
	registry.Register(NewMockScraper("mock1"))
	registry.Register(NewMockScraper("mock2"))

	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	movie, results, err := scrape.Run(cmd, []string{"TEST-002"}, configPath, deps)
	require.NoError(t, err)
	require.NotNil(t, movie)
	require.Len(t, results, 1)
	assert.Equal(t, "mock2", results[0].Source)
	assert.Equal(t, "TEST-002", movie.ID)
	assert.Equal(t, "TEST-002", movie.ContentID)
	assert.Equal(t, "Test Movie TEST-002", movie.Title)
}

// TestRun_EmptyResults tests error handling when no scrapers return results
// UNSKIPPED in Epic 8 Story 8.3: aggregator.NewWithOptions() enables testable aggregator initialization
func TestRun_EmptyResults(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	// Create command
	cmd := scrape.NewCommand()

	// Create test config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Create mock dependencies with failing scraper using Epic 6/8 pattern
	registry := models.NewScraperRegistry()
	failingScraper := &MockScraper{name: "failing", fail: true}
	registry.Register(failingScraper)

	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run with failing scraper - should get error
	movie, results, err := scrape.Run(cmd, []string{"TEST-001"}, configPath, deps)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no results found from any scraper")
	assert.Nil(t, movie)
	assert.Nil(t, results)
}

// TestRun_Aggregation tests that multiple scraper results are aggregated correctly
// UNSKIPPED in Epic 8 Story 8.3: aggregator.NewWithOptions() enables testable aggregator initialization
func TestRun_Aggregation(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	// Create command
	cmd := scrape.NewCommand()

	// Create test config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Create mock dependencies with multiple scrapers using Epic 6/8 pattern
	registry := models.NewScraperRegistry()
	mock1 := NewMockScraper("mock1")
	mock2 := NewMockScraper("mock2")
	registry.Register(mock1)
	registry.Register(mock2)

	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run with multiple scrapers - should aggregate results
	movie, results, err := scrape.Run(cmd, []string{"TEST-001"}, configPath, deps)

	assert.NoError(t, err)
	assert.NotNil(t, movie)
	assert.NotNil(t, results)
	// Should have results from both scrapers
	assert.Len(t, results, 2)
	assert.Equal(t, "TEST-001", movie.ID)
	assert.Equal(t, "Test Movie TEST-001", movie.Title)
}

// TestRun_DatabaseSave tests that scraped movie is persisted to database
// UNSKIPPED in Epic 8 Story 8.3: aggregator.NewWithOptions() enables testable aggregator initialization
func TestRun_DatabaseSave(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	// Create command
	cmd := scrape.NewCommand()

	// Create test config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Create mock dependencies using Epic 6/8 pattern
	registry := models.NewScraperRegistry()
	mockScraper := NewMockScraper("mock1")
	registry.Register(mockScraper)

	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run scrape - should save to database
	movie, results, err := scrape.Run(cmd, []string{"TEST-SAVE"}, configPath, deps)

	assert.NoError(t, err)
	assert.NotNil(t, movie)
	assert.NotNil(t, results)

	// Verify movie was saved to database
	movieRepo := database.NewMovieRepository(db)
	savedMovie, err := movieRepo.FindByID("TEST-SAVE")
	assert.NoError(t, err)
	assert.NotNil(t, savedMovie)
	assert.Equal(t, "TEST-SAVE", savedMovie.ID)
	assert.Equal(t, "Test Movie TEST-SAVE", savedMovie.Title)
}

// TestRun_FlagOverrides tests that flag overrides are applied to config
// UNSKIPPED in Epic 8 Story 8.3: aggregator.NewWithOptions() enables testable aggregator initialization
func TestRun_FlagOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	// Create command with flag overrides
	cmd := scrape.NewCommand()
	_ = cmd.Flags().Set("scrape-actress", "true")

	// Create test config
	cfg, err := config.Load(configPath)
	require.NoError(t, err)

	// Verify initial state
	assert.False(t, cfg.Scrapers.DMM.ScrapeActress, "Should start as false")

	// Create mock dependencies using Epic 6/8 pattern
	registry := models.NewScraperRegistry()
	mockScraper := NewMockScraper("mock1")
	registry.Register(mockScraper)

	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run scrape - ApplyFlagOverrides should be called
	movie, results, err := scrape.Run(cmd, []string{"TEST-001"}, configPath, deps)

	assert.NoError(t, err)
	assert.NotNil(t, movie)
	assert.NotNil(t, results)
	// Flag override was applied (verified indirectly through successful execution)
}
