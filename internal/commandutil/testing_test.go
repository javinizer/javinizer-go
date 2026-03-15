package commandutil

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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

	// Use unique named in-memory SQLite database per test for isolation
	// Format: file:test_name?mode=memory&cache=shared&_fk=1
	// Each test gets its own database, preventing cross-test pollution
	// URL-escape test name to handle subtests with special characters (?, #, /, etc.)
	escapedName := url.PathEscape(t.Name())
	dbName := fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", escapedName)
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	require.NoError(t, err, "Failed to open in-memory database")

	// Limit connection pool to ensure migrations are visible to all queries
	sqlDB, err := db.DB()
	require.NoError(t, err, "Failed to get underlying sql.DB")
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// Run all migrations
	err = db.AutoMigrate(
		&models.Movie{},
		&models.MovieTranslation{},
		&models.Actress{},
		&models.Genre{},
		&models.GenreReplacement{},
		&models.ActressAlias{},
		&models.MovieTag{},
		&models.History{},
		&models.ContentIDMapping{},
	)
	require.NoError(t, err, "Failed to run migrations")

	return db
}

// mockScraper implements models.Scraper interface for testing.
// It returns configurable, deterministic results for testing purposes.
type mockScraper struct {
	name   string
	result *models.ScraperResult
	err    error
}

// Name returns the scraper's identifier.
func (m *mockScraper) Name() string {
	return m.name
}

// Search returns the pre-configured result or error.
func (m *mockScraper) Search(id string) (*models.ScraperResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// GetURL returns a mock URL for the given movie ID.
func (m *mockScraper) GetURL(id string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return fmt.Sprintf("https://mock-%s.example.com/movies/%s", m.name, id), nil
}

// IsEnabled always returns true for mock scrapers.
func (m *mockScraper) IsEnabled() bool {
	return true
}

// setupMockScraperRegistry creates a registry with deterministic mock scrapers.
// The results map keys are scraper names, values are the results they should return.
//
// Usage:
//
//	results := map[string]*models.ScraperResult{
//	    "r18dev": {Source: "r18dev", Title: "Test Movie"},
//	    "dmm":    {Source: "dmm", Title: "テストムービー"},
//	}
//	registry := setupMockScraperRegistry(t, results)
func setupMockScraperRegistry(t *testing.T, results map[string]*models.ScraperResult) *models.ScraperRegistry {
	t.Helper()

	registry := models.NewScraperRegistry()

	for name, result := range results {
		registry.Register(&mockScraper{
			name:   name,
			result: result,
			err:    nil,
		})
	}

	// If no results provided, add a default mock scraper
	if len(results) == 0 {
		releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
		registry.Register(&mockScraper{
			name: "mock",
			result: &models.ScraperResult{
				Source:      "mock",
				SourceURL:   "https://mock.example.com/movies/TEST-001",
				ID:          "TEST-001",
				ContentID:   "test00001",
				Title:       "Mock Test Movie",
				ReleaseDate: &releaseDate,
				Runtime:     120,
				Director:    "Mock Director",
				Maker:       "Mock Maker",
				Actresses: []models.ActressInfo{
					{
						FirstName:    "Test",
						LastName:     "Actress",
						JapaneseName: "テスト女優",
					},
				},
				Genres: []string{"Drama", "Romance"},
			},
			err: nil,
		})
	}

	return registry
}

// setupMockScraperRegistryWithError creates a registry with a mock scraper that returns errors.
// Useful for testing error handling paths.
//
// Usage:
//
//	registry := setupMockScraperRegistryWithError(t, "r18dev", fmt.Errorf("network error"))
func setupMockScraperRegistryWithError(t *testing.T, name string, err error) *models.ScraperRegistry {
	t.Helper()

	registry := models.NewScraperRegistry()
	registry.Register(&mockScraper{
		name:   name,
		result: nil,
		err:    err,
	})

	return registry
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
		cfg.Output.FolderFormat = format
	}
}

// WithOutputFile sets the output file format template.
func WithOutputFile(format string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Output.FileFormat = format
	}
}

// WithNFOEnabled sets whether NFO generation is enabled.
func WithNFOEnabled(enabled bool) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Metadata.NFO.Enabled = enabled
	}
}

// WithDownloadCover sets whether cover download is enabled.
func WithDownloadCover(enabled bool) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Output.DownloadCover = enabled
	}
}

// WithVideoExtensions sets the video file extensions.
func WithVideoExtensions(extensions []string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Matching.Extensions = extensions
	}
}

// WithMatchingPatterns sets the JAV ID matching pattern (enables regex mode).
func WithMatchingPatterns(patterns []string) ConfigOption {
	return func(cfg *config.Config) {
		if len(patterns) > 0 {
			cfg.Matching.RegexEnabled = true
			cfg.Matching.RegexPattern = patterns[0] // Use first pattern
		}
	}
}

// WithMinFileSize sets the minimum file size in MB.
func WithMinFileSize(sizeMB int) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Matching.MinSizeMB = sizeMB
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
	cfg := config.DefaultConfig()

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

// createTestVideoFile creates a dummy video file for testing.
// Returns the full path to the created file.
//
// Usage:
//
//	path := createTestVideoFile(t, tmpDir, "IPX-535.mp4")
func createTestVideoFile(t *testing.T, dir string, filename string) string {
	t.Helper()

	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte("dummy video content"), 0644)
	require.NoError(t, err, "Failed to create test video file")

	return path
}

// createTestMovieFiles creates a set of test movie files with various patterns.
// Returns a slice of full paths to the created files.
//
// Usage:
//
//	paths := createTestMovieFiles(t, tmpDir)
//	// Returns files like: IPX-535.mp4, ABC-123.mkv, XYZ-789-pt1.mp4, etc.
func createTestMovieFiles(t *testing.T, dir string) []string {
	t.Helper()

	filenames := []string{
		"IPX-535.mp4",
		"ABC-123.mkv",
		"XYZ-789.avi",
		"DEF-456-pt1.mp4",
		"DEF-456-pt2.mp4",
		"SSIS-001Z.mp4",
		"T28-567.mkv",
		"random_file.mp4", // Should not match
	}

	paths := make([]string, 0, len(filenames))
	for _, filename := range filenames {
		path := createTestVideoFile(t, dir, filename)
		paths = append(paths, path)
	}

	return paths
}

// captureOutput runs a function and captures its stdout/stderr.
// Returns the captured stdout and stderr as strings.
//
// Usage:
//
//	stdout, stderr := captureOutput(t, func() {
//	    fmt.Println("Hello")
//	    fmt.Fprintln(os.Stderr, "Error")
//	})
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	// Save original stdout/stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	// Create pipes
	rOut, wOut, err := os.Pipe()
	require.NoError(t, err, "Failed to create stdout pipe")
	rErr, wErr, err := os.Pipe()
	require.NoError(t, err, "Failed to create stderr pipe")

	// Ensure restoration happens even on panic or t.Fatal
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	// Replace stdout/stderr
	os.Stdout = wOut
	os.Stderr = wErr

	// Capture output in goroutines
	outChan := make(chan string)
	errChan := make(chan string)

	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rOut)
		rOut.Close()
		outChan <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, rErr)
		rErr.Close()
		errChan <- buf.String()
	}()

	// Run function
	fn()

	// Close writers to signal goroutines
	wOut.Close()
	wErr.Close()

	// Get captured output
	stdout = <-outChan
	stderr = <-errChan

	return stdout, stderr
}

// executeCmdWithOutput executes a Cobra command and captures output.
// Returns the captured output and any error from command execution.
//
// Usage:
//
//	cmd := &cobra.Command{
//	    Run: func(cmd *cobra.Command, args []string) {
//	        fmt.Println("Hello from command")
//	    },
//	}
//	output, err := executeCmdWithOutput(t, cmd, []string{"arg1", "arg2"})
func executeCmdWithOutput(t *testing.T, cmd *cobra.Command, args []string) (output string, err error) {
	t.Helper()

	// Capture output
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs(args)

	// Execute command
	err = cmd.Execute()

	// Get output
	output = buf.String()

	return output, err
}

// assertMovieInDB checks if a movie with given ID exists in the database.
// Fails the test if the movie is not found.
//
// Usage:
//
//	assertMovieInDB(t, db, "IPX-535")
func assertMovieInDB(t *testing.T, db *gorm.DB, movieID string) {
	t.Helper()

	var movie models.Movie
	err := db.First(&movie, "id = ?", movieID).Error
	require.NoError(t, err, "Expected movie %s to exist in database", movieID)
}

// assertMovieNotInDB checks if a movie with given ID does NOT exist in the database.
// Fails the test if the movie is found.
//
// Usage:
//
//	assertMovieNotInDB(t, db, "INVALID-001")
func assertMovieNotInDB(t *testing.T, db *gorm.DB, movieID string) {
	t.Helper()

	var movie models.Movie
	err := db.First(&movie, "id = ?", movieID).Error
	require.Error(t, err, "Expected movie %s to NOT exist in database", movieID)
	assert.Equal(t, gorm.ErrRecordNotFound, err)
}

// assertFileExists checks if a file exists at the given path.
// Fails the test if the file does not exist.
//
// Usage:
//
//	assertFileExists(t, "/path/to/file.mp4")
func assertFileExists(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	require.NoError(t, err, "Expected file to exist: %s", path)
}

// assertFileNotExists checks if a file does NOT exist at the given path.
// Fails the test if the file exists.
//
// Usage:
//
//	assertFileNotExists(t, "/path/to/deleted.mp4")
func assertFileNotExists(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	require.Error(t, err, "Expected file to NOT exist: %s", path)
	require.True(t, os.IsNotExist(err), "Expected file not found error, got: %v", err)
}

// createTestMovie creates a test movie with default values.
// Useful for quickly creating test data.
//
// Usage:
//
//	movie := createTestMovie("IPX-535", "Test Movie Title")
func createTestMovie(id, title string) *models.Movie {
	releaseDate := time.Date(2023, 5, 1, 0, 0, 0, 0, time.UTC)
	// ContentID is the primary key, generate it from ID (lowercase, no hyphens)
	contentID := strings.ToLower(strings.ReplaceAll(id, "-", ""))
	return &models.Movie{
		ID:          id,
		ContentID:   contentID,
		Title:       title,
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Test Director",
		Maker:       "Test Maker",
		Label:       "Test Label",
		Series:      "Test Series",
		RatingScore: 8.5,
		RatingVotes: 100,
		Description: "Test description",
		Actresses: []models.Actress{
			{
				DMMID:        12345,
				FirstName:    "Test",
				LastName:     "Actress",
				JapaneseName: "テスト女優",
			},
		},
		Genres: []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
		},
		SourceName: "mock",
		SourceURL:  "https://mock.example.com/movies/" + id,
	}
}

// insertTestMovie inserts a test movie into the database using the repository pattern.
// Returns the movie ID.
//
// Usage:
//
//	db := setupTestDB(t)
//	movieID := insertTestMovie(t, db, "IPX-535", "Test Movie")
func insertTestMovie(t *testing.T, db *gorm.DB, id, title string) string {
	t.Helper()

	movie := createTestMovie(id, title)

	dbWrapper := &database.DB{DB: db}
	repo := database.NewMovieRepository(dbWrapper)

	err := repo.Upsert(movie)
	require.NoError(t, err, "Failed to insert test movie")

	return movie.ID
}

// countMoviesInDB returns the total number of movies in the database.
//
// Usage:
//
//	count := countMoviesInDB(t, db)
//	assert.Equal(t, 5, count)
func countMoviesInDB(t *testing.T, db *gorm.DB) int {
	t.Helper()

	var count int64
	err := db.Model(&models.Movie{}).Count(&count).Error
	require.NoError(t, err, "Failed to count movies")

	return int(count)
}

// countTagsForMovie returns the number of tags for a specific movie.
//
// Usage:
//
//	count := countTagsForMovie(t, db, "IPX-535")
//	assert.Equal(t, 3, count)
func countTagsForMovie(t *testing.T, db *gorm.DB, movieID string) int {
	t.Helper()

	var count int64
	err := db.Model(&models.MovieTag{}).Where("movie_id = ?", movieID).Count(&count).Error
	require.NoError(t, err, "Failed to count tags for movie")

	return int(count)
}

// setupTestDatabaseWithData creates a database and populates it with test movies.
// Returns the database and a slice of movie IDs.
//
// Usage:
//
//	db, movieIDs := setupTestDatabaseWithData(t, 5)
//	// Database now has 5 test movies
func setupTestDatabaseWithData(t *testing.T, numMovies int) (*gorm.DB, []string) {
	t.Helper()

	db := setupTestDB(t)
	movieIDs := make([]string, numMovies)

	for i := 0; i < numMovies; i++ {
		id := fmt.Sprintf("TEST-%03d", i+1)
		title := fmt.Sprintf("Test Movie %d", i+1)
		movieIDs[i] = insertTestMovie(t, db, id, title)
	}

	return db, movieIDs
}

// createTestDirectoryStructure creates a test directory structure with movies.
// Returns the root directory and map of created files.
//
// Usage:
//
//	rootDir, files := createTestDirectoryStructure(t)
//	// Creates: rootDir/subdir1/movie1.mp4, rootDir/subdir2/movie2.mkv, etc.
func createTestDirectoryStructure(t *testing.T) (string, map[string]string) {
	t.Helper()

	rootDir := t.TempDir()

	// Create subdirectories
	subdir1 := filepath.Join(rootDir, "subdir1")
	subdir2 := filepath.Join(rootDir, "subdir2")

	require.NoError(t, os.MkdirAll(subdir1, 0777))
	require.NoError(t, os.MkdirAll(subdir2, 0777))

	// Create files in different locations
	files := map[string]string{
		"root_movie":    createTestVideoFile(t, rootDir, "IPX-535.mp4"),
		"subdir1_movie": createTestVideoFile(t, subdir1, "ABC-123.mkv"),
		"subdir2_movie": createTestVideoFile(t, subdir2, "XYZ-789.avi"),
	}

	return rootDir, files
}

// createTestDependencies creates a Dependencies instance for testing.
//
// By default, it uses MOCK scrapers to avoid network calls and ensure fast,
// deterministic tests. For integration tests that need real scrapers, use
// createTestDependenciesWithRealScrapers().
//
// Usage:
//
//	cfg := createTestConfig(t)
//	deps := createTestDependencies(t, cfg)  // Uses mocks
//	defer deps.Close()
func createTestDependencies(t *testing.T, cfg *config.Config) *Dependencies {
	t.Helper()
	return createTestDependenciesWithRegistry(t, cfg, nil)
}

// createTestDependenciesWithRealScrapers creates Dependencies with REAL scrapers.
//
// Use this ONLY for integration tests that need to test actual scraper behavior.
// These tests should be marked with `if testing.Short() { t.Skip() }` to allow
// fast unit test runs.
//
// Usage:
//
//	cfg := createTestConfig(t)
//	if testing.Short() {
//		t.Skip("Skipping integration test with real scrapers")
//	}
//	deps := createTestDependenciesWithRealScrapers(t, cfg)
//	defer deps.Close()
func createTestDependenciesWithRealScrapers(t *testing.T, cfg *config.Config) *Dependencies {
	t.Helper()

	deps, err := NewDependencies(cfg)
	require.NoError(t, err, "Failed to create test dependencies")

	return deps
}

// createTestDependenciesWithRegistry creates Dependencies with a custom scraper registry.
//
// If registry is nil, creates a mock registry (default for unit tests).
// This function is the base implementation used by both createTestDependencies
// and createTestDependenciesWithRealScrapers.
//
// Usage:
//
//	cfg := createTestConfig(t)
//	registry := createMockScraperRegistry()
//	deps := createTestDependenciesWithRegistry(t, cfg, registry)
//	defer deps.Close()
func createTestDependenciesWithRegistry(t *testing.T, cfg *config.Config, registry *models.ScraperRegistry) *Dependencies {
	t.Helper()

	if cfg == nil {
		t.Fatal("config cannot be nil")
	}

	// Initialize database
	db, err := database.New(cfg)
	require.NoError(t, err, "Failed to initialize database")

	// Run migrations
	err = db.AutoMigrate()
	require.NoError(t, err, "Failed to run migrations")

	// Use provided registry or create a mock one
	if registry == nil {
		registry = createMockScraperRegistry()
	}

	return &Dependencies{
		Config:          cfg,
		DB:              db,
		ScraperRegistry: registry,
	}
}

// resetLoggerForTests resets the global logger to a test-friendly state.
// This ensures that log messages appear in stdout as plain text, which is
// necessary for tests that capture and verify log output.
//
// Usage:
//
//	resetLoggerForTests(t)
//	// Now logging.Info() will output plain text to stdout
//
// Background: Some tests (like TestLoadConfig_CompleteFlow) initialize
// the logger with JSON format, which pollutes subsequent tests that expect
// plain text log output. This helper ensures a clean slate.
func resetLoggerForTests(t *testing.T) {
	t.Helper()

	// Reset to default logger configuration (text format, stdout)
	err := logging.InitLogger(&logging.Config{
		Level:  "info",
		Format: "text",
		Output: "stdout",
	})
	require.NoError(t, err, "Failed to reset logger for tests")
}

// ============================================================================
// Mock Scraper Infrastructure
// ============================================================================

// MockScraper implements the models.Scraper interface for testing without network calls
type MockScraper struct {
	name         string
	enabled      bool
	results      map[string]*models.ScraperResult
	searchErrors map[string]error
	urls         map[string]string
}

// NewMockScraper creates a new mock scraper
func NewMockScraper(name string) *MockScraper {
	return &MockScraper{
		name:         name,
		enabled:      true,
		results:      make(map[string]*models.ScraperResult),
		searchErrors: make(map[string]error),
		urls:         make(map[string]string),
	}
}

// Name returns the scraper's identifier
func (m *MockScraper) Name() string {
	return m.name
}

// IsEnabled returns whether this scraper is enabled
func (m *MockScraper) IsEnabled() bool {
	return m.enabled
}

// Search attempts to find and scrape metadata for the given movie ID
func (m *MockScraper) Search(id string) (*models.ScraperResult, error) {
	// Check if we have a predefined error for this ID
	if err, ok := m.searchErrors[id]; ok {
		return nil, err
	}

	// Check if we have a predefined result for this ID
	if result, ok := m.results[id]; ok {
		return result, nil
	}

	// No result found
	return nil, fmt.Errorf("mock scraper %s: no result for %s", m.name, id)
}

// GetURL attempts to find the URL for a given movie ID
func (m *MockScraper) GetURL(id string) (string, error) {
	if url, ok := m.urls[id]; ok {
		return url, nil
	}
	return "", fmt.Errorf("mock scraper %s: no URL for %s", m.name, id)
}

// AddResult adds a predefined result for testing
func (m *MockScraper) AddResult(id string, result *models.ScraperResult) {
	m.results[id] = result
}

// AddError adds a predefined error for testing
func (m *MockScraper) AddError(id string, err error) {
	m.searchErrors[id] = err
}

// AddURL adds a predefined URL for testing
func (m *MockScraper) AddURL(id string, url string) {
	m.urls[id] = url
}

// SetEnabled sets whether this scraper is enabled
func (m *MockScraper) SetEnabled(enabled bool) {
	m.enabled = enabled
}

// createMockScraperRegistry creates a scraper registry with mock scrapers for testing
func createMockScraperRegistry() *models.ScraperRegistry {
	registry := models.NewScraperRegistry()

	// Add mock r18dev scraper
	mockR18 := NewMockScraper("r18dev")
	registry.Register(mockR18)

	// Add mock dmm scraper
	mockDMM := NewMockScraper("dmm")
	registry.Register(mockDMM)

	return registry
}

// MockDownloader is a mock implementation of MediaDownloader for testing
type MockDownloader struct {
	results []downloader.DownloadResult
	err     error
}

// NewMockDownloader creates a new mock downloader with predefined results
func NewMockDownloader(results []downloader.DownloadResult, err error) *MockDownloader {
	return &MockDownloader{
		results: results,
		err:     err,
	}
}

// DownloadAll implements MediaDownloader interface
func (m *MockDownloader) DownloadAll(movie *models.Movie, destDir string, multipart *downloader.MultipartInfo) ([]downloader.DownloadResult, error) {
	return m.results, m.err
}
