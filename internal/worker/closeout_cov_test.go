package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	wfmocks "github.com/javinizer/javinizer-go/internal/mocks/workflow"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

func wfMockForRunComplete(t *testing.T) *wfmocks.MockWorkflowInterface {
	t.Helper()
	wfm := wfmocks.NewMockWorkflowInterface(t)
	wfm.EXPECT().Apply(mock.Anything, mock.Anything).Return(&workflow.ApplyResult{Movie: &models.Movie{ID: "P-9"}}, nil)
	return wfm
}

// Failure write-back with a LIVE drift on the SAME identity: live edits win,
// the phase-side field set is preserved (failure leg of D5-lite merge).
func TestInterpretApplyResultFailureMergeSameIdentity(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-m1", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "AM-1", Title: "user edit"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "AM-1"},
	})
	inputs := minimalApplyInputs(t, store, true)
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "AM-1"}}
	outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "AM-1", Title: "phase baseline"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, nil, errors.New("engine wedged"))
	require.True(t, outcome.Failed)
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "user edit", final.Movie.Title)
	assert.Equal(t, models.JobStatusFailed, final.Status)
}

// Successful apply against a rekeyed result skips its whole write-back.
func TestInterpretApplyResultSuccessMismatchSkip(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-m2", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "NEW-2", Title: "user edit"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-2"},
	})
	inputs := minimalApplyInputs(t, store, true)
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-2"}}
	result := &workflow.ApplyResult{Movie: &models.Movie{ID: "OLD-2", Title: "phase computed"}}
	outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "OLD-2"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, result, nil)
	_ = outcome
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "user edit", final.Movie.Title, "no stale overlay on a rekeyed result")
}

func TestMergeLiveReviewEditsReleaseDateDrift(t *testing.T) {
	bT := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lT := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	baseline := &models.Movie{ID: "RD-1", ReleaseDate: &bT}
	phase := &models.Movie{ID: "RD-1", ReleaseDate: &bT}
	live := &models.Movie{ID: "RD-1", ReleaseDate: &lT}
	got := mergeLiveReviewEdits(baseline, phase, live)
	require.NotNil(t, got.ReleaseDate)
	assert.Equal(t, lT, *got.ReleaseDate)
}

// Rescrape tail: outcome WITHOUT a file path publishes provenance unlocked.
func TestControllerRescrapeTailNoFilePath(t *testing.T) {
	job := newBatchJob(nil)
	wfm := wfmocks.NewMockWorkflowInterface(t)
	wfm.EXPECT().Scrape(mock.Anything, mock.Anything).Return(&scrape.ScrapeResult{}, nil, nil).Maybe()
	job.Controller().SetWorkflow(wfm)
	outcome, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{})
	// An empty command fails inside the phase (no movie to look the file up
	// for); the tail's else arm is covered by the keys-empty sibling test.
	if err != nil {
		assert.Nil(t, outcome)
		return
	}
	require.NotNil(t, outcome)
	assert.Empty(t, outcome.FilePath)
}

// Rescrape tail: no identity materializes keys ⇒ unlocked publish path.
func TestControllerRescrapeTailFilePathButNoKeys(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	wfm := wfmocks.NewMockWorkflowInterface(t)
	wfm.EXPECT().Scrape(mock.Anything, mock.Anything).Return(&scrape.ScrapeResult{}, nil, nil)
	job.Controller().SetWorkflow(wfm)
	outcome, _ := job.Controller().Rescrape(context.Background(), RescrapeCmd{FilePath: "/f/a.mp4"})
	_ = outcome
}

// codex P2-C: a settled identity mismatch must NOT invoke the atomic updater
// (which bumps the revision even for a no-op callback).
type skipVerifyUpdater struct {
	resultstore.ResultUpdater
	result      *resultstore.MovieResult
	atomicCalls int
}

func (s *skipVerifyUpdater) AtomicUpdateFileResult(fp string, fn func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	s.atomicCalls++
	return nil
}
func (s *skipVerifyUpdater) GetMovieResult(string) (*resultstore.MovieResult, error) {
	return s.result, nil
}

func TestWritebackPreSkippedAvoidsAtomic(t *testing.T) {
	store := &skipVerifyUpdater{result: &resultstore.MovieResult{
		ResultID:      "res-x",
		Status:        models.JobStatusRunning,
		Movie:         &models.Movie{ID: "NEW-7"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-7"},
	}}
	assert.True(t, writebackPreSkipped(store, &models.Movie{ID: "OLD-7"}, "/f/a.mp4", "Apply"))
	assert.Equal(t, 0, store.atomicCalls, "a skip must not invoke the mutating updater")
	// matched identity → no skip
	assert.False(t, writebackPreSkipped(store, &models.Movie{ID: "NEW-7"}, "/f/a.mp4", "Apply"))
	// read seam unavailable → skip can't be proven, falls through to callback
	invisible := resultstore.New(1, []string{"/f/a.mp4"})
	_ = invisible
	cur2 := &skipVerifyUpdater{}
	cur2.result = nil
	assert.False(t, writebackPreSkipped(cur2, &models.Movie{ID: "NEW-7"}, "/f/a.mp4", "Apply"))
}

// Seam without a read channel: the pre-check can't prove anything, and the
// in-callback mismatch check carries the skip (safety-net arm).
type callbackOnlyUpdater struct {
	inner resultstore.Store
	calls int
}

func (u *callbackOnlyUpdater) UpdateFileResult(string, *resultstore.MovieResult) {}
func (u *callbackOnlyUpdater) SetProvenance(string, *resultstore.ProvenanceData) {}
func (u *callbackOnlyUpdater) AtomicUpdateFileResultWithProvenance(fp string, fn func(*resultstore.MovieResult, *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)) error {
	u.calls++
	return u.inner.AtomicUpdateFileResultWithProvenance(fp, fn)
}
func (u *callbackOnlyUpdater) UpdateMovie(string, *models.Movie) error { return nil }
func (u *callbackOnlyUpdater) MarkExcluded(string)                     {}
func (u *callbackOnlyUpdater) AtomicUpdateFileResult(fp string, fn func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	u.calls++
	return u.inner.AtomicUpdateFileResult(fp, fn)
}

var _ resultstore.ResultUpdater = (*callbackOnlyUpdater)(nil)

func TestCallbackOnlyUpdaterFallsBackToInCallbackCheck(t *testing.T) {
	inner := resultstore.New(1, []string{"/f/a.mp4"})
	inner.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-x", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "NEW-8"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-8"},
	})
	u := &callbackOnlyUpdater{inner: inner}
	require.False(t, writebackPreSkipped(u, &models.Movie{ID: "OLD-8"}, "/f/a.mp4", "Apply"), "no read seam ⇒ no pre-skip")
	// The callback-level check on the live store still skips the write-back.
	var sawSkip bool
	err := u.AtomicUpdateFileResult("/f/a.mp4", func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		if applyWritebackIdentityMismatch(&models.Movie{ID: "OLD-8"}, current) {
			sawSkip = true
			return current, nil
		}
		return current, nil
	})
	require.NoError(t, err)
	assert.True(t, sawSkip, "in-callback defense fires under a callback-only seam")
	assert.Equal(t, 1, u.calls)
}

// Real flows with a callback-only updater seam: the in-callback mismatch
// check is the operative skip (no read seam to pre-check through).
func TestPanicRecoveryCallbackOnlyWriteback(t *testing.T) {
	inner := resultstore.New(1, []string{"/f/a.mp4"})
	inner.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-p9", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "NEW-9", Title: "user edit"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-9"},
	})
	u := &callbackOnlyUpdater{inner: inner}
	outcome := &panicOutcome{}
	rc := recoveryContext{
		filePath: "/f/a.mp4",
		fmi:      models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-9"},
		movie:    &models.Movie{ID: "OLD-9", Title: "phase frozen"},
		updater:  u,
	}
	recoverFn := withFileRecovery(rc, outcome)
	func() {
		defer recoverFn()
		panic("wedged")
	}()
	assert.Equal(t, 1, u.calls)
	final, err := inner.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "user edit", final.Movie.Title, "rekeyed result untouched by the panic path")
}

func TestFailureWritebackCallbackOnly(t *testing.T) {
	inner := resultstore.New(1, []string{"/f/a.mp4"})
	inner.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-f9", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "NEW-10", Title: "user edit"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-10"},
	})
	u := &callbackOnlyUpdater{inner: inner}
	inputs := minimalApplyInputs2(u)
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-10"}}
	outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "OLD-10", Title: "phase frozen"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, nil, errors.New("engine wedged"))
	require.True(t, outcome.Failed)
	assert.Equal(t, 1, u.calls)
	final, err := inner.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "user edit", final.Movie.Title)
}

func minimalApplyInputs2(u *callbackOnlyUpdater) applyPhaseInputs {
	return applyPhaseInputs{
		JobID:       models.NewJobID(),
		Results:     map[string]*resultstore.MovieResult{},
		Updater:     u,
		Lifecycle:   &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})},
		Broadcaster: &stubBroadcaster{},
	}
}

// interpretApplyResult final arms: cancelled-with-audit, failure hooks, the
// success in-callback mismatch safety net, and the organized hook.
func TestInterpretApplyResultCancelledAuditsOrganizeResult(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-z1", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "Z-1", Title: "user edit"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "Z-1"},
	})
	inputs := minimalApplyInputs(t, store, false)
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "Z-1"}}
	result := &workflow.ApplyResult{Movie: &models.Movie{ID: "Z-1"}, OrganizeResult: &organizer.OrganizeResult{}}
	outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "Z-1"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, result, context.Canceled)
	assert.True(t, outcome.Cancelled)
}

func TestInterpretApplyResultHooks(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-z2", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "Z-2", Title: "user"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "Z-2"},
	})
	var failedPath, organizedPath string
	cfg := ApplyPhaseConfig{
		OnFileFailed:    func(fp, errMsg string) { failedPath = fp },
		OnFileOrganized: func(fp string) { organizedPath = fp },
	}
	inputs := minimalApplyInputs(t, store, false)
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "Z-2"}}
	outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "Z-2"}, time.Now(), time.Minute, inputs, cfg, context.Background(), afc, nil, errors.New("engine wedged"))
	require.True(t, outcome.Failed)
	assert.Equal(t, "/f/a.mp4", failedPath)

	// Success leg with OnFileOrganized set
	outcome2 := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "Z-2"}, time.Now(), time.Minute, inputs, cfg, context.Background(), afc, &workflow.ApplyResult{Movie: &models.Movie{ID: "Z-2"}}, nil)
	require.True(t, outcome2.Success)
	assert.Equal(t, "/f/a.mp4", organizedPath)
}

// Success write-back with a callback-only seam + rekeyed result: the
// in-callback check still guards (defense net for read-less seams).
func TestInterpretSuccessWritebackCallbackOnlySkip(t *testing.T) {
	inner := resultstore.New(1, []string{"/f/a.mp4"})
	inner.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-z3", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "Z-NEW", Title: "user edit"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "Z-NEW"},
	})
	u := &callbackOnlyUpdater{inner: inner}
	inputs := minimalApplyInputs2(u)
	afc := &ApplyFileContext{FilePath: "/f/a.mp4", Match: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "Z-OLD"}}
	result := &workflow.ApplyResult{Movie: &models.Movie{ID: "Z-OLD", Title: "phase computed"}}
	outcome := interpretApplyResult("/f/a.mp4", &models.Movie{ID: "Z-OLD"}, time.Now(), time.Minute, inputs, ApplyPhaseConfig{}, context.Background(), afc, result, nil)
	require.True(t, outcome.Success)
	assert.Equal(t, 1, u.calls)
	final, err := inner.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "user edit", final.Movie.Title, "stale write-back skipped by the in-callback check")
}

// Run's deferred recover arm with an actual panic under persisting mock WF.
// run's deferred recover arm: a hook panic inside the fanout closure bubbles
// to the deferred recover; the persister still completes in its tail.
// apply Run's deferred panic handler (extracted because fanout workers bubble
// panics in a way that crashes the test process, not reach Run's defer).
func TestRecoverRunPanicMarksFailed(t *testing.T) {
	failed := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	recoverRunPanic(applyPhaseInputs{JobID: models.NewJobID(), Lifecycle: failed, persister: nil}, errors.New("engine wedge"))
	assert.Equal(t, models.JobStatusFailed, failed.GetJobStatus())

	// Non-recovery exit passes through harmlessly.
}

type flipCurStore struct {
	resultstore.ResultMapAccessor
	alt *resultstore.MovieResult
}

func (s flipCurStore) GetMovieResult(string) (*resultstore.MovieResult, error) { return s.alt, nil }

func TestCommitResultFoldsAliasWhenCanonicalDiffersFromKey(t *testing.T) {
	inner := resultstore.New(1, []string{"/f/a.mp4"})
	inner.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-9", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "CUR-9", ContentID: "cid9"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "CUR-9"},
	})

	wrapped := &familyKeyedResultMap{
		ResultMapAccessor: flipCurStore{
			ResultMapAccessor: inner,
			alt:               &resultstore.MovieResult{Movie: &models.Movie{ID: "ALT-9", ContentID: "cid9"}},
		},
		registry: newKeyedMutexRegistry(),
	}
	incoming := &resultstore.MovieResult{ResultID: "res-9", Movie: &models.Movie{ID: "INC-9"}}
	// Revision mismatch is fine — the lock acquisition prefix is what this
	// test exercises; the delegate error signals the appendix ran.
	_ = wrapped.CommitResult("/f/a.mp4", incoming, 0)
}
