package batch

import (
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateBatchMoviePosterFromURL_RekeysToReassignedMovieAfterLockWait is
// the deterministic pin of the A→B re-key fix for the poster-from-URL
// endpoint: while the request waits on movie A's poster-source lock, a
// rescrape-corrected commit re-keys the target result to movie B
// (FileMatchInfo.MovieID and Movie.ID move, exactly what the rescrape commit
// writes). The pre-fix handler would wake with A's lock and keep the PRE-wait
// movieID/posterID — downloading into A's {A}-full.jpg cache and calling
// UpdatePosterFromURL(A, ...) (modifying the sibling result still at A) while
// returning success for B, which stays unchanged. The fixed handler
// re-resolves movieID AND the lock key from the post-lock state, hands the
// lock off from A to B (release before re-acquire — proven here by the
// request blocking on the still-held B key after A is released), downloads to
// B's cache, and persists B only.
func TestUpdateBatchMoviePosterFromURL_RekeysToReassignedMovieAfterLockWait(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	img := posterRefreshJPEG(t, 700, 1000, color.RGBA{R: 0x10, G: 0x90, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	}))
	t.Cleanup(srv.Close)
	posterURL := srv.URL + "/poster.jpg"

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieA, movieB = "FURK-A", "FURK-B"
	fileTarget := "/path/to/from-url-target.mp4" // the result being refreshed (A→B)
	fileSibling := "/path/to/still-a.mp4"        // another result that REMAINS on A
	job := createJobWithWF(deps, cfg, []string{fileTarget, fileSibling})
	setJobResult(job, fileTarget, &resultstore.MovieResult{
		ResultID:      "res-target",
		FileMatchInfo: models.FileMatchInfo{Path: fileTarget, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Movie A", Poster: models.PosterState{PosterURL: "https://example.com/a.jpg"}},
	})
	setJobResult(job, fileSibling, &resultstore.MovieResult{
		ResultID:      "res-sibling",
		FileMatchInfo: models.FileMatchInfo{Path: fileSibling, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Movie A", Poster: models.PosterState{PosterURL: "https://example.com/a.jpg"}},
	})

	// Seed A's cached source so an orphaned A download is observable.
	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	aFull := filepath.Join(posterDir, movieA+"-full.jpg")
	bFull := filepath.Join(posterDir, movieB+"-full.jpg")
	require.NoError(t, os.WriteFile(aFull, []byte("stale-a-cache"), 0o644))
	aBytes, err := os.ReadFile(aFull)
	require.NoError(t, err)

	jobID := job.GetID()
	jobIface, ok := deps.JobStore.GetBatchJob(jobID)
	require.True(t, ok)
	// Same signaling trick as the crop rekey test: the first
	// FindMovieResultForMovieID is the pre-lock poster-ID resolution — the
	// last store read before the handler blocks on A's lock.
	ready := make(chan struct{})
	wrapped := &signalingMovieLookup{BatchJobInterface: jobIface, firstResolve: ready}
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: wrapped}

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	// Hold BOTH keys so each lock the request takes (and lets go of) is
	// observable. Cleanup-guarded: a failing assertion must not leak a global
	// lock into other tests.
	releaseA := worker.AcquirePosterSourceLock(jobID, movieA)
	releaseB := worker.AcquirePosterSourceLock(jobID, movieB)
	aHeld, bHeld := true, true
	t.Cleanup(func() {
		if aHeld {
			releaseA()
		}
		if bHeld {
			releaseB()
		}
	})

	done := make(chan int, 1)
	go func() { done <- postPosterFromURL(t, router, jobID, "res-target", posterURL) }()

	// Pre-lock resolution done with posterID=A: the request is now parked on
	// A's lock. Commit the rescrape-corrected result for B.
	<-ready
	require.NoError(t, jobIface.UpdateMovie(t.Context(), fileTarget, &models.Movie{
		ID: movieB, Title: "Movie B",
		Poster: models.PosterState{PosterURL: "https://example.com/b.jpg"},
	}))

	// Free A: the fixed handler wakes with A's lock, observes the re-key,
	// releases A, and must now block on the still-held B key — it must NOT
	// proceed against A's cache/state.
	releaseA()
	aHeld = false
	select {
	case code := <-done:
		t.Fatalf("poster-from-url completed (%d) without waiting for the re-keyed movie's lock — it downloaded into/persisted movie A", code)
	case <-time.After(150 * time.Millisecond):
	}

	releaseB()
	bHeld = false
	select {
	case code := <-done:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("poster-from-url did not proceed after the re-keyed movie's lock was released")
	}

	// The whole refresh targeted B: B's cache got the downloaded image (bytes
	// are renamed verbatim into {posterID}-full.jpg) and B's result carries
	// the new poster state.
	refreshed := job.GetStatus().Results[fileTarget]
	require.NotNil(t, refreshed)
	require.NotNil(t, refreshed.Movie)
	require.Equal(t, movieB, refreshed.Movie.ID)
	assert.Equal(t, posterURL, refreshed.Movie.Poster.PosterURL)
	assert.NotEmpty(t, refreshed.Movie.Poster.CroppedPosterURL)
	bNow, err := os.ReadFile(bFull)
	require.NoError(t, err, "the download must land in B's cache")
	assert.Equal(t, img, bNow, "B's cached full-size source must be the downloaded image")

	// ...while A is untouched: the sibling result still at A keeps its poster
	// state, and A's cached full-size source was never read or overwritten.
	sibling := job.GetStatus().Results[fileSibling]
	require.NotNil(t, sibling)
	require.NotNil(t, sibling.Movie)
	assert.Equal(t, "https://example.com/a.jpg", sibling.Movie.Poster.PosterURL,
		"movie A must not receive B's poster URL")
	assert.Empty(t, sibling.Movie.Poster.CroppedPosterURL)
	aNow, err := os.ReadFile(aFull)
	require.NoError(t, err)
	assert.Equal(t, aBytes, aNow, "A's cached source must be untouched — no orphaned A download")

	// Both keys end lock-free: A was released on the handoff, B via the defer.
	assertPosterSourceLockFreeAPI(t, jobID, movieA)
	assertPosterSourceLockFreeAPI(t, jobID, movieB)
}

// TestUpdateBatchMoviePosterFromURL_UnchangedMovieIDKeepsSingleLock is the
// unchanged-ID complement: when the post-lock re-read resolves the SAME
// poster key, the handler must not reach for any other movie's lock — an
// over-eager re-key would deadlock here against the unrelated held key.
func TestUpdateBatchMoviePosterFromURL_UnchangedMovieIDKeepsSingleLock(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	img := posterRefreshJPEG(t, 600, 900, color.RGBA{G: 0x60, A: 0xff})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(img)
	}))
	t.Cleanup(srv.Close)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieID = "FUQ-001"
	filePath := "/path/to/FUQ-001.mp4"
	job := createJobWithWF(deps, cfg, []string{filePath})
	setJobResult(job, filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Unique"},
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	// An unrelated movie's lock stays held for the whole request: a handler
	// that only ever takes its own key sails through.
	releaseUnrelated := worker.AcquirePosterSourceLock(job.GetID(), "UNRELATED-1")
	defer releaseUnrelated()
	done := make(chan int, 1)
	go func() { done <- postPosterFromURL(t, router, job.GetID(), movieID, srv.URL+"/poster.jpg") }()
	select {
	case code := <-done:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("poster-from-url blocked on a lock other than its own movie key")
	}
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterFromURL_PostLockReResolveRejectsInvalidMovieID
// covers the re-resolution error branch: when the post-lock re-read resolves
// a movie ID that fails safe-filename validation, the request is rejected
// with 400 and — via the deferred release — leaves the PRE-wait lock free.
func TestUpdateBatchMoviePosterFromURL_PostLockReResolveRejectsInvalidMovieID(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const jobID, movieID, badID = "job-fromurl-rekey-bad", "FUR-001", "../evil"
	resultA := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/FUR-001.mp4", MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "A"},
	}
	resultBad := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/FUR-001.mp4", MovieID: badID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: badID, Title: "evil"},
	}
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID("res-bad").Return(resultA, "/path/to/FUR-001.mp4", true).Once()
	// The post-lock re-read resolves the path-traversal ID.
	mockJob.EXPECT().GetFileResultByResultID("res-bad").Return(resultBad, "/path/to/FUR-001.mp4", true).Once()
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{"/path/to/FUR-001.mp4"})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(resultA, nil)
	mockJob.EXPECT().FindMovieResultForMovieID(badID).Return(resultBad, nil)
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(testkit.GetTestRuntime(deps)))

	require.Equal(t, http.StatusBadRequest,
		postPosterFromURL(t, router, jobID, "res-bad", "https://example.com/poster.jpg"))
	assertPosterSourceLockFreeAPI(t, jobID, movieID)
}
