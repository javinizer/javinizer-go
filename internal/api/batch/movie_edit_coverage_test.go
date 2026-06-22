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
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

func TestUpdateBatchMovie_InvalidJSONBody(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("PATCH", "/batch/"+job.GetID()+"/results/ABC-123", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestUpdateBatchMovie_SuccessWithDBUpsert(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
	setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", Title: "Original"},
		StartedAt:     time.Now(),
	})

	body, _ := json.Marshal(contracts.UpdateMovieRequest{Movie: &contracts.MovieView{ID: "IPX-535", Title: "Updated"}})
	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("PATCH", "/batch/"+job.GetID()+"/results/IPX-535", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

func TestUpdateBatchMoviePosterCrop_InvalidJSONBody(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/ABC-123/poster-crop", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestUpdateBatchMoviePosterCrop_TraversalMovieID(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	req := httptest.NewRequest("POST", "/batch/job1/results/..%2F..%2Fetc/poster-crop", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestUpdateBatchMoviePosterCrop_JobNotFoundEdit(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	req := httptest.NewRequest("POST", "/batch/nonexistent/results/ABC-123/poster-crop", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestUpdateBatchMoviePosterCrop_MovieNotInJob(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/NONEXISTENT/poster-crop", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestUpdateBatchMoviePosterFromURL_BadJSON(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("POST", "/batch/job1/results/ABC-123/poster-from-url", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestUpdateBatchMoviePosterFromURL_TraversalMovieID(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "https://example.com/poster.jpg"})
	req := httptest.NewRequest("POST", "/batch/job1/movies/../etc/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestUpdateBatchMoviePosterFromURL_EditJobNotFound(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "https://example.com/poster.jpg"})
	req := httptest.NewRequest("POST", "/batch/nonexistent/results/ABC-123/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestUpdateBatchMoviePosterFromURL_MovieNotInJobResults(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/file.mp4"})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "https://example.com/poster.jpg"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/NONEXISTENT/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestUpdateBatchMoviePosterFromURL_InvalidURLFormat(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
	setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
		StartedAt:     time.Now(),
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "not-a-valid-url"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/IPX-535/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestUpdateBatchMoviePosterFromURL_LocalhostBlockedBySSRF(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
	setJobResult(job, "/path/to/IPX-535.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
		StartedAt:     time.Now(),
	})

	// httptest.NewServer binds to 127.0.0.1 — SSRF blocks localhost by default
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: ts.URL + "/poster.jpg"})
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/IPX-535/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req)

	// SSRF blocks localhost URLs → 400
	assert.Equal(t, 400, w2.Code)
}

func TestUpdateBatchMoviePosterFromURL_EmptyMovieIDParam(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "https://example.com/poster.jpg"})
	req := httptest.NewRequest("POST", "/batch/job1/results//poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.True(t, w.Code == 400 || w.Code == 404)
}

func TestUpdateBatchMoviePosterFromURL_DotMovieIDParam(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "https://example.com/poster.jpg"})
	req := httptest.NewRequest("POST", "/batch/job1/results/./poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestUpdateBatchMovie_MultiPartFileUpdate(t *testing.T) {
	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-535-CD1.mp4", "/path/to/IPX-535-CD2.mp4"})
	movie := &models.Movie{ID: "IPX-535", Title: "Original"}
	setJobResult(job, "/path/to/IPX-535-CD1.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535-CD1.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		StartedAt:     time.Now(),
	})
	setJobResult(job, "/path/to/IPX-535-CD2.mp4", &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535-CD2.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		StartedAt:     time.Now(),
	})

	updatedMovie := &contracts.MovieView{ID: "IPX-535", Title: "Updated"}
	body, _ := json.Marshal(contracts.UpdateMovieRequest{Movie: updatedMovie})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest("PATCH", "/batch/"+job.GetID()+"/results/IPX-535", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}
