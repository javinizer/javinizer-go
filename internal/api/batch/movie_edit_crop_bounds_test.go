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
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type fixedJobStore struct {
	worker.JobStoreInterface
	job worker.BatchJobInterface
}

func (s *fixedJobStore) GetBatchJob(string) (worker.BatchJobInterface, bool) { return s.job, true }

func TestUpdateBatchMoviePosterCrop_UpdateFailureReturns500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "FAIL-001"
	result := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/FAIL-001.mp4", MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Fail"},
	}

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, "/path/to/FAIL-001.mp4", true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{"/path/to/FAIL-001.mp4"})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(result, nil)
	mockJob.EXPECT().UpdatePosterCrop(movieID, mock.Anything, mock.Anything).Return(assert.AnError)

	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	// Give CropWithBounds a real poster so the crop succeeds and the state
	// update is the failing step under test.
	posterDir := filepath.Join("data", "temp", "posters", "job-any")
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 900, 600)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/results/"+movieID+"/poster-crop", bytes.NewBufferString(`{"x":100,"y":0,"width":472,"height":600}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

func TestUpdateBatchMovie_CropBoundsValidation(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := createJobWithWF(deps, cfg, []string{"/path/to/VAL-001.mp4"})
	setJobResult(job, "/path/to/VAL-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/VAL-001.mp4", MovieID: "VAL-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "VAL-001", Title: "Validation"},
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	patchMovie := func(bounds *contracts.CropBounds) *httptest.ResponseRecorder {
		view := contracts.MovieViewFromModel(&models.Movie{ID: "VAL-001", Title: "Validation"})
		view.PosterCropBounds = bounds
		body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/VAL-001", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("rejects invalid bounds", func(t *testing.T) {
		for _, b := range []*contracts.CropBounds{
			{X: -1, Y: 0, Width: 100, Height: 100},
			{X: 0, Y: -1, Width: 100, Height: 100},
			{X: 0, Y: 0, Width: 0, Height: 100},
			{X: 0, Y: 0, Width: 100, Height: -50},
		} {
			rec := patchMovie(b)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "bounds %+v should be rejected", *b)
		}
	})

	t.Run("accepts valid bounds", func(t *testing.T) {
		rec := patchMovie(&contracts.CropBounds{X: 0, Y: 0, Width: 400, Height: 600})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("accepts absent bounds", func(t *testing.T) {
		rec := patchMovie(nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}
