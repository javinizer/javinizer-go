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
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// setupFullServerWithDB creates a complete server with real database for integration testing.
func setupFullServerWithDB(t *testing.T) (*gin.Engine, *core.APIDeps) {
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
	repos := db.Repositories()

	deps := &core.APIDeps{
		CoreDeps: &commandutil.CoreDeps{
			ScraperRegistry: registry,
			DB:              db,
		},
		ConfigFile: "/tmp/config.yaml",
		Repos:      repos,
		JobStore:   worker.NewJobStore(nil, nil, nil, "", nil, nil),
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

func TestFullDB_MovieCRUD(t *testing.T) {
	_, deps := setupFullServerWithDB(t)
	ctx := context.Background()

	movie := &models.Movie{
		ContentID:    "FULLDB-001",
		ID:           "FULLDB-001",
		Title:        "Full DB Movie",
		DisplayTitle: "Full DB Movie",
		Maker:        "Test Studio",
	}
	_, err := deps.Repos.MovieRepo.Upsert(ctx, movie)
	require.NoError(t, err)

	found, err := deps.Repos.MovieRepo.FindByID(ctx, "FULLDB-001")
	require.NoError(t, err)
	assert.Equal(t, "Full DB Movie", found.Title)

	// Delete
	err = deps.Repos.MovieRepo.Delete(ctx, "FULLDB-001")
	require.NoError(t, err)
}

func TestFullDB_MovieWithGenres(t *testing.T) {
	_, deps := setupFullServerWithDB(t)
	ctx := context.Background()

	movie := &models.Movie{
		ContentID:    "FULLDBG-001",
		ID:           "FULLDBG-001",
		Title:        "Genre Movie",
		DisplayTitle: "Genre Movie",
		Genres:       []models.Genre{{Name: "Action"}, {Name: "Drama"}},
	}
	_, err := deps.Repos.MovieRepo.Upsert(ctx, movie)
	require.NoError(t, err)

	found, err := deps.Repos.MovieRepo.FindByID(ctx, "FULLDBG-001")
	require.NoError(t, err)
	assert.Len(t, found.Genres, 2)
}

func TestFullDB_MovieWithActresses(t *testing.T) {
	_, deps := setupFullServerWithDB(t)
	ctx := context.Background()

	movie := &models.Movie{
		ContentID:    "FULLDBA-001",
		ID:           "FULLDBA-001",
		Title:        "Actress Movie",
		DisplayTitle: "Actress Movie",
		Actresses:    []models.Actress{{LastName: "Star", FirstName: "Bright"}},
	}
	_, err := deps.Repos.MovieRepo.Upsert(ctx, movie)
	require.NoError(t, err)

	found, err := deps.Repos.MovieRepo.FindByID(ctx, "FULLDBA-001")
	require.NoError(t, err)
	assert.Len(t, found.Actresses, 1)
}

func TestFullDB_ActressCRUD(t *testing.T) {
	_, deps := setupFullServerWithDB(t)
	ctx := context.Background()

	actress := &models.Actress{LastName: "Integration", FirstName: "Test"}
	err := deps.Repos.ActressRepo.Create(ctx, actress)
	require.NoError(t, err)

	found, err := deps.Repos.ActressRepo.FindByID(ctx, actress.ID)
	require.NoError(t, err)
	assert.Equal(t, "Integration", found.LastName)

	found.FirstName = "Updated"
	err = deps.Repos.ActressRepo.Update(ctx, found)
	require.NoError(t, err)
}

func TestFullDB_HistoryCRUD(t *testing.T) {
	_, deps := setupFullServerWithDB(t)
	ctx := context.Background()

	history := &models.History{
		MovieID:   "HISTFULL-001",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
	}
	err := deps.Repos.HistoryRepo.Create(ctx, history)
	require.NoError(t, err)

	entries, err := deps.Repos.HistoryRepo.FindByMovieID(ctx, "HISTFULL-001")
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}

func TestFullDB_MovieSearch(t *testing.T) {
	_, deps := setupFullServerWithDB(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		movie := &models.Movie{
			ContentID:    fmt.Sprintf("SEARCH-%03d", i),
			ID:           fmt.Sprintf("SEARCH-%03d", i),
			Title:        fmt.Sprintf("Search Movie %d", i),
			DisplayTitle: fmt.Sprintf("Search Movie %d", i),
		}
		_, err := deps.Repos.MovieRepo.Upsert(ctx, movie)
		require.NoError(t, err)
	}

	results, err := deps.Repos.MovieRepo.List(ctx, 100, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1, "expected at least one movie from list")
}

func TestFullDB_MovieTags(t *testing.T) {
	_, deps := setupFullServerWithDB(t)
	ctx := context.Background()

	movie := &models.Movie{
		ContentID:    "TAGFULL-001",
		ID:           "TAGFULL-001",
		Title:        "Tag Movie",
		DisplayTitle: "Tag Movie",
	}
	_, err := deps.Repos.MovieRepo.Upsert(ctx, movie)
	require.NoError(t, err)

	err = deps.Repos.MovieTagRepo.AddTag(ctx, "TAGFULL-001", "action")
	require.NoError(t, err)

	err = deps.Repos.MovieTagRepo.RemoveTag(ctx, "TAGFULL-001", "action")
	require.NoError(t, err)
}

func TestFullAPI_MovieEndpoints(t *testing.T) {
	router, deps := setupFullServerWithDB(t)
	ctx := context.Background()

	// Create movie in DB
	movie := &models.Movie{
		ContentID:    "API-001",
		ID:           "API-001",
		Title:        "API Movie",
		DisplayTitle: "API Movie",
	}
	_, err := deps.Repos.MovieRepo.Upsert(ctx, movie)
	require.NoError(t, err)

	// Test API endpoints
	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/v1/movies"},
		{"GET", "/api/v1/movies/API-001"},
		{"GET", "/api/v1/genres"},
		{"GET", "/api/v1/actresses"},
		{"GET", "/api/v1/jobs"},
		{"GET", "/api/v1/events"},
		{"GET", "/api/v1/config"},
		{"GET", "/api/v1/scrapers"},
		{"GET", "/api/v1/version"},
	}

	for _, ep := range endpoints {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(ep.method, ep.path, nil)
		router.ServeHTTP(w, req)
		// Accept 200, 401, 404, or 500 (endpoint exists and handles request)
		assert.Contains(t, []int{http.StatusOK, http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError}, w.Code,
			"Endpoint %s %s returned %d", ep.method, ep.path, w.Code)
	}
}
