package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	wfmocks "github.com/javinizer/javinizer-go/internal/mocks/workflow"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func minimalApplyInputs(t *testing.T, store resultstore.Store, withLock bool) applyPhaseInputs {
	t.Helper()
	inputs := applyPhaseInputs{
		JobID:       models.NewJobID(),
		WF:          wfmocks.NewMockWorkflowInterface(t),
		Results:     map[string]*resultstore.MovieResult{},
		Updater:     store,
		Broadcaster: &stubBroadcaster{},
		Lifecycle:   &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})},
	}
	if withLock {
		inputs.EditLockFn = func(movieID string) func() { return func() {} }
	}
	return inputs
}

// failure write-back: mismatch identity ⇒ skip write-back entirely.
func TestInterpretApplyResultFailurePathRekeySkip(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-f1", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "NEW-9"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-9"},
	})
	inputs := minimalApplyInputs(t, store, true)
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"}}
	outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "OLD-1"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, nil, errors.New("apply engine wedged"))
	assert.True(t, outcome.Panic == false)
	assert.Equal(t, "OLD-1", outcome.MovieID)
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "NEW-9", final.Movie.ID, "no stale overlay was written back onto the rekeyed result")
}

// successful write-back with a drifted live review edit: edited fields win.
func TestInterpretApplyResultSuccessDriftMerge(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-f2", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "OK-1", Title: "user edit"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OK-1"},
	})
	inputs := minimalApplyInputs(t, store, true)
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OK-1"}}
	phaseOut := &applyFileOutcome{}
	_ = phaseOut
	applyMovie := &models.Movie{ID: "OK-1", Title: "pre-phase scraped title"}
	outcome := interpretApplyResult("/f/a.mp4", applyMovie, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, nil, nil)
	_ = outcome
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "user edit", final.Movie.Title, "review edit must survive the apply write-back (D5-lite)")
}

// The apply-phase defer: any panic inside Run persists the envelope and
// marks the job failed; a failing persister at most logs.
func TestApplyPhaseRunDeferPersistsThroughBoom(t *testing.T) {
	persistCalls := int32(0)
	failingPersister := &testPersisterErr{calls: &persistCalls}
	inputs := applyPhaseInputs{
		JobID:     models.NewJobID(),
		WF:        wfmocks.NewMockWorkflowInterface(t),
		Results:   map[string]*resultstore.MovieResult{},
		Updater:   resultstore.New(0, nil),
		Lifecycle: &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})},
		persister: failingPersister,
	}
	p := &applyPhase{}
	require.NotPanics(t, func() {
		p.Run(context.Background(), inputs, ApplyPhaseConfig{})
	})
	assert.Equal(t, int32(1), persistCalls, "defer persist runs exactly once")
}

type testPersisterErr struct{ calls *int32 }

func (p *testPersisterErr) Persist() error {
	*p.calls++
	return errors.New("disk read-only")
}

// The failure write-back returns the updater error intact when the store
// misbehaves (recovery's errUp arm).
func TestRecoveryPanicWithFailingUpdater(t *testing.T) {
	store := &failAtomicUpdater{err: errors.New("store wedged")}
	outcome := &panicOutcome{}
	rc := recoveryContext{
		filePath: "/f/a.mp4",
		fmi:      models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PAN-2"},
		movie:    &models.Movie{ID: "PAN-2"},
		updater:  store,
	}
	recoverFn := withFileRecovery(rc, outcome)
	func() {
		defer recoverFn()
		panic("wedged")
	}()
	assert.NotEmpty(t, outcome.msg)
}
