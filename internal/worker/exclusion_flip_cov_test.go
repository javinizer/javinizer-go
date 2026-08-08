package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// Legacy (no committer) arm: publish marks everything excluded, cancelIfAll
// runs Cancel() on any Pending/Running job — the all-excluded auto-cancel
// contract the review pane's bulk-exclude relies on (codex r31).
func TestExcludeFamilyLegacyCancelsWhenAllExcluded(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "FC-L1", "")
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	pe := NewPosterEditor(store, store, nil)
	pe.attachEnv(&posterEditEnv{lifecycle: lc, persistFn: func() error { return nil }})
	m := &LockedMovieOps{pe: pe, movieID: "FC-L1"}
	require.NoError(t, m.ExcludeFamily(context.Background()))
	assert.Equal(t, models.JobStatusCancelled, lc.GetJobStatus())
}

// audit R4: the candidate envelope's exclusion map must be computed under the
// envelope lock — a racing exclusion commit on another family shares only the
// in-memory store, and a pre-captured snapshot would erase its entry.
func TestExcludeFamilyEnvelopeCapturesRacingExclusion(t *testing.T) {
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "FAM-A", "")
	seedFamilyResult(store, "/f/b.mp4", "res-b", "FAM-B", "")
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	committer := NewEditCommitter(racingMarkTransactor{store: store}, newKeyedMutexRegistry(), "JOB-X", newKeyedMutexRegistry())
	var captured map[string]bool
	pe := NewPosterEditor(store, store, nil)
	pe.attachEnv(&posterEditEnv{
		committer: committer,
		envelope: func(_ map[string]*resultstore.MovieResult, _ map[string]*resultstore.ProvenanceData, excluded map[string]bool) (*models.Job, error) {
			captured = excluded
			return nil, nil // nil row: skip the DB upsert leg in this fixture
		},
		lifecycle: lc,
	})
	m := &LockedMovieOps{pe: pe, movieID: "FAM-A"}
	require.NoError(t, m.ExcludeFamily(context.Background()))
	assert.True(t, captured["/f/a.mp4"], "op A's own exclusion present")
	assert.True(t, captured["/f/b.mp4"], "racing exclusion from family B survives into the durable envelope")
}

// racingMarkTransactor lands a competing exclusion INSIDE the transaction
// window (after any pre-commit capture would run) so the regression test pins
// the in-lock recompute.
type racingMarkTransactor struct {
	store resultstore.ResultUpdater
}

func (tr racingMarkTransactor) WithEditTx(_ context.Context, fn func(database.EditUnit) error) error {
	tr.store.MarkExcluded("/f/b.mp4") // B's racing exclusion lands mid-commit
	return fn(database.EditUnit{})
}

// Marker-flip after the commit must STILL drive the full lifecycle Cancel()
// (done channel close + CompletedAt + status transition) — Waiting on Done()
// for a marked job must complete, not hang.
func TestExcludeFamilyCommitterShareCancelCompletesLifecycle(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "FC-MARK", "")
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	committer := NewEditCommitter(&okTransactor{}, newKeyedMutexRegistry(), "JOB-FCM", newKeyedMutexRegistry())
	pe := NewPosterEditor(store, store, nil)
	pe.attachEnv(&posterEditEnv{
		committer: committer,
		envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
			return nil, nil // nil row: upsert leg skips
		},
		lifecycle: lc,
	})
	m := &LockedMovieOps{pe: pe, movieID: "FC-MARK"}
	require.NoError(t, m.ExcludeFamily(context.Background()))
	assert.Equal(t, models.JobStatusCancelled, lc.GetJobStatus())
	assert.True(t, lc.CompletedAt != nil)
	select {
	case <-lc.Done():
		// CompletedAt mirrors Done: cancel completed the transition
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled job's done channel must close after exclusion")
	}
}

// commit-leg tx failure: no transient Cancelled is ever published to the
// live lifecycle (codex r33) — the in-memory job simply remains its original
// status; there is nothing to roll back.
func TestExcludeFamilyCommitterTxnFailureLeavesLiveStatus(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-x", "FC-T1", "")
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-FCX", newKeyedMutexRegistry())
	pe := NewPosterEditor(store, store, nil)
	pe.attachEnv(&posterEditEnv{
		committer: committer,
		envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
			return nil, nil // nil row: upsert leg skips
		},
		lifecycle: lc,
	})
	m := &LockedMovieOps{pe: pe, movieID: "FC-T1"}
	require.ErrorContains(t, m.ExcludeFamily(context.Background()), "tx wedged")
	assert.Equal(t, models.JobStatusRunning, lc.GetJobStatus(), "tx failure leaves the live lifecycle untouched")
}

// The terminal predicate must not fire when there are no results to speak of
// (an empty results snapshot encodes no Cancelled status).
func TestAllExcludedTerminalEmptySnapshotSkips(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"}) // no results registered
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	assert.False(t, allExcludedTerminal(lc, map[string]bool{"/f/a.mp4": true}, store))
}

func TestAllExcludedTerminalPredicateMatrix(t *testing.T) {
	mk := func(status models.JobStatus, results []string) (*JobLifecycle, resultstore.Store) {
		store := resultstore.New(1, []string{"/f/a.mp4"})
		for _, fp := range results {
			seedFamilyResult(store, fp, "res-"+fp, "FC-PM", "")
		}
		return &JobLifecycle{Status: status, done: make(chan struct{})}, store
	}
	// all covered + cancellable → terminal
	lc1, s1 := mk(models.JobStatusRunning, []string{"/f/a.mp4"})
	assert.True(t, allExcludedTerminal(lc1, map[string]bool{"/f/a.mp4": true}, s1))
	lc2, s2 := mk(models.JobStatusPending, []string{"/f/a.mp4"})
	assert.True(t, allExcludedTerminal(lc2, map[string]bool{"/f/a.mp4": true}, s2))
	// a result outside the exclusion set → not terminal
	lc3, s3 := mk(models.JobStatusRunning, []string{"/f/a.mp4"})
	seedFamilyResult(s3, "/f/b.mp4", "res-b", "OTHER-1", "")
	assert.False(t, allExcludedTerminal(lc3, map[string]bool{"/f/a.mp4": true}, s3))
	// terminal already → predicate stays false (cancelIfAll reads status)
	lc4, s4 := mk(models.JobStatusCompleted, []string{"/f/a.mp4"})
	assert.False(t, allExcludedTerminal(lc4, map[string]bool{"/f/a.mp4": true}, s4))
	// nil lifecycle → false
	_, s5 := mk(models.JobStatusRunning, []string{"/f/a.mp4"})
	assert.False(t, allExcludedTerminal(nil, map[string]bool{"/f/a.mp4": true}, s5))
}

// codex r33 regression: the committed envelope row carries Cancelled while
// the LIVE lifecycle still reads Running at envelope-encode time — a racing
// AcquireEditAccess therefore never observes the transient status, and a
// failing commit cannot persist it.
func TestExcludeFamilyEncodesCancelledInCandidateOnly(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-cc", "FC-CC", "")
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	jobs := mocks.NewMockJobRepositoryInterface(t)
	var persisted *models.Job
	jobs.EXPECT().Upsert(mock.Anything, mock.AnythingOfType("*models.Job")).
		Run(func(_ context.Context, j *models.Job) { persisted = j }).Return(nil)
	committer := NewEditCommitter(&execTransactor{unit: database.EditUnit{Jobs: jobs}}, newKeyedMutexRegistry(), "JOB-CC", newKeyedMutexRegistry())
	pe := NewPosterEditor(store, store, nil)
	var liveAtEncode models.JobStatus
	pe.attachEnv(&posterEditEnv{
		committer: committer,
		envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
			liveAtEncode = lc.GetJobStatus()
			return &models.Job{Status: lc.GetJobStatus()}, nil
		},
		lifecycle: lc,
	})
	m := &LockedMovieOps{pe: pe, movieID: "FC-CC"}
	require.NoError(t, m.ExcludeFamily(context.Background()))
	require.NotNil(t, persisted, "terminal exclusion must persist the envelope row")
	assert.Equal(t, models.JobStatusCancelled, persisted.Status, "candidate envelope encodes Cancelled")
	assert.Equal(t, models.JobStatusRunning, liveAtEncode, "live lifecycle must NOT carry the transient Cancelled during the commit")
	assert.Equal(t, models.JobStatusCancelled, lc.GetJobStatus(), "post-commit Cancel() still completes the real transition")
}
