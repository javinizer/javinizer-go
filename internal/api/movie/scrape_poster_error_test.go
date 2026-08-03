package movie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// errPosterGen always fails poster generation — a download, source-validation,
// or image-processing failure must reach the response contract instead of
// being discarded (`_ = GeneratePoster(...)`), Codex round-9 P1-B.
type errPosterGen struct {
	err   error
	calls int32
}

func (g *errPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	atomic.AddInt32(&g.calls, 1)
	return g.err
}

// TestScrapeMovie_PosterGenerationErrorSurfaced pins the response contract:
// a failed poster generation on an otherwise-successful scrape is reported in
// ScrapeResponse.Errors while the scrape result itself still returns 200.
func TestScrapeMovie_PosterGenerationErrorSurfaced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}}}
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result:  &models.ScraperResult{Source: "r18dev", ID: "PERR-001", Title: "Poster Error"},
	})

	gen := &errPosterGen{err: errors.New("poster source image failed validation")}
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow), WithPosterGen(gen))
	router := gin.New()
	router.POST("/scrape", scrapeMovie(movieDeps))

	body, _ := json.Marshal(contracts.ScrapeRequest{ID: "PERR-001"})
	req := httptest.NewRequest(http.MethodPost, "/scrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp contracts.ScrapeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Movie, "the scrape itself succeeded — the movie is still returned")
	assert.Equal(t, int32(1), atomic.LoadInt32(&gen.calls))
	require.Len(t, resp.Errors, 1,
		"a discarded poster generation error would leave ScrapeResponse.Errors empty on success")
	assert.Contains(t, resp.Errors[0], "poster generation failed")
	assert.Contains(t, resp.Errors[0], "poster source image failed validation")

	// A working generator leaves Errors empty (omitted by the contract).
	okDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow), WithPosterGen(&okPosterGen{}))
	okRouter := gin.New()
	okRouter.POST("/scrape", scrapeMovie(okDeps))
	body2, _ := json.Marshal(contracts.ScrapeRequest{ID: "OK-002"})
	req2 := httptest.NewRequest(http.MethodPost, "/scrape", bytes.NewBuffer(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	okRouter.ServeHTTP(w2, req2)
	assert.NotContains(t, w2.Body.String(), `"errors"`,
		"errors must be omitted when poster generation succeeds")
}

// TestRescrapeMovie_PosterGenerationErrorSurfaced pins the same contract on
// the rescrape endpoint (MovieResponse.Errors).
func TestRescrapeMovie_PosterGenerationErrorSurfaced(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}}}
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&mockScraperWithResults{
		name:    "r18dev",
		enabled: true,
		result:  &models.ScraperResult{Source: "r18dev", ID: "PERR-002", Title: "Poster Error"},
	})

	gen := &errPosterGen{err: errors.New("poster download failed")}
	movieDeps := NewMovieDeps(deps.Repos.MovieRepo, WithWorkflow(testkit.GetTestRuntime(deps).GetWorkflow), WithPosterGen(gen))
	router := gin.New()
	router.POST("/movies/:id/rescrape", rescrapeMovie(movieDeps))

	body, _ := json.Marshal(contracts.RescrapeRequest{SelectedScrapers: []string{"r18dev"}})
	req := httptest.NewRequest(http.MethodPost, "/movies/PERR-002/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp contracts.MovieResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Movie)
	assert.Equal(t, int32(1), atomic.LoadInt32(&gen.calls))
	require.Len(t, resp.Errors, 1,
		"the rescrape endpoint must surface poster generation failures, not ack silence")
	assert.Contains(t, resp.Errors[0], "poster generation failed")
}

// okPosterGen succeeds without side effects.
type okPosterGen struct{}

func (g *okPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	return nil
}
