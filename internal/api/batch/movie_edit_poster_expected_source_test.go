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

// TestUpdateBatchMoviePosterCrop_ExpectedPosterRevisionGuard pins the Codex
// P2 same-URL guard: a rescrape or poster-from-URL refresh can replace the
// cached {movieID}-full.jpg bytes while keeping the SAME effective source
// URL, leaving expected_source_url AND both pre/post-lock snapshots equal
// even though the displayed image's coordinate space changed. The client
// echoes the cache generation token (X-Poster-Revision: mtime-ns + size, see
// poster.AssetRevision) captured with the displayed image as
// expected_poster_revision, and the server validates it under the
// poster-source lock against the CURRENT cache file's revision.
func TestUpdateBatchMoviePosterCrop_ExpectedPosterRevisionGuard(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	setup := func(t *testing.T, movieID string) (*worker.BatchJob, *gin.Engine, string) {
		t.Helper()
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		filePath := "/path/to/" + movieID + ".mp4"
		job := createJobWithWF(deps, cfg, []string{filePath})
		setJobResult(job, filePath, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			// The effective source URL stays IDENTICAL across the test — only the
			// cached image bytes (the generation) change.
			Movie: &models.Movie{ID: movieID, Title: "Measured", Poster: models.PosterState{
				PosterURL: "https://x/same-source.jpg",
			}},
		})
		posterDir := filepath.Join("data", "temp", "posters", job.GetID())
		require.NoError(t, os.MkdirAll(posterDir, 0o755))
		writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 1000, 600)
		router := gin.New()
		router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
		return job, router, posterDir
	}

	post := func(t *testing.T, router *gin.Engine, job *worker.BatchJob, movieID, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-crop", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// revisionOf mirrors poster.AssetRevision over the on-disk cache file the
	// crop endpoint's poster manager stats (both read cfg.System.TempDir).
	revisionOf := func(t *testing.T, posterDir, movieID string) string {
		t.Helper()
		fi, err := os.Stat(filepath.Join(posterDir, movieID+"-full.jpg"))
		require.NoError(t, err)
		return fmt.Sprintf("%d-%d", fi.ModTime().UnixNano(), fi.Size())
	}

	t.Run("same-URL content refresh with a stale revision rejects 409 without mutating state or cache", func(t *testing.T) {
		const movieID = "EMR-001"
		job, router, posterDir := setup(t, movieID)
		fullPath := filepath.Join(posterDir, movieID+"-full.jpg")
		staleRev := revisionOf(t, posterDir, movieID)

		// Same-URL refresh: the cached -full.jpg is regenerated from the UNCHANGED
		// source URL — the URL guard sees no drift, only the generation moved.
		writeJPEG(t, fullPath, 1200, 800)
		require.NotEqual(t, staleRev, revisionOf(t, posterDir, movieID))

		rec := post(t, router, job, movieID,
			`{"x":10,"y":10,"width":200,"height":200,"expected_source_url":"https://x/same-source.jpg","expected_poster_revision":"`+staleRev+`"}`)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "poster image changed")

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		assert.Nil(t, stored.Movie.Poster.CropBounds)
		assert.NoFileExists(t, filepath.Join(posterDir, movieID+".jpg"), "the 409 leg returns before CropWithBounds, so no preview is written")
		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	})

	t.Run("matching revision succeeds", func(t *testing.T) {
		const movieID = "EMR-002"
		job, router, posterDir := setup(t, movieID)
		rev := revisionOf(t, posterDir, movieID)

		rec := post(t, router, job, movieID,
			`{"x":10,"y":10,"width":200,"height":200,"expected_source_url":"https://x/same-source.jpg","expected_poster_revision":"`+rev+`"}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		stored := storedMovieResult(t, job, movieID)
		require.NotNil(t, stored.Movie)
		require.NotNil(t, stored.Movie.Poster.CropBounds)
		assert.Equal(t, 200, stored.Movie.Poster.CropBounds.Width)
		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	})

	t.Run("presented revision with a vanished cache file rejects 409", func(t *testing.T) {
		const movieID = "EMR-003"
		job, router, posterDir := setup(t, movieID)
		rev := revisionOf(t, posterDir, movieID)
		require.NoError(t, os.Remove(filepath.Join(posterDir, movieID+"-full.jpg")))

		// The generation the client measured no longer exists — even though the
		// URL guard passes, the crop cannot measure the old coordinate space.
		// (A legacy client WITHOUT expected_poster_revision would hit the
		// legacy-source 400 here instead; the revision guard fires first.)
		rec := post(t, router, job, movieID,
			`{"x":10,"y":10,"width":200,"height":200,"expected_poster_revision":"`+rev+`"}`)
		require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "poster image changed")
		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	})
}
