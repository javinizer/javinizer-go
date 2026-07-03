package batch

import (
	"bytes"
	"context"
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
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
)

// badRegexConfig returns a config whose matching regex is invalid. This makes
// the workflow factory unbuildable (buildMatcher fails with a non-Scraper
// construction error), so Snapshot().BatchJobFactory() returns nil while
// prepareBatchRequest — which only reads the job store + apiCfg — still
// succeeds. That combination drives the 503 nil-guard branches added at every
// BatchJobFactory() call site (issue #44).
func badRegexConfig() *config.Config {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "(unclosed["
	cfg.API.Security.AllowedDirectories = []string{"/output", "/path"}
	cfg.Output.Operation.OperationMode = "organize"
	return cfg
}

func TestOrganizeJob_FactoryUnavailable_503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	deps := createTestDeps(t, badRegexConfig(), "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/organize", organizeJob(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizeRequest{Destination: "/output"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/organize", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", w.Body.String())
}

func TestUpdateBatchJob_FactoryUnavailable_503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	deps := createTestDeps(t, badRegexConfig(), "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/update", updateBatchJob(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/update", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", w.Body.String())
}

func TestBatchRescrapeMovies_FactoryUnavailable_503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	deps := createTestDeps(t, badRegexConfig(), "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
	setJobStatus(job, models.JobStatusCompleted)

	router := gin.New()
	router.POST("/batch/:id/movies/batch-rescrape", batchRescrapeMovies(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.BulkRescrapeRequest{MovieIDs: []string{"IPX-535"}, SelectedScrapers: []string{"mock"}})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/movies/batch-rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", w.Body.String())
}

func TestRescrapeBatchMovie_FactoryUnavailable_503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	deps := createTestDeps(t, badRegexConfig(), "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
	result := &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", Title: "Original Title"},
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/path/to/IPX-535.mp4", result)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/rescrape", rescrapeBatchMovie(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.BatchRescrapeRequest{SelectedScrapers: []string{"mock"}})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/IPX-535/rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, "body=%s", w.Body.String())
}

// TestStartScrapeUseCase_BatchWorkflowError covers the BatchWorkflow error
// branch in StartScrapeUseCase: with a bad-regex config the workflow factory
// cannot be built, so snap.BatchWorkflow(jobID) returns an error that
// StartScrapeUseCase propagates.
func TestStartScrapeUseCase_BatchWorkflowError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	deps := createTestDeps(t, badRegexConfig(), "")
	rt := testkit.GetTestRuntime(deps)

	_, err := StartScrapeUseCase(context.Background(), rt, StartScrapeInput{
		Files:            []string{"/path/to/file.mp4"},
		SelectedScrapers: []string{"mock"},
	})
	assert.Error(t, err)
}

// TestBatchRescrapeMovies_RunningJob_409 covers the rescrapeNotAllowed=true
// branch in batchRescrapeMovies: a running job must be rejected with 409 before
// the handler reaches the factory/snapshot section.
func TestBatchRescrapeMovies_RunningJob_409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{"/path"}
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
	setJobStatus(job, models.JobStatusRunning)

	router := gin.New()
	router.POST("/batch/:id/movies/batch-rescrape", batchRescrapeMovies(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.BulkRescrapeRequest{MovieIDs: []string{"IPX-535"}, SelectedScrapers: []string{"mock"}})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/movies/batch-rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code, "body=%s", w.Body.String())
}

// TestBatchRescrapeMovies_DeletedJob_410 covers the statusSnap.IsDeleted
// true-branch in batchRescrapeMovies: a logically-deleted job (even if its
// status is Pending/Completed) must return 410 Gone before reaching the
// factory/snapshot section.
func TestBatchRescrapeMovies_DeletedJob_410(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{"/path"}
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
	setJobStatus(job, models.JobStatusCompleted)
	setJobDeleted(job, true)

	router := gin.New()
	router.POST("/batch/:id/movies/batch-rescrape", batchRescrapeMovies(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.BulkRescrapeRequest{MovieIDs: []string{"IPX-535"}, SelectedScrapers: []string{"mock"}})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/movies/batch-rescrape", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusGone, w.Code, "body=%s", w.Body.String())
}
