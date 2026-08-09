package batch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// F-R14-2 integration: an admission conflict surfacing from Rescrape (here:
// an outstanding promote witness fencing the movie) reaches the client as
// 409, not a flattened 500.
func TestRescrapeBatchMovie_WitnessFenceConflictIs409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tempDir := t.TempDir()
	origWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(origWd) })

	cfg := &config.Config{System: config.SystemConfig{TempDir: "data/temp"}}
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})
	job := createJobWithWF(deps, cfg, []string{"/tmp/IPXC-9.mp4"})
	setJobResult(job, "/tmp/IPXC-9.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPXC-9.mp4", MovieID: "IPXC-9"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPXC-9", Title: "Old Title"},
	})

	// Outstanding promote witness for the movie — fences regeneration.
	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, deps.GetFs().MkdirAll(posterDir, 0o755))
	require.NoError(t, afero.WriteFile(deps.GetFs(), filepath.Join(posterDir, ".promote-IPXC-9.json"), []byte("{}"), 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "IPXC-9",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPXC-9/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code, "witness fence must surface as 409: %s", rec.Body.String())
}

// The mid-rescrape Gone arm: a job whose results tracker flips gone between
// admission and commit gets the 410, never a false success.
func TestRescrapeBatchMovie_GoneMidRescrapeIs410(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.CoreDeps.GetRegistry().RegisterInstance(&noPosterStubScraper{})
	job := createJobWithWF(deps, cfg, []string{"/tmp/IPXG-9.mp4"})
	setJobResult(job, "/tmp/IPXG-9.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/tmp/IPXG-9.mp4", MovieID: "IPXG-9"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPXG-9", Title: "Old Title"},
	})

	// The job object stays admission-passable, but the results tracker reports
	// gone at commit time — the race the Gone arm exists for.
	job.Results().SetGoneChecker(func() bool { return true })

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.BatchRescrapeRequest{
		SelectedScrapers:  []string{"stub-no-poster"},
		ManualSearchInput: "IPXG-9",
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/IPXG-9/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusGone, rec.Code, "mid-rescrape gone must be 410: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "deleted during rescrape")
}
