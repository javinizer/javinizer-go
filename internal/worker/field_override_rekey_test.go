package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overrideRekeyStore wraps a resultstore.Store to signal after the FIRST
// GetFileResultByResultID for a target resultID — ApplyFieldOverride's
// pre-lock read — and to count every lookup of that resultID, so tests can
// pin exactly how many under-lock re-reads the movie-ID re-resolution loop
// performs (same signaling pattern as the api/batch signaledBatchJob).
type overrideRekeyStore struct {
	resultstore.Store
	resultID  string
	once      sync.Once
	firstRead chan struct{}
	calls     atomic.Int32
}

func (s *overrideRekeyStore) GetFileResultByResultID(resultID string) (*resultstore.MovieResult, string, bool) {
	result, filePath, found := s.Store.GetFileResultByResultID(resultID)
	if resultID == s.resultID {
		s.calls.Add(1)
		s.once.Do(func() { close(s.firstRead) })
	}
	return result, filePath, found
}

// TestApplyFieldOverride_ReResolvesMovieIDAfterRekeyUnderLock pins the Codex
// finding "re-resolve the fan-out ID after acquiring the lock": the override
// starts on movie A, waits on A's poster lock behind a rescrape that re-keys
// the selected result A→B, and must then follow the result to B — the
// fan-out (FindFilePathsForMovieID) targets B's results under B's lock, and
// nothing leaks onto the movies still indexed at A. Mirrors the crop
// endpoint's convergence loop (updateBatchMoviePosterCrop).
func TestApplyFieldOverride_ReResolvesMovieIDAfterRekeyUnderLock(t *testing.T) {
	const (
		jobID    = "job-override-rekey"
		movieA   = "REKEY-AAA"
		movieB   = "REKEY-BBB"
		fileA1   = "/lib/rekey-cd1.mp4"
		fileA2   = "/lib/rekey-cd2.mp4"
		fileB    = "/lib/rekey-existing.mp4"
		resultID = "res-rekey-target"
	)
	movie, prov := overrideFixture() // Maker "Orig Maker"; dmm contributes "DMM Studio"
	movie.ID = movieA
	tracker := resultstore.New(3, []string{fileA1, fileA2, fileB})
	tracker.UpdateFileResult(fileA1, &resultstore.MovieResult{
		ResultID:      resultID,
		FileMatchInfo: models.FileMatchInfo{Path: fileA1, MovieID: movieA},
		Movie:         movie,
		Status:        models.JobStatusCompleted,
	})
	tracker.UpdateFileResult(fileA2, &resultstore.MovieResult{
		ResultID:      "res-rekey-sibling",
		FileMatchInfo: models.FileMatchInfo{Path: fileA2, MovieID: movieA},
		Movie:         movie.Clone(),
		Status:        models.JobStatusCompleted,
	})
	existingB := movie.Clone()
	existingB.ID = movieB
	tracker.UpdateFileResult(fileB, &resultstore.MovieResult{
		ResultID:      "res-rekey-existing-b",
		FileMatchInfo: models.FileMatchInfo{Path: fileB, MovieID: movieB},
		Movie:         existingB,
		Status:        models.JobStatusCompleted,
	})
	tracker.SetProvenance(fileA1, prov)

	wrapped := &overrideRekeyStore{Store: tracker, resultID: resultID, firstRead: make(chan struct{})}
	je := &jobEditorImpl{store: wrapped, jobID: jobID}

	// The "rescrape" side: hold movie A's poster lock so the override's
	// pre-lock read completes and it then parks on A's lock — the exact
	// window in which a rescrape commits the A→B rekey.
	releaseA := AcquirePosterSourceLock(jobID, movieA)

	done := make(chan error, 1)
	go func() {
		_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
		done <- err
	}()

	select {
	case <-wrapped.firstRead:
	case <-time.After(2 * time.Second):
		releaseA()
		t.Fatal("override never performed its pre-lock read")
	}

	// The rescrape commits the rekey — the file's result now belongs to B —
	// mirroring CompleteRescrape's CommitResult write (Movie.ID AND
	// FileMatchInfo.MovieID both move).
	require.NoError(t, tracker.AtomicUpdateFileResult(fileA1, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		m := current.Movie.Clone()
		m.ID = movieB
		current.Movie = m
		current.FileMatchInfo.MovieID = movieB
		return current, nil
	}))

	// Pin the release-before-acquire handoff onto the DESTINATION lock: with
	// B's lock held from this goroutine, the re-resolving override must stay
	// parked even after A's lock is released — it can only proceed once B is
	// free, proving the read/plan/persist sequence runs under B's lock and
	// that the override never holds A and B at once in the wrong order.
	releaseB := AcquirePosterSourceLock(jobID, movieB)
	releaseA()

	select {
	case err := <-done:
		releaseB()
		t.Fatalf("override completed (%v) while the destination movie's poster lock was held — it must re-resolve to B", err)
	case <-time.After(150 * time.Millisecond):
	}
	releaseB()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("override did not finish after the destination lock was released")
	}

	// The override landed on the rekeyed target...
	target, _, found := tracker.GetFileResultByResultID(resultID)
	require.True(t, found)
	require.NotNil(t, target.Movie)
	assert.Equal(t, movieB, target.Movie.ID)
	assert.Equal(t, "DMM Studio", target.Movie.Maker)

	// ...fanned out to the PRE-EXISTING movie-B result (B's family, resolved
	// from the post-rekey read)...
	destFamily, err := tracker.GetMovieResult(fileB)
	require.NoError(t, err)
	assert.Equal(t, "DMM Studio", destFamily.Movie.Maker,
		"the fan-out must target the results indexed at the RE-RESOLVED movie ID B")

	// ...and NOT onto the sibling still indexed at A.
	sibling, err := tracker.GetMovieResult(fileA2)
	require.NoError(t, err)
	assert.Equal(t, "Orig Maker", sibling.Movie.Maker,
		"no part of the override may leak onto movies still indexed at the stale ID A")

	// Exactly four lookups: pre-lock, under A (rekey observed), under B
	// (converged), and the post-persist confirmation read.
	assert.Equal(t, int32(4), wrapped.calls.Load(),
		"re-resolution must add exactly one under-lock re-read")

	assertPosterSourceLockFree(t, jobID, movieA)
	assertPosterSourceLockFree(t, jobID, movieB)
}

// TestApplyFieldOverride_UnchangedMovieIDHoldsSingleLock is the unchanged-ID
// companion: when the under-lock re-read yields the SAME movie ID, the
// override completes under the original lock with exactly one re-read — no
// release/re-acquire handoff, no extra lookups.
func TestApplyFieldOverride_UnchangedMovieIDHoldsSingleLock(t *testing.T) {
	const (
		jobID    = "job-override-stable"
		movieA   = "STABLE-AAA"
		fileA    = "/lib/stable.mp4"
		resultID = "res-stable"
	)
	movie, prov := overrideFixture()
	movie.ID = movieA
	tracker := resultstore.New(1, []string{fileA})
	tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
		ResultID:      resultID,
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Movie:         movie,
		Status:        models.JobStatusCompleted,
	})
	tracker.SetProvenance(fileA, prov)
	wrapped := &overrideRekeyStore{Store: tracker, resultID: resultID, firstRead: make(chan struct{})}
	je := &jobEditorImpl{store: wrapped, jobID: jobID}

	releaseA := AcquirePosterSourceLock(jobID, movieA)
	done := make(chan error, 1)
	go func() {
		_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "maker", "dmm")
		done <- err
	}()
	select {
	case <-wrapped.firstRead:
	case <-time.After(2 * time.Second):
		releaseA()
		t.Fatal("override never performed its pre-lock read")
	}
	// Parked on A's lock: an unchanged movie ID means there is no destination
	// handoff — nothing may complete while A's lock is held.
	select {
	case err := <-done:
		releaseA()
		t.Fatalf("override finished (%v) while A's poster lock was held", err)
	case <-time.After(150 * time.Millisecond):
	}
	releaseA()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("override did not finish after A's lock was released")
	}

	final, _, found := tracker.GetFileResultByResultID(resultID)
	require.True(t, found)
	require.NotNil(t, final.Movie)
	assert.Equal(t, "DMM Studio", final.Movie.Maker)
	// Three lookups: pre-lock, the single under-lock re-read (converged on
	// the first iteration), and the post-persist confirmation read.
	assert.Equal(t, int32(3), wrapped.calls.Load(),
		"an unchanged movie ID must not add re-reads or a lock handoff")
	assertPosterSourceLockFree(t, jobID, movieA)
}
