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
			// Per-part identity fields, populated from each part's own
			// FileMatchInfo the way scrapeResultToMovieResult does: the fan-out
			// tests prove these survive an override instead of being stamped
			// with the SELECTED part's values.
			OriginalFileName: fp,
			Description:      "stored description of " + fp,
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
// the selected source carried identically (every part's poster state is
// synchronized from the selected clone, post-SyncCropIntentWithSource, so
// CropBounds/ShouldCropPoster intent AND the generator-stamped
// CroppedPosterURL stay consistent across parts), the old crop bounds
// cleared everywhere, the shared -full.jpg refresh invoked exactly once (one
// movie-wide asset for N parts), and provenance fanned out so every part
// attributes the override to the chosen source. While poster state is
// shared, per-part identity fields are NOT: each sibling keeps its own
// FileMatchInfo-derived OriginalFileName (template <FILENAME>/NFO original
// path input) and its own non-overridden fields (merge, not clone).
func TestApplyFieldOverride_MultipartPosterOverrideSyncsAllParts(t *testing.T) {
	je, tracker, resultID, part1, part2 := multipartOverrideFixture(t, "AUD-001", "https://old.example/poster.jpg", "", false)
	gen := &stubOverridePosterGen{stampCroppedURL: "fresh-preview-url"}
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
		assert.Equal(t, "fresh-preview-url", res.Movie.Poster.CroppedPosterURL,
			"part %s: the refreshed preview URL must reach every part (it is stamped by the refresh, "+
				"not by applyFieldOverride, so without the poster-state sync the sibling would keep the stale preview)", fp)
		assert.Equal(t, fp, res.Movie.OriginalFileName,
			"part %s must keep its OWN FileMatchInfo-derived file name — a wholesale clone fan-out "+
				"stamps the selected part's name onto siblings, and template contexts render <FILENAME> from it", fp)
		assert.Equal(t, "stored description of "+fp, res.Movie.Description,
			"part %s must keep non-overridden fields from its OWN stored snapshot", fp)
		prov := tracker.GetProvenance(fp)
		require.NotNil(t, prov, "part %s: provenance must fan out with the movie", fp)
		assert.Equal(t, "dmm", prov.FieldSources["poster_url"], "part %s provenance", fp)
	}
	assert.Equal(t, 1, gen.calls, "the refresh targets the movie-wide shared cache exactly once")
	assert.Equal(t, "AUD-001", gen.movieID)
	assert.Equal(t, "dmm-poster-url", gen.posterURL)
	assertPosterSourceLockFree(t, "job-mp", "AUD-001")
}

// TestApplyFieldOverride_MultipartTitleOverridePreservesPerPartIdentity pins
// the merge-not-clone fan-out for a NON-poster field (Codex: "preserve
// per-part fields during override fan-out"): the overridden title (and its
// linked DisplayTitle) converges on every part, while each part keeps its
// own FileMatchInfo-derived OriginalFileName and its own untouched
// Description from its OWN snapshot — a wholesale clone of the selected part
// would stamp CD1's values onto CD2, and template contexts
// (internal/template/context.go) would render the sibling's <FILENAME>/NFO
// original path with the wrong source file. Poster state is untouched by a
// title override: the sync mirrors the selected part's retained crop.
func TestApplyFieldOverride_MultipartTitleOverridePreservesPerPartIdentity(t *testing.T) {
	je, tracker, resultID, part1, part2 := multipartOverrideFixture(t, "TTL-001", "https://old.example/poster.jpg", "", false)

	updated, _, err := je.ApplyFieldOverride(context.Background(), resultID, "title", "dmm")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "DMM Title", updated.Movie.Title)

	for _, fp := range []string{part1, part2} {
		res, getErr := tracker.GetMovieResult(fp)
		require.NoError(t, getErr)
		require.NotNil(t, res.Movie)
		assert.Equal(t, "DMM Title", res.Movie.Title, "part %s gains the overridden title", fp)
		assert.Equal(t, "DMM Title", res.Movie.DisplayTitle,
			"part %s: display title follows the title override", fp)
		assert.Equal(t, fp, res.Movie.OriginalFileName,
			"part %s keeps its own file name", fp)
		assert.Equal(t, "stored description of "+fp, res.Movie.Description,
			"part %s keeps non-overridden fields from its OWN snapshot", fp)
		assert.NotNil(t, res.Movie.Poster.CropBounds,
			"part %s: a title override leaves poster state alone — the recorded crop survives", fp)
	}
	assertPosterSourceLockFree(t, "job-mp", "TTL-001")
}

// TestMergeOverrideOntoPart unit-covers the fan-out merge: identity fields
// come from the PART, the poster state (incl. the generator-stamped
// CroppedPosterURL and cleared bounds) is mirrored from the SELECTED clone,
// neither input is mutated, and an applyFieldOverride failure propagates
// (the ApplyFieldOverride wrapper aborts before any persistence on it).
func TestMergeOverrideOntoPart(t *testing.T) {
	_, prov := overrideFixture()
	part := &models.Movie{
		ID:               "MRG-001",
		Title:            "old part title",
		OriginalFileName: "part2.mp4",
		Description:      "part2's own description",
		Poster:           models.PosterState{PosterURL: "old-url", CropBounds: &models.CropBounds{X: 1}},
	}
	selected := &models.Movie{
		ID:               "MRG-001",
		Title:            "DMM Title",
		OriginalFileName: "part1.mp4",
		Description:      "the selected part's description",
		Poster:           models.PosterState{PosterURL: "dmm-poster-url", CroppedPosterURL: "fresh-preview-url", ShouldCropPoster: true},
	}

	merged, err := mergeOverrideOntoPart(part, selected, prov, "title", "dmm")
	require.NoError(t, err)
	assert.Equal(t, "DMM Title", merged.Title, "the overridden field lands on the part's own snapshot")
	assert.Equal(t, "part2.mp4", merged.OriginalFileName, "per-part identity fields survive")
	assert.Equal(t, "part2's own description", merged.Description, "non-overridden fields are the part's own")
	assert.Equal(t, "dmm-poster-url", merged.Poster.PosterURL, "poster source mirrors the selected part")
	assert.Equal(t, "fresh-preview-url", merged.Poster.CroppedPosterURL)
	assert.Nil(t, merged.Poster.CropBounds, "the selected part's cleared bounds mirror over the part's stale crop")
	assert.NotSame(t, part, merged)
	// Neither input was mutated.
	assert.Equal(t, "old part title", part.Title)
	require.NotNil(t, part.Poster.CropBounds)

	_, err = mergeOverrideOntoPart(part, selected, prov, "no-such-field", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported field")
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

// TestPlanMultipartOverride unit-covers the fan-out planner: each part's
// write carries the per-part merged movie (its own identity fields), a part
// whose stored snapshot is unreadable falls back to the wholesale clone
// with a nil revert snapshot, and a (hypothetical — the public path already
// applied the same override successfully) merge failure aborts planning
// before any persistence.
func TestPlanMultipartOverride(t *testing.T) {
	je, tracker, _, part1, part2 := multipartOverrideFixture(t, "PLN-001", "https://old.example/poster.jpg", "", false)
	selected, err := tracker.GetMovieResult(part1)
	require.NoError(t, err)
	_, prov := overrideFixture()

	planned, err := je.planMultipartOverride([]string{part1, part2}, selected.Movie, prov, "title", "dmm")
	require.NoError(t, err)
	require.Len(t, planned, 2)
	for _, part := range planned {
		assert.Equal(t, "DMM Title", part.movie.Title)
		assert.Equal(t, part.filePath, part.movie.OriginalFileName,
			"part %s keeps its own file name in the planned write", part.filePath)
		require.NotNil(t, part.original)
		assert.NotSame(t, part.original, part.movie)
	}

	// An unreadable snapshot plans the wholesale clone with nil original.
	je.store = &overrideFailStore{Store: tracker, lookupErrPath: part2}
	planned, err = je.planMultipartOverride([]string{part1, part2}, selected.Movie, prov, "title", "dmm")
	require.NoError(t, err)
	require.Len(t, planned, 2)
	assert.Nil(t, planned[1].original)
	assert.Same(t, selected.Movie, planned[1].movie)

	// A merge failure aborts the whole plan with a part-attributed error.
	_, err = je.planMultipartOverride([]string{part1}, selected.Movie, prov, "no-such-field", "dmm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merge field override onto part")
	assert.Contains(t, err.Error(), part1)
}

// TestApplyFieldOverride_MultipartMergeFailureAbortsBeforePersist pins the
// planner-error guard in ApplyFieldOverride via the synth-provenance
// fallback: with NO raw ScraperResults, applyFieldOverride synthesizes the
// selectable source from each part's own cached movie
// (ScraperResultFromCachedMovie keys it off Movie.SourceName). A sibling
// whose cached movie names a DIFFERENT source therefore fails the merge
// after the selected part already validated, and the override must abort
// with a part-attributed error BEFORE any part is persisted (the per-part
// movies are planned ahead of the UpdateMovie loop) — and the
// poster-source lock still released.
func TestApplyFieldOverride_MultipartMergeFailureAbortsBeforePersist(t *testing.T) {
	const movieID = "MRG-X1"
	const part1, part2 = "part1.mp4", "part2.mp4"
	tracker := resultstore.New(1, []string{part1, part2})
	for fp, sourceName := range map[string]string{part1: "", part2: "other-scraper"} {
		resID := "res-mrg-1"
		if fp == part2 {
			resID = "res-mrg-2"
		}
		tracker.UpdateFileResult(fp, &resultstore.MovieResult{
			ResultID:      resID,
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: movieID},
			Movie:         &models.Movie{ID: movieID, Title: "Cached Title", SourceName: sourceName},
			Status:        models.JobStatusCompleted,
		})
		tracker.SetProvenance(fp, &resultstore.ProvenanceData{})
	}
	je := &jobEditorImpl{store: tracker, jobID: "job-mrg"}

	updated, _, err := je.ApplyFieldOverride(context.Background(), "res-mrg-1", "title", "scraper")
	require.Error(t, err)
	assert.Nil(t, updated)
	assert.Contains(t, err.Error(), "merge field override onto part")
	assert.Contains(t, err.Error(), part2)
	assert.Contains(t, err.Error(), "did not contribute")

	for _, fp := range []string{part1, part2} {
		res, getErr := tracker.GetMovieResult(fp)
		require.NoError(t, getErr)
		assert.Equal(t, "Cached Title", res.Movie.Title,
			"part %s must be untouched — the abort happens before any persistence", fp)
	}
	assertPosterSourceLockFree(t, "job-mrg", movieID)
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
