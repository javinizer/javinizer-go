package worker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wfmocks "github.com/javinizer/javinizer-go/internal/mocks/workflow"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- BeginPhase context-aware queued wait (codex r36 P2) ---

func TestBeginPhasePreCancelledCtxReturnsImmediately(t *testing.T) {
	b := newAdmissionBarrier()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.BeginPhase(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	// pendingPhase must be drained — a fresh phase start is unblocked.
	p, err2 := b.BeginPhase(context.Background())
	require.NoError(t, err2)
	p.Fail()
}

func TestBeginPhaseCancelMidWaitDrainsPendingPhase(t *testing.T) {
	b := newAdmissionBarrier()
	rel, err := b.AdmitShared()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := b.BeginPhase(ctx)
		done <- err
	}()
	// Wait until the queued phase is actually parked behind the shared lease
	// (pendingPhase blocks a second shared admission while registered).
	assert.Eventually(t, func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.pendingPhase == 1
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	b.mu.Lock()
	pending := b.pendingPhase
	b.mu.Unlock()
	assert.Zero(t, pending, "cancelled queued phase must deregister pendingPhase")
	// Shared admissions flow again even while the first lease is still held.
	rel2, err2 := b.AdmitShared()
	require.NoError(t, err2)
	rel2()
	rel()
}

// --- EditPhaseBusyError / Is ---

func TestEditPhaseBusyErrorShapes(t *testing.T) {
	withPhase := &EditPhaseBusyError{JobID: "J1", Status: models.JobStatusRunning, Phase: "scrape"}
	assert.Contains(t, withPhase.Error(), "scrape phase in progress")
	bare := &EditPhaseBusyError{JobID: "J1", Status: models.JobStatusPending}
	assert.Contains(t, bare.Error(), "J1")
	assert.NotContains(t, bare.Error(), "phase in progress")
	require.ErrorIs(t, withPhase, ErrEditNotAdmitted)
	require.NotErrorIs(t, withPhase, ErrJobGone)
}

// --- admissionBarrier: shared/exclusive/phase choreography ---

func TestAdmissionSharedRejectsGoneBarrier(t *testing.T) {
	b := newAdmissionBarrier()
	rel, err := b.AdmitExclusive()
	require.NoError(t, err)
	b.MarkGone()
	rel()
	_, err = b.AdmitShared()
	require.ErrorIs(t, err, ErrJobGone)
}

func TestAdmissionSharedWaitsBehindExclusiveAndSeesGone(t *testing.T) {
	b := newAdmissionBarrier()
	rel, ok := b.TryAdmitExclusive()
	require.True(t, ok)
	admitted := make(chan error, 1)
	go func() {
		_, err := b.AdmitShared()
		admitted <- err
	}()
	time.Sleep(40 * time.Millisecond)
	select {
	case <-admitted:
		t.Fatal("shared admission must wait behind the exclusive lease")
	default:
	}
	b.MarkGone()
	rel()
	require.ErrorIs(t, <-admitted, ErrJobGone, "waiters queued before the delete observe gone, not a lease")
}

func TestAdmissionExclusiveDrainsSharedLeases(t *testing.T) {
	b := newAdmissionBarrier()
	rs1, err := b.AdmitShared()
	require.NoError(t, err)
	rs2, err := b.AdmitShared()
	require.NoError(t, err)
	got := make(chan func(), 1)
	go func() {
		rel, _ := b.AdmitExclusive()
		got <- rel
	}()
	time.Sleep(40 * time.Millisecond)
	select {
	case <-got:
		t.Fatal("exclusive must block while shared leases are held")
	default:
	}
	rs1()
	select {
	case <-got:
		t.Fatal("exclusive must block while one shared lease remains")
	default:
	}
	rs2()
	(<-got)()
}

func TestAdmissionPollExclusiveWaitContract(t *testing.T) {
	b := newAdmissionBarrier()
	_, ok := b.PollExclusiveWait()
	assert.False(t, ok, "Poll without Enter is a contract bug")
	b.EnterExclusiveWait()
	sharedDone := make(chan error, 1)
	go func() {
		_, err := b.AdmitShared()
		sharedDone <- err
	}()
	time.Sleep(30 * time.Millisecond)
	select {
	case <-sharedDone:
		t.Fatal("shared admission parks behind a pending exclusive waiter")
	default:
	}
	rel, ok := b.PollExclusiveWait()
	require.True(t, ok, "the entered waiter claims the lease once no shared holds exist")
	rel()
	require.NoError(t, <-sharedDone)
}

func TestAdmissionCancelExclusiveWaitUnderflowIsSafe(t *testing.T) {
	b := newAdmissionBarrier()
	b.CancelExclusiveWait()
}

func TestPhaseEntryWaitsThenDowngrades(t *testing.T) {
	b := newAdmissionBarrier()
	rel, err := b.AdmitShared()
	require.NoError(t, err)
	started := make(chan *phaseEntry, 1)
	go func() {
		p, _ := b.BeginPhase(context.Background())
		started <- p
	}()
	time.Sleep(40 * time.Millisecond)
	select {
	case <-started:
		t.Fatal("phase start parks behind a held shared lease")
	default:
	}
	rel()
	p := <-started
	phaseRel := p.Downgrade()
	r2, err := b.AdmitShared()
	require.NoError(t, err, "after the phase downgrade, edits share the lease")
	r2()
	phaseRel()
}

func TestBeginPhaseGoneWhileQueued(t *testing.T) {
	b := newAdmissionBarrier()
	rel, err := b.AdmitShared()
	require.NoError(t, err)
	started := make(chan error, 1)
	go func() {
		_, err := b.BeginPhase(context.Background())
		started <- err
	}()
	time.Sleep(40 * time.Millisecond)
	b.MarkGone()
	rel()
	require.ErrorIs(t, <-started, ErrJobGone)
}

func TestPhaseEntryFailReleasesLease(t *testing.T) {
	b := newAdmissionBarrier()
	p, err := b.BeginPhase(context.Background())
	require.NoError(t, err)
	p.Fail()
	rel, ok := b.TryAdmitExclusive()
	require.True(t, ok, "Fail releases the start-window lease")
	rel()
}

// --- tombstoneRegistry ---

func TestTombstoneRegistryLifecycleAndExpiry(t *testing.T) {
	tr := newTombstoneRegistry(40 * time.Millisecond)
	tr.Mark("JOB-X")
	assert.True(t, tr.Contains("JOB-X"))
	tr.Unmark("JOB-X")
	assert.False(t, tr.Contains("JOB-X"))
	tr.Mark("JOB-Y")
	time.Sleep(60 * time.Millisecond)
	assert.False(t, tr.Contains("JOB-Y"), "expired tombstones are swept lazily")
	assert.False(t, tr.Contains("JOB-NEVER"))
}

func TestNewTombstoneRegistryDefaultTTL(t *testing.T) {
	tr := newTombstoneRegistry(0)
	assert.Equal(t, defaultTombstoneTTL, tr.ttl)
}

// --- keyedMutexRegistry ---

func TestKeyedMutexCaseFolding(t *testing.T) {
	rg := newKeyedMutexRegistry()
	rel1 := rg.Acquire("abc-1 ")
	acquired := make(chan func(), 1)
	go func() { acquired <- rg.Acquire(" ABC-1") }()
	select {
	case <-time.After(30 * time.Millisecond):
	case rel2 := <-acquired:
		rel2()
		t.Fatal("case variants of a key must contend on the same mutex")
	}
	rel1()
	(<-acquired)()
}

func TestAcquireManySortsDedupsAndSkipsEmpties(t *testing.T) {
	rg := newKeyedMutexRegistry()
	rel := rg.AcquireMany([]string{"zyx", "", "  ", "zyx", " abc"})
	rel()
	noop := rg.AcquireMany(nil)
	noop()
	rel = rg.AcquireMany([]string{"zyx"})
	rel()
}

func TestAcquirePairLexicalOrderAndFoldedDedup(t *testing.T) {
	rg := newKeyedMutexRegistry()
	rel := rg.AcquirePair("bbb", "aaa")
	rel()
	rel = rg.AcquirePair("dup-1", " DUP-1")
	rel()
}

// familyKeyedResultMap.CommitResult locks every identity surface before
// delegating to the store commit.
func TestCommitResultLocksAllIdentitySurfaces(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-1", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "CAN-1", ContentID: "can1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "AL-1"},
	})
	store.SetFileMatchInfo("/f/a.mp4", models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "AL-1"})
	wrapped := &familyKeyedResultMap{ResultMapAccessor: store, registry: newKeyedMutexRegistry()}
	cur, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	incoming := &resultstore.MovieResult{ResultID: "res-1", Movie: &models.Movie{ID: "INC-9"}}
	require.Error(t, wrapped.CommitResult("/f/a.mp4", incoming, cur.Revision+1), "stale revision rejected")
	require.NoError(t, wrapped.CommitResult("/f/a.mp4", incoming, cur.Revision), "matching revision commits")
}

// codex r36 P1: provenance publishes inside the SAME family-locked section as
// the result commit — a successful CommitResultWithProvenance must leave both
// the result AND the provenance visible, with no interleave window.
func TestCommitResultWithProvenanceLockedSection(t *testing.T) {
	store := resultstore.New(1, []string{"/f/p.mp4"})
	seedFamilyResult(store, "/f/p.mp4", "res-p", "PROV-1", "")
	wrapped := &familyKeyedResultMap{ResultMapAccessor: store, updater: store, registry: newKeyedMutexRegistry()}
	cur, err := store.GetMovieResult("/f/p.mp4")
	require.NoError(t, err)
	prov := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "r18.dev"}}
	incoming := &resultstore.MovieResult{ResultID: "res-p", Movie: &models.Movie{ID: "PROV-2"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/p.mp4", MovieID: "PROV-2"}}
	require.NoError(t, wrapped.CommitResultWithProvenance("/f/p.mp4", incoming, cur.Revision, prov))
	got, err := store.GetMovieResult("/f/p.mp4")
	require.NoError(t, err)
	assert.Equal(t, "PROV-2", got.Movie.ID, "result committed")
	gotProv := store.GetProvenance("/f/p.mp4")
	require.NotNil(t, gotProv, "provenance published in the same section")
	assert.Equal(t, map[string]string{"title": "r18.dev"}, gotProv.FieldSources)
}

// A commit that FAILS the revision check must not leave half-state behind:
// neither the result nor the provenance moves.
func TestCommitResultWithProvenanceFailurePublishesNeither(t *testing.T) {
	store := resultstore.New(1, []string{"/f/q.mp4"})
	seedFamilyResult(store, "/f/q.mp4", "res-q", "PROV-3", "")
	wrapped := &familyKeyedResultMap{ResultMapAccessor: store, updater: store, registry: newKeyedMutexRegistry()}
	cur, err := store.GetMovieResult("/f/q.mp4")
	require.NoError(t, err)
	prov := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "x"}}
	incoming := &resultstore.MovieResult{ResultID: "res-q", Movie: &models.Movie{ID: "PROV-9"}}
	require.Error(t, wrapped.CommitResultWithProvenance("/f/q.mp4", incoming, cur.Revision+99, prov))
	assert.Nil(t, store.GetProvenance("/f/q.mp4"), "failed commit publishes no provenance")
	still, err := store.GetMovieResult("/f/q.mp4")
	require.NoError(t, err)
	assert.Equal(t, "PROV-3", still.Movie.ID, "failed commit leaves the stored result untouched")
}

// Zero-value provenance and a nil updater both keep publish-side effects off
// (parity with the retired controller tail gate).
func TestCommitResultWithProvenanceGates(t *testing.T) {
	mk := func(withUpdater bool) (resultstore.Store, *familyKeyedResultMap) {
		store := resultstore.New(1, []string{"/f/g.mp4"})
		seedFamilyResult(store, "/f/g.mp4", "res-g", "G-1", "")
		var upd resultstore.ResultUpdater
		if withUpdater {
			upd = store
		}
		return store, &familyKeyedResultMap{ResultMapAccessor: store, updater: upd, registry: newKeyedMutexRegistry()}
	}
	// zero-value prov: nothing to publish
	s1, w1 := mk(true)
	cur, err := s1.GetMovieResult("/f/g.mp4")
	require.NoError(t, err)
	in := &resultstore.MovieResult{ResultID: "res-g", Movie: &models.Movie{ID: "G-2"}}
	require.NoError(t, w1.CommitResultWithProvenance("/f/g.mp4", in, cur.Revision, &resultstore.ProvenanceData{}))
	assert.Nil(t, s1.GetProvenance("/f/g.mp4"), "empty provenance is never published")
	// nil updater: commit proceeds, publish skipped
	s2, w2 := mk(false)
	cur2, err := s2.GetMovieResult("/f/g.mp4")
	require.NoError(t, err)
	require.NoError(t, w2.CommitResultWithProvenance("/f/g.mp4", in, cur2.Revision, &resultstore.ProvenanceData{FieldSources: map[string]string{"t": "x"}}))
	assert.Nil(t, s2.GetProvenance("/f/g.mp4"), "nil updater cannot publish")
}

// CompleteRescape publishes provenance through the fallback setter path when
// the ResultMap is the bare store (no keyed wrapper).
func TestCompleteRescapePublishesProvenance(t *testing.T) {
	store := resultstore.New(1, []string{"/f/c.mp4"})
	seedFamilyResult(store, "/f/c.mp4", "res-c", "CR-1", "")
	cur, err := store.GetMovieResult("/f/c.mp4")
	require.NoError(t, err)
	phase := &rescrapePhase{}
	prov := &resultstore.ProvenanceData{FieldSources: map[string]string{"studio": "dmm"}}
	in := &resultstore.MovieResult{ResultID: "res-c", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "CR-1"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/c.mp4", MovieID: "CR-1"}}
	outcome, err := phase.CompleteRescrape(rescrapePhaseInputs{ResultMap: store, Lifecycle: &JobLifecycle{Status: models.JobStatusCompleted, done: make(chan struct{})}}, "/f/c.mp4", in, cur.Revision, "CR-1", "CR-1", prov)
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	require.NotNil(t, store.GetProvenance("/f/c.mp4"), "bare-store fallback publishes provenance after commit")
	assert.Equal(t, map[string]string{"studio": "dmm"}, store.GetProvenance("/f/c.mp4").FieldSources)
}

// --- claimed-but-aborted queued launches (codex r45/r46) ---

// A launch cancelled WHILE QUEUED (claim ran, BeginPhase parked on a live
// shared lease) terminates coherently: Cancelled, marker CLEARED before the
// persist, phaseDone closed so Wait() joins, and pendingPhase drained.
func TestStartApplyAbortedQueuedLaunchIsCancelledAndPersists(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.lifecycle.Status = models.JobStatusCompleted
	persists := 0
	job.deps.PersistFn = func() error { persists++; return nil }
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- job.Controller().StartApply(ctx, ApplyPhaseConfig{}) }()
	assert.Eventually(t, func() bool {
		job.admission.mu.Lock()
		defer job.admission.mu.Unlock()
		return job.admission.pendingPhase == 1
	}, 2*time.Second, 5*time.Millisecond, "launch parked in the phase queue")
	cancel()
	require.NoError(t, <-done, "aborted launch reports success to the caller")
	assert.Equal(t, models.JobStatusCancelled, job.lifecycle.GetJobStatus())
	assert.Equal(t, "", job.lifecycle.CurrentPhase(), "marker cleared pre-persist (row stays edit-admissible)")
	assert.GreaterOrEqual(t, persists, 1, "terminal cancellation persisted without a phase goroutine")
	job.admission.mu.Lock()
	pending := job.admission.pendingPhase
	job.admission.mu.Unlock()
	assert.Zero(t, pending, "pendingPhase drained on abort")
	require.ErrorContains(t, job.Controller().Wait(), "cancelled")
	rel()
}

// --- apply write-back family lock keys (codex r37 P2) ---

func TestApplyFamilyLockKeyMatrix(t *testing.T) {
	locked := [][]string{}
	fn := func(ids ...string) func() { locked = append(locked, ids); return func() {} }
	inputs := applyPhaseInputs{EditLockFn: fn}

	unlock := applyFamilyLock(inputs, "AL-1", "CAN-1")
	unlock()
	assert.Equal(t, [][]string{{"AL-1", "CAN-1"}}, locked, "alias+canonical ride ONE variadic acquisition (registry dedups/folds/sorts)")

	locked = [][]string{}
	applyFamilyLock(inputs, "SAME-1", "same-1")()
	assert.Equal(t, [][]string{{"SAME-1", "same-1"}}, locked, "equal identities fold to one mutex inside AcquireMany")

	locked = [][]string{}
	applyFamilyLock(inputs, "", "CAN-2")()
	assert.Equal(t, [][]string{{"", "CAN-2"}}, locked, "empty alias skipped by AcquireMany")

	locked = [][]string{}
	applyFamilyLock(inputs, "AL-2", " ")()
	assert.Equal(t, [][]string{{"AL-2", " "}}, locked, "blank canonical skipped by AcquireMany")

	assert.NotPanics(t, func() { applyFamilyLock(applyPhaseInputs{}, "A", "B")() }, "nil EditLockFn is a no-op lock")
}

// codex r42 regression: the write-back acquisition rides the registry's
// FOLDED total order — an edit holding the matcher alias blocks the apply
// (no slip-through, no deadlock), even for fold-unstable pairs Z/_.
func TestApplyFamilyLockSharesRegistryTotalOrder(t *testing.T) {
	reg := newKeyedMutexRegistry()
	inputs := applyPhaseInputs{EditLockFn: func(ids ...string) func() { return reg.AcquireMany(ids) }}
	holdEdit := reg.Acquire("Z")
	done := make(chan struct{})
	go func() {
		applyFamilyLock(inputs, "Z", "_")()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("apply write-back dispatched while the edit held the alias key")
	case <-time.After(150 * time.Millisecond):
	}
	holdEdit()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("apply write-back deadlocked against the released edit key")
	}
}

func TestApplyFamilyKeyIDs(t *testing.T) {
	afc := &ApplyFileContext{
		Match: models.FileMatchInfo{MovieID: "CAN-3"},
		MovieResult: &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{MovieID: "AL-3"},
		},
	}
	alias, canon := applyFamilyKeyIDs(afc)
	assert.Equal(t, "AL-3", alias, "matcher alias comes from the stored result, not the rewritten cmd")
	assert.Equal(t, "CAN-3", canon)

	nilMR := &ApplyFileContext{Match: models.FileMatchInfo{MovieID: "CAN-4"}}
	a2, c2 := applyFamilyKeyIDs(nilMR)
	assert.Equal(t, "", a2)
	assert.Equal(t, "CAN-4", c2)
}
