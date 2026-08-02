package batch

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"image"

	"github.com/gin-gonic/gin"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dbmocks "github.com/javinizer/javinizer-go/internal/mocks"
)

// newFailingPersistJobStore returns a real JobStore whose job repository
// upsert ALWAYS fails — the genuine production failure path (JobStore →
// dbJobPersistence → persistToDatabase → JobRepo.Upsert), not a stubbed
// interface. Every edit endpoint's PersistJobByID call then returns an error,
// and the job's PersistError is recorded as before.
func newFailingPersistJobStore(t *testing.T, cfg *config.Config) *worker.JobStore {
	t.Helper()
	repo := dbmocks.NewMockJobRepositoryInterface(t)
	repo.On("List", mock.Anything).Return([]models.Job{}, nil)
	repo.On("Upsert", mock.Anything, mock.Anything).Return(errors.New("job repository unavailable"))
	return worker.NewJobStore(repo, nil, nil, cfg.System.TempDir, nil, nil)
}

// assertPersistFailed500 checks the shared outcome of a job-envelope persist
// failure: the handler must NOT acknowledge the edit with 200.
func assertPersistFailed500(t *testing.T, rec *httptest.ResponseRecorder, job *worker.BatchJob) {
	t.Helper()
	require.Equal(t, http.StatusInternalServerError, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")
	assert.Contains(t, job.GetPersistError(), "upsert failed",
		"the failed upsert is still recorded as the job's PersistError")
}

// TestUpdateBatchMoviePosterCrop_PersistFailureReturns500 is the Codex P2
// finding "report failed crop-envelope persistence": CropBounds live ONLY in
// the job envelope (not the movies table), so a 200 on a failed envelope
// persist means a restart silently loses the crop the client thinks
// succeeded. The endpoint must surface the failure as a 5xx.
func TestUpdateBatchMoviePosterCrop_PersistFailureReturns500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const movieID = "CPER-001"
	filePath := "/path/to/CPER-001.mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Crop Persist"},
	})
	seedCropFullPoster(t, job.GetID(), movieID)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	code, rec := postPosterCrop(router, job.GetID(), movieID)
	require.Equal(t, http.StatusInternalServerError, code, "body: %s", rec.Body.String())
	assertPersistFailed500(t, rec, job)

	// The in-memory edit happened (the crop is applied before persisting) —
	// the 500 tells the client it is NOT durable; nothing claims otherwise.
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterFromURL_PersistFailureReturns500 covers the same
// swallowed-persist class in the poster-from-URL endpoint: the downloaded
// source/bounds state lives in the job envelope, so a failed persist must
// surface instead of acking.
func TestUpdateBatchMoviePosterFromURL_PersistFailureReturns500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	cleanup := ssrf.SetLookupIPForTest(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)
	chdirWorkDir(t)

	img := image.NewRGBA(image.Rect(0, 0, 800, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 90, B: 90, A: 255})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const movieID = "PFER-001"
	filePath := "/path/to/PFER-001.mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "From URL", Poster: models.PosterState{
			PosterURL: "https://example.com/old-poster.jpg",
		}},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/poster.jpg"})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertPersistFailed500(t, rec, job)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMovie_PersistFailureReturns500 covers the whole-movie PATCH:
// UpdateMovie persisted fine in-memory, but the job-envelope persist failed —
// the handler must not acknowledge the edit as durable.
func TestUpdateBatchMovie_PersistFailureReturns500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const movieID = "MPER-001"
	filePath := "/path/to/MPER-001.mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		// No poster source: the PATCH leaves the effective source unchanged, so
		// no poster refresh or download is involved in the failure branch.
		Movie: &models.Movie{ID: movieID, Title: "Movie Edit"},
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	view := contracts.MovieViewFromModel(&models.Movie{ID: movieID, Title: "Movie Edit (edited)"})
	body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/"+movieID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertPersistFailed500(t, rec, job)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestOverrideBatchMovieField_PersistFailureReturns500 covers the field
// override endpoint: an applied override that only exists in memory must not
// be acknowledged when the envelope persist fails.
func TestOverrideBatchMovieField_PersistFailureReturns500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

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
	router.ServeHTTP(rec, req)

	assertPersistFailed500(t, rec, job)
}

// seedCropFullPoster writes a decodable -full.jpg into the current-working-dir
// temp poster cache, mirroring the crop lock tests' on-disk fixture.
func seedCropFullPoster(t *testing.T, jobID, movieID string) {
	t.Helper()
	posterDir := filepath.Join("data", "temp", "posters", jobID)
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 1000, 600)
}

// postPosterCrop issues a POST to the crop endpoint and returns status + recorder.
func postPosterCrop(router *gin.Engine, jobID, resultID string) (int, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, "/batch/"+jobID+"/results/"+resultID+"/poster-crop",
		bytes.NewBufferString(`{"x":10,"y":10,"width":200,"height":200}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec
}
