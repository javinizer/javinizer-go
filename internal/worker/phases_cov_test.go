package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wfmocks "github.com/javinizer/javinizer-go/internal/mocks/workflow"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// StartScrape must NOT launch when the phase-entry marker fails to persist
// (D16 fail-closed); the terminal failure is retried durably.
func TestStartScrapeMarkerPersistFailureAborts(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	calls := 0
	job.deps.PersistFn = func() error { calls++; return errors.New("disk wedged") }
	err := job.Controller().StartScrape(context.Background(), []string{"/f/a.mp4"}, ScrapePhaseConfig{})
	require.ErrorContains(t, err, "persist phase-entry marker")
	assert.GreaterOrEqual(t, calls, 2, "abort persists twice: marker failure + terminal-state retry")
	assert.Equal(t, models.JobStatusFailed, job.lifecycle.GetJobStatus())
}

func TestStartScrapeGoneBarrierRejects(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.admission.MarkGone()
	err := job.Controller().StartScrape(context.Background(), []string{"/f/a.mp4"}, ScrapePhaseConfig{})
	require.ErrorIs(t, err, ErrJobGone)
}

func TestStartApplyGoneBarrierAndMarkerArms(t *testing.T) {
	t.Run("gone entry rejects", func(t *testing.T) {
		job := newBatchJob([]string{"/f/a.mp4"})
		job.lifecycle.Status = models.JobStatusCompleted
		job.admission.MarkGone()
		err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
		require.Error(t, err)
	})
	t.Run("marker persist failure aborts and retries", func(t *testing.T) {
		job := newBatchJob([]string{"/f/a.mp4"})
		job.lifecycle.Status = models.JobStatusCompleted
		job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
		calls := 0
		job.deps.PersistFn = func() error { calls++; return errors.New("disk wedged") }
		err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
		require.ErrorContains(t, err, "persist phase-entry marker")
		assert.GreaterOrEqual(t, calls, 2)
	})
}

// --- withFileRecovery panic path under edit lock ---

type panicOutcome struct{ msg string }

func (o *panicOutcome) setPanic(msg string) { o.msg = msg }

func TestWithFileRecoveryApplysExistingLockedPanicMerge(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-p1", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "PAN-1", Title: "live edit title"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PAN-1"},
	})
	var outcome = &panicOutcome{}
	locked := false
	rc := recoveryContext{
		filePath: "/f/a.mp4",
		fmi:      models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PAN-1"},
		movie:    &models.Movie{ID: "PAN-1", Title: "pre-phase frozen baseline"},
		updater:  store,
		editLockFn: func(movieID string) func() {
			assert.Equal(t, "PAN-1", movieID)
			locked = true
			return func() { locked = false }
		},
	}
	func2Call := withFileRecovery(rc, outcome)
	// invoke the recovery path by panicking after deferring
	func() {
		defer func2Call()
		panic("phase goroutine wedged")
	}()
	assert.NotEmpty(t, outcome.msg)
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusFailed, final.Status)
	// The live review edit survived the panic write-back (D5-lite).
	assert.Equal(t, "live edit title", final.Movie.Title, "mid-phase edits win review-editable fields on panic write-back")
	assert.False(t, locked, "the synthetic family key was released")
}

// --- three-way write-back merge arms ---

func TestMergeLiveReviewEditsArms(t *testing.T) {
	baseline := &models.Movie{ID: "M-1", Title: "b", Maker: "bm"}

	t.Run("phaseOut nil and live nil returns baseline clone", func(t *testing.T) {
		got := mergeLiveReviewEdits(baseline, nil, nil)
		require.NotSame(t, baseline, got)
		assert.Equal(t, "M-1", got.ID)
	})
	t.Run("phaseOut nil: baseline merges against live drift only", func(t *testing.T) {
		live := &models.Movie{ID: "M-1", Title: "user title", Maker: "bm"}
		got := mergeLiveReviewEdits(baseline, nil, live)
		assert.Equal(t, "user title", got.Title)
	})
	t.Run("live nil: pure phase output", func(t *testing.T) {
		phaseOut := &models.Movie{ID: "M-1", Title: "phase", Maker: "bm"}
		got := mergeLiveReviewEdits(baseline, phaseOut, nil)
		assert.Equal(t, "phase", got.Title)
	})
	t.Run("baseline nil passthrough", func(t *testing.T) {
		phaseOut := &models.Movie{ID: "M-1", Title: "phase"}
		live := &models.Movie{ID: "M-1", Title: "user"}
		got := mergeLiveReviewEdits(nil, phaseOut, live)
		assert.Equal(t, "phase", got.Title, "no baseline ⇒ nothing provably drifted")
	})

	t.Run("every review-editable drift wins", func(t *testing.T) {
		b := &models.Movie{ID: "M-2"}
		phase := &models.Movie{ID: "M-2"}
		live := &models.Movie{
			ID: "M-2ALT", Title: "t", DisplayTitle: "d", OriginalTitle: "o",
			Director: "dir", Maker: "mk", Label: "lb", Series: "sr", Runtime: 1,
			ReleaseYear: 2024, RatingScore: 9.5, RatingVotes: 7, Description: "ds",
			TrailerURL: "tr", ContentID: "cid",
			Actresses:   []models.Actress{{ID: 1}},
			Genres:      []models.Genre{{ID: 9}},
			Screenshots: []string{"s"},
		}
		live.Poster.PosterURL = "https://i.example/p.jpg"
		got := mergeLiveReviewEdits(b, phase, live)
		assert.Equal(t, "M-2ALT", got.ID)
		assert.Equal(t, "t", got.Title)
		assert.Equal(t, "d", got.DisplayTitle)
		assert.Equal(t, "dir", got.Director)
		assert.Equal(t, "mk", got.Maker)
		assert.Equal(t, "lb", got.Label)
		assert.Equal(t, "ds", got.Description)
		require.Len(t, got.Actresses, 1)
		require.Len(t, got.Genres, 1)
		require.Len(t, got.Screenshots, 1)
	})
}

func TestApplyWritebackIdentityMismatchArms(t *testing.T) {
	pm := &models.Movie{ID: "A-1"}
	assert.False(t, applyWritebackIdentityMismatch(nil, &resultstore.MovieResult{}))
	assert.False(t, applyWritebackIdentityMismatch(pm, nil))

	movieFree := &resultstore.MovieResult{FileMatchInfo: models.FileMatchInfo{MovieID: "a-1"}}
	assert.False(t, applyWritebackIdentityMismatch(pm, movieFree), "alias fallback fold-matches")

	moved := &resultstore.MovieResult{
		Movie:         &models.Movie{ID: "B-9"},
		FileMatchInfo: models.FileMatchInfo{MovieID: "B-9"},
	}
	assert.True(t, applyWritebackIdentityMismatch(pm, moved))
	assert.True(t, applyWritebackIdentityMismatch(&models.Movie{ID: "A-1"}, &resultstore.MovieResult{FileMatchInfo: models.FileMatchInfo{MovieID: "B-9"}}))
}

func TestApplyMatchFollowedByLiveIdentityArms(t *testing.T) {
	fm := models.FileMatchInfo{MovieID: "A-1"}
	assert.Equal(t, "A-1", applyMatchFollowedByLiveIdentity(fm, nil).MovieID)
	assert.Equal(t, "A-1", applyMatchFollowedByLiveIdentity(fm, &resultstore.MovieResult{}).MovieID)
	assert.Equal(t, "A-1", applyMatchFollowedByLiveIdentity(fm, &resultstore.MovieResult{FileMatchInfo: models.FileMatchInfo{MovieID: "A-1"}}).MovieID)
	assert.Equal(t, "B-7", applyMatchFollowedByLiveIdentity(fm, &resultstore.MovieResult{FileMatchInfo: models.FileMatchInfo{MovieID: "B-7"}}).MovieID)
}
