package batch

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// TestUpdateBatchMoviePosterCrop_PersistFailureRestoresRekeyedResultIdentity
// pins Codex round-9 P1-C (crop leg): the compensation must revert the
// COMPLETE stored result, not just the movie. The fixture is the divergent
// re-keyed state FMI=OLDK / Movie.ID=NEWK; pre-fix the rollback replayed
// UpdateMovie(origMovie), which re-stamps FileMatchInfo.MovieID from
// Movie.ID (resultUpdater.UpdateMovie) — a failed crop on this result left
// its family/index identity MOVED to NEWK despite claiming an exact
// rollback. Post-fix the compensation restores the pre-crop snapshot through
// RestoreMovieResult (full result, FileMatchInfo included).
func TestUpdateBatchMoviePosterCrop_PersistFailureRestoresRekeyedResultIdentity(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const oldKey, newKey = "CRF-A", "CRF-B"
	fileTarget := "/path/to/crf-crop-target.mp4"
	job := createJobWithWF(deps, cfg, []string{fileTarget})
	setJobResult(job, fileTarget, &resultstore.MovieResult{
		ResultID:      "res-crf-crop",
		FileMatchInfo: models.FileMatchInfo{Path: fileTarget, MovieID: oldKey}, // pre-convergence
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: newKey, Title: "Rekeyed", Poster: models.PosterState{
			PosterURL:        "https://example.com/src.jpg",
			ShouldCropPoster: true,
		}},
	})

	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	writeJPEG(t, filepath.Join(posterDir, newKey+"-full.jpg"), 800, 1200)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	code := postPosterCropBounds(router, job.GetID(), "res-crf-crop")
	// The crop itself succeeds; the always-failing envelope persist must
	// reject the request AND roll back the committed in-memory crop.
	require.Equal(t, http.StatusInternalServerError, code)

	restored := storedMovieResultByPath(t, job, fileTarget)
	require.NotNil(t, restored.Movie)
	assert.Equal(t, newKey, restored.Movie.ID)
	assert.Equal(t, oldKey, restored.FileMatchInfo.MovieID,
		"exact rollback must restore FileMatchInfo.MovieID — pre-fix UpdateMovie re-stamped it from Movie.ID")
	assert.Nil(t, restored.Movie.Poster.CropBounds, "the rejected crop bounds must be reverted")
	assert.True(t, restored.Movie.Poster.ShouldCropPoster,
		"the pre-crop crop intent must be restored from the snapshot")

	assertPosterSourceLockFreeAPI(t, job.GetID(), newKey)
}

// TestUpdateBatchMoviePosterFromURL_PersistFailureRestoresRekeyedResultIdentity
// pins the from-URL half of P1-C: same divergent fixture, same claim — a
// failed envelope persist must restore the result's full pre-edit identity,
// including FileMatchInfo.MovieID and the pre-edit poster URL.
func TestUpdateBatchMoviePosterFromURL_PersistFailureRestoresRekeyedResultIdentity(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const oldKey, newKey = "URLF-A", "URLF-B"
	fileTarget := "/path/to/urlf-target.mp4"
	job := createJobWithWF(deps, cfg, []string{fileTarget})
	setJobResult(job, fileTarget, &resultstore.MovieResult{
		ResultID:      "res-urlf",
		FileMatchInfo: models.FileMatchInfo{Path: fileTarget, MovieID: oldKey},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: newKey, Title: "Rekeyed", Poster: models.PosterState{
			PosterURL:        srv.URL + "/old.jpg",
			ShouldCropPoster: true,
			CropBounds:       &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
		}},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	body := []byte(fmt.Sprintf(`{"url":%q}`, srv.URL+"/new.jpg"))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/res-urlf/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())

	restored := storedMovieResultByPath(t, job, fileTarget)
	require.NotNil(t, restored.Movie)
	assert.Equal(t, newKey, restored.Movie.ID)
	assert.Equal(t, oldKey, restored.FileMatchInfo.MovieID,
		"exact rollback must restore FileMatchInfo.MovieID — pre-fix UpdateMovie re-stamped it from Movie.ID")
	assert.Equal(t, srv.URL+"/old.jpg", restored.Movie.Poster.PosterURL,
		"the pre-edit poster URL must be restored")
	assert.Equal(t, &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4}, restored.Movie.Poster.CropBounds,
		"the pre-edit recorded crop must be restored")
	assert.True(t, restored.Movie.Poster.ShouldCropPoster)

	assertPosterSourceLockFreeAPI(t, job.GetID(), newKey)
}
