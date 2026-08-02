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
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupOverrideJob(t *testing.T) (*core.APIDeps, *worker.BatchJob, string) {
	t.Helper()
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	filePath := "/path/to/IPX-535.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	resultID := "IPX-535"
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      resultID,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", ContentID: "IPX-535", Title: "Aggregated", Maker: "AggregatedMaker"},
		StartedAt:     time.Now(),
	})
	job.ResultsWriter().SetProvenance(filePath, &resultstore.ProvenanceData{
		FieldSources: map[string]string{"maker": "r18dev"},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", Maker: "R18Maker", Title: "R18Title"},
			{Source: "dmm", Maker: "DMMMaker", Title: "DMMTitle"},
		},
	})
	return deps, job, resultID
}

func TestGetBatchMovieSources_Success(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)

	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/"+job.GetID()+"/results/"+resultID+"/sources", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp contracts.SourceResultsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Results, 2)
	assert.Equal(t, "r18dev", resp.Results[0].Source)
	assert.Equal(t, "dmm", resp.Results[1].Source)
}

func TestGetBatchMovieSources_JobNotFound(t *testing.T) {
	deps, _, _ := setupOverrideJob(t)
	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/nonexistent/results/ABC/sources", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestGetBatchMovieSources_ResultNotFound(t *testing.T) {
	deps, job, _ := setupOverrideJob(t)
	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/"+job.GetID()+"/results/NONEXISTENT/sources", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestGetBatchMovieSources_EmptyProvenance(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	filePath := "/path/to/ABC-123.mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      "ABC-123",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "ABC-123"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-123"},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.GET("/batch/:id/results/:resultId/sources", getBatchMovieSources(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch/"+job.GetID()+"/results/ABC-123/sources", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp contracts.SourceResultsResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// No in-memory ScraperResults, but the fallback synthesizes a single-source
	// result from the aggregated movie so the viewer is never empty.
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "scraper", resp.Results[0].Source)
	assert.Equal(t, "ABC-123", resp.Results[0].ID)
}

func TestOverrideBatchMovieField_Success(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code, "body: %s", w.Body.String())
	var resp contracts.FieldOverrideResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp.Movie)
	assert.Equal(t, "DMMMaker", resp.Movie.Maker)
	assert.Equal(t, "dmm", resp.FieldSources["maker"])
}

func TestOverrideBatchMovieField_BadJSON(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

func TestOverrideBatchMovieField_JobNotFound(t *testing.T) {
	deps, _, _ := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest("POST", "/batch/nonexistent/results/ABC/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestOverrideBatchMovieField_ResultNotFound(t *testing.T) {
	deps, job, _ := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/NONEXISTENT/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestOverrideBatchMovieField_UnknownSource(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "nonexistent"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

func TestOverrideBatchMovieField_UnsupportedField(t *testing.T) {
	deps, job, resultID := setupOverrideJob(t)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "bogus_field", Source: "dmm"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, 400, w.Code)
}

// TestOverrideBatchMovieField_CompensateRunsUnderPosterLock pins P1-2
// (F3 evolution): the envelope persist + persist-failure compensation pair
// runs INSIDE ApplyFieldOverride's poster-source lock — there is no
// release→re-acquire gap left for a competing edit to land in.
//
// Deterministic interleave: the persist hook blocks INSIDE the critical
// section, then a competing edit (probe) tries to take the same lock. The
// probe can only acquire after persist+compensate complete, so it observes
// the fully-compensated (pre-override) state; without the lock it would
// acquire immediately and observe the uncompensated override.
func TestOverrideBatchMovieField_CompensateRunsUnderPosterLock(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	persistStarted := make(chan struct{})
	persistRelease := make(chan struct{})
	// The store's synchronous createJob persist would otherwise trip the
	// blocking hook during setup: only the armed hook (the handler attempt)
	// blocks.
	armed := make(chan struct{})
	deps.JobStore = newFailingPersistJobStoreWithHook(t, cfg, func() {
		select {
		case <-armed:
		default:
			return
		}
		select {
		case <-persistStarted:
		default:
			close(persistStarted)
		}
		<-persistRelease
	})

	filePath := "/path/to/IPX-535.mp4"
	const resultID = "IPX-535"
	job := deps.JobStore.CreateJobBatch([]string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      resultID,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: resultID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: resultID, ContentID: resultID, Title: "Aggregated", Maker: "AggregatedMaker"},
		StartedAt:     time.Now(),
	})
	job.ResultsWriter().SetProvenance(filePath, &resultstore.ProvenanceData{
		FieldSources: map[string]string{"maker": "r18dev"},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", Maker: "R18Maker", Title: "R18Title"},
			{Source: "dmm", Maker: "DMMMaker", Title: "DMMTitle"},
		},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))
	body, _ := json.Marshal(contracts.FieldOverrideRequest{Field: "maker", Source: "dmm"})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+resultID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	close(armed)
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		router.ServeHTTP(rec, req)
	}()

	<-persistStarted // the handler is inside the persist → holding the lock (F3)

	// Probe: a competing edit over the same (jobID, movieID) poster-source
	// lock. Whatever it observes must be the state AFTER the handler's whole
	// persist+compensate critical section.
	probeObserved := make(chan string, 1)
	go func() {
		release := worker.AcquirePosterSourceLock(job.GetID(), resultID)
		defer release()
		if r, _, found := job.Results().GetFileResultByResultID(resultID); found && r.Movie != nil {
			probeObserved <- r.Movie.Maker
		} else {
			probeObserved <- "<missing>"
		}
	}()

	close(persistRelease)
	<-handlerDone

	require.Equal(t, http.StatusInternalServerError, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "failed to persist job state")

	assert.Equal(t, "AggregatedMaker", <-probeObserved,
		"the probe runs only after the compensation reverted the override — the lock covers persist+compensate")

	// Final state is fully compensated: the movie and the provenance
	// attribution are back to the pre-override values.
	stored := storedMovieResult(t, job, resultID)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "AggregatedMaker", stored.Movie.Maker)
	prov := job.Results().GetProvenance(filePath)
	require.NotNil(t, prov)
	assert.Equal(t, "r18dev", prov.FieldSources["maker"], "the pre-override provenance attribution is restored")
	assertPosterSourceLockFreeAPI(t, job.GetID(), resultID)
}
