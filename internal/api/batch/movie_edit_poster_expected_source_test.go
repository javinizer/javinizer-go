package batch

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateBatchMoviePosterCrop_ClientMeasuredSourceGuard pins the Codex P2
// cross-tab/device guard: the client sends the effective poster source URL
// its crop coordinates were measured against (expected_source_url), and the
// server validates THAT value against the effective source under the
// poster-source lock. Without it, a source swap committed by another tab or
// a source-edit that lands BEFORE this POST arrives defeats the pre/post-lock
// guard (both the pre-wait snapshot and the post-lock re-read already name
// image B) and the request would apply image A's coordinates to image B.
func TestUpdateBatchMoviePosterCrop_ClientMeasuredSourceGuard(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	setup := func(t *testing.T, movieID string) (*worker.BatchJob, *gin.Engine) {
		t.Helper()
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		filePath := "/path/to/" + movieID + ".mp4"
		job := createJobWithWF(deps, cfg, []string{filePath})
		setJobResult(job, filePath, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: movieID, Title: "Measured", Poster: models.PosterState{
				// The source NOW effective server-side (another tab's edit
				// already committed it before this crop request arrived).
				PosterURL: "https://x/image-B.jpg",
			}},
		})
		posterDir := filepath.Join("data", "temp", "posters", job.GetID())
		require.NoError(t, os.MkdirAll(posterDir, 0o755))
		writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 1000, 600)
		router := gin.New()
		router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
		return job, router
	}

	post := func(t *testing.T, router *gin.Engine, job *worker.BatchJob, movieID, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-crop", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("mismatched client-measured source rejects 409 without mutating state or cache", func(t *testing.T) {
		const movieID = "EMS-001"
		job, router := setup(t, movieID)
		previewPath := filepath.Join("data", "temp", "posters", job.GetID(), movieID+".jpg")
		expectedPreview, readErr := os.ReadFile(filepath.Join("data", "temp", "posters", job.GetID(), movieID+"-full.jpg"))
		require.NoError(t, readErr)

		rec := post(t, router, job, movieID, `{"x":10,"y":10,"width":200,"height":200,"expected_source_url":"https://x/image-A.jpg"}`)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "poster source changed")

		// A stale-coordinate crop must not install bounds or overwrite the
		// preview the still-effective image B preview would come from.
		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.Nil(t, stored.Movie.Poster.CropBounds)
		assert.NoFileExists(t, previewPath, "the 409 leg returns before CropWithBounds, so no preview is written")

		currentFull, readErr := os.ReadFile(filepath.Join("data", "temp", "posters", job.GetID(), movieID+"-full.jpg"))
		require.NoError(t, readErr)
		assert.Equal(t, expectedPreview, currentFull)
		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	})

	t.Run("matching client-measured source succeeds", func(t *testing.T) {
		const movieID = "EMS-002"
		job, router := setup(t, movieID)

		rec := post(t, router, job, movieID, `{"x":10,"y":10,"width":200,"height":200,"expected_source_url":"https://x/image-B.jpg"}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		require.NotNil(t, stored.Movie.Poster.CropBounds)
		assert.Equal(t, 200, stored.Movie.Poster.CropBounds.Width)
		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	})

	t.Run("omitted expected source keeps the old pre/post-lock guard behavior (legacy client)", func(t *testing.T) {
		const movieID = "EMS-003"
		job, router := setup(t, movieID)

		rec := post(t, router, job, movieID, `{"x":10,"y":10,"width":200,"height":200}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.NotNil(t, stored.Movie.Poster.CropBounds)
		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	})
}
