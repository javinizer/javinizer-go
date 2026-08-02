package batch

import (
	"net/http"
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

// TestUpdateBatchMovie_RekeysToReassignedMovieAfterLockWait is the
// deterministic pin of the whole-movie PATCH A→B re-key fix: while the PATCH
// waits on movie A's poster-source lock, a rescrape-corrected commit re-keys
// the target result to movie B (FileMatchInfo.MovieID and Movie.ID move,
// exactly what the rescrape commit writes). The pre-fix handler refreshed
// current/filePaths from the post-lock state but kept holding A's lock, so
// its poster refresh and whole-movie writes could interleave with a crop,
// poster-from-URL, or field override holding B's lock. The fixed handler
// re-resolves the lock key from the post-lock state, hands the lock off from
// A to B (release before re-acquire — proven here by the request blocking on
// the still-held B key after A is released), refreshes B's cache, and
// persists B's file paths only.
func TestUpdateBatchMovie_RekeysToReassignedMovieAfterLockWait(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	allowTestHTTPServerURL(t)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieA, movieB = "PREK-A", "PREK-B"
	fileTarget := "/path/to/patch-rekey-target.mp4" // the result being PATCHed (A→B)
	fileSibling := "/path/to/patch-still-a.mp4"     // another result that REMAINS on A
	job := createJobWithWF(deps, cfg, []string{fileTarget, fileSibling})
	setJobResult(job, fileTarget, &resultstore.MovieResult{
		ResultID:      "res-target",
		FileMatchInfo: models.FileMatchInfo{Path: fileTarget, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Movie A", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})
	setJobResult(job, fileSibling, &resultstore.MovieResult{
		ResultID:      "res-sibling",
		FileMatchInfo: models.FileMatchInfo{Path: fileSibling, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Movie A", Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"}},
	})

	// Seed A's cached source so an orphaned A refresh is observable; B has no
	// cache yet — one appearing proves the refresh followed the re-key.
	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	aFull := filepath.Join(posterDir, movieA+"-full.jpg")
	bFull := filepath.Join(posterDir, movieB+"-full.jpg")
	require.NoError(t, os.WriteFile(aFull, srv.images["/old.jpg"], 0o644))
	aBytes, err := os.ReadFile(aFull)
	require.NoError(t, err)

	jobID := job.GetID()
	jobIface, ok := deps.JobStore.GetBatchJob(jobID)
	require.True(t, ok)
	// Same signaling trick as the stale-read test: the first
	// GetFileResultByResultID is the pre-lock lookup — the last store read
	// before the handler blocks on A's lock, making the re-key-under-wait
	// deterministic.
	ready := make(chan struct{})
	wrappedJob := &signaledBatchJob{BatchJobInterface: jobIface, firstLookup: ready}
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: wrappedJob}

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

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

	// The whole-movie payload the review client assembled for the movie it
	// PATCHes; its poster_url picks a new source so the handler's refresh
	// must regenerate the (re-keyed) movie's cached -full.jpg before
	// persisting. movie.ID keys the {movieID}-full.jpg cache file.
	done := make(chan int, 1)
	go func() { done <- patchWholeMovie(t, router, jobID, "res-target", movieB, srv.URL+"/a.jpg") }()

	// Pre-lock lookup done with lock key = A: the request is now parked on
	// A's lock. Commit the rescrape-corrected result for B.
	<-ready
	require.NoError(t, jobIface.UpdateMovie(t.Context(), fileTarget, &models.Movie{
		ID: movieB, Title: "Movie B",
		Poster: models.PosterState{PosterURL: srv.URL + "/old.jpg"},
	}))

	// Free A: the fixed handler wakes with A's lock, observes the re-key,
	// releases A, and must now block on the still-held B key — it must NOT
	// proceed with its refresh/writes against B's state while holding only
	// A's lock.
	releaseA()
	aHeld = false
	select {
	case code := <-done:
		t.Fatalf("PATCH completed (%d) without waiting for the re-keyed movie's lock — it refreshed/persisted state holding only A's lock", code)
	case <-time.After(150 * time.Millisecond):
	}

	releaseB()
	bHeld = false
	select {
	case code := <-done:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("PATCH did not proceed after the re-keyed movie's lock was released")
	}

	// The whole edit targeted B: the refreshed source landed in B's cache and
	// B's result carries the new poster state.
	refreshed := job.GetStatus().Results[fileTarget]
	require.NotNil(t, refreshed)
	require.NotNil(t, refreshed.Movie)
	require.Equal(t, movieB, refreshed.Movie.ID)
	assert.Equal(t, srv.URL+"/a.jpg", refreshed.Movie.Poster.PosterURL)
	bNow, err := os.ReadFile(bFull)
	require.NoError(t, err, "the refreshed poster must land in B's cache")
	assert.Equal(t, srv.images["/a.jpg"], bNow, "B's cached full-size source must be the refreshed image")

	// ...while A is untouched: the sibling result still at A keeps its poster
	// state, and A's cached full-size source was never overwritten.
	sibling := job.GetStatus().Results[fileSibling]
	require.NotNil(t, sibling)
	require.NotNil(t, sibling.Movie)
	assert.Equal(t, movieA, sibling.Movie.ID)
	assert.Equal(t, srv.URL+"/old.jpg", sibling.Movie.Poster.PosterURL,
		"movie A must not receive B's poster edit")
	aNow, err := os.ReadFile(aFull)
	require.NoError(t, err)
	assert.Equal(t, aBytes, aNow, "A's cached source must be untouched")

	// Both keys end lock-free: A was released on the handoff, B via the defer.
	assertPosterSourceLockFreeAPI(t, jobID, movieA)
	assertPosterSourceLockFreeAPI(t, jobID, movieB)
	assert.Equal(t, int64(1), srv.hits["/a.jpg"].Load(), "the new source is refreshed exactly once")
}

// TestUpdateBatchMovie_UnchangedMovieIDKeepsSingleLock is the unchanged-ID
// complement: when the post-lock re-read resolves the SAME lock key, the
// handler must not reach for any other movie's lock — an over-eager re-key
// would deadlock here against the unrelated held key.
func TestUpdateBatchMovie_UnchangedMovieIDKeepsSingleLock(t *testing.T) {
	srv := newPosterConcurrencyServer(t)
	const movieID = "PUNQ-001"
	deps, job, _ := setupPosterRaceJob(t, srv, movieID)

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	// An unrelated movie's lock stays held for the whole request: a handler
	// that only ever takes its own key sails through.
	releaseUnrelated := worker.AcquirePosterSourceLock(job.GetID(), "UNRELATED-1")
	defer releaseUnrelated()
	done := make(chan int, 1)
	go func() { done <- patchPosterURL(t, router, job.GetID(), movieID, srv.URL+"/a.jpg") }()
	select {
	case code := <-done:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("PATCH blocked on a lock other than its own movie key")
	}
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMovie_PostLockReResolveRejectsInvalidMovieID covers the
// re-resolution error branch: when the post-lock re-read resolves a movie ID
// that fails safe-filename validation, the PATCH is rejected with 400 and —
// via the deferred release — leaves the acquired lock free.
func TestUpdateBatchMovie_PostLockReResolveRejectsInvalidMovieID(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const jobID, movieID, badID = "job-patch-rekey-bad", "PRB-001", "../evil"
	filePath := "/path/to/PRB-001.mp4"
	resultA := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "A"},
	}
	resultBad := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: badID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: badID, Title: "evil"},
	}
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID("res-bad").Return(resultA, filePath, true).Once()
	// The post-lock re-read resolves the path-traversal ID.
	mockJob.EXPECT().GetFileResultByResultID("res-bad").Return(resultBad, filePath, true).Once()
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{filePath})
	mockJob.EXPECT().FindFilePathsForMovieID(badID).Return([]string{filePath})
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	router := gin.New()
	router.PATCH("/batch/:id/results/:resultId", updateBatchMovie(testkit.GetTestRuntime(deps)))

	require.Equal(t, http.StatusBadRequest,
		patchWholeMovie(t, router, jobID, "res-bad", movieID, "https://example.com/poster.jpg"))
	assertPosterSourceLockFreeAPI(t, jobID, movieID)
}
