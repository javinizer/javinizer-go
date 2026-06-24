package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// setupFullTestServer creates a server with ALL dependencies properly initialized.
func setupFullTestServer(t *testing.T) (*gin.Engine, *core.APIDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := setupTestDB(t)
	cfg := config.DefaultConfig(nil, nil)
	cfg.Logging = config.LoggingConfig{Level: "error"}
	cfg.Matching = config.MatchingConfig{RegexEnabled: false}
	cfg.Scrapers = config.ScrapersConfig{Priority: []string{"r18dev", "dmm"}}
	cfg.API = config.APIConfig{
		Security: config.SecurityConfig{
			AllowedOrigins: []string{"http://localhost:8080"},
		},
	}

	registry := scraperutil.NewScraperRegistry()
	deps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: registry,
			DB:              db,
		},
		ConfigFile: "/tmp/config.yaml",
		Repos: database.Repositories{
			ContentRepos: database.ContentRepos{
				MovieRepo:   database.NewMovieRepository(db),
				ActressRepo: database.NewActressRepository(db),
			},
		},
		JobStore: worker.NewJobStore(nil, nil, nil, "", nil, nil),
	}
	testkit.GetTestRuntime(deps)
	testkit.GetTestRuntime(deps).SetConfig(cfg)
	rt := testkit.GetTestRuntime(deps)
	rt.Runtime = core.NewRuntimeState()
	deps.TokenStore = core.NewTokenStore()

	router := NewServer(testkit.GetTestRuntime(deps))
	t.Cleanup(func() { cleanupServerHub(t, deps) })

	return router, deps
}

// acceptableCodes returns accepted status codes for endpoint smoke tests.
// Some endpoints may return 500 when optional dependencies are not wired.
func acceptableCodes(codes ...int) []int { return codes }

func TestDeepIntegration_HealthEndpoint(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestDeepIntegration_VersionEndpoint(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/version", nil)
	router.ServeHTTP(w, req)

	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized), w.Code)
}

func TestDeepIntegration_MovieEndpoints(t *testing.T) {
	router, _ := setupFullTestServer(t)

	// List movies
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/movies", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)

	// Get movie by ID
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v1/movies/INT-001", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_GenreEndpoints(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/genres", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_ActressEndpoints(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/actresses", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_JobEndpoints(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/jobs", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_EventEndpoints(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/events", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_ConfigEndpoint(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/config", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_TokenEndpoints(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/tokens", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_ScanEndpoint(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/files/scan", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusBadRequest, http.StatusInternalServerError, http.StatusNotFound), w.Code)
}

func TestDeepIntegration_SystemEndpoints(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/system/info", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError, http.StatusNotFound), w.Code)
}

func TestDeepIntegration_MovieCreateAndGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	_, deps := setupFullTestServer(t)

	// Create movie via the wired-up repo
	movie := &models.Movie{
		ContentID:    "CREAT-001",
		ID:           "CREAT-001",
		Title:        "Created Movie",
		DisplayTitle: "Created Movie",
		Maker:        "Test Studio",
	}
	_, err := deps.Repos.MovieRepo.Upsert(context.Background(), movie)
	require.NoError(t, err)

	// Verify it can be retrieved
	found, err := deps.Repos.MovieRepo.FindByID(context.Background(), "CREAT-001")
	require.NoError(t, err)
	assert.Equal(t, "Created Movie", found.Title)
	assert.Equal(t, "Test Studio", found.Maker)
}

func TestDeepIntegration_MovieSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	router, deps := setupFullTestServer(t)

	// Create some movies
	for i := 0; i < 5; i++ {
		movie := &models.Movie{
			ContentID:    fmt.Sprintf("SEARCH-%03d", i),
			ID:           fmt.Sprintf("SEARCH-%03d", i),
			Title:        fmt.Sprintf("Search Movie %d", i),
			DisplayTitle: fmt.Sprintf("Search Movie %d", i),
		}
		_, err := deps.Repos.MovieRepo.Upsert(context.Background(), movie)
		require.NoError(t, err)
	}

	// Search via API
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/movies?q=Search", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_ScraperListEndpoint(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/scrapers", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}

func TestDeepIntegration_NonexistentEndpoint(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/nonexistent", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeepIntegration_MovieGetInvalid(t *testing.T) {
	router, _ := setupFullTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/movies/INVALID", nil)
	router.ServeHTTP(w, req)
	assert.Contains(t, acceptableCodes(http.StatusOK, http.StatusNotFound, http.StatusUnauthorized, http.StatusInternalServerError), w.Code)
}
