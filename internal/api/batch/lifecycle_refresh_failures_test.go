package batch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestPR260BatchProjectionReadFailureLeavesSnapshotUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	movie := &models.Movie{ContentID: "PR260-not-persisted", Title: "snapshot", Credits: []models.MovieCredit{{CreditedName: "snapshot credit"}}}
	result := &resultstore.MovieResult{Movie: movie, FileMatchInfo: models.FileMatchInfo{MovieID: movie.ContentID}}
	job := &worker.BatchJobStatus{Results: map[string]*resultstore.MovieResult{"input.mp4": result}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/batch/projection?include_data=true", nil).WithContext(ctx)
	require.ErrorIs(t, refreshBatchJobMovies(c, deps, job), context.Canceled)
	require.Same(t, result, job.Results["input.mp4"])
	require.Same(t, movie, result.Movie)
	require.Equal(t, "snapshot credit", result.Movie.Credits[0].CreditedName)
}

func TestPR260BatchProjectionMissingRecordKeepsSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	movie := &models.Movie{ContentID: "PR260-missing-batch", ID: "PR260-missing-batch", Title: "original", Credits: []models.MovieCredit{{CreditedName: "unresolved"}}}
	result := &resultstore.MovieResult{Movie: movie, FileMatchInfo: models.FileMatchInfo{MovieID: movie.ID}}
	job := &worker.BatchJobStatus{Results: map[string]*resultstore.MovieResult{"missing.mp4": result, "nil.mp4": nil, "blank.mp4": {Movie: &models.Movie{Title: "blank"}}}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/batch/missing?include_data=true", nil)
	require.NoError(t, refreshBatchJobMovies(c, deps, job))
	require.Same(t, result, job.Results["missing.mp4"])
	require.Same(t, movie, result.Movie)
	require.Equal(t, "unresolved", result.Movie.Credits[0].CreditedName)
	require.Nil(t, job.Results["nil.mp4"])
	require.Equal(t, "blank", job.Results["blank.mp4"].Movie.Title)
}

func TestPR260BatchFullGetProjectionFailureDoesNotReturnSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{}, "")
	job := deps.JobStore.CreateJobBatch([]string{"failure.mp4"})
	movie := &models.Movie{ContentID: "PR260-canceled-get", ID: "PR260-canceled-get", Title: "snapshot"}
	setJobResult(job, "failure.mp4", &resultstore.MovieResult{Movie: movie, FileMatchInfo: models.FileMatchInfo{MovieID: movie.ID}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/batch/"+job.GetID()+"?include_data=true", nil).WithContext(ctx)
	getBatchJobFull(deps, c, job.GetID())
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.Contains(t, recorder.Body.String(), "failed to refresh batch movie projections")
	require.NotContains(t, recorder.Body.String(), "snapshot")
	require.True(t, strings.Contains(recorder.Body.String(), "canceled"))
}
