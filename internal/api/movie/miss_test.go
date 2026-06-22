package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- scrapeMovie: cancelled request context ---

func TestScrapeMovie_Miss_CancelledContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow), WithAllowedDirs(testkit.GetTestRuntime(deps).GetAPIConfig().AllowedDirectories))

	reqBody := contracts.ScrapeRequest{ID: "IPX-535"}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest("POST", "/api/v1/scrape", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	// Cancel the request context
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)
	c.Request = req

	scrapeMovie(movieDeps)(c)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// --- scrapeMovie: workflow not available ---

func TestScrapeMovie_Miss_WorkflowNotAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo)

	reqBody := contracts.ScrapeRequest{ID: "IPX-535"}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/scrape", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	scrapeMovie(movieDeps)(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- scrapeMovie: invalid JSON body ---

func TestScrapeMovie_Miss_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/scrape", bytes.NewReader([]byte("invalid json")))
	c.Request.Header.Set("Content-Type", "application/json")

	scrapeMovie(movieDeps)(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- getMovie: not found ---

func TestGetMovie_Miss_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/movies/NF-001", nil)
	c.Params = gin.Params{{Key: "id", Value: "NF-001"}}

	getMovie(movieDeps)(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- listMovies: repo error ---

func TestListMovies_Miss_RepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	movieDeps := NewMovieDeps(&errMovieRepo{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/movies?limit=20&offset=0", nil)

	listMovies(movieDeps)(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- rescrapeMovie: empty selected scrapers ---

func TestRescrapeMovie_Miss_EmptyScrapers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	reqBody := contracts.RescrapeRequest{SelectedScrapers: []string{}}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/movies/IPX-535/rescrape", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "IPX-535"}}

	rescrapeMovie(movieDeps)(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- rescrapeMovie: workflow not available ---

func TestRescrapeMovie_Miss_WorkflowNotAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo)

	reqBody := contracts.RescrapeRequest{SelectedScrapers: []string{"r18dev"}}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/movies/IPX-535/rescrape", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "IPX-535"}}

	rescrapeMovie(movieDeps)(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- rescrapeMovie: invalid JSON body ---

func TestRescrapeMovie_Miss_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/movies/IPX-535/rescrape", bytes.NewReader([]byte("bad json")))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "IPX-535"}}

	rescrapeMovie(movieDeps)(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- compareNFO: workflow not available ---

func TestCompareNFO_Miss_WorkflowNotAvailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	nfoPath := filepath.Join(tempDir, "WF-001.nfo")
	require.NoError(t, os.WriteFile(nfoPath, []byte(`<?xml version="1.0"?><movie><title>Test</title></movie>`), 0644))

	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	// Remove workflow
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithAllowedDirs(testkit.GetTestRuntime(deps).GetAPIConfig().AllowedDirectories))

	reqBody := contracts.NFOComparisonRequest{NFOPath: nfoPath, ScalarStrategy: "prefer-scraper"}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/movies/WF-001/compare-nfo", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "WF-001"}}

	compareNFO(movieDeps)(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- compareNFO: invalid preset ---

func TestCompareNFO_Miss_InvalidPreset(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	nfoPath := filepath.Join(tempDir, "INV-001.nfo")
	require.NoError(t, os.WriteFile(nfoPath, []byte(`<?xml version="1.0"?><movie><title>Test</title></movie>`), 0644))

	cfg := &config.Config{
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
			},
		},
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow), WithAllowedDirs(testkit.GetTestRuntime(deps).GetAPIConfig().AllowedDirectories))

	reqBody := contracts.NFOComparisonRequest{NFOPath: nfoPath, Preset: "invalid-preset"}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/movies/INV-001/compare-nfo", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "INV-001"}}

	compareNFO(movieDeps)(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- validateNFOPath: invalid path ---

func TestValidateNFOPath_Miss_InvalidPath(t *testing.T) {
	_, err := validateNFOPath("/nonexistent/path/file.nfo", []string{"/some/dir"})
	require.Error(t, err)
}

// --- validateNFOPath: empty allowed dirs ---

func TestValidateNFOPath_Miss_EmptyAllowedDirs(t *testing.T) {
	_, err := validateNFOPath("/some/path/file.nfo", []string{})
	require.Error(t, err)
	assert.Equal(t, ErrNFOAccessDenied, err)
}

// --- errMovieRepo: a repo that always returns errors ---

type errMovieRepo struct{}

func (e *errMovieRepo) Create(_ context.Context, _ *models.Movie) error {
	return fmt.Errorf("db error")
}
func (e *errMovieRepo) Update(_ context.Context, _ *models.Movie) error {
	return fmt.Errorf("db error")
}
func (e *errMovieRepo) Upsert(_ context.Context, _ *models.Movie) (*models.Movie, error) {
	return nil, fmt.Errorf("db error")
}
func (e *errMovieRepo) UpsertWithTranslations(_ context.Context, _ *models.Movie, _ []models.GenreTranslationData, _ []models.ActressTranslationData) (*models.Movie, error) {
	return nil, fmt.Errorf("db error")
}
func (e *errMovieRepo) FindByID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, fmt.Errorf("db error")
}
func (e *errMovieRepo) FindByContentID(_ context.Context, _ string) (*models.Movie, error) {
	return nil, fmt.Errorf("db error")
}
func (e *errMovieRepo) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("db error")
}
func (e *errMovieRepo) List(_ context.Context, _, _ int) ([]models.Movie, error) {
	return nil, fmt.Errorf("db error")
}
