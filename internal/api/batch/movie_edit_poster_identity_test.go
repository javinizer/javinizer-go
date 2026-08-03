package batch

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// TestUpdateBatchMoviePosterCrop_UsesEffectiveMovieIDForCacheAndFanOut pins
// Codex P1-4: the crop endpoint must derive the poster-op identity with the
// SAME canonical-ID precedence as posterLockKeyFor (Movie.ID when set,
// FileMatchInfo.MovieID otherwise) for BOTH the cache key and the fan-out.
// The fixture below is the divergent state FMI=OLDK / Movie.ID=NEWK (a
// result re-keyed by its stored movie before its FileMatchInfo converged):
// pre-fix the endpoint resolved OLDK's family via FileMatchInfo.MovieID —
// cropping OLDK's cached source and fanning bounds out over the OLD
// movie-ID family while the effective result lives at NEWK.
func TestUpdateBatchMoviePosterCrop_UsesEffectiveMovieIDForCacheAndFanOut(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const oldKey, newKey = "OLDK-001", "NEWK-001"
	// Path order matters: the OTHER family's result must sort BEFORE the
	// cropped one so FindMovieResultForMovieID(OLDK) resolves to it.
	fileOther := "/path/to/a-other.mp4"
	fileCrop := "/path/to/z-crop-target.mp4"
	job := createJobWithWF(deps, cfg, []string{fileOther, fileCrop})
	setJobResult(job, fileOther, &resultstore.MovieResult{
		ResultID:      "res-other",
		FileMatchInfo: models.FileMatchInfo{Path: fileOther, MovieID: oldKey},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldKey, Title: "Other", Poster: models.PosterState{PosterURL: "https://example.com/old.jpg"}},
	})
	// The cropped result: stored movie re-keyed to NEWK, FileMatchInfo not
	// yet converged (the state UpdatePosterCrop's FMI <- movie.ID line
	// exists to repair).
	setJobResult(job, fileCrop, &resultstore.MovieResult{
		ResultID:      "res-crop-diverged",
		FileMatchInfo: models.FileMatchInfo{Path: fileCrop, MovieID: oldKey},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: newKey, Title: "Effective", Poster: models.PosterState{PosterURL: "https://example.com/new.jpg"}},
	})

	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	oldFull := filepath.Join(posterDir, oldKey+"-full.jpg")
	newFull := filepath.Join(posterDir, newKey+"-full.jpg")
	writeJPEG(t, oldFull, 1000, 600)
	writeJPEG(t, newFull, 800, 1200)
	oldFullBytes, err := os.ReadFile(oldFull)
	require.NoError(t, err)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	code := postPosterCropBounds(router, job.GetID(), "res-crop-diverged")
	require.Equal(t, http.StatusOK, code)

	// The crop targeted the EFFECTIVE movie's cache (NEWK *-full.jpg is
	// 800x1200): the recorded source dimensions prove the measured image.
	cropped := storedMovieResultByPath(t, job, fileCrop)
	require.NotNil(t, cropped.Movie)
	require.NotNil(t, cropped.Movie.Poster.CropBounds, "the effective result must receive the crop")
	assert.Equal(t, 800, cropped.Movie.Poster.CropBounds.ImageWidth,
		"bounds must be measured against NEWK's cached source, not OLDK's")
	assert.Equal(t, 1200, cropped.Movie.Poster.CropBounds.ImageHeight)

	// The OLD family is untouched: no bounds, no cache mutation.
	other := storedMovieResultByPath(t, job, fileOther)
	require.NotNil(t, other.Movie)
	assert.Nil(t, other.Movie.Poster.CropBounds, "the old-key family must not receive the crop")
	gotOld, err := os.ReadFile(oldFull)
	require.NoError(t, err)
	assert.Equal(t, oldFullBytes, gotOld, "OLDK's cached source must not be cropped for NEWK's request")
	_, statErr := os.Stat(filepath.Join(posterDir, oldKey+".jpg"))
	assert.True(t, os.IsNotExist(statErr), "OLDK's preview must not be written by NEWK's crop")

	assertPosterSourceLockFreeAPI(t, job.GetID(), oldKey)
	assertPosterSourceLockFreeAPI(t, job.GetID(), newKey)
}

// TestUpdateBatchMoviePosterFromURL_UsesEffectiveMovieIDForFanOut mirrors the
// crop half of P1-4 for the poster-from-URL endpoint: with a divergent
// FMI/Movie.ID result the download cache write, the lock key, and the
// UpdatePosterFromURL fan-out must all key on the effective Movie.ID.
func TestUpdateBatchMoviePosterFromURL_UsesEffectiveMovieIDForFanOut(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)
	srv := newPatchPosterSourceServer(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const oldKey, newKey = "OLDU-001", "NEWU-001"
	fileOther := "/path/to/a-other.mp4"
	fileCrop := "/path/to/z-url-target.mp4"
	job := createJobWithWF(deps, cfg, []string{fileOther, fileCrop})
	setJobResult(job, fileOther, &resultstore.MovieResult{
		ResultID:      "res-other-u",
		FileMatchInfo: models.FileMatchInfo{Path: fileOther, MovieID: oldKey},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: oldKey, Title: "Other", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	setJobResult(job, fileCrop, &resultstore.MovieResult{
		ResultID:      "res-url-diverged",
		FileMatchInfo: models.FileMatchInfo{Path: fileCrop, MovieID: oldKey},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: newKey, Title: "Effective", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	body := []byte(fmt.Sprintf(`{"url":%q}`, srv.URL+"/new.jpg"))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/res-url-diverged/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	updated := storedMovieResultByPath(t, job, fileCrop)
	require.NotNil(t, updated.Movie)
	assert.Equal(t, srv.URL+"/new.jpg", updated.Movie.Poster.PosterURL,
		"the effective result must receive the new poster URL")

	other := storedMovieResultByPath(t, job, fileOther)
	require.NotNil(t, other.Movie)
	assert.Equal(t, srv.URL+"/old.jpg", other.Movie.Poster.PosterURL,
		"the old-key family must not receive the URL fan-out")

	assertPosterSourceLockFreeAPI(t, job.GetID(), oldKey)
	assertPosterSourceLockFreeAPI(t, job.GetID(), newKey)
}

// TestUpdateBatchMoviePosterCrop_RejectsSourceSwappedDuringLockWait pins
// Codex P1-5: the client measured the crop coordinates against the poster
// source visible when it issued the request; if a source-changing edit
// completes while the request waits on the poster-source lock, those
// coordinates describe the OLD image and must not be applied to the new
// one — the handler re-reads the effective poster source after acquiring
// the lock and rejects with 409 (stale-conflict, parity with the
// concurrent-rescrape rejection) WITHOUT mutating the cache.
func TestUpdateBatchMoviePosterCrop_RejectsSourceSwappedDuringLockWait(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieID = "STAL-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      "res-stale",
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Stale", Poster: models.PosterState{
			PosterURL: "https://example.com/old-source.jpg",
		}},
	})

	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	fullPath := filepath.Join(posterDir, movieID+"-full.jpg")
	writeJPEG(t, fullPath, 1000, 600)
	fullBytes, err := os.ReadFile(fullPath)
	require.NoError(t, err)

	jobID := job.GetID()
	jobIface, ok := deps.JobStore.GetBatchJob(jobID)
	require.True(t, ok)
	ready := make(chan struct{})
	wrapped := &signalingMovieLookup{BatchJobInterface: jobIface, firstResolve: ready}
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: wrapped}

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	// Hold the movie's poster-source lock so the crop request blocks after
	// capturing its pre-wait source snapshot (the signal fires at
	// resolvePosterID — the last read before acquisition).
	release := worker.AcquirePosterSourceLock(jobID, movieID)
	done := make(chan int, 1)
	go func() { done <- postPosterCropBounds(router, jobID, "res-stale") }()

	<-ready
	select {
	case code := <-done:
		release()
		t.Fatalf("crop completed (%d) before the lock was released", code)
	case <-time.After(150 * time.Millisecond):
	}

	// A source-changing edit commits while the request waits: the stored
	// movie's effective poster source moves to a different URL.
	require.NoError(t, jobIface.UpdateMovie(t.Context(), filePath, &models.Movie{
		ID: movieID, Title: "Stale",
		Poster: models.PosterState{PosterURL: "https://example.com/new-source.jpg"},
	}))
	release()

	select {
	case code := <-done:
		require.Equal(t, http.StatusConflict, code,
			"a crop measured against the pre-wait source must be rejected, not applied to the new image")
	case <-time.After(5 * time.Second):
		t.Fatal("crop did not complete after the lock was released")
	}

	// No cache or state mutation: the full-size source is byte-identical and
	// no crop state landed on the movie.
	gotFull, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, fullBytes, gotFull, "the rejected crop must not touch the cache")
	current := storedMovieResultByPath(t, job, filePath)
	require.NotNil(t, current.Movie)
	assert.Nil(t, current.Movie.Poster.CropBounds)

	assertPosterSourceLockFreeAPI(t, jobID, movieID)
}
