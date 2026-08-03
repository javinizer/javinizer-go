package batch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	dbmocks "github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// storedMovieResultByPath reads back a part's result from the real BatchJob
// status snapshot, keying on the file path (multipart parts share a movie ID,
// which storedMovieResult would not distinguish).
func storedMovieResultByPath(t *testing.T, job *worker.BatchJob, filePath string) *resultstore.MovieResult {
	t.Helper()
	for _, r := range job.GetStatus().Results {
		if r != nil && r.FileMatchInfo.Path == filePath {
			return r
		}
	}
	t.Fatalf("no movie result for %s", filePath)
	return nil
}

// newFailingPersistJobStoreWithHook is newFailingPersistJobStore plus a hook
// fired inside the always-failing envelope upsert — used to damage the poster
// cache between the refresh and the rollback so restore failures are
// exercised deterministically.
func newFailingPersistJobStoreWithHook(t *testing.T, cfg *config.Config, hook func()) *worker.JobStore {
	t.Helper()
	repo := dbmocks.NewMockJobRepositoryInterface(t)
	repo.On("List", mock.Anything).Return([]models.Job{}, nil)
	repo.On("Upsert", mock.Anything, mock.Anything).
		Run(func(mock.Arguments) {
			if hook != nil {
				hook()
			}
		}).
		Return(errors.New("job repository unavailable"))
	return worker.NewJobStore(repo, nil, nil, cfg.System.TempDir, nil, nil)
}

// corruptPosterDir returns a hook that makes every subsequent restore into
// tempPosterDir fail: the directory is replaced by a regular file so
// MkdirAll/write error out.
func corruptPosterDir(tempPosterDir string) func() {
	return func() {
		_ = os.RemoveAll(tempPosterDir)
		_ = os.WriteFile(tempPosterDir, []byte("blocker"), 0o644)
	}
}

// TestUpdateBatchMovie_EnvelopePersistFailureRevertsStateAndRestoresCache pins
// F-A for the whole-movie PATCH: the refresh already replaced the cached
// poster when the envelope persist fails, so the handler must (a) revert the
// in-memory parts to their pre-request movies, (b) restore the pre-refresh
// cache, and (c) NOT ack success. Multipart: BOTH parts must revert.
func TestUpdateBatchMovie_EnvelopePersistFailureRevertsStateAndRestoresCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const movieID = "EPER-001"
	file1 := "/path/to/" + movieID + "-cd1.mp4"
	file2 := "/path/to/" + movieID + "-cd2.mp4"
	oldBounds := &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4}
	job := createJobWithWF(deps, cfg, []string{file1, file2})
	setJobResult(job, file1, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: file1, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Old Title", OriginalFileName: movieID + "-cd1", Poster: models.PosterState{
			PosterURL: srv.URL + "/old.jpg", CropBounds: oldBounds,
		}},
	})
	setJobResult(job, file2, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: file2, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Old Title", OriginalFileName: movieID + "-cd2", Poster: models.PosterState{
			PosterURL: srv.URL + "/old.jpg", CropBounds: oldBounds,
		}},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
	previewPath := filepath.Join(tempPosterDir, movieID+".jpg")
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, os.WriteFile(fullPath, srv.oldJPEG, 0o644))
	require.NoError(t, os.WriteFile(previewPath, oldPreview, 0o644))

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":%q,"title":"New Title","poster_url":%q}}`, movieID, srv.URL+"/new.jpg")
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")
	assert.Equal(t, 1, srv.newHits, "sanity: the refresh really downloaded the new image before the persist failed")

	// In-memory state is reverted to the pre-request movies — BOTH parts.
	for _, filePath := range []string{file1, file2} {
		res := storedMovieResultByPath(t, job, filePath)
		require.NotNil(t, res.Movie)
		assert.Equal(t, srv.URL+"/old.jpg", res.Movie.Poster.PosterURL,
			"%s must not keep the refreshed source a restart would lose", filePath)
		assert.Equal(t, "Old Title", res.Movie.Title, "%s must revert fully, not only poster fields", filePath)
		require.NotNil(t, res.Movie.Poster.CropBounds, "%s must regain the pre-request crop bounds", filePath)
		assert.Equal(t, *oldBounds, *res.Movie.Poster.CropBounds)
	}

	// The refreshed cache rolls back to the pre-refresh bytes.
	full, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, srv.oldJPEG, full, "-full.jpg must be restored so envelope and cache agree after restart")
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview)

	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMovie_EnvelopePersistFailureRollbackFailureSurfaced pins the
// same PATCH branch with a BROKEN cache restore: the rollback failure must
// surface in the error message instead of being swallowed.
func TestUpdateBatchMovie_EnvelopePersistFailureRollbackFailureSurfaced(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	// The corruption hook needs the poster dir, which is only known after the
	// job exists — bind it lazily.
	var corrupt func()
	deps.JobStore = newFailingPersistJobStoreWithHook(t, cfg, func() {
		if corrupt != nil {
			corrupt()
		}
	})

	const movieID = "EPER-002"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempPosterDir, movieID+"-full.jpg"), srv.oldJPEG, 0o644))
	corrupt = corruptPosterDir(tempPosterDir)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":%q,"title":"New","poster_url":%q}}`, movieID, srv.URL+"/new.jpg")
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")
	assert.Contains(t, rec.Body.String(), "poster rollback failed",
		"a failed cache restore must surface alongside the persist error")
}

// TestUpdateBatchMoviePosterFromURL_EnvelopePersistFailureRevertsStateAndRestoresCache
// pins F-A for the poster-from-URL endpoint: DownloadFromURL has replaced the
// cached -full.jpg/preview when the envelope persist fails — the in-memory
// poster state must revert to the pre-request movie and the cache must be
// restored, so a restart (envelope-only) and the cache describe the same
// image.
func TestUpdateBatchMoviePosterFromURL_EnvelopePersistFailureRevertsStateAndRestoresCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const movieID = "EPER-003"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "From URL Old", Poster: models.PosterState{
			PosterURL:        srv.URL + "/old.jpg",
			CroppedPosterURL: "old-preview",
			ShouldCropPoster: false, // poster-grade prior source
		}},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
	previewPath := filepath.Join(tempPosterDir, movieID+".jpg")
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, os.WriteFile(fullPath, srv.oldJPEG, 0o644))
	require.NoError(t, os.WriteFile(previewPath, oldPreview, 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/new.jpg"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")

	// In-memory state reverted: the download's URL/preview did not stick.
	res := storedMovieResult(t, job, movieID)
	require.NotNil(t, res.Movie)
	assert.Equal(t, srv.URL+"/old.jpg", res.Movie.Poster.PosterURL)
	assert.Equal(t, "old-preview", res.Movie.Poster.CroppedPosterURL)
	assert.False(t, res.Movie.Poster.ShouldCropPoster)

	// Cache restored to the pre-download bytes.
	full, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, srv.oldJPEG, full)
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview)

	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterFromURL_EnvelopePersistFailureRollbackFailureSurfaced
// covers the same endpoint with a broken cache restore: the failure must
// surface in the 500 body.
func TestUpdateBatchMoviePosterFromURL_EnvelopePersistFailureRollbackFailureSurfaced(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	var corrupt func()
	deps.JobStore = newFailingPersistJobStoreWithHook(t, cfg, func() {
		if corrupt != nil {
			corrupt()
		}
	})

	const movieID = "EPER-004"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempPosterDir, movieID+"-full.jpg"), srv.oldJPEG, 0o644))
	corrupt = corruptPosterDir(tempPosterDir)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/new.jpg"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")
	assert.Contains(t, rec.Body.String(), "poster rollback failed")
}

// TestOverrideBatchMovieField_EnvelopePersistFailureRevertsStateAndRestoresCache
// pins F-A for the field-override endpoint: a poster_url override refreshed
// the cached poster (in-job poster generator wired), then the envelope
// persist failed — the ApplyFieldOverride compensation must revert the
// in-memory movie and restore the pre-override cache bytes.
func TestOverrideBatchMovieField_EnvelopePersistFailureRevertsStateAndRestoresCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	// A real poster generator on the JOB deps so ApplyFieldOverride's
	// refresh machinery runs (jobs created without one skip the refresh).
	gen := poster.NewScrapePosterGenerator(
		poster.NewPosterManager(afero.NewOsFs(), filepath.Join("data", "temp"), srv.Client()).
			WithSSRFCheck(func(string) error { return nil }),
		"", "")

	const movieID = "EPER-005"
	filePath := "/path/to/" + movieID + ".mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath}, &worker.JobConfig{
		BatchJobDeps: worker.BatchJobDeps{
			BatchCfg:  worker.BatchJobConfig{MaxWorkers: cfg.Performance.MaxWorkers},
			PosterGen: gen,
		},
	})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      movieID,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Aggregated", Poster: models.PosterState{
			PosterURL:        srv.URL + "/old.jpg",
			CroppedPosterURL: "old-preview",
			ShouldCropPoster: false,
		}},
	})
	job.ResultsWriter().SetProvenance(filePath, &resultstore.ProvenanceData{
		FieldSources: map[string]string{"poster_url": "r18dev"},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", PosterURL: srv.URL + "/old.jpg", ShouldCropPoster: false},
			{Source: "dmm", PosterURL: srv.URL + "/new.jpg", ShouldCropPoster: false},
		},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
	previewPath := filepath.Join(tempPosterDir, movieID+".jpg")
	oldPreview := posterRefreshJPEG(t, 80, 120, color.RGBA{G: 0x7f, A: 0xff})
	require.NoError(t, os.WriteFile(fullPath, srv.oldJPEG, 0o644))
	require.NoError(t, os.WriteFile(previewPath, oldPreview, 0o644))

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.FieldOverrideRequest{Field: "poster_url", Source: "dmm"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")
	assert.Equal(t, 1, srv.newHits, "sanity: the override refreshed the cache before the persist failed")

	res := storedMovieResult(t, job, movieID)
	require.NotNil(t, res.Movie)
	assert.Equal(t, srv.URL+"/old.jpg", res.Movie.Poster.PosterURL,
		"the override must revert so in-memory state does not reference an unpersisted source")
	assert.Equal(t, "old-preview", res.Movie.Poster.CroppedPosterURL)

	full, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, srv.oldJPEG, full, "the refreshed cache must roll back with the reverted state")
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview)

	// The pre-override provenance attribution is restored as well.
	prov := job.ResultsWriter().GetProvenance(filePath)
	require.NotNil(t, prov)
	assert.Equal(t, "r18dev", prov.FieldSources["poster_url"])

	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestOverrideBatchMovieField_EnvelopePersistFailureCompensateFailureSurfaced
// covers the same endpoint with a broken cache restore: the compensation
// failure surfaces as "override revert failed" (with the poster rollback
// detail joined in).
func TestOverrideBatchMovieField_EnvelopePersistFailureCompensateFailureSurfaced(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	var corrupt func()
	deps.JobStore = newFailingPersistJobStoreWithHook(t, cfg, func() {
		if corrupt != nil {
			corrupt()
		}
	})

	gen := poster.NewScrapePosterGenerator(
		poster.NewPosterManager(afero.NewOsFs(), filepath.Join("data", "temp"), srv.Client()).
			WithSSRFCheck(func(string) error { return nil }),
		"", "")

	const movieID = "EPER-006"
	filePath := "/path/to/" + movieID + ".mp4"
	job := deps.JobStore.CreateJobBatch([]string{filePath}, &worker.JobConfig{
		BatchJobDeps: worker.BatchJobDeps{
			BatchCfg:  worker.BatchJobConfig{MaxWorkers: cfg.Performance.MaxWorkers},
			PosterGen: gen,
		},
	})
	setJobResult(job, filePath, &resultstore.MovieResult{
		ResultID:      movieID,
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Aggregated", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	job.ResultsWriter().SetProvenance(filePath, &resultstore.ProvenanceData{
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", PosterURL: srv.URL + "/old.jpg", ShouldCropPoster: false},
			{Source: "dmm", PosterURL: srv.URL + "/new.jpg", ShouldCropPoster: false},
		},
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempPosterDir, movieID+"-full.jpg"), srv.oldJPEG, 0o644))
	corrupt = corruptPosterDir(tempPosterDir)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/field-override", overrideBatchMovieField(testkit.GetTestRuntime(deps)))
	body, err := json.Marshal(contracts.FieldOverrideRequest{Field: "poster_url", Source: "dmm"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/field-override", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "override revert failed")
	assert.Contains(t, rec.Body.String(), "poster rollback failed")
}

// TestUpdateBatchMovie_NilMovieResultStillRefreshesCache pins F-G: a
// whole-movie PATCH against a result with NO stored movie used to skip the
// poster-source refresh entirely (every poster block sat inside
// `if current.Movie != nil`), persisting the new source with no cached
// -full.jpg — the reviewer would then measure a crop against nothing/the old
// image while Organize downloads the patched one. The refresh now runs with
// an empty prior source, generating the cache from scratch.
func TestUpdateBatchMovie_NilMovieResultStillRefreshesCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	srv := newPatchPosterSourceServer(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "NM-001"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         nil, // scrape produced no movie payload for this result
	})

	tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
	fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":%q,"title":"First Edit","poster_url":%q}}`, movieID, srv.URL+"/new.jpg")
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/"+movieID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, srv.newHits, "the new source must be downloaded into the cache even for a nil-movie result")
	content, err := os.ReadFile(fullPath)
	require.NoError(t, err, "the cache must be populated for the PATCHed-in source")
	assert.Equal(t, srv.newJPEG, content)

	current := storedMovieResult(t, job, movieID)
	require.NotNil(t, current.Movie)
	assert.Equal(t, srv.URL+"/new.jpg", current.Movie.Poster.PosterURL)
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMovie_EnvelopePersistFailureRestoresNilMovieSibling pins
// Codex P1-3 for the whole-movie PATCH compensation: a multipart sibling
// whose stored result legitimately has a nil Movie carries a PRESENT
// snapshot (distinct from a failed lookup), so a failed envelope persist
// must re-seat that part to its pre-request nil-Movie state through
// RestoreMovieResult — pre-fix the compensation keyed on
// original(*models.Movie) == nil, conflating "lookup failed" with "stored
// movie nil", skipped the revert, and left the rejected edit on the
// sibling.
func TestUpdateBatchMovie_EnvelopePersistFailureRestoresNilMovieSibling(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	const movieID = "NMP-001"
	file1 := "/path/to/" + movieID + "-cd1.mp4"
	file2 := "/path/to/" + movieID + "-cd2.mp4"
	job := createJobWithWF(deps, cfg, []string{file1, file2})
	setJobResult(job, file1, &resultstore.MovieResult{
		ResultID:      "res-nmp-1",
		FileMatchInfo: models.FileMatchInfo{Path: file1, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Old Title", OriginalFileName: movieID + "-cd1"},
	})
	// Sibling part: a real stored result, family-indexed via
	// FileMatchInfo.MovieID, but with a legitimately nil Movie.
	setJobResult(job, file2, &resultstore.MovieResult{
		ResultID:      "res-nmp-2",
		FileMatchInfo: models.FileMatchInfo{Path: file2, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	body := fmt.Sprintf(`{"movie":{"id":%q,"title":"New Title"}}`, movieID)
	req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/res-nmp-1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")

	// The nil-Movie sibling must be restored to NO MOVIE — not left holding
	// the rejected edit's fanned-out movie.
	res2 := storedMovieResultByPath(t, job, file2)
	assert.Nil(t, res2.Movie, "the nil-Movie sibling must be re-seated to its pre-request result")
	assert.Equal(t, movieID, res2.FileMatchInfo.MovieID, "family membership survives the nil-movie restore")

	// The movie-bearing part reverts to its pre-request movie.
	res1 := storedMovieResultByPath(t, job, file1)
	require.NotNil(t, res1.Movie)
	assert.Equal(t, "Old Title", res1.Movie.Title)

	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}
