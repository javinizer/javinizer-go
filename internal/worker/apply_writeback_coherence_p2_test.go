package worker

// POSTER-WRITE-HARDENING P2 — Apply-phase coherence red suite (D5).
//
// NOTE: P1's 40-round review pre-landed the write-back merge machinery
// (mergeLiveReviewEdits / applyFamilyLock / identity no-op guards), so the
// three tests below are regression LOCK-INS: they pin the D5 scenarios the
// phase gates depend on. They must stay green; a future refactor that breaks
// them breaks the P2 contract.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Seam presence contract: apply write-backs acquire the per-movie edit key
// through applyFamilyLock, which routes BOTH identity spellings through the
// registry's one total order; a nil seam degrades to a no-op release.
func TestApplyPhaseInputs_ExposesEditLockForMovie(t *testing.T) {
	var acquired []string
	released := false
	inputs := applyPhaseInputs{EditLockFn: func(ids ...string) func() {
		acquired = append(acquired, ids...)
		return func() { released = true }
	}}
	unlock := applyFamilyLock(inputs, "ALIAS-9", "CANON-9")
	unlock()
	assert.Equal(t, []string{"ALIAS-9", "CANON-9"}, acquired, "both identity keys flow into the seam")
	assert.True(t, released, "release returned and honored")

	applyFamilyLock(applyPhaseInputs{}, "A", "B")() // nil seam ⇒ no-op, must not panic
}

// D5 identity-mismatch rule: a rekey mid-apply makes the phase output stale
// for the live family — success AND failure write-backs no-op WITHOUT a
// revision bump (a blind no-op inside AtomicUpdate would still bump it).
func TestApplyWriteBack_IdentityMismatchIsNoop(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store := resultstore.New(1, []string{"/f/a.mp4"})
		store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
			ResultID:      "res-s",
			Status:        models.JobStatusRunning,
			Movie:         &models.Movie{ID: "NEW-1", Title: "rekeyed-live"},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"},
		})
		before, err := store.GetMovieResult("/f/a.mp4")
		require.NoError(t, err)
		inputs := minimalApplyInputs(t, store, true)
		afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"}}
		result := &workflow.ApplyResult{Movie: &models.Movie{ID: "OLD-1", Title: "phase-stale"}}
		interpretApplyResult("/f/a.mp4", &models.Movie{ID: "OLD-1"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, result, nil)
		final, ferr := store.GetMovieResult("/f/a.mp4")
		require.NoError(t, ferr)
		assert.Equal(t, before.Revision, final.Revision, "mismatch success write-back: no revision bump")
		assert.Equal(t, "rekeyed-live", final.Movie.Title, "stale phase metadata never overlays the rekeyed row")
	})

	t.Run("failure", func(t *testing.T) {
		store := resultstore.New(1, []string{"/f/a.mp4"})
		store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
			ResultID:      "res-f",
			Status:        models.JobStatusRunning,
			Movie:         &models.Movie{ID: "NEW-1", Title: "rekeyed-live"},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"},
		})
		before, err := store.GetMovieResult("/f/a.mp4")
		require.NoError(t, err)
		inputs := minimalApplyInputs(t, store, true)
		afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"}}
		outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "OLD-1"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, nil, errors.New("engine wedged"))
		assert.True(t, outcome.Failed)
		final, ferr := store.GetMovieResult("/f/a.mp4")
		require.NoError(t, ferr)
		assert.Equal(t, before.Revision, final.Revision, "mismatch failure write-back: no revision bump")
		assert.Equal(t, "rekeyed-live", final.Movie.Title)
		assert.Equal(t, models.JobStatusRunning, final.Status, "failure status not stamped onto the rekeyed row")
	})

	t.Run("panic", func(t *testing.T) {
		store := resultstore.New(1, []string{"/f/a.mp4"})
		store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
			ResultID:      "res-p",
			Status:        models.JobStatusRunning,
			Movie:         &models.Movie{ID: "NEW-1", Title: "rekeyed-live"},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"},
		})
		before, err := store.GetMovieResult("/f/a.mp4")
		require.NoError(t, err)
		outcome := &applyFileOutcome{}
		rc := recoveryContext{
			filePath: "/f/a.mp4",
			fmi:      models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"},
			movie:    &models.Movie{ID: "OLD-1", Title: "phase-stale"},
			updater:  store,
		}
		recoverFn := withFileRecovery(rc, outcome)
		func() {
			defer recoverFn()
			panic("boom")
		}()
		assert.True(t, outcome.Failed)
		final, ferr := store.GetMovieResult("/f/a.mp4")
		require.NoError(t, ferr)
		assert.Equal(t, before.Revision, final.Revision, "mismatch panic write-back: no revision bump")
		assert.Equal(t, "rekeyed-live", final.Movie.Title)
	})
}

// D5 headline scenario: a review edit committed mid-apply survives EVERY
// write-back path (success, failure, panic-recovery). Editable-set coverage:
// crop geometry (Poster payload) AND title — live drift beats the phase's
// stale value; phase-computed state always stays phase-side.
func TestApplyPhase_PostApplyWriteBack_PreservesConcurrentReviewEdits(t *testing.T) {
	const path = "/f/wb.mp4"
	baselineMovie := func() *models.Movie {
		return &models.Movie{ID: "WB-1", Title: "phase-entry-title", Poster: models.PosterState{PosterURL: "https://s/poster.jpg"}}
	}
	seedLiveEdit := func(t *testing.T) resultstore.Store {
		// The user edited title + crop geometry mid-phase (drift vs baseline).
		store := resultstore.New(1, []string{path})
		store.UpdateFileResult(path, &resultstore.MovieResult{
			ResultID: "res-wb",
			Status:   models.JobStatusRunning,
			Movie: &models.Movie{ID: "WB-1", Title: "user-edit", Poster: models.PosterState{
				PosterURL: "https://s/poster.jpg", CroppedPosterURL: "wb-cropped.jpg",
				PosterCropBounds: &models.CropBounds{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5},
			}},
			FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: "WB-1"},
		})
		return store
	}
	afcFor := func() *ApplyFileContext {
		return &ApplyFileContext{FilePath: path, Match: models.FileMatchInfo{Path: path, MovieID: "WB-1"}}
	}
	assertEditsPreserved := func(t *testing.T, store resultstore.Store) *resultstore.MovieResult {
		t.Helper()
		final, err := store.GetMovieResult(path)
		require.NoError(t, err)
		require.NotNil(t, final.Movie)
		assert.Equal(t, "user-edit", final.Movie.Title, "mid-apply review edit survives the write-back")
		require.NotNil(t, final.Movie.Poster.PosterCropBounds, "committed geometry survives")
		assert.Equal(t, 0.5, final.Movie.Poster.PosterCropBounds.Width)
		assert.Equal(t, "wb-cropped.jpg", final.Movie.Poster.CroppedPosterURL)
		return final
	}

	t.Run("success", func(t *testing.T) {
		store := seedLiveEdit(t)
		inputs := minimalApplyInputs(t, store, true)
		// Phase-side output: a stale-metadata movie computed from the baseline,
		// carrying phase-computed description edits the user never made.
		phaseOut := baselineMovie()
		result := &workflow.ApplyResult{Movie: phaseOut}
		interpretApplyResult(path, baselineMovie(), time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afcFor(), result, nil)
		final := assertEditsPreserved(t, store)
		assert.Equal(t, models.JobStatusRunning, final.Status)
	})

	t.Run("failure", func(t *testing.T) {
		store := seedLiveEdit(t)
		inputs := minimalApplyInputs(t, store, true)
		outcome := interpretApplyResult(path, baselineMovie(), time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afcFor(), nil, errors.New("engine wedged"))
		assert.True(t, outcome.Failed)
		final := assertEditsPreserved(t, store)
		assert.Equal(t, models.JobStatusFailed, final.Status, "failure status still writes")
	})

	t.Run("panic", func(t *testing.T) {
		store := seedLiveEdit(t)
		outcome := &applyFileOutcome{}
		rc := recoveryContext{
			filePath: path,
			fmi:      models.FileMatchInfo{Path: path, MovieID: "WB-1"},
			movie:    baselineMovie(),
			updater:  store,
		}
		recoverFn := withFileRecovery(rc, outcome)
		func() {
			defer recoverFn()
			panic("boom")
		}()
		assert.True(t, outcome.Failed)
		final := assertEditsPreserved(t, store)
		assert.Equal(t, models.JobStatusFailed, final.Status)
		assert.NotEmpty(t, final.Error)
	})
}
