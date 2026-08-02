package batch

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	dbmocks "github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ssrf"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// TestUpdateBatchMoviePosterFromURL_ResponseCarriesServerCropIntent pins the
// audit-7 desync fix: the poster-from-URL response must echo the crop intent
// PosterEditor.UpdatePosterFromURL derived from the job state, so the client
// can overlay it instead of guessing false (the temp preview is ALWAYS
// auto-cropped; a hard-coded false would let a later whole-movie Save resubmit
// false with an unchanged poster_source — treated as deliberate — and Organize
// would download the image whole while the preview showed it cropped).
//
// The intent derivation itself lives in
// PosterEditor.cropIntentAfterPosterFromURL: with no provenance source
// matching the new URL (the case here — a hand-typed URL matches no scraper
// result), the class of the PRIOR effective source decides:
//   - cover-backed prior (source needed the default crop)      -> true
//   - poster-grade prior (explicit poster, no default crop)    -> false
func TestUpdateBatchMoviePosterFromURL_ResponseCarriesServerCropIntent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)

	// Deterministic 200: route SSRF DNS through a public resolver answer so
	// the loopback httptest server is not rejected as a private address.
	cleanup := ssrf.SetLookupIPForTest(func(string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	t.Cleanup(cleanup)
	chdirWorkDir(t)

	// Landscape source prompts the cover-crop path, mirroring the manager's
	// processing of a real cover download.
	img := image.NewRGBA(image.Rect(0, 0, 800, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 90, B: 90, A: 255})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	t.Cleanup(srv.Close)

	cases := []struct {
		name     string
		movieID  string
		prior    models.PosterState
		wantCrop bool
		wantWhy  string
	}{
		{
			name:    "cover-backed prior defaults to crop",
			movieID: "PCIN-001",
			prior: models.PosterState{
				// Cover-backed effective source: the scraper's landscape cover
				// doubles as poster source and is flagged for the default crop.
				CoverURL:         "https://example.com/cover.jpg",
				CroppedPosterURL: "/api/v1/temp/posters/old-cropped.jpg",
				ShouldCropPoster: true,
			},
			wantCrop: true,
			wantWhy:  "a cover-backed prior source must yield should_crop_poster=true so Organize's default crop matches the auto-cropped preview",
		},
		{
			name:    "poster-grade prior keeps no-crop intent",
			movieID: "PCIN-002",
			prior: models.PosterState{
				// Poster-grade prior: an explicit poster image already serving
				// cropped, with no default crop recorded against it.
				PosterURL:        "https://example.com/explicit-poster.jpg",
				CroppedPosterURL: "/api/v1/temp/posters/old-cropped.jpg",
				ShouldCropPoster: false,
			},
			wantCrop: false,
			wantWhy:  "a poster-grade prior source must yield should_crop_poster=false so Organize downloads the poster-grade replacement whole",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			deps := createTestDeps(t, cfg, "")

			filePath := "/path/to/" + tc.movieID + ".mp4"
			job := createJobWithWF(deps, cfg, []string{filePath})
			movie := &models.Movie{ID: tc.movieID, Title: "Crop Intent", Poster: tc.prior}
			setJobResult(job, filePath, &resultstore.MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: tc.movieID},
				Status:        models.JobStatusCompleted,
				Movie:         movie,
			})

			router := gin.New()
			router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
			body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/poster.jpg"})
			req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+tc.movieID+"/poster-from-url", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
			var resp contracts.PosterFromURLResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, tc.wantCrop, resp.ShouldCropPoster, tc.wantWhy)
			assert.Equal(t, srv.URL+"/poster.jpg", resp.PosterURL)
			assert.NotEmpty(t, resp.CroppedPosterURL)

			// The response value must be the persisted job state, not a
			// request-side guess: re-read the stored result and confirm parity.
			stored := storedMovieResult(t, job, tc.movieID)
			require.NotNil(t, stored.Movie)
			assert.Equal(t, tc.wantCrop, stored.Movie.Poster.ShouldCropPoster,
				"the response must echo the persisted intent, not recomputed or defaulted logic")
			assertPosterSourceLockFreeAPI(t, job.GetID(), tc.movieID)
		})
	}
}

// TestUpdateBatchMoviePosterFromURL_CompensateNilPartAndRevertFailure exercises
// the two degenerate legs of the handler's compensation closure: a multipart
// group where one part has NO movie in memory (the revert loop must skip it)
// and a failing movies-table upsert on the real part (the revert failure must
// surface in the 500 message next to the persist error, not be swallowed).
func TestUpdateBatchMoviePosterFromURL_CompensateNilPartAndRevertFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	img := image.NewRGBA(image.Rect(0, 0, 800, 500))
	for y := 0; y < 500; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: 90, G: 90, B: 90, A: 255})
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	deps.JobStore = newFailingPersistJobStore(t, cfg)

	// A movies table whose writes always fail: UpdatePosterFromURL's
	// best-effort FindByID just logs, but the compensation's per-part
	// UpdateMovie upsert fails — exercising the "revert of part" addend.
	movieRepo := dbmocks.NewMockMovieRepositoryInterface(t)
	movieRepo.EXPECT().FindByID(mock.Anything, mock.Anything).Return(nil, errors.New("movies table gone"))
	movieRepo.EXPECT().Upsert(mock.Anything, mock.Anything).Return(nil, errors.New("movies table gone"))

	fc, _ := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.ScraperRegistry, deps.CoreDeps.DB.Repositories())
	factory, ferr := workflow.NewWorkflowFactory(fc)
	require.NoError(t, ferr)
	wf, ferr := factory.NewWorkflow("")
	require.NoError(t, ferr)

	const movieID = "PCIN-003"
	file1 := "/path/to/" + movieID + "-cd1.mp4"
	file2 := "/path/to/" + movieID + "-cd2.mp4"
	job := deps.JobStore.CreateJobBatch([]string{file1, file2}, &worker.JobConfig{
		BatchJobDeps: worker.BatchJobDeps{
			WF:        wf,
			MovieRepo: movieRepo,
			BatchCfg: worker.BatchJobConfig{
				MaxWorkers:      cfg.Performance.MaxWorkers,
				WorkerTimeout:   time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
				ScraperPriority: cfg.Scrapers.Priority,
			},
		},
	})
	setJobResult(job, file1, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: file1, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Part With Movie", Poster: models.PosterState{
			PosterURL: srv.URL + "/old.jpg",
		}},
	})
	// Sibling part with NO in-memory movie: the compensate loop must skip it.
	setJobResult(job, file2, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: file2, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         nil,
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/new.jpg"})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "persist")
	assert.Contains(t, rec.Body.String(), "revert of part",
		"the failed in-memory revert must surface alongside the persist error")
	assert.Contains(t, rec.Body.String(), "movies table gone")
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterFromURL_DownloadErrorGeneric500 covers the generic
// download-error classification: an error containing none of the
// SSRF/invalid-URL/download/status keywords must surface as a plain 500. The
// oversized-image guard ("image too large", >50 MB) is the deterministic
// trigger.
func TestUpdateBatchMoviePosterFromURL_DownloadErrorGeneric500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initTestWebSocket(t)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	oversized := make([]byte, (50<<20)+2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(oversized)
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")

	const movieID = "PCIN-004"
	filePath := "/path/to/" + movieID + ".mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Oversized"},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
	body, _ := json.Marshal(contracts.PosterFromURLRequest{URL: srv.URL + "/poster.jpg"})
	req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "image too large")
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}
