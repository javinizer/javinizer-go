package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// vanishingResultStore wraps a resultstore.Store and makes the targeted
// result disappear on the getCallsOn lookup of GetFileResultByResultID, so
// tests can drive ApplyFieldOverride's post-lock re-read-miss path (the
// result vanished while this call waited on the shared lock).
type vanishingResultStore struct {
	resultstore.Store
	resultID    string
	vanishOn    int
	lookupCalls int
}

func (v *vanishingResultStore) GetFileResultByResultID(resultID string) (*resultstore.MovieResult, string, bool) {
	if resultID == v.resultID {
		v.lookupCalls++
		if v.lookupCalls >= v.vanishOn {
			return nil, "", false
		}
	}
	return v.Store.GetFileResultByResultID(resultID)
}

// TestAcquirePosterSourceLock_SerializesPerKeyAndEvicts pins the three
// contract points of the shared lock: the same (jobID, movieID) key blocks,
// a different key does not, and the map entry is evicted once the last
// holder releases (no unbounded growth).
func TestAcquirePosterSourceLock_SerializesPerKeyAndEvicts(t *testing.T) {
	const jobID, movieID = "job-lock", "MOV-LOCK"

	first := AcquirePosterSourceLock(jobID, movieID)

	second := make(chan func(), 1)
	go func() { second <- AcquirePosterSourceLock(jobID, movieID) }()

	select {
	case release := <-second:
		release()
		t.Fatal("a second acquire on the same key must block while the first is held")
	case <-time.After(100 * time.Millisecond):
	}

	// A different key (same job, other movie; other job, same movie) never
	// contends: per-poster granularity.
	otherMovie := AcquirePosterSourceLock(jobID, "MOV-OTHER")
	otherMovie()
	otherJob := AcquirePosterSourceLock("job-other", movieID)
	otherJob()

	first()

	select {
	case release := <-second:
		release()
	case <-time.After(2 * time.Second):
		t.Fatal("the blocked acquirer must proceed once the holder releases")
	}

	// Both holders gone: the entry must have been evicted.
	_, loaded := posterSourceLockEntries.Load(jobID + "\x00" + movieID)
	assert.False(t, loaded, "the refcounted entry must be evicted after the last release")
}

// assertPosterSourceLockFree proves no goroutine still holds the lock for
// (jobID, movieID): a fresh acquire must complete immediately. A leaked lock
// (a missed release on an error path) would deadlock future edits, so the
// acquisition runs in a goroutine with a bounded wait instead of hanging the
// test.
func assertPosterSourceLockFree(t *testing.T, jobID, movieID string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		release := AcquirePosterSourceLock(jobID, movieID)
		release()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("poster source lock for (%s, %s) was not released", jobID, movieID)
	}
}

// TestApplyFieldOverride_PosterSourceLockReleasedOnAllPaths is the
// deadlock-safety table for the override path: whether the override fails
// before the lock, fails inside the refresh, fails at persistence, or
// succeeds — for poster-source AND non-source field keys alike — the shared
// per-(jobID, movieID) lock must be free afterwards; a leak would deadlock
// every future poster edit for that movie.
func TestApplyFieldOverride_PosterSourceLockReleasedOnAllPaths(t *testing.T) {
	const (
		oldPoster = "https://old.example/poster.jpg"
		oldCover  = "https://old.example/cover.jpg"
		newPoster = "dmm-poster-url" // overrideFixture's dmm source URLs
		newCover  = "dmm-cover-url"
	)

	newFixture := func() (*jobEditorImpl, string) {
		je, _, resultID := overrideRefreshFixture(t, oldPoster, oldCover, newPoster, newCover)
		return je, resultID
	}

	t.Run("result not found never touches the lock", func(t *testing.T) {
		je, _ := newFixture()
		_, _, err := je.ApplyFieldOverride(context.Background(), "res-missing", "poster_url", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("unknown source releases the lock", func(t *testing.T) {
		je, resultID := newFixture()
		_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "nosuch")
		require.Error(t, err)
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("refresh failure releases the lock", func(t *testing.T) {
		je, resultID := newFixture()
		je.posterGen = &stubOverridePosterGen{err: errors.New("download failed")}
		_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "refresh poster after source change")
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("persistence failure after refresh releases the lock", func(t *testing.T) {
		je, resultID := newFixture()
		je.posterGen = &stubOverridePosterGen{}
		repo := mocks.NewMockMovieRepositoryInterface(t)
		repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
		je.movieRepo = repo
		_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "persist field override")
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("successful poster_url override releases the lock", func(t *testing.T) {
		je, resultID := newFixture()
		je.posterGen = &stubOverridePosterGen{}
		updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, newPoster, updated.Movie.Poster.PosterURL)
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("cover_url override behind an explicit poster releases the lock", func(t *testing.T) {
		je, resultID := newFixture()
		updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "cover_url", "dmm")
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, newCover, updated.Movie.Poster.CoverURL)
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("empty movie ID falls back to the FileMatchInfo key", func(t *testing.T) {
		movie, prov := overrideFixture()
		movie.ID = "" // no stored movie ID: the match-info movie ID keys the lock
		movie.Poster.PosterURL = oldPoster
		movie.Poster.CoverURL = ""
		filePath := "test-empty-id.mp4"
		tracker := resultstore.New(1, []string{filePath})
		tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
			ResultID:      "res-empty-id",
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "MATCH-001"},
			Movie:         movie,
			Status:        models.JobStatusCompleted,
		})
		tracker.SetProvenance(filePath, prov)

		je := &jobEditorImpl{store: tracker, jobID: "job-empty", posterGen: &stubOverridePosterGen{}}
		updated, _, err := je.ApplyFieldOverride(context.Background(), "res-empty-id", "poster_url", "dmm")
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, newPoster, updated.Movie.Poster.PosterURL)
		assertPosterSourceLockFree(t, "job-empty", "MATCH-001")
	})

	t.Run("successful non-source override releases the lock", func(t *testing.T) {
		je, resultID := newFixture()
		updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, "DMM Studio", updated.Movie.Maker)
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("non-source persist failure releases the lock", func(t *testing.T) {
		je, resultID := newFixture()
		repo := mocks.NewMockMovieRepositoryInterface(t)
		repo.On("Upsert", mock.Anything, mock.Anything).Return(nil, errors.New("db down"))
		je.movieRepo = repo
		_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "persist field override")
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("result vanishing after the lock re-read releases the lock", func(t *testing.T) {
		je, store, resultID := overrideRefreshFixture(t, oldPoster, oldCover, newPoster, newCover)
		// First lookup (pre-lock existence check) succeeds, the post-lock
		// re-read misses: the result went away while this call waited on the
		// shared per-(job, movie) lock.
		je.store = &vanishingResultStore{Store: store, resultID: resultID, vanishOn: 2}
		_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assertPosterSourceLockFree(t, "job1", "ABC-001")
	})

	t.Run("non-poster fields also wait on the shared lock", func(t *testing.T) {
		je, resultID := newFixture()
		// Hold the shared poster lock from the test goroutine: EVERY whole-
		// movie-writing override (title, maker, ...) must wait for it — this is
		// Finding B's lock hoist: a manual crop holding the lock must never
		// race an override's clone→persist and get erased by the stale clone.
		release := AcquirePosterSourceLock("job1", "ABC-001")
		done := make(chan error, 1)
		go func() {
			_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
			done <- err
		}()
		select {
		case err := <-done:
			release()
			t.Fatalf("maker override completed (%v) while the shared poster-source lock was held", err)
		case <-time.After(150 * time.Millisecond):
		}
		release()
		require.NoError(t, <-done)
	})
}

// TestApplyFieldOverride_NonSourceOverrideSerializesWithManualCrop pins
// Finding B: a manual crop and a NON-source field override (maker) run
// concurrently against the same (job, movie). The crop side mirrors the
// manual-crop endpoint's serialization (updateBatchMoviePosterCrop holds the
// shared per-(job, movie) lock across the state update); the override path
// holds the same lock across its whole-movie read-clone-mutate-write. A slow
// DB upsert widens the override's clone→persist window so that, without the
// lock, the ordering is deterministic: the override clones the movie BEFORE
// the crop persists its bounds and then UpdateMovie writes the stale clone,
// erasing the successful crop. With the lock held by every override and the
// post-lock re-read, BOTH edits persist on the final movie.
func TestApplyFieldOverride_NonSourceOverrideSerializesWithManualCrop(t *testing.T) {
	for round := 0; round < 3; round++ {
		movie, prov := overrideFixture()
		movieID := fmt.Sprintf("CRP-%03d", round)
		movie.ID = movieID
		filePath := "test-crop-race-" + movieID + ".mp4"
		resultID := "res-crop-race-" + movieID
		jobID := "job-crop-race-" + movieID
		tracker := resultstore.New(1, []string{filePath})
		tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
			ResultID:      resultID,
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Movie:         movie,
			Status:        models.JobStatusCompleted,
		})
		tracker.SetProvenance(filePath, prov)

		// Slow the DB upsert: pre-fix this makes the stale whole-movie write
		// land AFTER the crop's persist every time (override enters Upsert at
		// ~t+1ms, sleeps 15ms; crop persists at ~t+6ms).
		repo := mocks.NewMockMovieRepositoryInterface(t)
		repo.On("Upsert", mock.Anything, mock.Anything).
			Run(func(mock.Arguments) { time.Sleep(15 * time.Millisecond) }).
			Return(&models.Movie{}, nil)

		je := &jobEditorImpl{
			store:        tracker,
			posterEditor: NewPosterEditor(tracker, tracker, nil),
			jobID:        jobID,
			movieRepo:    repo,
		}

		bounds := &models.CropBounds{X: 10, Y: 10, Width: 400, Height: 600, ImageWidth: 1000, ImageHeight: 1500}
		cropErr := make(chan error, 1)
		overrideErr := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		// Manual-crop path: hold the shared lock across the state update,
		// exactly like updateBatchMoviePosterCrop. The sleep widens its
		// lock-hold so the override's pre-fix stale write lands after the
		// persist (post-fix the lock makes the ordering irrelevant).
		go func() {
			defer wg.Done()
			release := AcquirePosterSourceLock(jobID, movieID)
			defer release()
			time.Sleep(5 * time.Millisecond)
			cropErr <- je.UpdatePosterCrop(movieID, "/tmp/cropped.jpg", bounds)
		}()
		go func() {
			defer wg.Done()
			// Head start for the crop goroutine to win AcquirePosterSourceLock
			// first — the deterministic pre-fix interleave.
			time.Sleep(time.Millisecond)
			_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
			overrideErr <- err
		}()
		wg.Wait()

		require.NoError(t, <-cropErr, "round %d", round)
		require.NoError(t, <-overrideErr, "round %d", round)

		final, _, ok := tracker.GetFileResultByResultID(resultID)
		require.True(t, ok)
		require.NotNil(t, final.Movie)
		assert.Equal(t, "DMM Studio", final.Movie.Maker, "round %d: the maker override must survive the concurrent crop", round)
		require.NotNil(t, final.Movie.Poster.CropBounds,
			"round %d: the crop bounds must survive the concurrent non-source override (no lost update)", round)
		assert.Equal(t, 1000, final.Movie.Poster.CropBounds.ImageWidth)
		assert.Equal(t, "/tmp/cropped.jpg", final.Movie.Poster.CroppedPosterURL)

		finalProv := tracker.GetProvenance(filePath)
		require.NotNil(t, finalProv)
		assert.Equal(t, "dmm", finalProv.FieldSources["maker"], "round %d", round)
		assertPosterSourceLockFree(t, jobID, movieID)
	}
}
