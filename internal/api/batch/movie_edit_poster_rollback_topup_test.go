package batch

// Patch-coverage top-up for the poster endpoints' hardening legs the PR's
// exercise suite did not reach:
//   - crop/download failure legs with a BROKEN RestoreAssets ("poster
//     rollback failed" must ride the surfaced message)
//   - multipart compensation with an UNREADABLE per-part snapshot (nil prior
//     annotation, never silently skipped)
//   - poster-from-URL envelope persist with ErrJobGone (410)
//   - effectivePosterSourceOf's nil-movie guard

import (
	"bytes"
	"encoding/json"
	"errors"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// jamRestoreWriteFs fails create-for-write OpenFile calls on one name suffix
// so RestoreAssets' afero.WriteFile leg errors while reads and the crop's
// staging write (different suffix) stay healthy.
type jamRestoreWriteFs struct {
	afero.Fs
	jamSuffix string
	err       error
}

func (f *jamRestoreWriteFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_CREATE != 0 && strings.HasSuffix(filepath.ToSlash(name), f.jamSuffix) {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// TestUpdateBatchMoviePosterCrop_CropFailureRollbackFailureSurfaces pins the
// ride-along on the NON-legacy crop failure leg: CropWithBounds fails (the
// staged encode write is jammed) AND restoring the pre-crop snapshot fails
// (the preview write is jammed) — the restore failure must surface in the
// 400 message instead of dropping the belt-and-braces guarantee silently.
func TestUpdateBatchMoviePosterCrop_CropFailureRollbackFailureSurfaces(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "CRRJ-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Crop Jammed Restore", Poster: models.PosterState{
			PosterURL:        "https://old.example/poster.jpg",
			ShouldCropPoster: true,
		}},
	})

	mem := afero.NewMemMapFs()
	tempPosterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, mem.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"),
		posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0xcc, A: 0xff}), 0o644))
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"), oldPreview, 0o644))
	deps.Fs = &truncateThenFailWriteFs{
		Fs:         &jamRestoreWriteFs{Fs: mem, jamSuffix: "/" + movieID + ".jpg", err: errors.New("injected restore write failure")},
		failSuffix: "/" + movieID + ".jpg.tmp",
		writeErr:   errors.New("injected encode write failure"),
		armed:      true,
	}

	body, _ := json.Marshal(contracts.PosterCropRequest{X: 10, Y: 20, Width: 300, Height: 400})
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-crop", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "crop failed")
	assert.Contains(t, rec.Body.String(), "poster rollback failed",
		"a failed pre-crop-snapshot restore must ride the rejection message")

	// The crop never installed (staging jammed) and the restore could not
	// rewrite the preview: the pre-crop bytes survive either way.
	gotPreview, readErr := afero.ReadFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"))
	require.NoError(t, readErr)
	assert.Equal(t, oldPreview, gotPreview)
}

// TestUpdateBatchMoviePosterCrop_UpdateFailureNoPrecropSnapshotSurfaces pins
// the multipart compensation corner where a sibling's snapshot LOOKUP failed
// (nil prior ≠ legitimately nil Movie): the failed UpdatePosterCrop must
// surface "no pre-crop snapshot" for that part instead of silently keeping
// its rejected crop state.
func TestUpdateBatchMoviePosterCrop_UpdateFailureNoPrecropSnapshotSurfaces(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "NCS-001"
	fp1 := "/path/to/" + movieID + "-cd1.mp4"
	fp2 := "/path/to/" + movieID + "-cd2.mp4"
	res1 := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fp1, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Multipart Crop", Poster: models.PosterState{
			ShouldCropPoster: true,
		}},
	}

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(res1, fp1, true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{fp1, fp2})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(res1, nil)
	mockJob.EXPECT().GetMovieResult(fp1).Return(res1, nil)
	// The sibling's pre-crop snapshot lookup FAILS — nil prior lands in
	// origResults and the compensation must say so.
	mockJob.EXPECT().GetMovieResult(fp2).Return(nil, assert.AnError)
	mockJob.EXPECT().UpdatePosterCrop(movieID, mock.Anything, mock.Anything).Return(assert.AnError)
	mockJob.EXPECT().RestoreMovieResult(mock.Anything, fp1, mock.Anything).Return(nil)

	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	posterDir := filepath.Join("data", "temp", "posters", "job-any")
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 900, 600)
	oldPreview := posterRefreshJPEG(t, 160, 240, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, os.WriteFile(filepath.Join(posterDir, movieID+".jpg"), oldPreview, 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/results/"+movieID+"/poster-crop",
		bytes.NewBufferString(`{"x":100,"y":0,"width":472,"height":600}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Failed to update job state")
	assert.Contains(t, rec.Body.String(), "no pre-crop snapshot for part "+fp2,
		"the unreadable sibling snapshot must surface on the rejection")
	assert.NotContains(t, rec.Body.String(), "poster rollback failed")

	preview, err := os.ReadFile(filepath.Join(posterDir, movieID+".jpg"))
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview, "the crop-overwritten preview is byte-restored")

	assertPosterSourceLockFreeAPI(t, "job-any", movieID)
	assertJobEnvelopeLockFree(t, "job-any")
}

// TestUpdateBatchMoviePosterFromURL_UpdateFailureNoPreeditSnapshotSurfaces
// pins the same corner for the poster-from-URL endpoint: the download
// succeeded, UpdatePosterFromURL failed, and one sibling's pre-edit snapshot
// lookup had failed — the 500 must name the un-revertible sibling.
func TestUpdateBatchMoviePosterFromURL_UpdateFailureNoPreeditSnapshotSurfaces(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "NPU-001"
	fp1 := "/path/to/" + movieID + "-cd1.mp4"
	fp2 := "/path/to/" + movieID + "-cd2.mp4"
	res1 := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fp1, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Multipart URL", Poster: models.PosterState{
			PosterURL:        srv.URL + "/old.jpg",
			ShouldCropPoster: false,
		}},
	}

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(res1, fp1, true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{fp1, fp2})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(res1, nil)
	mockJob.EXPECT().GetMovieResult(fp1).Return(res1, nil)
	mockJob.EXPECT().GetMovieResult(fp2).Return(nil, assert.AnError)
	mockJob.EXPECT().UpdatePosterFromURL(mock.Anything, movieID, srv.URL+"/new.jpg", mock.Anything).Return(assert.AnError)
	mockJob.EXPECT().RestoreMovieResult(mock.Anything, fp1, mock.Anything).Return(nil)

	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/new.jpg"})
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Failed to update job state")
	assert.Contains(t, rec.Body.String(), "no pre-edit snapshot for part "+fp2)
	assert.NotContains(t, rec.Body.String(), "poster rollback failed")
	assert.Equal(t, 1, srv.newHits, "the download ran before the state update failed")

	assertPosterSourceLockFreeAPI(t, "job-any", movieID)
	assertJobEnvelopeLockFree(t, "job-any")
}

// renameFailRestoreJamFs drives DownloadFromURL's post-mutation rename
// failure AND jams the RestoreAssets write-back, so both the download error
// and its rollback failure surface together.
type renameFailRestoreJamFs struct {
	afero.Fs
	renameFailSuffix string
	renameErr        error
	jamWriteSuffix   string
	jamWriteErr      error
}

func (f *renameFailRestoreJamFs) Rename(oldname, newname string) error {
	if strings.HasSuffix(filepath.ToSlash(newname), f.renameFailSuffix) {
		return f.renameErr
	}
	return f.Fs.Rename(oldname, newname)
}

func (f *renameFailRestoreJamFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_CREATE != 0 && strings.HasSuffix(filepath.ToSlash(name), f.jamWriteSuffix) {
		return nil, f.jamWriteErr
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// TestUpdateBatchMoviePosterFromURL_DownloadFailureRollbackFailureSurfaces
// pins the download-leg rollback surfacing: DownloadFromURL fails AFTER its
// mutation boundary (the finalize rename is jammed, with the old -full.jpg
// already removed) AND the snapshot restore fails — the 502 must carry both
// the download error and "poster rollback failed".
func TestUpdateBatchMoviePosterFromURL_DownloadFailureRollbackFailureSurfaces(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "DLRJ-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Restore Jammed", Poster: models.PosterState{
			PosterURL: "https://old.example/poster.jpg",
		}},
	})

	mem := afero.NewMemMapFs()
	tempPosterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, mem.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"),
		posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0xcc, A: 0xff}), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"),
		posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff}), 0o644))
	deps.Fs = &renameFailRestoreJamFs{
		Fs:               mem,
		renameFailSuffix: movieID + "-full.jpg",
		renameErr:        errors.New("injected rename failure"),
		jamWriteSuffix:   movieID + "-full.jpg",
		jamWriteErr:      errors.New("injected restore write failure"),
	}

	srv := newPatchPosterSourceServer(t)
	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/new.jpg"})
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "failed to finalize image download")
	assert.Contains(t, rec.Body.String(), "poster rollback failed",
		"a failed post-mutation download must surface its broken rollback")

	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.Equal(t, "https://old.example/poster.jpg", current.Movie.Poster.PosterURL,
		"job state must not move when the download already failed")
}

// TestUpdateBatchMoviePosterFromURL_JobGoneReturns410 pins A13 for the
// poster-from-URL endpoint: the job vanishes between the handler's lookup
// and its envelope persist — the in-memory edit and the downloaded cache are
// compensated and the client gets 410 (never a false 200).
func TestUpdateBatchMoviePosterFromURL_JobGoneReturns410(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "GURL-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Gone URL", Poster: models.PosterState{
			PosterURL:        "https://old.example/poster.jpg",
			ShouldCropPoster: false,
		}},
	})

	mem := afero.NewMemMapFs()
	tempPosterDir := filepath.Join(cfg.System.TempDir, "posters", job.GetID())
	require.NoError(t, mem.MkdirAll(tempPosterDir, 0o755))
	oldFull := posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0xcc, A: 0xff})
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"), oldFull, 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"), oldPreview, 0o644))
	deps.Fs = mem

	deps.JobStore = &jobGoneStore{JobStoreInterface: deps.JobStore}

	srv := newPatchPosterSourceServer(t)
	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/new.jpg"})
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusGone, rec.Code, rec.Body.String())

	// The compensation restored the pre-download cache …
	gotFull, readErr := afero.ReadFile(mem, filepath.Join(tempPosterDir, movieID+"-full.jpg"))
	require.NoError(t, readErr)
	assert.Equal(t, oldFull, gotFull)
	gotPreview, readErr := afero.ReadFile(mem, filepath.Join(tempPosterDir, movieID+".jpg"))
	require.NoError(t, readErr)
	assert.Equal(t, oldPreview, gotPreview)
	// … and reverted the in-memory URL.
	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.Equal(t, "https://old.example/poster.jpg", current.Movie.Poster.PosterURL)
}

// TestEffectivePosterSourceOf pins the effective-source helper: PosterURL
// wins over CoverURL, and a nil movie degenerates to the empty source (the
// pre/post-lock stale-source guard's no-source case).
func TestEffectivePosterSourceOf(t *testing.T) {
	assert.Equal(t, "", effectivePosterSourceOf(nil))
	assert.Equal(t, "https://x/poster.jpg", effectivePosterSourceOf(&models.Movie{
		Poster: models.PosterState{PosterURL: "https://x/poster.jpg", CoverURL: "https://x/cover.jpg"},
	}))
	assert.Equal(t, "https://x/cover.jpg", effectivePosterSourceOf(&models.Movie{
		Poster: models.PosterState{CoverURL: "https://x/cover.jpg"},
	}))
}
