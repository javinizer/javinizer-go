package batch

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"

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

func TestUpdateBatchMoviePosterFromURL_SuccessClearsBoundsAndPersists(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	img := image.NewRGBA(image.Rect(0, 0, 800, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 90, B: 90, A: 255})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := createJobWithWF(deps, cfg, []string{"/path/to/FRU-001.mp4"})
	setJobResult(job, "/path/to/FRU-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/FRU-001.mp4", MovieID: "FRU-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "FRU-001", Title: "From URL", Poster: models.PosterState{
			PosterURL:  "https://example.com/old-poster.jpg",
			CropBounds: &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
		}},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/poster.jpg"})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/FRU-001/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for _, r := range job.GetStatus().Results {
		require.NotNil(t, r.Movie)
		assert.Equal(t, srv.URL+"/poster.jpg", r.Movie.Poster.PosterURL)
		assert.Nil(t, r.Movie.Poster.CropBounds, "replacing the poster image must invalidate stored crop bounds")
	}
}

func TestUpdateBatchMoviePosterFromURL_DownloadGenericErrorReturns500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	// Block the poster temp dir: a file (not directory) sits at the configured
	// TempDir path, so MkdirAll fails with a generic (non-SSRF/non-download) error.
	require.NoError(t, os.WriteFile("data", []byte("blocker"), 0o644))

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join("data", "temp")
	deps := createTestDeps(t, cfg, "")
	job := createJobWithWF(deps, cfg, []string{"/path/to/G500-001.mp4"})
	setJobResult(job, "/path/to/G500-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/G500-001.mp4", MovieID: "G500-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "G500-001", Title: "G500"},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "https://example.com/poster.jpg"})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/G500-001/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

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

	t.Run("rejects negative max_poster_height", func(t *testing.T) {
		rec := patchMovie(&contracts.CropBounds{X: 0, Y: 0, Width: 100, Height: 100, MaxPosterHeight: -1})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
