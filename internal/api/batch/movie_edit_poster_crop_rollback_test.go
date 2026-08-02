package batch

import (
	"errors"
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// TestUpdateBatchMoviePosterCrop_EnvelopePersistFailureRestoresPreviewCache
// pins the Codex P2 finding "restore the preview when crop persistence
// fails": CropWithBounds has already overwritten the shared preview
// ({posterID}.jpg) when PersistJobByID fails. The endpoint must restore the
// pre-crop cache bytes alongside reverting the in-memory bounds/intent —
// otherwise the persisted movie keeps its old bounds+intent while the
// preview file shows the REJECTED new crop (an uncached reload displays it,
// while Organize would apply the old/default crop).
func TestUpdateBatchMoviePosterCrop_EnvelopePersistFailureRestoresPreviewCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const movieID = "EPER-007"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		// Pre-crop state worth byte-restoring: an uncropped preview URL and
		// the recorded cover-crop intent.
		Movie: &models.Movie{ID: movieID, Title: "Crop Cache Rollback", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/pre-crop.jpg",
			ShouldCropPoster: true,
		}},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
	previewPath := filepath.Join(tempPosterDir, movieID+".jpg")
	oldFull := posterRefreshJPEG(t, 1000, 600, color.RGBA{R: 0x11, B: 0x22, A: 0xff})
	oldPreview := posterRefreshJPEG(t, 160, 240, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, os.WriteFile(fullPath, oldFull, 0o644))
	require.NoError(t, os.WriteFile(previewPath, oldPreview, 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	code, rec := postPosterCrop(router, job.GetID(), movieID)
	require.Equal(t, http.StatusInternalServerError, code, "body: %s", rec.Body.String())
	assertPersistFailed500(t, rec, job)

	// The in-memory crop reverted (bounds/intent) — as pinned by F7 — and
	// NOW the cache bytes came back too: the preview shows the pre-crop
	// image again, not the rejected crop.
	stored := storedMovieResult(t, job, movieID)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "/api/v1/temp/posters/pre-crop.jpg", stored.Movie.Poster.CroppedPosterURL)
	assert.True(t, stored.Movie.Poster.ShouldCropPoster)
	assert.Nil(t, stored.Movie.Poster.CropBounds)

	full, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, oldFull, full, "-full.jpg must be byte-identical to the pre-crop source")
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview,
		"the preview must be byte-identical to the pre-crop bytes — the rejected crop must not stay on disk")

	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterCrop_EnvelopePersistFailureRollbackFailureSurfaced
// covers the same branch with a BROKEN cache restore: the rollback failure
// must surface in the 500 message alongside the persist error (parity with
// the PATCH and poster-from-URL rollback-failure branches).
func TestUpdateBatchMoviePosterCrop_EnvelopePersistFailureRollbackFailureSurfaced(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	// The corruption hook needs the poster dir, which is only known after
	// the job exists — bind it lazily (same pattern as the PATCH variant).
	var corrupt func()
	deps.JobStore = newFailingPersistJobStoreWithHook(t, cfg, func() {
		if corrupt != nil {
			corrupt()
		}
	})

	const movieID = "EPER-008"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Crop Rollback Fail"},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	writeJPEG(t, filepath.Join(tempPosterDir, movieID+"-full.jpg"), 1000, 600)
	corrupt = corruptPosterDir(tempPosterDir)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	code, rec := postPosterCrop(router, job.GetID(), movieID)
	require.Equal(t, http.StatusInternalServerError, code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")
	assert.Contains(t, rec.Body.String(), "poster rollback failed",
		"a failed cache restore must surface alongside the persist error")
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterCrop_SnapshotFailureRejectsBeforeCrop pins the
// snapshot covenant on the crop endpoint: when the pre-crop asset snapshot
// cannot be captured, the request is rejected BEFORE CropWithBounds
// overwrites the preview — never crop against a cache state the endpoint
// cannot roll back (the same covenant manager.SnapshotAssets documents and
// the poster-from-URL endpoint already enforces).
func TestUpdateBatchMoviePosterCrop_SnapshotFailureRejectsBeforeCrop(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "EPER-009"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Snapshot Reject", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/untouched.jpg",
			ShouldCropPoster: true,
		}},
	})

	// Make the snapshot fail deterministically: the -full.jpg path exists as
	// a DIRECTORY, so SnapshotAssets' read errors out (not an absent file).
	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(filepath.Join(tempPosterDir, movieID+"-full.jpg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempPosterDir, movieID+".jpg"), []byte("old-preview-bytes"), 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	code, rec := postPosterCrop(router, job.GetID(), movieID)
	require.Equal(t, http.StatusInternalServerError, code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Failed to snapshot poster assets")

	// Rejected BEFORE the crop ran: the preview is untouched and so is the
	// in-memory movie.
	preview, err := os.ReadFile(filepath.Join(tempPosterDir, movieID+".jpg"))
	require.NoError(t, err)
	assert.Equal(t, []byte("old-preview-bytes"), preview, "the crop never ran — the preview stands")
	stored := storedMovieResult(t, job, movieID)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "/api/v1/temp/posters/untouched.jpg", stored.Movie.Poster.CroppedPosterURL)
	assert.True(t, stored.Movie.Poster.ShouldCropPoster)
	assert.Nil(t, stored.Movie.Poster.CropBounds)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterCrop_PersistFailureRestoresAbsentPreview pins the
// snapshot's absence half: a movie with NO pre-crop preview file whose crop
// persist fails must have the freshly written preview REMOVED by the restore
// (RestoreAssets reproduces absence), not linger as a crop no persisted
// state produced.
func TestUpdateBatchMoviePosterCrop_PersistFailureRestoresAbsentPreview(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const movieID = "EPER-010"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Absent Preview"},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	writeJPEG(t, filepath.Join(tempPosterDir, movieID+"-full.jpg"), 1000, 600)
	previewPath := filepath.Join(tempPosterDir, movieID+".jpg")
	_, statErr := os.Stat(previewPath)
	require.True(t, errors.Is(statErr, os.ErrNotExist), "fixture: no pre-crop preview")

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	code, rec := postPosterCrop(router, job.GetID(), movieID)
	require.Equal(t, http.StatusInternalServerError, code, "body: %s", rec.Body.String())
	assertPersistFailed500(t, rec, job)

	_, statErr = os.Stat(previewPath)
	assert.True(t, errors.Is(statErr, os.ErrNotExist),
		"the restore must REMOVE the preview the rejected crop created — absence at snapshot time reproduces as absence")
	full, err := os.ReadFile(filepath.Join(tempPosterDir, movieID+"-full.jpg"))
	require.NoError(t, err)
	assert.NotEmpty(t, full, "the full source pre-existed and must stand")
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}
