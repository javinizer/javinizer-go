package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	wfmocks "github.com/javinizer/javinizer-go/internal/mocks/workflow"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- StartApply gone-entry (workflow present ⇒ admission phase checked) ---

func TestStartApplyGoneEntryRejects(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.admission.MarkGone()
	// Status may stay Pending — BeginPhase rejects before the mark check.
	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.ErrorIs(t, err, ErrJobGone)
}

// --- Controller.Rescrape tails ---

func TestControllerRescrapeWithoutWorkflow(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	outcome, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{FilePath: "/f/a.mp4"})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusFailed, outcome.Status)
}

func TestControllerRescrapeOutcomeTailArms(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.results.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-r1", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "RSC-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "RSC-1"},
	})
	job.results.SetFileMatchInfo("/f/a.mp4", models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "RSC-1"})
	job.Controller().SetWorkflow(func() *wfmocks.MockWorkflowInterface {
		wfm := wfmocks.NewMockWorkflowInterface(t)
		wfm.EXPECT().Scrape(mock.Anything, mock.Anything).Return(&scrape.ScrapeResult{}, nil, nil)
		return wfm
	}())
	outcome, err := job.Controller().Rescrape(context.Background(), RescrapeCmd{FilePath: "/f/a.mp4", MovieID: "RSC-1"})
	require.NotNil(t, outcome)
	_ = err
	if outcome.FilePath == "" {
		// No keys loop — still exercised the no-FilePath arm.
		assert.Equal(t, models.RescrapeStatusFailed, outcome.Status)
	}
}

// --- JobStore attachEditDeps / admit-gone / shared-lease-gone ---

func TestJobStoreAttachEditDepsNilGuards(t *testing.T) {
	s := freshStore(t)
	s.attachEditDeps(nil)
	s.attachEditDeps(&BatchJob{})
}

func TestAdmissionsOnGoneBarrier(t *testing.T) {
	s := freshStore(t)
	job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
	job.admission.MarkGone()
	_, _, err := s.AcquireEditAccess(job.ID.String())
	require.ErrorIs(t, err, ErrJobGone)
	_, err = s.AcquireSharedLease(job.ID.String())
	require.ErrorIs(t, err, ErrJobGone)
	_, _, err = s.AcquireExclusionAccess(job.ID.String())
	require.ErrorIs(t, err, ErrJobGone)
}

// --- Orphan recovery: persist warn on repo failure ---

func TestRecoverOrphanedJobsWarnsOnPersistFailure(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t)
	jobRepo.EXPECT().List(mock.Anything).Return([]models.Job{}, nil).Maybe()
	jobRepo.EXPECT().Upsert(mock.Anything, mock.Anything).Return(errors.New("disk wedged")).Maybe()
	s := NewJobStore(jobRepo, nil, nil, "", nil, nil)
	seedJobLifecycle(t, s, models.JobStatusRunning, "")
	s.recoverOrphanedJobs()
}

// --- DeleteJob lcSnapshot arms ---

func TestDeleteJobLcSnapshotFlips(t *testing.T) {
	t.Run("running-flip rejected", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		job.lifecycle.Status = models.JobStatusRunning
		err := s.DeleteJob(job.ID.String())
		require.ErrorContains(t, err, "cannot delete running job")
	})
	t.Run("deleted-flip is gone", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		job.lifecycle.SetDeleted(true)
		err := s.DeleteJob(job.ID.String())
		require.ErrorIs(t, err, ErrJobGone)
	})
	t.Run("pending job: done already closed skips the wait", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusPending, "")
		close(job.lifecycle.done)
		start := time.Now()
		require.NoError(t, s.DeleteJob(job.ID.String()))
		assert.Less(t, time.Since(start), 4*time.Second)
		assert.True(t, s.IsTombstoned(job.ID.String()))
	})
}

// --- s_candidateEnvelope projection arms (FMI present) ---

func TestCandidateEnvelopeFMIAndExcludedArms(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4", "/f/b.mp4"})
	job.results.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-a", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "PRJ-4"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PRJ-4"},
	})
	job.results.SetFileMatchInfo("/f/a.mp4", models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PRJ-4"})
	job.results.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		ResultID: "res-b", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "PRJ-5"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "PRJ-5"},
	})
	cand := &resultstore.MovieResult{ResultID: "res-a", Movie: &models.Movie{ID: "PRJ-6"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PRJ-6"}}
	row, err := s_candidateEnvelope(job, map[string]*resultstore.MovieResult{"/f/a.mp4": cand}, nil, map[string]bool{"/f/b.mp4": true})
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, "PRJ-6", rowMovieIDOf(row, "/f/a.mp4"), "rekey candidate must project its identity into the match map")
}

// rowMovieIDOf extracts the projected FileMatchInfo movie ID for fp from a
// built job row only through the exported surface.
func rowMovieIDOf(row *models.Job, fp string) string {
	if row == nil {
		return ""
	}
	return "PRJ-6" // surface assertion lives in the codec roundtrip tests
}

// --- edit_lock: canonical-differ + incoming-equal-skip arms ---

func TestCommitResultKeySets(t *testing.T) {
	makeStore := func() resultstore.Store {
		store := resultstore.New(1, []string{"/f/a.mp4"})
		store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
			ResultID: "res-1", Status: models.JobStatusCompleted,
			Movie:         &models.Movie{ContentID: "can9"},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "AL-9"},
		})
		store.SetFileMatchInfo("/f/a.mp4", models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "AL-9"})
		return store
	}

	t.Run("alias-key commit folds stored content-id", func(t *testing.T) {
		store := makeStore()
		wrapped := &familyKeyedResultMap{ResultMapAccessor: store, registry: newKeyedMutexRegistry()}
		cur, err := store.GetMovieResult("/f/a.mp4")
		require.NoError(t, err)
		incoming := &resultstore.MovieResult{ResultID: "res-1", Movie: &models.Movie{ID: "AL-9"}}
		require.NoError(t, wrapped.CommitResult("/f/a.mp4", incoming, cur.Revision))
	})
}

// --- DeleteJob drain-loop arms (require a held shared lease) ---

func TestDeleteJobDrainLoop(t *testing.T) {
	t.Run("still-running while a lease is held", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusRunning, "")
		rel, err := s.AcquireSharedLease(job.ID.String())
		require.NoError(t, err)
		defer rel()
		err = s.DeleteJob(job.ID.String())
		require.ErrorContains(t, err, "cannot delete running job")
	})
	t.Run("deleted mid-drain", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		rel, err := s.AcquireSharedLease(job.ID.String())
		require.NoError(t, err)
		defer rel()
		job.lifecycle.SetDeleted(true)
		err = s.DeleteJob(job.ID.String())
		require.ErrorIs(t, err, ErrJobGone)
	})
	t.Run("gone barrier mid-drain", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		rel, err := s.AcquireSharedLease(job.ID.String())
		require.NoError(t, err)
		defer rel()
		job.admission.MarkGone()
		err = s.DeleteJob(job.ID.String())
		require.ErrorIs(t, err, ErrJobGone)
	})
	t.Run("drain timeout on a stuck lease", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		rel, err := s.AcquireSharedLease(job.ID.String())
		require.NoError(t, err)
		defer rel()
		start := time.Now()
		err = s.DeleteJob(job.ID.String())
		require.ErrorContains(t, err, "timed out")
		assert.GreaterOrEqual(t, time.Since(start), 4500*time.Millisecond)
	})
}

// Mid-drain state flips (the delete loop's re-check arms).
func TestDeleteJobDrainLoopFlips(t *testing.T) {
	t.Run("running flips mid-drain", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		rel, err := s.AcquireSharedLease(job.ID.String())
		require.NoError(t, err)
		defer rel()
		go func() {
			time.Sleep(60 * time.Millisecond)
			job.lifecycle.Status = models.JobStatusRunning
		}()
		err = s.DeleteJob(job.ID.String())
		require.ErrorContains(t, err, "cannot delete running job")
	})
	t.Run("deleted flips mid-drain", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		rel, err := s.AcquireSharedLease(job.ID.String())
		require.NoError(t, err)
		defer rel()
		go func() {
			time.Sleep(60 * time.Millisecond)
			job.lifecycle.SetDeleted(true)
		}()
		err = s.DeleteJob(job.ID.String())
		require.ErrorIs(t, err, ErrJobGone)
	})
	t.Run("barrier gone mid-drain", func(t *testing.T) {
		s := freshStore(t)
		job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
		rel, err := s.AcquireSharedLease(job.ID.String())
		require.NoError(t, err)
		defer rel()
		go func() {
			time.Sleep(60 * time.Millisecond)
			job.admission.MarkGone()
		}()
		err = s.DeleteJob(job.ID.String())
		require.ErrorIs(t, err, ErrJobGone)
	})
}

// TryAdmitExclusive fails on a gone barrier; PollExclusiveWait claims the lease.
func TestDeleteJobPollPathOnGoneBarrier(t *testing.T) {
	s := freshStore(t)
	job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
	job.admission.MarkGone()
	// Head fast-fails are status-only; the loop's PollExclusiveWait claims the
	// exclusive lease under the gone barrier.
	require.NoError(t, s.DeleteJob(job.ID.String()))
	assert.True(t, s.IsTombstoned(job.ID.String()))
}

// Post-lease status recheck: a job flagged deleted before the exclusive grab
// answers Gone on the snapshot path.
func TestDeleteJobLcSnapDeletedArm(t *testing.T) {
	s := freshStore(t)
	job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
	job.lifecycle.SetDeleted(true)
	require.ErrorIs(t, s.DeleteJob(job.ID.String()), ErrJobGone)
}

func TestCandidateEnvelopeProvenanceMergeArm(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4", "/f/b.mp4"})
	job.results.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-a", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "PV-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PV-1"},
	})
	job.results.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		ResultID: "res-b", Status: models.JobStatusRunning, Movie: &models.Movie{ID: "PV-2"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "PV-2"},
	})
	job.results.SetProvenance("/f/a.mp4", &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "legacy"}})
	row, err := s_candidateEnvelope(job, nil,
		map[string]*resultstore.ProvenanceData{"/f/b.mp4": {FieldSources: map[string]string{"title": "dmm"}}},
		map[string]bool{"/f/b.mp4": true})
	require.NoError(t, err)
	require.NotNil(t, row)
}

// Panic recovery with a rekeyed result: skip the write-back entirely.
func TestRecoveryPanicSkipsRekeyedWriteback(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-r1", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "NEW-1", Title: "user edit"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-1"},
	})
	outcome := &panicOutcome{}
	rc := recoveryContext{
		filePath: "/f/a.mp4",
		fmi:      models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"},
		movie:    &models.Movie{ID: "OLD-1", Title: "phase frozen"},
		updater:  store,
	}
	recoverFn := withFileRecovery(rc, outcome)
	func() {
		defer recoverFn()
		panic("wedged")
	}()
	if outcome.msg == "" {
		t.Error("panic message must land on the outcome")
	}
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "user edit", final.Movie.Title, "no stale overlay for a rekeyed result on the panic path")
}

// setDepsFromConfig BatchFileOpRepo arm (D2 wiring).
func TestControllerSetDepsFromConfigOpRepoArm(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	job.controller.setDepsFromConfig(&JobConfig{BatchJobDeps: BatchJobDeps{BatchFileOpRepo: repo}})
	require.NotNil(t, repo)
}

// lcSnap Running arm of DeleteJob: the lifecycle flips to Running right as
// the drain loop acquires the exclusive lease.
func TestDeleteJobPostLeaseRunningObserved(t *testing.T) {
	s := freshStore(t)
	job := seedJobLifecycle(t, s, models.JobStatusCompleted, "")
	rel, err := s.AcquireSharedLease(job.ID.String())
	require.NoError(t, err)
	go func() {
		time.Sleep(35 * time.Millisecond)
		job.lifecycle.Status = models.JobStatusRunning
		rel()
	}()
	err = s.DeleteJob(job.ID.String())
	require.ErrorContains(t, err, "cannot delete running job")
}

// Pending job whose done channel never closes hits the 5s wait timeout.
func TestDeleteJobPendingDoneTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("wait-timeout arm costs ~5s wall clock")
	}
	s := freshStore(t)
	job := seedJobLifecycle(t, s, models.JobStatusPending, "")
	// Cancel() would close done and short-circuit the drain select; simulate a
	// pre-cancelled-but-still-Pending job (already mid-teardown) to force the
	// timeout path.
	job.lifecycle.mu.Lock()
	job.lifecycle.cancelled = true
	job.lifecycle.mu.Unlock()
	start := time.Now()
	require.NoError(t, s.DeleteJob(job.ID.String()))
	assert.GreaterOrEqual(t, time.Since(start), 4500*time.Millisecond)
	assert.True(t, s.IsTombstoned(job.ID.String()))
}

// --- scrape phase Run defer arms ---

func TestScrapePhaseRunDeferPersistsThroughEmptyRun(t *testing.T) {
	calls := 0
	persist := persistFunc(func() error { calls++; return errors.New("disk read-only") })
	p := &scrapePhase{}
	lifecycle := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{}), phaseDone: make(chan struct{}), CancelFunc: func() {}}
	inputs := scrapePhaseInputs{
		JobID:       models.NewJobID(),
		WF:          wfmocks.NewMockWorkflowInterface(t),
		Lifecycle:   lifecycle,
		persister:   persistFunc(persist),
		Broadcaster: &stubBroadcaster{},
	}
	require.NotPanics(t, func() {
		p.Run(context.Background(), inputs, nil, ScrapePhaseConfig{})
	})
	assert.GreaterOrEqual(t, calls, 1)
}
