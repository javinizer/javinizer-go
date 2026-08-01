package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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
// succeeds, the shared per-(jobID, movieID) lock must be free afterwards —
// a leak would deadlock every future poster edit for that movie.
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

	t.Run("non-poster fields do not take the shared lock", func(t *testing.T) {
		je, resultID := newFixture()
		// Hold the shared poster lock from the test goroutine: an unrelated
		// field override must complete without waiting for it.
		release := AcquirePosterSourceLock("job1", "ABC-001")
		defer release()
		updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, "DMM Studio", updated.Movie.Maker)
	})
}
