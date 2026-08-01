package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// multipartOverrideFixture builds a jobEditorImpl over a TWO-part completed
// result (part1/part2 sharing movieID) whose movies carry the given current
// poster/cover URLs, a recorded crop, and per-part provenance whose "dmm"
// raw result contributes the standard override URLs. Mirrors
// overrideRefreshFixture but for the multipart fan-out: the selected result
// is res-cd1 (part1), the sibling is res-cd2 (part2).
func multipartOverrideFixture(t *testing.T, movieID, currentPosterURL, currentCoverURL string, shouldCrop bool) (*jobEditorImpl, resultstore.Store, string, string, string) {
	t.Helper()
	const (
		part1   = "part1.mp4"
		part2   = "part2.mp4"
		res_cd1 = "res-cd1"
	)
	_, prov := overrideFixture()
	tracker := resultstore.New(1, []string{part1, part2})
	for _, fp := range []string{part1, part2} {
		m := &models.Movie{
			ID: movieID,
			Poster: models.PosterState{
				PosterURL:        currentPosterURL,
				CoverURL:         currentCoverURL,
				ShouldCropPoster: shouldCrop,
				CropBounds:       &models.CropBounds{X: 0, Y: 0, Width: 100, Height: 200},
			},
		}
		resultID := res_cd1
		if fp == part2 {
			resultID = "res-cd2"
		}
		tracker.UpdateFileResult(fp, &resultstore.MovieResult{
			ResultID:      resultID,
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: movieID},
			Movie:         m,
			Status:        models.JobStatusCompleted,
		})
		tracker.SetProvenance(fp, prov)
	}
	return &jobEditorImpl{store: tracker, jobID: "job-mp"}, tracker, res_cd1, part1, part2
}

// overrideFailStore wraps a resultstore.Store so tests can fail UpdateMovie
// selectively per part (and per movie state, to distinguish the update write
// from a compensation revert) and fail the pre-update GetMovieResult lookup
// for one part.
type overrideFailStore struct {
	resultstore.Store
	// failUpdate, when non-nil for a path, is consulted on UpdateMovie for
	// that path; its error is returned instead of persisting.
	failUpdate map[string]func(m *models.Movie) error
	// lookupErrPath makes GetMovieResult fail for this path (simulating a
	// vanished original snapshot).
	lookupErrPath string
}

func (s *overrideFailStore) UpdateMovie(filePath string, m *models.Movie) error {
	if pred, ok := s.failUpdate[filePath]; ok {
		if err := pred(m); err != nil {
			return err
		}
	}
	return s.Store.UpdateMovie(filePath, m)
}

func (s *overrideFailStore) GetMovieResult(filePath string) (*resultstore.MovieResult, error) {
	if filePath == s.lookupErrPath {
		return nil, errors.New("injected lookup failure")
	}
	return s.Store.GetMovieResult(filePath)
}

// noIndexStore hides the movie-ID index so ApplyFieldOverride's
// FindFilePathsForMovieID returns nothing and the single-path fallback runs.
type noIndexStore struct{ resultstore.Store }

func (noIndexStore) FindFilePathsForMovieID(string) []string { return nil }

// TestApplyFieldOverride_MultipartPosterOverrideSyncsAllParts pins the
// Finding-A fix: a poster_url override on part 1 of a 2-part movie must
// persist the new source to BOTH parts, with the crop intent re-derived from
// the selected source carried identically (every part receives the same
// clone, post-SyncCropIntentWithSource), the old crop bounds cleared
// everywhere, the shared -full.jpg refresh invoked exactly once (one
// movie-wide asset for N parts), and provenance fanned out so every part
// attributes the override to the chosen source.
func TestApplyFieldOverride_MultipartPosterOverrideSyncsAllParts(t *testing.T) {
	je, tracker, resultID, part1, part2 := multipartOverrideFixture(t, "AUD-001", "https://old.example/poster.jpg", "", false)
	gen := &stubOverridePosterGen{}
	je.posterGen = gen

	updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.NoError(t, err)
	require.NotNil(t, updated)

	for _, fp := range []string{part1, part2} {
		res, getErr := tracker.GetMovieResult(fp)
		require.NoError(t, getErr)
		require.NotNil(t, res.Movie)
		assert.Equal(t, "dmm-poster-url", res.Movie.Poster.PosterURL,
			"part %s must persist the overridden poster source — a sibling left at the old URL "+
				"would share the refreshed -full.jpg and hand the downloader the stale source", fp)
		assert.Nil(t, res.Movie.Poster.CropBounds,
			"part %s: the source change must clear crop bounds measured against the old image", fp)
		assert.True(t, res.Movie.Poster.ShouldCropPoster,
			"part %s: the selected dmm source's crop intent (true) must travel with the image", fp)
		prov := tracker.GetProvenance(fp)
		require.NotNil(t, prov, "part %s: provenance must fan out with the movie", fp)
		assert.Equal(t, "dmm", prov.FieldSources["poster_url"], "part %s provenance", fp)
	}
	assert.Equal(t, 1, gen.calls, "the refresh targets the movie-wide shared cache exactly once")
	assert.Equal(t, "AUD-001", gen.movieID)
	assert.Equal(t, "dmm-poster-url", gen.posterURL)
	assertPosterSourceLockFree(t, "job-mp", "AUD-001")
}

// TestApplyFieldOverride_MultipartCoverOverrideSyncsAllParts is the
// sibling-cover variant of Finding A: on a cover-backed movie (no poster
// URL), a cover_url override changes the effective poster source — both
// parts must converge on the new cover with cover-backed crop intent
// restored, and the shared cache refresh must fire once.
func TestApplyFieldOverride_MultipartCoverOverrideSyncsAllParts(t *testing.T) {
	je, tracker, resultID, part1, part2 := multipartOverrideFixture(t, "AUD-002", "", "https://old.example/cover.jpg", false)
	gen := &stubOverridePosterGen{}
	je.posterGen = gen

	updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "cover_url", "dmm")
	require.NoError(t, err)
	require.NotNil(t, updated)

	for _, fp := range []string{part1, part2} {
		res, getErr := tracker.GetMovieResult(fp)
		require.NoError(t, getErr)
		require.NotNil(t, res.Movie)
		assert.Equal(t, "dmm-cover-url", res.Movie.Poster.CoverURL, "part %s", fp)
		assert.Nil(t, res.Movie.Poster.CropBounds, "part %s", fp)
		assert.True(t, res.Movie.Poster.ShouldCropPoster,
			"part %s: a cover-backed source restores cover-backed crop intent on every part", fp)
		prov := tracker.GetProvenance(fp)
		require.NotNil(t, prov)
		assert.Equal(t, "dmm", prov.FieldSources["cover_url"], "part %s provenance", fp)
	}
	assert.Equal(t, 1, gen.calls, "the effective source changed exactly once for the shared asset")
	assertPosterSourceLockFree(t, "job-mp", "AUD-002")
}

// TestApplyFieldOverride_MultipartPersistFailureCompensatesAndRollsBack pins
// the compensation order: when the sibling part's persist fails after the
// selected part succeeded and the shared cache was already refreshed, the
// selected part is reverted to its pre-override movie BEFORE the poster cache
// snapshot is restored, so no part keeps the new source against the old
// cached image.
func TestApplyFieldOverride_MultipartPersistFailureCompensatesAndRollsBack(t *testing.T) {
	je, tracker, resultID, part1, part2 := multipartOverrideFixture(t, "AUD-003", "https://old.example/poster.jpg", "", false)
	gen := &stubOverrideSnapshotter{}
	je.posterGen = gen
	failedStore := &overrideFailStore{
		Store: tracker,
		failUpdate: map[string]func(m *models.Movie) error{
			part2: func(*models.Movie) error { return errors.New("sibling persist down") },
		},
	}
	je.store = failedStore

	updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Contains(t, err.Error(), "persist field override")
	assert.NotContains(t, err.Error(), "revert of part",
		"the selected part's revert must succeed")
	assert.NotContains(t, err.Error(), "poster rollback failed")

	// Part 1 was compensated back to its pre-override movie; part 2 never
	// persisted the override: NO part holds the new source the rollback just
	// erased from the cache.
	for _, fp := range []string{part1, part2} {
		res, getErr := tracker.GetMovieResult(fp)
		require.NoError(t, getErr)
		require.NotNil(t, res.Movie)
		assert.Equal(t, "https://old.example/poster.jpg", res.Movie.Poster.PosterURL,
			"part %s must be back at the old source after compensation", fp)
		require.NotNil(t, res.Movie.Poster.CropBounds,
			"part %s: the still-valid crop must survive the rejected override", fp)
	}
	assert.Equal(t, 1, gen.calls, "the refresh ran once before the sibling persist failed")
	assert.Equal(t, 1, gen.restoreCalls, "the refreshed cache must be rolled back after persistence failed")
	assertPosterSourceLockFree(t, "job-mp", "AUD-003")
}

// TestApplyFieldOverride_MultipartPersistFailureSkipsRevertWithoutOriginal
// covers the nil-original compensation branch: when the earlier part's
// pre-update snapshot could not be read (GetMovieResult error), the revert
// for that part is skipped entirely — the failure is still reported and the
// poster cache is still rolled back.
func TestApplyFieldOverride_MultipartPersistFailureSkipsRevertWithoutOriginal(t *testing.T) {
	je, tracker, resultID, part1, part2 := multipartOverrideFixture(t, "AUD-004", "https://old.example/poster.jpg", "", false)
	gen := &stubOverrideSnapshotter{}
	je.posterGen = gen
	failedStore := &overrideFailStore{
		Store:         tracker,
		lookupErrPath: part1,
		failUpdate: map[string]func(m *models.Movie) error{
			part2: func(*models.Movie) error { return errors.New("sibling persist down") },
		},
	}
	je.store = failedStore

	updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Contains(t, err.Error(), "persist field override")
	assert.NotContains(t, err.Error(), "revert of part")
	// With no original snapshot, part 1 cannot be reverted; it keeps the new
	// source but the cache is restored regardless — the desync is surfaced by
	// the error instead of being silently hidden.
	res, getErr := tracker.GetMovieResult(part1)
	require.NoError(t, getErr)
	assert.Equal(t, "dmm-poster-url", res.Movie.Poster.PosterURL)
	assert.Equal(t, 1, gen.restoreCalls)
	assertPosterSourceLockFree(t, "job-mp", "AUD-004")
}

// TestApplyFieldOverride_MultipartRevertFailureSurfaced covers the failed-
// compensation branch: when the earlier part's revert also fails, that
// failure must surface alongside the primary persist error (never swallowed),
// and the cache rollback still runs.
func TestApplyFieldOverride_MultipartRevertFailureSurfaced(t *testing.T) {
	const oldURL = "https://old.example/poster.jpg"
	je, tracker, resultID, part1, part2 := multipartOverrideFixture(t, "AUD-005", oldURL, "", false)
	gen := &stubOverrideSnapshotter{}
	je.posterGen = gen
	failedStore := &overrideFailStore{
		Store: tracker,
		failUpdate: map[string]func(m *models.Movie) error{
			part2: func(*models.Movie) error { return errors.New("sibling persist down") },
			// The update write carries the NEW URL; the compensation revert
			// re-persists the part's pre-override movie (old URL) — fail only
			// the revert by keying on the URL.
			part1: func(m *models.Movie) error {
				if m.Poster.PosterURL == oldURL {
					return errors.New("revert persist down")
				}
				return nil
			},
		},
	}
	je.store = failedStore

	_, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist field override")
	assert.Contains(t, err.Error(), "revert of part")
	assert.Contains(t, err.Error(), part1)
	assert.Equal(t, 1, gen.restoreCalls, "the cache rollback must still run even when a revert fails")
	assertPosterSourceLockFree(t, "job-mp", "AUD-005")
}

// TestApplyFieldOverride_UnindexedMovieIDFallsBackToSelectedPath covers the
// multipart loop's empty-index fallback: when the store has no movie-ID→paths
// entry (e.g. an unindexed movie), the override persists to just the selected
// result's file path instead of silently doing nothing.
func TestApplyFieldOverride_UnindexedMovieIDFallsBackToSelectedPath(t *testing.T) {
	je, tracker, resultID := overrideRefreshFixture(t, "https://old.example/poster.jpg", "", "dmm-poster-url", "")
	je.store = noIndexStore{Store: tracker}
	je.jobID = "job-noidx"
	je.posterGen = &stubOverridePosterGen{}

	updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "poster_url", "dmm")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "dmm-poster-url", updated.Movie.Poster.PosterURL)
	current, _, found := tracker.GetFileResultByResultID(resultID)
	require.True(t, found)
	assert.Equal(t, "dmm-poster-url", current.Movie.Poster.PosterURL)
	assertPosterSourceLockFree(t, "job-noidx", "ABC-001")
}
