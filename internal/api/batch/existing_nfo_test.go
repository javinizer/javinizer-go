package batch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const existingNFOSampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>[MKMP-094] Ayaka Tomoda</title>
  <id>IPX-535</id>
  <uniqueid type="contentid" default="true">ipx00535</uniqueid>
  <plot>An existing NFO plot that differs from the scraped description.</plot>
  <runtime>120</runtime>
  <maker>SOD</maker>
  <label>IP Premium</label>
  <genre>Digital Mosaic</genre>
  <thumb aspect="poster">https://example.com/covers/existing.jpg</thumb>
</movie>`

func writeExistingNFOTempFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func newExistingNFOConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Metadata.NFO.Format.FilenameTemplate = "<ID>.nfo"
	cfg.Metadata.NFO.Feature.Enabled = true
	return cfg
}

func setupExistingNFOJob(t *testing.T, cfg *config.Config, dir string, scrapedMovie *models.Movie) (*core.APIDeps, string, string) {
	t.Helper()
	deps := createTestDeps(t, cfg, "")

	videoPath := filepath.Join(dir, scrapedMovie.ID+".mp4")
	job := deps.JobStore.CreateJobBatch([]string{videoPath})
	result := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: videoPath, MovieID: scrapedMovie.ID},
		Status:        models.JobStatusCompleted,
		Movie:         scrapedMovie,
		StartedAt:     time.Now(),
	}
	setJobResult(job, videoPath, result)

	return deps, job.GetID(), result.ResultID
}

func setupExistingNFOJobWithResult(t *testing.T, cfg *config.Config, dir string, result *resultstore.MovieResult) (*core.APIDeps, string, string) {
	t.Helper()
	deps := createTestDeps(t, cfg, "")

	videoPath := filepath.Join(dir, "IPX-535.mp4")
	job := deps.JobStore.CreateJobBatch([]string{videoPath})
	setJobResult(job, videoPath, result)

	return deps, job.GetID(), result.ResultID
}

func callExistingNFOEndpoint(t *testing.T, rt *core.APIRuntime, jobID, resultID string) *httptest.ResponseRecorder {
	t.Helper()
	router := gin.New()
	router.GET("/batch/:id/results/:resultId/existing-nfo", getExistingNFO(rt))

	req := httptest.NewRequest(http.MethodGet, "/batch/"+jobID+"/results/"+resultID+"/existing-nfo", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestGetExistingNFO_Found(t *testing.T) {
	dir := t.TempDir()
	writeExistingNFOTempFile(t, dir, "IPX-535.nfo", existingNFOSampleXML)

	scraped := &models.Movie{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Ayaka Tomoda",
		Description: "A freshly scraped description.",
		Runtime:     120,
		Maker:       "SOD",
		Label:       "IP Premium",
	}

	cfg := newExistingNFOConfig()
	deps, jobID, resultID := setupExistingNFOJob(t, cfg, dir, scraped)
	rt := testkit.GetTestRuntime(deps)

	w := callExistingNFOEndpoint(t, rt, jobID, resultID)

	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.ExistingNFOResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	require.NotNil(t, resp.ExistingNFO, "existing_nfo should be populated when an NFO is found")
	assert.Equal(t, "IPX-535", resp.ExistingNFO.ID)
	assert.Equal(t, "[MKMP-094] Ayaka Tomoda", resp.ExistingNFO.Title)
	assert.Equal(t, "SOD", resp.ExistingNFO.Maker)

	require.NotEmpty(t, resp.NFODifferences, "differences should be non-empty when scraped and NFO values diverge")

	var titleDiff *contracts.FieldDifference
	for i := range resp.NFODifferences {
		if resp.NFODifferences[i].Field == "title" {
			titleDiff = &resp.NFODifferences[i]
			break
		}
	}
	require.NotNil(t, titleDiff, "expected a title difference")
	assert.Equal(t, "[MKMP-094] Ayaka Tomoda", titleDiff.NFOValue)
	assert.Equal(t, "Ayaka Tomoda", titleDiff.ScrapedValue)

	for _, d := range resp.NFODifferences {
		assert.NotEqual(t, "maker", d.Field, "identical fields must not appear in differences")
	}
}

func TestGetExistingNFO_NoNFO(t *testing.T) {
	dir := t.TempDir()

	scraped := &models.Movie{ID: "IPX-535", Title: "Ayaka Tomoda", Maker: "SOD"}

	cfg := newExistingNFOConfig()
	deps, jobID, resultID := setupExistingNFOJob(t, cfg, dir, scraped)
	rt := testkit.GetTestRuntime(deps)

	w := callExistingNFOEndpoint(t, rt, jobID, resultID)

	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.ExistingNFOResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Nil(t, resp.ExistingNFO)
	assert.Empty(t, resp.NFODifferences)
}

func TestGetExistingNFO_ParseFailure(t *testing.T) {
	dir := t.TempDir()
	writeExistingNFOTempFile(t, dir, "IPX-535.nfo", "not valid xml << broken")

	scraped := &models.Movie{ID: "IPX-535", Title: "Ayaka Tomoda", Maker: "SOD"}

	cfg := newExistingNFOConfig()
	deps, jobID, resultID := setupExistingNFOJob(t, cfg, dir, scraped)
	rt := testkit.GetTestRuntime(deps)

	w := callExistingNFOEndpoint(t, rt, jobID, resultID)

	require.Equal(t, http.StatusOK, w.Code)

	var resp contracts.ExistingNFOResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Nil(t, resp.ExistingNFO, "parse failure must be non-blocking and return an empty response")
	assert.Empty(t, resp.NFODifferences)
}

func TestGetExistingNFO_JobNotFound(t *testing.T) {
	cfg := newExistingNFOConfig()
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)

	w := callExistingNFOEndpoint(t, rt, "nonexistent-job", "any-result")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetExistingNFO_ResultNotFound(t *testing.T) {
	dir := t.TempDir()
	scraped := &models.Movie{ID: "IPX-535", Title: "Ayaka Tomoda"}

	cfg := newExistingNFOConfig()
	deps, jobID, _ := setupExistingNFOJob(t, cfg, dir, scraped)
	rt := testkit.GetTestRuntime(deps)

	w := callExistingNFOEndpoint(t, rt, jobID, "missing-result-id")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetExistingNFO_EarlyReturn(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "IPX-535.mp4")

	cases := []struct {
		name   string
		result *resultstore.MovieResult
	}{
		{
			name: "nil movie",
			result: &resultstore.MovieResult{
				ResultID:      "res-nil-movie",
				FileMatchInfo: models.FileMatchInfo{Path: videoPath, MovieID: "IPX-535"},
				Status:        models.JobStatusCompleted,
			},
		},
		{
			name: "empty movie id",
			result: &resultstore.MovieResult{
				ResultID:      "res-empty-id",
				FileMatchInfo: models.FileMatchInfo{Path: videoPath, MovieID: "IPX-535"},
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{Title: "missing id"},
			},
		},
		{
			name: "empty source path",
			result: &resultstore.MovieResult{
				ResultID:      "res-empty-path",
				FileMatchInfo: models.FileMatchInfo{MovieID: "IPX-535"},
				Status:        models.JobStatusCompleted,
				Movie:         &models.Movie{ID: "IPX-535"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newExistingNFOConfig()
			deps, jobID, resultID := setupExistingNFOJobWithResult(t, cfg, dir, tc.result)
			rt := testkit.GetTestRuntime(deps)

			w := callExistingNFOEndpoint(t, rt, jobID, resultID)

			require.Equal(t, http.StatusOK, w.Code)

			var resp contracts.ExistingNFOResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

			assert.Nil(t, resp.ExistingNFO)
			assert.Empty(t, resp.NFODifferences)
		})
	}
}
