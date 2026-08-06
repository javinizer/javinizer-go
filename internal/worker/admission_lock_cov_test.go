package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

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
		p, _ := b.BeginPhase()
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
		_, err := b.BeginPhase()
		started <- err
	}()
	time.Sleep(40 * time.Millisecond)
	b.MarkGone()
	rel()
	require.ErrorIs(t, <-started, ErrJobGone)
}

func TestPhaseEntryFailReleasesLease(t *testing.T) {
	b := newAdmissionBarrier()
	p, err := b.BeginPhase()
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
