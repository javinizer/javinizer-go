package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/ssrf"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestUpdateBatchMoviePosterFromURL_SuccessClearsBoundsAndPersists(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	img := image.NewRGBA(image.Rect(0, 0, 800, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 90, B: 90, A: 255})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := createJobWithWF(deps, cfg, []string{"/path/to/FRU-001.mp4"})
	setJobResult(job, "/path/to/FRU-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/FRU-001.mp4", MovieID: "FRU-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "FRU-001", Title: "From URL", Poster: models.PosterState{
			PosterURL:  "https://example.com/old-poster.jpg",
			CropBounds: &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
		}},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/poster.jpg"})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/FRU-001/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for _, r := range job.GetStatus().Results {
		require.NotNil(t, r.Movie)
		assert.Equal(t, srv.URL+"/poster.jpg", r.Movie.Poster.PosterURL)
		assert.Nil(t, r.Movie.Poster.CropBounds, "replacing the poster image must invalidate stored crop bounds")
	}
}

func TestUpdateBatchMoviePosterFromURL_DownloadGenericErrorReturns500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	// Block the poster temp dir: a file (not directory) sits at the configured
	// TempDir path, so MkdirAll fails with a generic (non-SSRF/non-download) error.
	require.NoError(t, os.WriteFile("data", []byte("blocker"), 0o644))

	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join("data", "temp")
	deps := createTestDeps(t, cfg, "")
	job := createJobWithWF(deps, cfg, []string{"/path/to/G500-001.mp4"})
	setJobResult(job, "/path/to/G500-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/G500-001.mp4", MovieID: "G500-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "G500-001", Title: "G500"},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: "https://example.com/poster.jpg"})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/G500-001/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

func TestUpdateBatchMoviePosterFromURL_UpdateFailureReturns500(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cleanup := ssrf.SetLookupIPForTest(func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)

	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	img := image.NewRGBA(image.Rect(0, 0, 800, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 90, B: 90, A: 255})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "FURL-500"
	result := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/FURL-500.mp4", MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Fail"},
	}

	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, "/path/to/FURL-500.mp4", true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{"/path/to/FURL-500.mp4"})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(result, nil)
	mockJob.EXPECT().UpdatePosterFromURL(mock.Anything, movieID, mock.Anything, mock.Anything).Return(assert.AnError)
	// F-A compensation on the state-update failure: pre-request movies are
	// captured per part and the revert runs before the cache restore.
	mockJob.EXPECT().GetMovieResult("/path/to/FURL-500.mp4").Return(result, nil)
	mockJob.EXPECT().UpdateMovie(mock.Anything, "/path/to/FURL-500.mp4", mock.Anything).Return(nil)

	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/poster.jpg"})
	req := httptest.NewRequest(http.MethodPost, "/batch/job-any/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
}

type fixedJobStore struct {
	worker.JobStoreInterface
	job worker.BatchJobInterface
}

func (s *fixedJobStore) GetBatchJob(string) (worker.BatchJobInterface, bool) { return s.job, true }

// TestUpdateBatchMoviePosterCrop_UpdateFailureRestoresStateAndCache pins the
// Codex P2 finding "restore assets when the crop state update fails":
// CropWithBounds has already overwritten the shared preview ({posterID}.jpg)
// when UpdatePosterCrop fails, so the handler must run the same movie+asset
// compensation the envelope-persist leg runs — the part's pre-crop movie is
// re-persisted through UpdateMovie and the snapshot restores the pre-crop
// cache bytes — instead of returning 500 with the rejected crop still
// installed while job state points at the old bounds (an uncached reload
// would display the rejected crop; multipart requests racing after it could
// carry it further).
func TestUpdateBatchMoviePosterCrop_UpdateFailureRestoresStateAndCache(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "FAIL-001"
	const filePath = "/path/to/FAIL-001.mp4"
	result := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Fail", Poster: models.PosterState{
			CroppedPosterURL: "/api/v1/temp/posters/pre-crop.jpg",
			ShouldCropPoster: true,
		}},
	}

	var reverts []*models.Movie
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, filePath, true)
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{filePath})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(result, nil)
	// The pre-crop per-part snapshot (F7 compensation capture) reads the part
	// once before the failing UpdatePosterCrop, and the compensation then
	// re-persists THAT exact snapshot through UpdateMovie.
	mockJob.EXPECT().GetMovieResult(filePath).Return(result, nil)
	mockJob.EXPECT().UpdatePosterCrop(movieID, mock.Anything, mock.Anything).Return(assert.AnError)
	mockJob.EXPECT().UpdateMovie(mock.Anything, filePath, mock.Anything).
		Run(func(_ context.Context, _ string, m *models.Movie) {
			reverts = append(reverts, m)
		}).Return(nil)

	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	// Give CropWithBounds a real poster so the crop succeeds and the state
	// update is the failing step under test; the seeded preview must come
	// back byte-identical from the snapshot restore.
	posterDir := filepath.Join("data", "temp", "posters", "job-any")
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	fullPath := filepath.Join(posterDir, movieID+"-full.jpg")
	previewPath := filepath.Join(posterDir, movieID+".jpg")
	writeJPEG(t, fullPath, 900, 600)
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
	// Every compensation leg succeeded, so no rollback failure rides along.
	assert.NotContains(t, rec.Body.String(), "revert of part")
	assert.NotContains(t, rec.Body.String(), "poster rollback failed")

	// The single part was reverted with its exact pre-crop snapshot.
	require.Len(t, reverts, 1, "the failed crop state update must trigger the per-part revert")
	assert.Same(t, result.Movie, reverts[0],
		"the revert re-persists the pre-crop snapshot captured before UpdatePosterCrop")

	// And the CropWithBounds-overwritten preview is byte-restored — the
	// rejected crop must not stay installed against the reverted state.
	preview, err := os.ReadFile(previewPath)
	require.NoError(t, err)
	assert.Equal(t, oldPreview, preview,
		"the preview must be byte-identical to the pre-crop bytes after the failed state update")

	assertPosterSourceLockFreeAPI(t, "job-any", movieID)
	assertJobEnvelopeLockFree(t, "job-any")
}

func TestUpdateBatchMovie_CropBoundsValidation(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := createJobWithWF(deps, cfg, []string{"/path/to/VAL-001.mp4"})
	setJobResult(job, "/path/to/VAL-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/VAL-001.mp4", MovieID: "VAL-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "VAL-001", Title: "Validation"},
	})

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	patchMovie := func(bounds *contracts.CropBounds) *httptest.ResponseRecorder {
		view := contracts.MovieViewFromModel(&models.Movie{ID: "VAL-001", Title: "Validation"})
		view.PosterCropBounds = bounds
		body, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/VAL-001", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("rejects invalid bounds", func(t *testing.T) {
		for _, b := range []*contracts.CropBounds{
			{X: -1, Y: 0, Width: 100, Height: 100},
			{X: 0, Y: -1, Width: 100, Height: 100},
			{X: 0, Y: 0, Width: 0, Height: 100},
			{X: 0, Y: 0, Width: 100, Height: -50},
		} {
			rec := patchMovie(b)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "bounds %+v should be rejected", *b)
		}
	})

	t.Run("accepts valid bounds", func(t *testing.T) {
		rec := patchMovie(&contracts.CropBounds{X: 0, Y: 0, Width: 400, Height: 600})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("accepts absent bounds", func(t *testing.T) {
		rec := patchMovie(nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})

	t.Run("rejects negative max_poster_height", func(t *testing.T) {
		rec := patchMovie(&contracts.CropBounds{X: 0, Y: 0, Width: 100, Height: 100, MaxPosterHeight: -1})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestUpdateBatchMovie_PosterCropBoundsPatchPresence(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	job := createJobWithWF(deps, cfg, []string{"/path/to/PRES-001.mp4"})
	stored := &models.CropBounds{X: 10, Y: 20, Width: 400, Height: 600}
	setJobResult(job, "/path/to/PRES-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/PRES-001.mp4", MovieID: "PRES-001"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "PRES-001", Title: "Presence", Poster: models.PosterState{
			PosterURL: "https://example.com/p.jpg", CropBounds: stored,
		}},
	})
	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
	patch := func(t *testing.T, body string) *models.CropBounds {
		req := httptest.NewRequest(http.MethodPatch, "/batch/"+job.GetID()+"/results/PRES-001", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		for _, result := range job.GetStatus().Results {
			if result.Movie != nil {
				return result.Movie.Poster.CropBounds
			}
		}
		t.Fatal("movie result not found")
		return nil
	}

	t.Run("omitted preserves bounds", func(t *testing.T) {
		got := patch(t, `{"movie":{"id":"PRES-001","title":"Presence","poster_url":"https://example.com/p.jpg"}}`)
		require.NotNil(t, got)
		assert.Equal(t, *stored, *got)
	})
	t.Run("explicit null clears bounds", func(t *testing.T) {
		assert.Nil(t, patch(t, `{"movie":{"id":"PRES-001","title":"Presence","poster_url":"https://example.com/p.jpg","poster_crop_bounds":null}}`))
	})
}

func TestUpdateBatchMovie_PosterCropBoundsFieldPresence(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)

	stored := models.CropBounds{X: 10, Y: 20, Width: 400, Height: 600, MaxPosterHeight: 1200, ImageWidth: 800, ImageHeight: 1200}

	setup := func(t *testing.T) (*core.APIDeps, *gin.Engine, string) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{"/path/to/PRES-001.mp4"})
		b := stored
		setJobResult(job, "/path/to/PRES-001.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "/path/to/PRES-001.mp4", MovieID: "PRES-001"},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: "PRES-001", Title: "Presence", Poster: models.PosterState{
				PosterURL:  "https://example.com/p.jpg",
				CropBounds: &b,
			}},
		})
		router := gin.New()
		router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))
		return deps, router, job.GetID()
	}

	storedBounds := func(t *testing.T, deps *core.APIDeps, jobID string) *models.CropBounds {
		job, ok := deps.JobStore.GetBatchJob(jobID)
		require.True(t, ok)
		for _, r := range job.GetStatus().Results {
			if r.Movie != nil {
				return r.Movie.Poster.CropBounds
			}
		}
		t.Fatal("no movie result found")
		return nil
	}

	// The movie object resends the unchanged poster source as a pre-field
	// client would; only poster_crop_bounds varies between cases.
	const baseMovie = `"id":"PRES-001","title":"Presence","poster_url":"https://example.com/p.jpg"`

	cases := []struct {
		name string
		body string
		want *models.CropBounds
	}{
		{
			name: "omitted field preserves stored bounds",
			body: `{"movie":{` + baseMovie + `}}`,
			want: &stored,
		},
		{
			name: "explicit null clears stored bounds",
			body: `{"movie":{` + baseMovie + `,"poster_crop_bounds":null}}`,
			want: nil,
		},
		{
			name: "explicit valid bounds replace stored bounds",
			body: `{"movie":{` + baseMovie + `,"poster_crop_bounds":{"x":1,"y":2,"width":300,"height":500}}}`,
			want: &models.CropBounds{X: 1, Y: 2, Width: 300, Height: 500},
		},
		{
			name: "explicit invalid bounds are rejected",
			body: `{"movie":{` + baseMovie + `,"poster_crop_bounds":{"x":-1,"y":0,"width":300,"height":500}}}`,
			want: &stored, // request fails; stored state untouched
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps, router, jobID := setup(t)

			req := httptest.NewRequest(http.MethodPatch, "/batch/"+jobID+"/results/PRES-001", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			got := storedBounds(t, deps, jobID)
			if tc.want == nil {
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
			if rec.Code == http.StatusOK {
				// The response and the stored state must not alias the original
				// job-state bounds (preservation copies the value).
				assert.NotSame(t, &stored, got)
			}
		})
	}
}
