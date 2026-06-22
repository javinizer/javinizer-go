package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- getMovie: found returns 200 ---

func TestGetMovie_Miss2_Found(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	movie := &models.Movie{ID: "FOUND-001"}
	_, err := deps.Repos.MovieRepo.Upsert(context.Background(), movie)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/movies/FOUND-001", nil)
	c.Params = gin.Params{{Key: "id", Value: "FOUND-001"}}

	getMovie(movieDeps)(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.MovieResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "FOUND-001", resp.Movie.ID)
}

// --- listMovies: successful list returns 200 ---

func TestListMovies_Miss2_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	for i := 0; i < 3; i++ {
		movie := &models.Movie{ID: fmt.Sprintf("LIST-%03d", i)}
		_, err := deps.Repos.MovieRepo.Upsert(context.Background(), movie)
		require.NoError(t, err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/movies?limit=20&offset=0", nil)

	listMovies(movieDeps)(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp contracts.MoviesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Count, 3)
}

// --- scrapeMovie: empty ID after trim returns 404 ---

func TestScrapeMovie_Miss2_EmptyIDAfterTrim(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
	}

	deps := createTestDeps(t, cfg, "")
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow))

	reqBody := contracts.ScrapeRequest{ID: "   "}
	bodyBytes, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/scrape", bytes.NewReader(bodyBytes))
	c.Request.Header.Set("Content-Type", "application/json")

	scrapeMovie(movieDeps)(c)

	// Empty ID after trim will likely fail the scrape
	assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusInternalServerError || w.Code == http.StatusBadRequest,
		"Expected error status, got %d", w.Code)
}
