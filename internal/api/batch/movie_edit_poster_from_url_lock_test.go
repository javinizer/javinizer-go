package batch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

// postPosterFromURL issues one poster-from-url request and returns the HTTP status.
func postPosterFromURL(t *testing.T, router *gin.Engine, jobID, resultID, url string) int {
	t.Helper()
	body, err := json.Marshal(contracts.PosterFromURLRequest{URL: url})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/batch/"+jobID+"/results/"+resultID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

// TestUpdateBatchMoviePosterFromURL_TakesSharedPosterSourceLock proves the
// poster-from-URL endpoint contends on the SAME per-(jobID, movieID) lock the
// crop endpoint, the whole-movie PATCH, and the field-override paths use:
// while the test goroutine holds it, a refresh request cannot complete; once
// released, it proceeds. Deterministic complement to the race test below.
func TestUpdateBatchMoviePosterFromURL_TakesSharedPosterSourceLock(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	img := posterRefreshJPEG(t, 800, 500, color.RGBA{R: 0x70, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieID = "FLK-001"
	job := createJobWithWF(deps, cfg, []string{"/path/to/FLK-001.mp4"})
	setJobResult(job, "/path/to/FLK-001.mp4", &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/FLK-001.mp4", MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "From URL"},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	release := worker.AcquirePosterSourceLock(job.GetID(), movieID)
	url := srv.URL + "/poster.jpg"
	done := make(chan int, 1)
	go func() { done <- postPosterFromURL(t, router, job.GetID(), movieID, url) }()

	select {
	case code := <-done:
		release()
		t.Fatalf("poster-from-url completed (%d) while the shared poster-source lock was held", code)
	case <-time.After(150 * time.Millisecond):
	}
	release()

	select {
	case code := <-done:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("poster-from-url did not proceed after the lock was released")
	}
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterFromURL_PosterSourceLockReleasedOnAllPaths is the
// deadlock-safety table for the poster-from-URL endpoint: it takes ONLY the
// shared poster-source lock (no overrideMu — parity with updateBatchMovie and
// updateBatchMoviePosterCrop; ApplyFieldOverride takes overrideMu before this
// lock and never after), so every outcome — success, a rejected URL, a failed
// download, or a failed state update — must leave the per-(jobID, movieID)
// lock free. A leaked release would wedge every future poster edit for that
// movie.
func TestUpdateBatchMoviePosterFromURL_PosterSourceLockReleasedOnAllPaths(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	img := posterRefreshJPEG(t, 800, 500, color.RGBA{G: 0x80, A: 0xff})
	mux := http.NewServeMux()
	mux.HandleFunc("/poster.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	})
	mux.HandleFunc("/broken.jpg", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	newJob := func(t *testing.T, movieID string) (*core.APIDeps, *worker.BatchJob) {
		t.Helper()
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		filePath := "/path/to/" + movieID + ".mp4"
		job := createJobWithWF(deps, cfg, []string{filePath})
		setJobResult(job, filePath, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Title: "From URL"},
		})
		return deps, job
	}

	newRouter := func(deps *core.APIDeps) *gin.Engine {
		router := gin.New()
		router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
		return router
	}

	t.Run("successful refresh releases the lock", func(t *testing.T) {
		deps, job := newJob(t, "FOK-001")
		require.Equal(t, http.StatusOK,
			postPosterFromURL(t, newRouter(deps), job.GetID(), "FOK-001", srv.URL+"/poster.jpg"))
		assertPosterSourceLockFreeAPI(t, job.GetID(), "FOK-001")
	})

	t.Run("SSRF rejection releases the lock", func(t *testing.T) {
		deps, job := newJob(t, "FSR-001")
		require.Equal(t, http.StatusBadRequest,
			postPosterFromURL(t, newRouter(deps), job.GetID(), "FSR-001", "ftp://example.com/poster.jpg"))
		assertPosterSourceLockFreeAPI(t, job.GetID(), "FSR-001")
	})

	t.Run("download failure releases the lock", func(t *testing.T) {
		deps, job := newJob(t, "FDL-001")
		require.Equal(t, http.StatusBadGateway,
			postPosterFromURL(t, newRouter(deps), job.GetID(), "FDL-001", srv.URL+"/broken.jpg"))
		assertPosterSourceLockFreeAPI(t, job.GetID(), "FDL-001")
	})

	t.Run("state update failure releases the lock", func(t *testing.T) {
		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		const jobID, movieID = "job-fromurl-fail", "FUP-001"
		filePath := "/path/to/FUP-001.mp4"
		result := &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID, Title: "Fail"},
		}
		mockJob := workermocks.NewMockBatchJobInterface(t)
		mockJob.EXPECT().GetFileResultByResultID(movieID).Return(result, filePath, true)
		mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{filePath})
		mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(result, nil)
		mockJob.EXPECT().UpdatePosterFromURL(mock.Anything, movieID, mock.Anything, mock.Anything).
			Return(assert.AnError)
		deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

		require.Equal(t, http.StatusInternalServerError,
			postPosterFromURL(t, newRouter(deps), jobID, movieID, srv.URL+"/poster.jpg"))
		assertPosterSourceLockFreeAPI(t, jobID, movieID)
	})
}

// TestUpdateBatchMoviePosterFromURL_SerializesWithManualCrop is the Finding C
// race: a poster-from-URL refresh and a manual crop run concurrently against
// the same job/movie. The refresh endpoint now holds the shared
// per-(jobID, movieID) lock across DownloadFromURL, UpdatePosterFromURL, and
// the persistence, so the two sequences cannot interleave. The invariant
// pinned here: after both requests finish, the persisted poster URL, the
// cached -full.jpg, and any recorded crop bounds ALL describe the same
// (post-refresh) image — no bounds measured against the pre-refresh image may
// survive on the refreshed source.
func TestUpdateBatchMoviePosterFromURL_SerializesWithManualCrop(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	// Distinct dimensions: bounds measured against /old.jpg (2000x3000) can
	// never be mistaken for bounds measured against /new.jpg (1000x1500). The
	// old image is a large NOISY jpeg (~100ms to decode) so the crop's
	// source-read → state-update window is far wider than the refresh's
	// 15ms download delay: without the shared lock, a crop that starts inside
	// that delay reads the pre-refresh image yet persists after the refresh —
	// the exact interleave from the finding.
	oldJPEG := noisyJPEG(t, 2000, 3000)
	newJPEG := posterRefreshJPEG(t, 1000, 1500, color.RGBA{R: 0x20, G: 0x40, B: 0xcc, A: 0xff})
	require.NotEqual(t, oldJPEG, newJPEG)
	mux := http.NewServeMux()
	mux.HandleFunc("/new.jpg", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(15 * time.Millisecond) // widen the refresh window so the two requests overlap
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(newJPEG)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	urlNew := srv.URL + "/new.jpg"

	// Several rounds raise the chance the crop's source read actually
	// overlaps the refresh window; every round must still converge to the
	// invariant.
	for round := 0; round < 4; round++ {
		movieID := fmt.Sprintf("FRACE-%03d", round)
		filePath := "/path/to/" + movieID + ".mp4"

		cfg := config.DefaultConfig(nil, nil)
		deps := createTestDeps(t, cfg, "")
		job := createJobWithWF(deps, cfg, []string{filePath})
		setJobResult(job, filePath, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: movieID, Title: "Race", Poster: models.PosterState{
				PosterURL: "https://example.com/old-poster.jpg",
			}},
		})
		tempPosterDir := filepath.Join("data", "temp", "posters", job.GetID())
		require.NoError(t, os.MkdirAll(tempPosterDir, 0o755))
		fullPath := filepath.Join(tempPosterDir, movieID+"-full.jpg")
		require.NoError(t, os.WriteFile(fullPath, oldJPEG, 0o644))

		router := gin.New()
		router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))
		router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

		var wg sync.WaitGroup
		cropCode := make(chan int, 1)
		fromURLCode := make(chan int, 1)
		wg.Add(2)
		// Start the refresh first and the crop inside its download window:
		// without the shared lock the crop reads the pre-refresh -full.jpg
		// yet persists after the refresh; with it, the crop blocks on the
		// lock until the refresh has finished downloading and persisting.
		go func() { defer wg.Done(); fromURLCode <- postPosterFromURL(t, router, job.GetID(), movieID, urlNew) }()
		time.Sleep(3 * time.Millisecond)
		go func() { defer wg.Done(); cropCode <- postPosterCropBounds(router, job.GetID(), movieID) }()
		wg.Wait()

		require.Equal(t, http.StatusOK, <-cropCode, "round %d: the crop is valid against either image", round)
		require.Equal(t, http.StatusOK, <-fromURLCode, "round %d", round)

		// The refresh is the last writer of the poster URL under every
		// serialized order (UpdatePosterCrop never touches PosterURL).
		assertCachedPosterMatchesStoredURL(t, job, movieID, fullPath, map[string][]byte{urlNew: newJPEG})

		current := storedMovieResult(t, job, movieID)
		require.NotNil(t, current.Movie)
		if b := current.Movie.Poster.PosterURL; b != urlNew {
			t.Fatalf("round %d: poster-from-url always wins the URL write; got %q", round, b)
		}
		assert.False(t, current.Movie.Poster.ShouldCropPoster,
			"round %d: an explicit URL poster is poster-grade in every order", round)
		if b := current.Movie.Poster.CropBounds; b != nil {
			// The crop persisted after the refresh: bounds must have been
			// measured against the post-refresh image — never the noisy
			// pre-refresh 2000x3000 one.
			assert.Equal(t, 1000, b.ImageWidth, "round %d: bounds measured against the pre-refresh image survived on the refreshed source", round)
			assert.Equal(t, 1500, b.ImageHeight, "round %d", round)
			assert.False(t, b.SourceWasCover, "round %d: a poster-grade source records poster intent", round)
		}
		// In every serialized order the final preview is derived from the
		// post-refresh image: either the crop measured and cut /new.jpg
		// after the refresh completed, or the refresh itself rewrote the
		// preview from /new.jpg after clearing the crop's pre-refresh
		// output.
		assertPreviewDerivedFromNewImage(t, filepath.Join(tempPosterDir, movieID+".jpg"))
		// A nil CropBounds is the other legitimate order: the crop persisted
		// first and the refresh cleared it (UpdatePosterFromURL sets nil).

		assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
	}
}
