package batch

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
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

// signalingMovieLookup wraps a BatchJobInterface and closes firstResolve when
// the first FindMovieResultForMovieID call returns. That call is the pre-lock
// poster-ID resolution inside updateBatchMoviePosterCrop (resolvePosterID) —
// the last store read before the handler blocks on the poster-source lock —
// so the channel fires once the cropping request has locked in its PRE-wait
// key, making the re-key-under-wait scenario deterministic.
type signalingMovieLookup struct {
	worker.BatchJobInterface
	firstResolve chan struct{}
	once         sync.Once
}

func (j *signalingMovieLookup) FindMovieResultForMovieID(movieID string) (*resultstore.MovieResult, error) {
	res, err := j.BatchJobInterface.FindMovieResultForMovieID(movieID)
	j.once.Do(func() { close(j.firstResolve) })
	return res, err
}

// TestUpdateBatchMoviePosterCrop_RekeysToReassignedMovieAfterLockWait is the
// deterministic pin of the A→B re-key fix: while the crop request waits on
// movie A's poster-source lock, a rescrape-corrected commit re-keys the
// cropped result to movie B (FileMatchInfo.MovieID and Movie.ID move, exactly
// what the rescrape commit writes). The post-lock re-read alone would refresh
// the RESULT but leave the request cropping A's cached source and calling
// UpdatePosterCrop(A, ...) — modifying the sibling result still at A while
// returning success for B. The fixed handler re-resolves movieID AND the lock
// key from the post-lock state, hands the lock off from A to B (release
// before re-acquire — proven here by the request blocking on the still-held
// B key after A is released), crops B's cache, and updates B only.
func TestUpdateBatchMoviePosterCrop_RekeysToReassignedMovieAfterLockWait(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieA, movieB = "REKY-A", "REKY-B"
	fileCrop := "/path/to/crop-target.mp4" // the result being cropped (A→B)
	fileSibling := "/path/to/still-a.mp4"  // another result that REMAINS on A
	job := createJobWithWF(deps, cfg, []string{fileCrop, fileSibling})
	setJobResult(job, fileCrop, &resultstore.MovieResult{
		ResultID:      "res-crop",
		FileMatchInfo: models.FileMatchInfo{Path: fileCrop, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Movie A", Poster: models.PosterState{PosterURL: "https://example.com/a.jpg"}},
	})
	setJobResult(job, fileSibling, &resultstore.MovieResult{
		ResultID:      "res-sibling",
		FileMatchInfo: models.FileMatchInfo{Path: fileSibling, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Movie A", Poster: models.PosterState{PosterURL: "https://example.com/a.jpg"}},
	})

	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	aFull := filepath.Join(posterDir, movieA+"-full.jpg")
	bFull := filepath.Join(posterDir, movieB+"-full.jpg")
	writeJPEG(t, aFull, 1000, 600)
	writeJPEG(t, bFull, 800, 1200)
	aBytes, err := os.ReadFile(aFull)
	require.NoError(t, err)

	jobID := job.GetID()
	jobIface, ok := deps.JobStore.GetBatchJob(jobID)
	require.True(t, ok)
	ready := make(chan struct{})
	wrapped := &signalingMovieLookup{BatchJobInterface: jobIface, firstResolve: ready}
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: wrapped}

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

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
	go func() { done <- postPosterCropBounds(router, jobID, "res-crop") }()

	// Pre-lock resolution done with posterID=A: the request is now parked on
	// A's lock. Commit the rescrape-corrected result for B.
	<-ready
	require.NoError(t, jobIface.UpdateMovie(t.Context(), fileCrop, &models.Movie{
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
		t.Fatalf("crop completed (%d) without waiting for the re-keyed movie's lock — it cropped/stamped movie A", code)
	case <-time.After(150 * time.Millisecond):
	}

	releaseB()
	bHeld = false
	select {
	case code := <-done:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("crop did not proceed after the re-keyed movie's lock was released")
	}

	// The whole crop operation targeted B: B's cached source was measured
	// (the recorded bounds carry the 800x1200 source dimensions) and B's
	// result carries the crop state.
	cropped := job.GetStatus().Results[fileCrop]
	require.NotNil(t, cropped)
	require.NotNil(t, cropped.Movie)
	require.Equal(t, movieB, cropped.Movie.ID)
	require.NotNil(t, cropped.Movie.Poster.CropBounds, "B must carry the recorded crop bounds")
	assert.Equal(t, 800, cropped.Movie.Poster.CropBounds.ImageWidth,
		"bounds must be measured against B's cached source, not A's 1000x600 image")
	assert.Equal(t, 1200, cropped.Movie.Poster.CropBounds.ImageHeight)
	assert.NotEmpty(t, cropped.Movie.Poster.CroppedPosterURL)

	// ...while A is untouched: the sibling result still at A got no crop
	// state, and A's cached full-size source was never read or overwritten.
	sibling := job.GetStatus().Results[fileSibling]
	require.NotNil(t, sibling)
	require.NotNil(t, sibling.Movie)
	assert.Nil(t, sibling.Movie.Poster.CropBounds, "movie A must not receive B's crop bounds")
	assert.Empty(t, sibling.Movie.Poster.CroppedPosterURL)
	aNow, err := os.ReadFile(aFull)
	require.NoError(t, err)
	assert.Equal(t, aBytes, aNow, "A's cached source must be untouched")

	// Both keys end lock-free: A was released on the handoff, B via the defer.
	assertPosterSourceLockFreeAPI(t, jobID, movieA)
	assertPosterSourceLockFreeAPI(t, jobID, movieB)
}

// TestUpdateBatchMoviePosterCrop_UnchangedMovieIDKeepsSingleLock is the
// unchanged-ID complement: when the post-lock re-read resolves the SAME
// poster key, the handler must not reach for any other movie's lock — an
// over-eager re-key would deadlock here against the unrelated held key.
func TestUpdateBatchMoviePosterCrop_UnchangedMovieIDKeepsSingleLock(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const movieID = "UNIQ-001"
	fileCrop := "/path/to/UNIQ-001.mp4"
	job := createJobWithWF(deps, cfg, []string{fileCrop})
	setJobResult(job, fileCrop, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileCrop, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "Unique"},
	})
	posterDir := filepath.Join("data", "temp", "posters", job.GetID())
	require.NoError(t, os.MkdirAll(posterDir, 0o755))
	writeJPEG(t, filepath.Join(posterDir, movieID+"-full.jpg"), 1000, 600)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	// An unrelated movie's lock stays held for the whole request: a handler
	// that only ever takes its own key sails through.
	releaseUnrelated := worker.AcquirePosterSourceLock(job.GetID(), "UNRELATED-1")
	defer releaseUnrelated()
	done := make(chan int, 1)
	go func() { done <- postPosterCropBounds(router, job.GetID(), movieID) }()
	select {
	case code := <-done:
		require.Equal(t, http.StatusOK, code)
	case <-time.After(5 * time.Second):
		t.Fatal("crop blocked on a lock other than its own movie key")
	}
	assertPosterSourceLockFreeAPI(t, job.GetID(), movieID)
}

// TestUpdateBatchMoviePosterCrop_PostLockReResolveRejectsInvalidMovieID
// covers the re-resolution error branch: when the post-lock re-read resolves
// a movie ID that fails safe-filename validation, the request is rejected
// with 400 and — via the deferred release — leaves the PRE-wait lock free.
func TestUpdateBatchMoviePosterCrop_PostLockReResolveRejectsInvalidMovieID(t *testing.T) {
	initTestWebSocket(t)
	gin.SetMode(gin.TestMode)
	chdirWorkDir(t)

	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	const jobID, movieID, badID = "job-rekey-bad", "RKB-001", "../evil"
	resultA := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/RKB-001.mp4", MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, Title: "A"},
	}
	resultBad := &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/RKB-001.mp4", MovieID: badID},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: badID, Title: "evil"},
	}
	mockJob := workermocks.NewMockBatchJobInterface(t)
	mockJob.EXPECT().GetFileResultByResultID("res-bad").Return(resultA, "/path/to/RKB-001.mp4", true).Once()
	// The post-lock re-read resolves the path-traversal ID.
	mockJob.EXPECT().GetFileResultByResultID("res-bad").Return(resultBad, "/path/to/RKB-001.mp4", true).Once()
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{"/path/to/RKB-001.mp4"})
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(resultA, nil)
	mockJob.EXPECT().FindMovieResultForMovieID(badID).Return(resultBad, nil)
	deps.JobStore = &fixedJobStore{JobStoreInterface: deps.JobStore, job: mockJob}

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-crop", updateBatchMoviePosterCrop(testkit.GetTestRuntime(deps)))

	require.Equal(t, http.StatusBadRequest, postPosterCropBounds(router, jobID, "res-bad"))
	assertPosterSourceLockFreeAPI(t, jobID, movieID)
}
