package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/api"
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
func createTestAPIServer(t *testing.T) *api.ServerDependencies {
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
	deps := &api.ServerDependencies{
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
	defer deps.DB.Close()

	router := api.NewServer(deps)

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
	defer deps.DB.Close()

	// Insert test movie
	movie := createTestMovie("IPX-123", "Test Movie")
	err := deps.MovieRepo.Upsert(movie)
	require.NoError(t, err)

	router := api.NewServer(deps)

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
	defer deps.DB.Close()

	// Insert test movie
	movie := createTestMovie("IPX-123", "Test Movie")
	err := deps.MovieRepo.Upsert(movie)
	require.NoError(t, err)

	router := api.NewServer(deps)

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
	defer deps.DB.Close()

	router := api.NewServer(deps)

	req, _ := http.NewRequest("GET", "/api/v1/movies/NONEXISTENT-999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Note: Additional API endpoint tests can be added here as needed
// Currently focusing on core endpoints that demonstrate router setup testing
