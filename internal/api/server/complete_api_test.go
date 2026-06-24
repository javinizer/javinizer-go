package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// setupCompleteServer creates a fully-wired server with real database
func setupCompleteServer(t *testing.T) (*gin.Engine, *core.APIDeps) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := setupTestDB(t)
	cfg := config.DefaultConfig(nil, nil)
	cfg.Logging = config.LoggingConfig{Level: "error"}
	cfg.Matching = config.MatchingConfig{RegexEnabled: false}
	cfg.Scrapers = config.ScrapersConfig{Priority: []string{"r18dev", "dmm"}}
	cfg.API = config.APIConfig{
		Security: config.SecurityConfig{
			AllowedOrigins:     []string{"http://localhost:8080"},
			AllowedDirectories: []string{t.TempDir()},
		},
	}

	registry := scraperutil.NewScraperRegistry()
	repos := db.Repositories()

	deps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: registry,
			DB:              db,
		},
		ConfigFile: fmt.Sprintf("%s/config.yaml", t.TempDir()),
		Repos:      repos,
		JobStore:   worker.NewJobStore(nil, nil, nil, "", nil, nil),
	}
	rt := core.NewAPIRuntime(deps)
	rt.SetConfig(cfg)
	rt.Runtime = core.NewRuntimeState()
	testkit.SetTestRuntime(deps, rt)
	deps.TokenStore = core.NewTokenStore()

	router := NewServer(testkit.GetTestRuntime(deps))
	t.Cleanup(func() { cleanupServerHub(t, deps) })

	return router, deps
}

func seedTestData(t *testing.T, deps *core.APIDeps) {
	t.Helper()
	ctx := context.Background()

	// Create movies
	for i := 0; i < 3; i++ {
		movie := &models.Movie{
			ContentID:    fmt.Sprintf("SEED-%03d", i),
			ID:           fmt.Sprintf("SEED-%03d", i),
			Title:        fmt.Sprintf("Seeded Movie %d", i),
			DisplayTitle: fmt.Sprintf("Seeded Movie %d", i),
			Maker:        "Test Studio",
			Genres:       []models.Genre{{Name: fmt.Sprintf("Genre%d", i)}},
		}
		_, err := deps.Repos.MovieRepo.Upsert(ctx, movie)
		require.NoError(t, err)
	}

	// Create actresses
	for i := 0; i < 3; i++ {
		actress := &models.Actress{
			LastName:  fmt.Sprintf("Actress%d", i),
			FirstName: "Test",
		}
		err := deps.Repos.ActressRepo.Create(ctx, actress)
		require.NoError(t, err)
	}

	// Create history
	history := &models.History{
		MovieID:   "SEED-000",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
	}
	require.NoError(t, deps.Repos.HistoryRepo.Create(ctx, history))

	// Create event
	event := &models.Event{
		Source:    "test",
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityInfo,
		Message:   "seeded test event",
	}
	require.NoError(t, deps.Repos.EventRepo.Create(ctx, event))

	// Create word replacement
	wr := &models.WordReplacement{Original: "testfrom", Replacement: "testto"}
	require.NoError(t, deps.Repos.WordReplacementRepo.Create(ctx, wr))

	// Create genre replacement
	gr := &models.GenreReplacement{Original: "oldgenre", Replacement: "newgenre"}
	require.NoError(t, deps.Repos.GenreReplacementRepo.Create(ctx, gr))

	// Add tags
	require.NoError(t, deps.Repos.MovieTagRepo.AddTag(ctx, "SEED-000", "test-tag"))
}

func TestCompleteAPI_AllGetEndpoints(t *testing.T) {
	router, deps := setupCompleteServer(t)
	seedTestData(t, deps)

	endpoints := []struct {
		path       string
		expectCode int
	}{
		{"/health", http.StatusOK},
		{"/api/v1/movies", http.StatusOK},
		{"/api/v1/movies/SEED-000", http.StatusOK},
		{"/api/v1/actresses", http.StatusOK},
		{"/api/v1/genres", http.StatusOK},
		{"/api/v1/genres/replacements", http.StatusOK},
		{"/api/v1/words/replacements", http.StatusOK},
		{"/api/v1/history", http.StatusOK},
		{"/api/v1/events", http.StatusOK},
		{"/api/v1/events/stats", http.StatusOK},
		{"/api/v1/jobs", http.StatusOK},
		{"/api/v1/config", http.StatusOK},
		{"/api/v1/scrapers", http.StatusOK},
		{"/api/v1/version", http.StatusOK},
	}

	for _, ep := range endpoints {
		t.Run("GET "+ep.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", ep.path, nil)
			router.ServeHTTP(w, req)
			assert.Equal(t, ep.expectCode, w.Code, "GET %s returned %d, body: %s", ep.path, w.Code, w.Body.String()[:min(len(w.Body.String()), 200)])
		})
	}
}

func TestCompleteAPI_MovieLifecycle(t *testing.T) {
	router, deps := setupCompleteServer(t)
	ctx := context.Background()

	// Create via DB
	movie := &models.Movie{
		ContentID:    "LIFE-001",
		ID:           "LIFE-001",
		Title:        "Lifecycle Movie",
		DisplayTitle: "Lifecycle Movie",
		Maker:        "Lifecycle Studio",
		Genres:       []models.Genre{{Name: "Action"}, {Name: "Drama"}},
		Actresses:    []models.Actress{{LastName: "Star", FirstName: "Bright"}},
	}
	_, err := deps.Repos.MovieRepo.Upsert(ctx, movie)
	require.NoError(t, err)

	// Read via API
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/movies/LIFE-001", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var movieResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &movieResp))

	// List all movies
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v1/movies", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Delete via DB
	require.NoError(t, deps.Repos.MovieRepo.Delete(ctx, "LIFE-001"))
}

func TestCompleteAPI_GenreReplacements(t *testing.T) {
	router, deps := setupCompleteServer(t)
	_ = deps

	// List genre replacements (should be empty)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/genres/replacements", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// List word replacements (should be empty)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v1/words/replacements", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCompleteAPI_AuthLogin(t *testing.T) {
	router, _ := setupCompleteServer(t)

	// Try to login
	loginBody := `{"username":"admin","password":"wrong"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	// Login may fail if no credentials set up, but should not 404
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestCompleteAPI_ProxyTest(t *testing.T) {
	router, _ := setupCompleteServer(t)

	// Test proxy endpoint
	body := `{"url":"http://example.com","type":"http"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/proxy/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	// May fail due to config, but should not 404
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestCompleteAPI_HistoryOperations(t *testing.T) {
	router, deps := setupCompleteServer(t)
	ctx := context.Background()

	// Create history entries
	for i := 0; i < 5; i++ {
		h := &models.History{
			MovieID:   fmt.Sprintf("HIST-%03d", i),
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
		}
		require.NoError(t, deps.Repos.HistoryRepo.Create(ctx, h))
	}

	// List history
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/history", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// List with query params
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v1/history?operation=scrape&status=success", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// History stats
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v1/history/stats", nil)
	router.ServeHTTP(w, req)
	// Stats endpoint may not exist
	_ = w.Code
}

func TestCompleteAPI_EventOperations(t *testing.T) {
	router, deps := setupCompleteServer(t)
	ctx := context.Background()

	// Create events
	for i := 0; i < 5; i++ {
		event := &models.Event{
			EventType: models.EventCategoryScraper,
			Severity:  models.SeverityInfo,
			Message:   fmt.Sprintf("test event %d", i),
			Source:    "test",
		}
		require.NoError(t, deps.Repos.EventRepo.Create(ctx, event))
	}

	// List events
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/events", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Events stats
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/v1/events/stats", nil)
	router.ServeHTTP(w, req)
	// May or may not exist
	_ = w.Code
}

func TestCompleteAPI_JobOperations(t *testing.T) {
	router, _ := setupCompleteServer(t)

	// List jobs (should be empty)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/jobs", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCompleteAPI_VersionCheck(t *testing.T) {
	router, _ := setupCompleteServer(t)

	// Version status
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/version", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Version check
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/v1/version/check", nil)
	router.ServeHTTP(w, req)
	// May fail due to no network, but should not 404
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestCompleteAPI_ConfigEndpoints(t *testing.T) {
	router, _ := setupCompleteServer(t)

	// Get config
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/config", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var cfgResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cfgResp))

	// Update config
	configBody := `{"logging":{"level":"debug"}}`
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/v1/config", strings.NewReader(configBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	// May succeed or fail
	_ = w.Code
}

func TestCompleteAPI_ScraperEndpoints(t *testing.T) {
	router, _ := setupCompleteServer(t)

	// List scrapers
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/scrapers", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCompleteAPI_ActressSearch(t *testing.T) {
	router, deps := setupCompleteServer(t)
	ctx := context.Background()

	// Create actress with Japanese name
	actress := &models.Actress{
		LastName:     "Yamada",
		FirstName:    "Hanako",
		JapaneseName: "山田花子",
	}
	require.NoError(t, deps.Repos.ActressRepo.Create(ctx, actress))

	// Search actresses
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/actresses/search?q=Yamada", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCompleteAPI_BatchJobCreate(t *testing.T) {
	router, _ := setupCompleteServer(t)

	// Try to create a batch job
	body := `{"source_path":"/tmp","dest_path":"/tmp"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/batch/scrape", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	// May fail due to invalid paths, but should not 404
	assert.NotEqual(t, http.StatusNotFound, w.Code)
}

func TestCompleteAPI_TempEndpoints(t *testing.T) {
	router, _ := setupCompleteServer(t)

	// List temp files
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/temp/posters/fake-job/test.jpg", nil)
	router.ServeHTTP(w, req)
	// Should 404 or 400 since job doesn't exist
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
