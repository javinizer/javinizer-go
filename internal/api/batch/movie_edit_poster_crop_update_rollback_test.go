package batch

import (
	"bytes"
	"context"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestUpdateBatchMoviePosterCrop_UpdateFailureRestoresMultipartPartsAndCache is
// the multipart leg of the Codex P2 finding "restore assets when the crop
// state update fails": UpdatePosterCrop fans out per part and CAN fail after
// mutating earlier parts, while CropWithBounds already overwrote the shared
// preview. The handler must revert EVERY part to its exact pre-crop snapshot
// (through RestoreMovieResult — the whole stored result, not just the movie)
// and byte-restore the pre-crop preview — a partial revert would leave a
// sibling multipart entry carrying the rejected crop into a later persist.
func TestUpdateBatchMoviePosterCrop_UpdateFailureRestoresMultipartPartsAndCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "FAIL-002"
	fp1 := "/path/to/" + movieID + "-cd1.mp4"
	fp2 := "/path/to/" + movieID + "-cd2.mp4"
	res1 := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fp1, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Multipart Crop", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/pre-crop.jpg",
			ShouldCropPoster: true,
		}},
	}
	res2 := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fp2, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Multipart Crop", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/pre-crop.jpg",
			ShouldCropPoster: true,
		}},
	}

	reverted := map[string]*resultstore.MovieResult{}
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(res1, fp1, true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{fp1, fp2})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(res1, nil)
	mockJob.EXPECT().GetMovieResult(fp1).Return(res1, nil)
	mockJob.EXPECT().GetMovieResult(fp2).Return(res2, nil)
	mockJob.EXPECT().UpdatePosterCrop(movieID, mock.Anything, mock.Anything).Return(assert.AnError)
	mockJob.EXPECT().RestoreMovieResult(mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ context.Context, fp string, prior *resultstore.MovieResult) {
			reverted[fp] = prior
		}).Return(nil)

	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	posterDir := filepath.Join("data", "temp", "posters", "job-any")
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 900, 600)
	previewPath := filepath.Join(posterDir, movieID+".jpg")
	oldPreview := posterRefreshJPEG(t, 160, 240, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, os.WriteFile(previewPath, oldPreview, 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/results/"+movieID+"/poster-crop", bytes.NewBufferString(`{"x":100,"y":0,"width":472,"height":600}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Failed to update job state")
	assert.NotContains(t, rec.Body.String(), "revert of part")
	assert.NotContains(t, rec.Body.String(), "poster rollback failed")

	// BOTH parts reverted with their own exact pre-crop snapshots.
	require.Len(t, reverted, 2, "every part of the movie must be reverted, not only the selected one")
	assert.Same(t, res1, reverted[fp1], "the revert must carry the WHOLE pre-crop result snapshot, not just its movie")
	assert.Same(t, res2, reverted[fp2])

	// The shared preview the crop overwrote is byte-restored.
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview)

	assertPosterSourceLockFreeAPI(t, "job-any", movieID)
	assertJobEnvelopeLockFree(t, "job-any")
}

// TestUpdateBatchMoviePosterCrop_UpdateFailureRevertFailureSurfaced covers the
// degenerate corner: the crop state update fails AND reverting a part fails —
// the revert failure must ride the 500 message alongside the update error
// (parity with the persist leg), never be swallowed. The cache restore still
// runs after the failed revert.
func TestUpdateBatchMoviePosterCrop_UpdateFailureRevertFailureSurfaced(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "FAIL-003"
	filePath := "/path/to/" + movieID + ".mp4"
	result := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Revert Fail"},
	}

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, filePath, true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{filePath})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(result, nil)
	mockJob.EXPECT().GetMovieResult(filePath).Return(result, nil)
	mockJob.EXPECT().UpdatePosterCrop(movieID, mock.Anything, mock.Anything).Return(assert.AnError)
	mockJob.EXPECT().RestoreMovieResult(mock.Anything, filePath, result).Return(assert.AnError)

	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	posterDir := filepath.Join("data", "temp", "posters", "job-any")
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 900, 600)
	previewPath := filepath.Join(posterDir, movieID+".jpg")
	oldPreview := posterRefreshJPEG(t, 160, 240, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, os.WriteFile(previewPath, oldPreview, 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/results/"+movieID+"/poster-crop", bytes.NewBufferString(`{"x":100,"y":0,"width":472,"height":600}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Failed to update job state")
	assert.Contains(t, rec.Body.String(), "revert of part",
		"the failed in-memory revert must surface alongside the update error")
	assert.NotContains(t, rec.Body.String(), "poster rollback failed",
		"the cache restore succeeded — only the revert failed")

	// The cache restore ran despite the failed revert (parts first, cache second).
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview)

	assertPosterSourceLockFreeAPI(t, "job-any", movieID)
	assertJobEnvelopeLockFree(t, "job-any")
}
