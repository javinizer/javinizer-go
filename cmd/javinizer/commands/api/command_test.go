package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/testutil"

	api "github.com/javinizer/javinizer-go/cmd/javinizer/commands/api"
	"github.com/javinizer/javinizer-go/internal/api/core"
	apiserver "github.com/javinizer/javinizer-go/internal/api/server"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	reg := scraperutil.NewScraperRegistry()
	scraper.RegisterAll(reg)
}

// MockScraper implements models.Scraper for testing
type MockScraper struct {
	name string
}

func (m *MockScraper) Name() string { return m.name }
func (m *MockScraper) Search(_ context.Context, _ string) (*models.ScraperResult, error) {
	return nil, nil
}
func (m *MockScraper) GetURL(_ context.Context, id string) (string, error) { return "", nil }
func (m *MockScraper) IsEnabled() bool                                     { return true }
func (m *MockScraper) Config() *models.ScraperSettings                     { return nil }
func (m *MockScraper) Close() error                                        { return nil }

func NewMockScraper(name string) *MockScraper {
	return &MockScraper{name: name}
}

func createTestMovie(id, title string) *models.Movie {
	return &models.Movie{
		ID:    id,
		Title: title,
	}
}

func setupTagTestDB(t *testing.T) (string, *database.DB) {
	t.Helper()

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")

	configContent := `config_version: 3
database:
  dsn: ":memory:"
  type: sqlite
server:
  host: localhost
  port: 8080
system:
  temp_dir: ` + tmpDir + `
`
	require.NoError(t, os.WriteFile(tmpFile, []byte(configContent), 0644))

	cfg, err := config.Load(tmpFile)
	require.NoError(t, err)

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)

	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return tmpFile, db
}

// createTestAPIServer creates a test API server with minimal dependencies
func createTestAPIServer(t *testing.T) *core.APIRuntime {
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
	registry := scraperutil.NewScraperRegistry()
	mockScraper := NewMockScraper("testscraper")
	registry.RegisterInstance(mockScraper)

	// Initialize job queue
	jobStore := worker.NewJobStore(nil, nil, nil, "", nil, nil)

	// Create server dependencies
	deps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: registry,
			DB:              db,
		},
		ConfigFile: configPath,
		Repos: database.Repositories{
			ContentRepos: database.ContentRepos{
				MovieRepo:   movieRepo,
				ActressRepo: actressRepo,
			},
		},
		JobStore: jobStore,
	}
	deps.CoreDeps.SetConfig(cfg)

	rt := core.NewAPIRuntime(deps)
	return rt
}

// TestAPIServer_HealthCheck tests the health check endpoint
func TestAPIServer_HealthCheck(t *testing.T) {
	rt := createTestAPIServer(t)
	defer func() { _ = rt.Deps().CoreDeps.DB.Close() }()

	router := apiserver.NewServer(rt)

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
	rt := createTestAPIServer(t)
	deps := rt.Deps()
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	// Insert test movie
	movie := createTestMovie("IPX-123", "Test Movie")
	_, err := deps.Repos.MovieRepo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	router := apiserver.NewServer(rt)

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
	rt := createTestAPIServer(t)
	deps := rt.Deps()
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	// Insert test movie
	movie := createTestMovie("IPX-123", "Test Movie")
	_, err := deps.Repos.MovieRepo.Upsert(context.TODO(), movie)
	require.NoError(t, err)

	router := apiserver.NewServer(rt)

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
	rt := createTestAPIServer(t)
	defer func() { _ = rt.Deps().CoreDeps.DB.Close() }()

	router := apiserver.NewServer(rt)

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
		t.Skip("skipping integration test in short mode")
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

	deps, _, err := api.Run(cmd, configPath, customHost, 0)
	require.NoError(t, err)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	currentCfg := deps.CoreDeps.GetConfig()
	assert.Equal(t, customHost, currentCfg.Server.Host, "host should be overridden")
	assert.Equal(t, 8080, currentCfg.Server.Port, "port should remain from config")
}

// TestRun_PortFlagOverride verifies --port flag overrides config
func TestRun_PortFlagOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
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

	deps, _, err := api.Run(cmd, configPath, "", customPort)
	require.NoError(t, err)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	currentCfg := deps.CoreDeps.GetConfig()
	assert.Equal(t, "localhost", currentCfg.Server.Host, "host should remain from config")
	assert.Equal(t, customPort, currentCfg.Server.Port, "port should be overridden")
}

// TestRun_BothFlagsOverride verifies both host and port can be overridden
func TestRun_BothFlagsOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
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

	deps, _, err := api.Run(cmd, configPath, customHost, customPort)
	require.NoError(t, err)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	currentCfg := deps.CoreDeps.GetConfig()
	assert.Equal(t, customHost, currentCfg.Server.Host)
	assert.Equal(t, customPort, currentCfg.Server.Port)
}

// TestRun_ConfigLoading verifies config is loaded correctly
func TestRun_ConfigLoading(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	// Verify config is loaded
	assert.Equal(t, configPath, deps.ConfigFile)
	assert.NotNil(t, deps.CoreDeps.GetConfig())
}

// TestRun_DatabaseInit verifies database initialization and migrations
func TestRun_DatabaseInit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	// Verify database is initialized
	assert.NotNil(t, deps.CoreDeps.DB)

	// Verify tables exist (migrations ran)
	var tableCount int
	deps.CoreDeps.DB.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount)
	assert.Greater(t, tableCount, 0, "should have tables after migrations")
}

// TestRun_ScraperRegistry verifies scraper initialization
func TestRun_ScraperRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	// Verify scraper registry
	assert.NotNil(t, deps.CoreDeps.ScraperRegistry)
	scrapers := deps.CoreDeps.ScraperRegistry.GetAll()
	assert.Greater(t, len(scrapers), 0, "should have registered scrapers")
}

// TestRun_Repositories verifies repository initialization
func TestRun_Repositories(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	// Verify repositories
	assert.NotNil(t, deps.Repos.MovieRepo, "MovieRepository should be initialized")
	assert.NotNil(t, deps.Repos.ActressRepo, "ActressRepository should be initialized")

	// Verify functional
	movies, err := deps.Repos.MovieRepo.List(context.TODO(), 10, 0)
	assert.NoError(t, err)
	assert.NotNil(t, movies)
}

// TestRun_ReloadConfig verifies that ReloadConfig works after initialization
func TestRun_ReloadConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	// Verify ReloadConfig can be called with a valid config
	cfg := deps.CoreDeps.GetConfig()
	require.NotNil(t, cfg)
	err = core.NewAPIRuntime(deps).ReloadConfig(cfg)
	assert.NoError(t, err, "ReloadConfig should succeed with valid config")
}

// TestRun_JobStore verifies job queue initialization
func TestRun_JobStore(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	assert.NotNil(t, deps.JobStore, "JobStore should be initialized")
	jobs := deps.JobStore.ListJobs()
	assert.NotNil(t, jobs)
	assert.Empty(t, jobs, "should start with no jobs")
}

// TestRun_TokenStoreInitialized verifies that the TokenStore is initialized
// for proxy verification. This is a regression guard for the critical fix
// that ensures backend-enforced test-before-save for proxy configuration.
// See: Proxy System Prevention Plan - Task 2
func TestRun_TokenStoreInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	configPath, _ := setupTagTestDB(t)

	cmd := api.NewCommand()
	deps, _, err := api.Run(cmd, configPath, "", 0)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = deps.CoreDeps.DB.Close() }()

	assert.NotNil(t, deps.TokenStore, "TokenStore must be initialized for proxy verification")
	assert.NotNil(t, deps.TokenStore, "TokenStore is required for backend save enforcement")
}

// TestRun_ErrorConfigNotFound verifies error when config doesn't exist
func TestRun_ErrorConfigNotFound(t *testing.T) {
	cmd := api.NewCommand()
	nonExistentPath := testutil.UnreachableConfigPath(t)

	deps, _, err := api.Run(cmd, nonExistentPath, "", 0)
	assert.Error(t, err, "should error when config not found")
	assert.Nil(t, deps)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRun_EnvironmentOverrides verifies that DEEPL_API_KEY and other env vars
// are applied before validation, preventing "api_key is required" errors
// when the key is provided via environment instead of config file.
func TestRun_EnvironmentOverrides(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("deepl_api_key_env_var_satisfies_validation", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `config_version: 3
database:
  dsn: ":memory:"
  type: sqlite
scrapers:
  priority: ["r18dev", "dmm"]
  timeout_seconds: 30
  request_timeout_seconds: 60
  r18dev:
    enabled: true
    language: en
  dmm:
    enabled: true
metadata:
  priority: {}
  translation:
    enabled: true
    provider: deepl
    deepl:
      mode: free
matching:
  extensions: [".mp4"]
  regex_enabled: false
server:
  host: localhost
  port: 8080
system:
  temp_dir: ` + tmpDir + `
`
		require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

		t.Setenv("DEEPL_API_KEY", "test-deepl-key-from-env")

		cmd := api.NewCommand()
		deps, _, err := api.Run(cmd, configPath, "", 0)
		require.NoError(t, err, "DEEPL_API_KEY env var should satisfy validation when config api_key is empty")
		require.NotNil(t, deps)
		defer func() { _ = deps.CoreDeps.DB.Close() }()

		currentCfg := deps.CoreDeps.GetConfig()
		assert.Equal(t, "test-deepl-key-from-env", currentCfg.Metadata.Translation.DeepL.APIKey,
			"env var DEEPL_API_KEY should be applied to config")
	})

	t.Run("deepl_missing_key_still_fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `config_version: 3
database:
  dsn: ":memory:"
  type: sqlite
scrapers:
  priority: ["r18dev", "dmm"]
  timeout_seconds: 30
  request_timeout_seconds: 60
  r18dev:
    enabled: true
    language: en
  dmm:
    enabled: true
metadata:
  priority: {}
  translation:
    enabled: true
    provider: deepl
    deepl:
      mode: free
matching:
  extensions: [".mp4"]
  regex_enabled: false
server:
  host: localhost
  port: 8080
system:
  temp_dir: ` + tmpDir + `
`
		require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

		t.Setenv("DEEPL_API_KEY", "")

		cmd := api.NewCommand()
		deps, _, err := api.Run(cmd, configPath, "", 0)
		assert.Error(t, err, "should still fail when deepl api_key is missing from both config and env")
		assert.Nil(t, deps)
		assert.Contains(t, err.Error(), "invalid configuration")
	})

	t.Run("openai_api_key_env_var", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")

		configContent := `config_version: 3
database:
  dsn: ":memory:"
  type: sqlite
scrapers:
  priority: ["r18dev", "dmm"]
  timeout_seconds: 30
  request_timeout_seconds: 60
  r18dev:
    enabled: true
    language: en
  dmm:
    enabled: true
metadata:
  priority: {}
  translation:
    enabled: true
    provider: openai
matching:
  extensions: [".mp4"]
  regex_enabled: false
server:
  host: localhost
  port: 8080
system:
  temp_dir: ` + tmpDir + `
`
		require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

		t.Setenv("OPENAI_API_KEY", "sk-test-openai-key-from-env")

		cmd := api.NewCommand()
		deps, _, err := api.Run(cmd, configPath, "", 0)
		require.NoError(t, err, "OPENAI_API_KEY env var should satisfy validation")
		require.NotNil(t, deps)
		defer func() { _ = deps.CoreDeps.DB.Close() }()

		currentCfg := deps.CoreDeps.GetConfig()
		assert.Equal(t, "sk-test-openai-key-from-env", currentCfg.Metadata.Translation.OpenAI.APIKey)
	})
}
