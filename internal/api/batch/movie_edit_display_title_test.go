package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateBatchMovie_ReDerivesDisplayTitleOnSave guards codex P2: after a
// Title edit is saved (no organize), persisted display_title must be re-derived
// from the new Title + the configured display_title template, not left stale.
// It also asserts the PATCH response returns the derived movie (not req.Movie).
func TestUpdateBatchMovie_ReDerivesDisplayTitleOnSave(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}},
		Metadata: config.MetadataConfig{
			NFO: config.NFOConfig{Format: config.NFOFormatConfig{DisplayTitle: "[<ID>] <TITLE>"}},
		},
	}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Original", DisplayTitle: "[TEST-001] Original"},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(batchDeps))

	// Request sends an edited Title but a STALE display_title (the pre-fix bug).
	edited := &contracts.MovieView{
		ID:           "TEST-001",
		Title:        "Updated",
		DisplayTitle: "[TEST-001] Original",
	}
	body, _ := json.Marshal(contracts.UpdateMovieRequest{Movie: edited})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/TEST-001", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp contracts.MovieResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Updated", resp.Movie.Title, "Title is preserved unchanged")
	assert.Equal(t, "[TEST-001] Updated", resp.Movie.DisplayTitle, "response carries the freshly re-derived display_title (not the stale request value)")

	// Persisted job result reflects the derived display_title.
	result, _, found := job.Results().GetFileResultByResultID("TEST-001")
	require.True(t, found)
	require.NotNil(t, result.Movie)
	assert.Equal(t, "Updated", result.Movie.Title)
	assert.Equal(t, "[TEST-001] Updated", result.Movie.DisplayTitle, "persisted display_title is fresh after Save")
}

// TestUpdateBatchMovie_DisplayTitleFallsBackToTitleWithoutTemplate covers the
// no-template + factory-unavailable degradation: when no display_title template
// is configured, DisplayTitle collapses to Title so a Save never persists a stale
// derived value. (RenderDisplayTitle returns "" only if the factory is nil; with
// a factory and no template, ApplyDisplayTitleFromSource sets DisplayTitle=Title.)
func TestUpdateBatchMovie_DisplayTitleFallsBackToTitleWithoutTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := &config.Config{Scrapers: config.ScrapersConfig{Priority: []string{"r18dev"}}}
	deps := createTestDeps(t, cfg, "")
	batchDeps := testkit.GetTestRuntime(deps)

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/TEST-001.mp4"})
	setJobResult(job, "/path/to/TEST-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/TEST-001.mp4", MovieID: "TEST-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001", Title: "Original", DisplayTitle: "[TEST-001] Original"},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.PATCH("/api/v1/batch/:id/results/:resultId", updateBatchMovie(batchDeps))

	edited := &contracts.MovieView{ID: "TEST-001", Title: "Updated", DisplayTitle: "[TEST-001] Original"}
	body, _ := json.Marshal(contracts.UpdateMovieRequest{Movie: edited})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/batch/"+job.GetID()+"/results/TEST-001", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp contracts.MovieResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Updated", resp.Movie.Title)
	assert.Equal(t, "Updated", resp.Movie.DisplayTitle, "with no template, DisplayTitle collapses to Title — never stale")
}
