package worker

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatedJobRepo records the Results JSON of every successful upsert and can
// (a) fail the NEXT upsert (simulating the edit's own envelope persist
// failing) and (b) signal + block when a successful upsert lands while an
// edit window is open, making the pre-fix race deterministic.
type gatedJobRepo struct {
	mu             sync.Mutex
	snapshots      []string
	failArmed      atomic.Bool
	windowOpen     atomic.Bool
	entered        chan struct{} // buffered; a successful upsert during the window signals here
	releasePersist chan struct{} // the blocked upsert waits for this
}

func (r *gatedJobRepo) Upsert(_ context.Context, job *models.Job) error {
	if r.failArmed.Swap(false) {
		return errors.New("injected upsert failure")
	}
	if r.windowOpen.Load() {
		select {
		case r.entered <- struct{}{}:
		default:
		}
		<-r.releasePersist
	}
	r.mu.Lock()
	r.snapshots = append(r.snapshots, job.Results)
	r.mu.Unlock()
	return nil
}

func (r *gatedJobRepo) Create(_ context.Context, _ *models.Job) error { return nil }
func (r *gatedJobRepo) Update(_ context.Context, _ *models.Job) error { return nil }
func (r *gatedJobRepo) FindByID(_ context.Context, _ string) (*models.Job, error) {
	return nil, nil
}
func (r *gatedJobRepo) List(_ context.Context) ([]models.Job, error) { return nil, nil }
func (r *gatedJobRepo) Delete(_ context.Context, _ string) error     { return nil }
func (r *gatedJobRepo) DeleteOrganizedOlderThan(_ context.Context, _ time.Time) error {
	return nil
}

// TestPersistFn_EnvelopeLock_DoesNotCaptureUncommittedEdit is the regression
// test for the review finding that phase-completion persistence (PersistFn)
// ran WITHOUT the per-job envelope lock: an edit inside its
// commit→persist→rollback window could have its committed mutation durably
// captured by a concurrent phase-boundary persist; when the edit's own
// persist then failed and it compensated only in memory, the rejected edit
// reappeared after restart despite the API erroring.
//
// Pre-fix this fails deterministically: the phase persist reaches Upsert
// while the edit window is open and captures "REJECTED-EDIT". Post-fix the
// phase persist blocks on the envelope lock until the window (including the
// in-memory rollback) closes, then persists only rolled-back state.
func TestPersistFn_EnvelopeLock_DoesNotCaptureUncommittedEdit(t *testing.T) {
	repo := &gatedJobRepo{
		entered:        make(chan struct{}, 1),
		releasePersist: make(chan struct{}),
	}
	jq := NewJobStore(repo, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	const filePath = "file1.mp4"
	baseline := func() *resultstore.MovieResult {
		return &resultstore.MovieResult{
			Status: models.JobStatusCompleted,
			Movie:  &models.Movie{ID: "TEST-001", Title: "baseline-title"},
		}
	}
	job.results.UpdateFileResult(filePath, baseline())

	committed := make(chan struct{})
	phasePersistDone := make(chan struct{})
	editPersistErr := make(chan error, 1)

	// Edit goroutine, mimicking the API edit handler's critical section:
	// envelope lock → commit mutation → persist (FAILS) → in-memory rollback → release.
	go func() {
		release := AcquireJobEnvelopeLock(job.ID.String())
		defer release()
		repo.windowOpen.Store(true)
		job.results.UpdateFileResult(filePath, &resultstore.MovieResult{
			Status: models.JobStatusCompleted,
			Movie:  &models.Movie{ID: "TEST-001", Title: "REJECTED-EDIT"},
		})
		close(committed)

		repo.failArmed.Store(true)
		editPersistErr <- jq.persistence.PersistJob(job) // fails → handler compensates

		// Give the unlocked (pre-fix) phase persist a deterministic chance — and
		// with the gate, certainty — to reach Upsert while the mutation is
		// still committed. Post-fix it is blocked on the envelope lock, so
		// `entered` never fires and we proceed after a short wait.
		select {
		case <-repo.entered:
			close(repo.releasePersist)
		case <-time.After(2 * time.Second):
		}

		// Persist failed: the edit rolls its mutation back in memory only.
		job.results.UpdateFileResult(filePath, baseline())
		repo.windowOpen.Store(false)
	}()

	// Phase-completion persist firing from a separate goroutine while the
	// edit is inside its window.
	go func() {
		<-committed
		job.deps.PersistFn()
		close(phasePersistDone)
	}()

	select {
	case <-phasePersistDone:
	case <-time.After(10 * time.Second):
		t.Fatal("phase-completion persist never returned (possible deadlock)")
	}
	require.Error(t, <-editPersistErr, "the edit's own persist must fail for the rollback scenario")

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.NotEmpty(t, repo.snapshots, "phase-completion persist must have written an envelope")
	for i, snap := range repo.snapshots {
		assert.False(t, strings.Contains(snap, "REJECTED-EDIT"),
			"envelope snapshot %d captured the edit's committed-but-later-rolled-back mutation", i)
	}
}
