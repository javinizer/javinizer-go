package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestListBatchJobs_PersistedWithOpCounts(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := createJobWithWF(deps, cfg, []string{"/path/to/IPX-535.mp4"})
	setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)
	deps.JobStore.PersistJob(job)

	router := gin.New()
	router.GET("/batch", listBatchJobs(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 1)
	assert.Equal(t, job.GetID(), resp.Jobs[0].ID)
	assert.Equal(t, int64(0), resp.Jobs[0].OperationCount)
}

func TestListBatchJobs_MultiplePersistedJobs(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job1 := createJobWithWF(deps, cfg, []string{"/path/to/A-001.mp4"})
	setJobStatus(job1, models.JobStatusCompleted)
	deps.JobStore.PersistJob(job1)

	job2 := createJobWithWF(deps, cfg, []string{"/path/to/B-002.mp4"})
	setJobStatus(job2, models.JobStatusCompleted)
	deps.JobStore.PersistJob(job2)

	router := gin.New()
	router.GET("/batch", listBatchJobs(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Jobs, 2)
}

func TestListBatchJobs_ExcludedDataPersisted(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-700.mp4"})
	excludeFile(job, "/path/to/IPX-700.mp4")
	deps.JobStore.PersistJob(job)

	router := gin.New()
	router.GET("/batch", listBatchJobs(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 1)
	// Excluded is deferred in listing — use GET /batch/{id} for excluded details
	assert.Equal(t, 1, resp.Total)
}

func TestListBatchJobs_ResultsParsedInPersistedJob(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-800.mp4"})
	setJobResult(job, "/path/to/IPX-800.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-800.mp4", MovieID: "IPX-800"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-800", Title: "Results Test"},
		StartedAt:     time.Now(),
	})
	setJobStatus(job, models.JobStatusCompleted)
	deps.JobStore.PersistJob(job)

	router := gin.New()
	router.GET("/batch", listBatchJobs(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("GET", "/batch", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp contracts.BatchJobListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Jobs, 1)
	// Results are deferred in listing — use GET /batch/{id}?include_data=true for full results
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, models.JobStatusCompleted, resp.Jobs[0].Status)
}

func TestCancelBatchJob_TerminalStatuses(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	tests := []struct {
		name   string
		status models.JobStatus
	}{
		{"already organized", models.JobStatusOrganized},
		{"already failed", models.JobStatusFailed},
		{"already cancelled", models.JobStatusCancelled},
		{"already reverted", models.JobStatusReverted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})
			setJobStatus(job, tt.status)

			router := gin.New()
			router.POST("/batch/:id/cancel", cancelBatchJob(testkit.GetTestRuntime(deps)))

			req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/cancel", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, 400, w.Code)
			assert.Contains(t, w.Body.String(), "already")
		})
	}
}

func TestCleanupJobTempPosters_RemovesDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := "test-job-cleanup"
	tempDir := "/tmp/testposters"
	posterDir := tempDir + "/posters/" + jobID
	require.NoError(t, fs.MkdirAll(posterDir, 0755))

	cleanupJobTempPosters(fs, jobID, tempDir)

	exists, _ := afero.DirExists(fs, posterDir)
	assert.False(t, exists)
}

func TestCleanupJobTempPosters_NonexistentDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	cleanupJobTempPosters(fs, "no-such-job", "/tmp/nonexistent")
}

func TestProcessBulkRescrapeMovie_Error(t *testing.T) {
	ctx := context.Background()
	job := &stubControlledJob{rescrapeErr: fmt.Errorf("scrape error")}
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)

	result := processBulkRescrapeMovie(ctx, "ABC-123", job, &contracts.BatchRescrapeRequest{}, factory)

	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "Rescrape failed")
}

func TestProcessBulkRescrapeMovie_GoneStatus(t *testing.T) {
	ctx := context.Background()
	job := &stubControlledJob{rescrapeResult: &worker.RescrapeResult{Status: models.RescrapeStatusGone}}
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)

	result := processBulkRescrapeMovie(ctx, "ABC-123", job, &contracts.BatchRescrapeRequest{}, factory)

	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "deleted during rescrape")
}

func TestProcessBulkRescrapeMovie_ConflictStatus(t *testing.T) {
	ctx := context.Background()
	job := &stubControlledJob{rescrapeResult: &worker.RescrapeResult{Status: models.RescrapeStatusConflict}}
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)

	result := processBulkRescrapeMovie(ctx, "ABC-123", job, &contracts.BatchRescrapeRequest{}, factory)

	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Contains(t, result.Error, "Concurrent rescrape conflict")
}

func TestProcessBulkRescrapeMovie_FailedStatus(t *testing.T) {
	ctx := context.Background()
	job := &stubControlledJob{rescrapeResult: &worker.RescrapeResult{Status: models.RescrapeStatusFailed, Error: "no scraper results"}}
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)

	result := processBulkRescrapeMovie(ctx, "ABC-123", job, &contracts.BatchRescrapeRequest{}, factory)

	assert.Equal(t, models.RescrapeStatusFailed, result.Status)
	assert.Equal(t, "no scraper results", result.Error)
}

func TestProcessBulkRescrapeMovie_SuccessStatus(t *testing.T) {
	ctx := context.Background()
	movie := &models.Movie{ID: "ABC-123", Title: "Test Movie"}
	job := &stubControlledJob{rescrapeResult: &worker.RescrapeResult{Status: models.RescrapeStatusSuccess, Movie: movie}}
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)

	result := processBulkRescrapeMovie(ctx, "ABC-123", job, &contracts.BatchRescrapeRequest{}, factory)

	assert.Equal(t, models.RescrapeStatusSuccess, result.Status)
	assert.Equal(t, "ABC-123", result.Movie.ID)
}

// stubControlledJob implements worker.BatchJobInterface for processBulkRescrapeMovie tests.
type stubControlledJob struct {
	rescrapeResult *worker.RescrapeResult
	rescrapeErr    error
	status         *worker.BatchJobStatus // optional; nil by default preserves prior behavior
}

func (s *stubControlledJob) GetID() string                                      { return "stub-job" }
func (s *stubControlledJob) GetJobStatus() models.JobStatus                     { return models.JobStatusCompleted }
func (s *stubControlledJob) GetStatus() *worker.BatchJobStatus                  { return s.status }
func (s *stubControlledJob) GetMovieResult(string) (*worker.MovieResult, error) { return nil, nil }
func (s *stubControlledJob) Subscribe() worker.JobEventSubscriber               { return nil }
func (s *stubControlledJob) FindFilePathsForMovieID(string) []string            { return nil }
func (s *stubControlledJob) FindMovieResultForMovieID(string) (*worker.MovieResult, error) {
	return nil, nil
}
func (s *stubControlledJob) GetMovieResultsForMovieID(string) []*worker.MovieResult    { return nil }
func (s *stubControlledJob) GetFileMatchInfosForMovieID(string) []models.FileMatchInfo { return nil }
func (s *stubControlledJob) GetFileResultByResultID(string) (*worker.MovieResult, string, bool) {
	return nil, "", false
}
func (s *stubControlledJob) StartScrape(context.Context, []string, worker.ScrapePhaseConfig) error {
	return nil
}
func (s *stubControlledJob) StartApply(context.Context, worker.ApplyPhaseConfig) error { return nil }
func (s *stubControlledJob) Wait() error                                               { return nil }
func (s *stubControlledJob) Rescrape(_ context.Context, _ worker.RescrapeCmd) (*worker.RescrapeResult, error) {
	return s.rescrapeResult, s.rescrapeErr
}
func (s *stubControlledJob) Cancel()                                                    {}
func (s *stubControlledJob) MarkReverted()                                              {}
func (s *stubControlledJob) Done() <-chan struct{}                                      { return nil }
func (s *stubControlledJob) SetWorkflow(workflow.WorkflowInterface)                     {} // Per DEEP-1: now on PhaseController
func (s *stubControlledJob) SetBatchCfg(worker.BatchJobConfig)                          {}
func (s *stubControlledJob) SetJobStatus(models.JobStatus)                              {}
func (s *stubControlledJob) SetOperationModeOverride(operationmode.OperationMode) error { return nil }
func (s *stubControlledJob) SetPersistError(string)                                     {}
func (s *stubControlledJob) GetResults() []worker.MovieResult                           { return nil }

// JobEditor methods (required by BatchJobInterface)
func (s *stubControlledJob) UpdateMovie(context.Context, string, *models.Movie) error { return nil }
func (s *stubControlledJob) ExcludeFile(string)                                       {}
func (s *stubControlledJob) UpdatePosterCrop(string, string) error                    { return nil }
func (s *stubControlledJob) UpdatePosterFromURL(context.Context, string, string, string) error {
	return nil
}
func (s *stubControlledJob) ApplyFieldOverride(context.Context, string, string, string) (*worker.MovieResult, *worker.ProvenanceData, error) {
	return nil, nil, nil
}
func (s *stubControlledJob) GetProvenance(string) *worker.ProvenanceData { return nil }
