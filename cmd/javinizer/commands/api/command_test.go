package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	api "github.com/javinizer/javinizer-go/cmd/javinizer/commands/api"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	internalapi "github.com/javinizer/javinizer-go/internal/api"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockScraper is a mock scraper for testing
type MockScraper struct {
	name string
}

func NewMockScraper(name string) *MockScraper {
	return &MockScraper{name: name}
}

func (m *MockScraper) Name() string {
	return m.name
}

func (m *MockScraper) Search(id string) (*models.ScraperResult, error) {
	return &models.ScraperResult{
		ID:    id,
		Title: "Test Movie",
	}, nil
}

func (m *MockScraper) GetURL(id string) (string, error) {
	return "http://test.com/" + id, nil
}

func (m *MockScraper) IsEnabled() bool {
	return true
}

// createTestMovie creates a test movie for database operations
func createTestMovie(id, title string) *models.Movie {
	return &models.Movie{
		ID:    id,
		Title: title,
	}
}

// setupTagTestDB creates a temporary database for testing
func setupTagTestDB(t *testing.T) (string, *database.DB) {
	t.Helper()

	// Create temp config file
	configContent := `
database:
  dsn: ":memory:"
scrapers:
  priority: ["r18dev", "dmm"]
metadata:
  priority: {}
matching:
  extensions: [".mp4"]
  regex_enabled: false
`
	tmpFile := t.TempDir() + "/config.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(configContent), 0644))

	// Load config
	cfg, err := config.Load(tmpFile)
	require.NoError(t, err)

	// Create database
	db, err := database.New(cfg)
	require.NoError(t, err)
	err = db.AutoMigrate()
	require.NoError(t, err)

	return tmpFile, db
}

// createTestAPIServer creates a test API server with minimal dependencies
func createTestAPIServer(t *testing.T) *internalapi.ServerDependencies {
	t.Helper()

	// Create test config
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Matching: config.MatchingConfig{
			Extensions:   []string{".mp4"},
			RegexEnabled: false,
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{},
		},
	}

	// Create test database
	configPath, db := setupTagTestDB(t)

	// Initialize repositories
	movieRepo := database.NewMovieRepository(db)
	actressRepo := database.NewActressRepository(db)

	// Initialize registry with mock scrapers
	registry := models.NewScraperRegistry()
	mockScraper := NewMockScraper("testscraper")
	registry.Register(mockScraper)

	// Initialize aggregator
	agg := aggregator.NewWithDatabase(cfg, db)

	// Initialize matcher
	mat, err := matcher.NewMatcher(&cfg.Matching)
	require.NoError(t, err)

	// Initialize job queue
	jobQueue := worker.NewJobQueue()

	// Create server dependencies
	deps := &internalapi.ServerDependencies{
		ConfigFile:  configPath,
		Registry:    registry,
		DB:          db,
		Aggregator:  agg,
		MovieRepo:   movieRepo,
		ActressRepo: actressRepo,
		Matcher:     mat,
		JobQueue:    jobQueue,
	}
	deps.SetConfig(cfg)

	return deps
}

// TestAPIServer_HealthCheck tests the health check endpoint
func TestAPIServer_HealthCheck(t *testing.T) {
	deps := createTestAPIServer(t)
	defer func() { _ = deps.DB.Close() }()

	router := internalapi.NewServer(deps)

	req, _ := http.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "ok", response["status"])
}

// TestAPIServer_ListMovies tests the list movies endpoint
func TestAPIServer_ListMovies(t *testing.T) {
	deps := createTestAPIServer(t)
	defer func() { _ = deps.DB.Close() }()

	// Insert test movie
	movie := createTestMovie("IPX-123", "Test Movie")
	err := deps.MovieRepo.Upsert(movie)
	require.NoError(t, err)

	router := internalapi.NewServer(deps)

	req, _ := http.NewRequest("GET", "/api/v1/movies", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	movies, ok := response["movies"].([]interface{})
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(movies), 1, "should return at least one movie")
}

// TestAPIServer_GetMovie tests the get movie by ID endpoint
func TestAPIServer_GetMovie(t *testing.T) {
	deps := createTestAPIServer(t)
	defer func() { _ = deps.DB.Close() }()

	// Insert test movie
	movie := createTestMovie("IPX-123", "Test Movie")
	err := deps.MovieRepo.Upsert(movie)
	require.NoError(t, err)

	router := internalapi.NewServer(deps)

	req, _ := http.NewRequest("GET", "/api/v1/movies/IPX-123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	movie_response, ok := response["movie"].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "IPX-123", movie_response["id"])
}

// TestAPIServer_GetMovie_NotFound tests 404 for non-existent movie
func TestAPIServer_GetMovie_NotFound(t *testing.T) {
	deps := createTestAPIServer(t)
	defer func() { _ = deps.DB.Close() }()

	router := internalapi.NewServer(deps)

	req, _ := http.NewRequest("GET", "/api/v1/movies/NONEXISTENT-999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Note: Additional API endpoint tests can be added here as needed
// Currently focusing on core endpoints that demonstrate router setup testing

// ==================================================
// CLI Command Tests (Epic 7 Story 7.1)
// Tests for NewCommand() and Run() functions
// ==================================================

// TestNewCommand_Structure verifies command structure and flags
func TestNewCommand_Structure(t *testing.T) {
	cmd := api.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "api", cmd.Use)
	assert.Contains(t, cmd.Aliases, "web", "web alias should be registered")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify flags are registered
	assert.NotNil(t, cmd.Flags().Lookup("host"), "host flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("port"), "port flag should be registered")
}

// TestNewCommand_FlagDefaults verifies default flag values
func TestNewCommand_FlagDefaults(t *testing.T) {
	cmd := api.NewCommand()

	// Host should default to empty (use config)
	host, err := cmd.Flags().GetString("host")
	assert.NoError(t, err)
	assert.Empty(t, host, "host should default to empty")

	// Port should default to 0 (use config)
	port, err := cmd.Flags().GetInt("port")
	assert.NoError(t, err)
	assert.Equal(t, 0, port, "port should default to 0")
}

// TestRun_HostFlagOverride verifies --host flag overrides config
func TestRun_HostFlagOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 8080
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	cmd := api.NewCommand()
	customHost := "127.0.0.1"

	deps, err := api.Run(cmd, configPath, customHost, 0)
	require.NoError(t, err)
	defer func() { _ = deps.DB.Close() }()

	currentCfg := deps.GetConfig()
	assert.Equal(t, customHost, currentCfg.Server.Host, "host should be overridden")
	assert.Equal(t, 8080, currentCfg.Server.Port, "port should remain from config")
}

// TestRun_PortFlagOverride verifies --port flag overrides config
func TestRun_PortFlagOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 8080
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	cmd := api.NewCommand()
	customPort := 9090

	deps, err := api.Run(cmd, configPath, "", customPort)
	require.NoError(t, err)
	defer func() { _ = deps.DB.Close() }()

	currentCfg := deps.GetConfig()
	assert.Equal(t, "localhost", currentCfg.Server.Host, "host should remain from config")
	assert.Equal(t, customPort, currentCfg.Server.Port, "port should be overridden")
}

// TestRun_BothFlagsOverride verifies both host and port can be overridden
func TestRun_BothFlagsOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)
	cfg, err := config.Load(configPath)
	require.NoError(t, err)
	cfg.Server.Host = "localhost"
	cfg.Server.Port = 8080
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	cmd := api.NewCommand()
	customHost := "0.0.0.0"
	customPort := 3000

	deps, err := api.Run(cmd, configPath, customHost, customPort)
	require.NoError(t, err)
	defer func() { _ = deps.DB.Close() }()

	currentCfg := deps.GetConfig()
	assert.Equal(t, customHost, currentCfg.Server.Host)
	assert.Equal(t, customPort, currentCfg.Server.Port)
}

// TestRun_ConfigLoading verifies config is loaded correctly
func TestRun_ConfigLoading(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.DB.Close() }()

	// Verify config is loaded
	assert.Equal(t, configPath, deps.ConfigFile)
	assert.NotNil(t, deps.GetConfig())
}

// TestRun_DatabaseInit verifies database initialization and migrations
func TestRun_DatabaseInit(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.DB.Close() }()

	// Verify database is initialized
	assert.NotNil(t, deps.DB)

	// Verify tables exist (migrations ran)
	var tableCount int
	deps.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount)
	assert.Greater(t, tableCount, 0, "should have tables after migrations")
}

// TestRun_ScraperRegistry verifies scraper initialization
func TestRun_ScraperRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.DB.Close() }()

	// Verify scraper registry
	assert.NotNil(t, deps.Registry)
	scrapers := deps.Registry.GetAll()
	assert.Greater(t, len(scrapers), 0, "should have registered scrapers")
}

// TestRun_Repositories verifies repository initialization
func TestRun_Repositories(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.DB.Close() }()

	// Verify repositories
	assert.NotNil(t, deps.MovieRepo, "MovieRepository should be initialized")
	assert.NotNil(t, deps.ActressRepo, "ActressRepository should be initialized")

	// Verify functional
	movies, err := deps.MovieRepo.List(10, 0)
	assert.NoError(t, err)
	assert.NotNil(t, movies)
}

// TestRun_Aggregator verifies aggregator initialization
func TestRun_Aggregator(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.DB.Close() }()

	assert.NotNil(t, deps.Aggregator, "Aggregator should be initialized")
	assert.NotNil(t, deps.Matcher, "Matcher should be initialized")
}

// TestRun_JobQueue verifies job queue initialization
func TestRun_JobQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.DB.Close() }()

	assert.NotNil(t, deps.JobQueue, "JobQueue should be initialized")
	jobs := deps.JobQueue.ListJobs()
	assert.NotNil(t, jobs)
	assert.Empty(t, jobs, "should start with no jobs")
}

// TestRun_ErrorConfigNotFound verifies error when config doesn't exist
func TestRun_ErrorConfigNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	cmd := api.NewCommand()
	nonExistentPath := "/nonexistent/config.yaml"

	deps, err := api.Run(cmd, nonExistentPath, "", 0)
	assert.Error(t, err, "should error when config not found")
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "failed to load config")
}
