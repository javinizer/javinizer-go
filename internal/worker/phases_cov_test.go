package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wfmocks "github.com/javinizer/javinizer-go/internal/mocks/workflow"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

func TestStartScrapeFinalPersistsAfterMarkerClear(t *testing.T) {
	job := newBatchJob(nil)
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	var persists atomic.Int32
	job.deps.PersistFn = func() error { persists.Add(1); return nil }
	require.NoError(t, job.Controller().StartScrape(context.Background(), nil, ScrapePhaseConfig{}))
	// Wait on the lifecycle phase channel: it closes once the phase goroutine's
	// OWN defer chain (persist + clear-and-persist) has fully unwound.
	deadline := time.Now().Add(3 * time.Second)
	for {
		job.lifecycle.mu.RLock()
		pd := job.lifecycle.phaseDone
		job.lifecycle.mu.RUnlock()
		if pd != nil {
			<-pd
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("phase goroutine never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, "", job.lifecycle.CurrentPhase())
	assert.GreaterOrEqual(t, int(persists.Load()), 2)
}

func TestPhaseEndPersistWarnsAreSwallowed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start func(ctx context.Context, job *BatchJob) error
	}{
		{"scrape", func(ctx context.Context, job *BatchJob) error {
			return job.Controller().StartScrape(ctx, nil, ScrapePhaseConfig{})
		}},
		{"apply", func(ctx context.Context, job *BatchJob) error {
			job.lifecycle.mu.Lock()
			job.lifecycle.Status = models.JobStatusCompleted
			job.lifecycle.mu.Unlock()
			return job.Controller().StartApply(ctx, ApplyPhaseConfig{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := newBatchJob(nil)
			job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
			var persistCalls atomic.Int32
			job.deps.PersistFn = func() error {
				if persistCalls.Add(1) == 1 {
					return nil // phase-entry marker writes must succeed (fail-closed start)
				}
				return errors.New("persist refused") // the clear-time final write fails
			}
			require.NoError(t, tc.start(context.Background(), job))
			deadline := time.Now().Add(3 * time.Second)
			for {
				job.lifecycle.mu.RLock()
				pd := job.lifecycle.phaseDone
				job.lifecycle.mu.RUnlock()
				if pd != nil {
					<-pd
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("phase goroutine never started")
				}
				time.Sleep(10 * time.Millisecond)
			}
			assert.Equal(t, "", job.lifecycle.CurrentPhase())
		})
	}
}

// StartScrape must NOT launch when the phase-entry marker fails to persist
// (D16 fail-closed); the terminal failure is retried durably.
// codex r45 P2: a duplicate StartApply behind a held shared lease must
// reject IMMEDIATELY — never queue pendingPhase ahead of legitimate
// admissions for the winner phase's full duration.
// codex r47 P1: /cancel while the claimed launch waits in BeginPhase must
// retire the launch — releasing the lease afterwards must NOT start any
// phase work.
func TestStartApplyCancelledWhileQueuedNeverRuns(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.lifecycle.Status = models.JobStatusCompleted
	job.deps.PersistFn = func() error { return nil }
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- job.Controller().StartApply(context.Background(), ApplyPhaseConfig{}) }()
	assert.Eventually(t, func() bool {
		job.admission.mu.Lock()
		defer job.admission.mu.Unlock()
		return job.admission.pendingPhase == 1
	}, 2*time.Second, 5*time.Millisecond, "launch parked in the phase queue")
	job.lifecycle.Cancel() // user-facing /cancel while queued
	require.NoError(t, <-done)
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())
	rel() // the held lease releases — the phase must NOT begin behind the cancel
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus(), "no phase transitions after cancellation")
	require.ErrorContains(t, job.Controller().Wait(), "cancelled")
}

func TestStartApplyDuplicateLaunchFailsFast(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.Status = models.JobStatusRunning
	job.lifecycle.CurrentPhase() // marker via setters below
	job.lifecycle.SetCurrentPhase(string(JobPhaseApply))
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	start := time.Now()
	err = job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	elapsed := time.Since(start)
	require.ErrorContains(t, err, "expected status")
	assert.Less(t, elapsed, 500*time.Millisecond, "reject happens before the phase wait queue")
	rel2, err2 := job.admission.AdmitShared()
	require.NoError(t, err2, "no pendingPhase was ever registered by the rejected launch")
	rel2()
	rel()
}

func TestStartScrapeDuplicateLaunchFailsFast(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.Status = models.JobStatusRunning
	job.lifecycle.SetCurrentPhase(string(JobPhaseScrape))
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	start := time.Now()
	err = job.Controller().StartScrape(context.Background(), []string{"/f/a.mp4"}, ScrapePhaseConfig{})
	assert.Less(t, time.Since(start), 500*time.Millisecond)
	require.ErrorContains(t, err, "expected status")
	rel2, _ := job.admission.AdmitShared()
	require.NotNil(t, rel2)
	rel2()
	rel()
}

// codex r44 P2: Wait() must join AFTER the marker-clear persist ran —
// the defer registration order puts close(phaseDone) LAST (LIFO). The
// marker-clear persist therefore always observes phaseDone STILL OPEN.
func TestStartScrapeWaitJoinsAfterMarkerClearPersist(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	openDuringClear := false
	job.deps.PersistFn = func() error {
		job.lifecycle.mu.RLock()
		phase := job.lifecycle.CurrentPhase()
		pd := job.lifecycle.phaseDone
		job.lifecycle.mu.RUnlock()
		if pd == nil || phase != "" {
			return nil // marker-set or in-run persists — not the terminal one
		}
		select {
		case <-pd:
		default:
			openDuringClear = true
		}
		return nil
	}
	require.NoError(t, job.Controller().StartScrape(context.Background(), nil, ScrapePhaseConfig{}))
	require.NoError(t, job.Controller().Wait())
	assert.True(t, openDuringClear, "the marker-clear persist must run before phaseDone closes")
}

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
		editLockFn: func(movieIDs ...string) func() {
			require.Equal(t, []string{"PAN-1"}, movieIDs)
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
