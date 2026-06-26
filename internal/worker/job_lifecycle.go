package worker

import (
	"context"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Lock ordering: lifecycle.mu → results.mu → job.mu. Never acquire in reverse order.
type JobLifecycle struct {
	mu          sync.RWMutex
	Status      models.JobStatus
	CompletedAt *time.Time
	OrganizedAt *time.Time
	RevertedAt  *time.Time
	done        chan struct{}
	CancelFunc  context.CancelFunc
	deleted     bool
	cancelled   bool // prevents double-invocation of cancelFunc in cancelAndMarkCancelled

	// markCompletedFn is a callback set by BatchJob during construction.
	// Per ADR-0041: MarkCompleted crosses the lifecycle/results boundary
	// (it sets status AND recalculates progress). The callback lets
	// JobLifecycle satisfy PhaseLifecycle without knowing about ResultTracker.
	//
	// Lock ordering contract: markCompletedFn is called while holding lifecycle.mu,
	// and the callback acquires results.mu and then job.mu. The required order is:
	//
	//	lifecycle.mu → results.mu → job.mu
	//
	// Any callback assigned here MUST NOT acquire locks in the reverse order,
	// or a deadlock will result. See ADR-0041 for the full rationale.
	markCompletedFn func()
}

func (lc *JobLifecycle) IsJobActive() bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	if lc.deleted {
		return false
	}
	// "Active" here is the narrow rescrape-management sense preserved by
	// TestBatchJob_IsJobActive: Pending (not yet started) and Completed (done,
	// still eligible for further action) are active; every in-flight or
	// terminal state (Running/Organized/Failed/Cancelled/Reverted) is not.
	// This is intentionally NOT `!isJobTransitioned`: isJobTransitioned serves
	// the gone-check (which excludes Organized so Organized jobs stay rescrapeable)
	// and would wrongly mark Organized active here. Spell the contract out.
	return lc.Status == models.JobStatusPending || lc.Status == models.JobStatusCompleted
}

func (lc *JobLifecycle) IsDeleted() bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.deleted
}

func (lc *JobLifecycle) setCancelFunc(cancelFunc context.CancelFunc) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.CancelFunc = cancelFunc
}

func (lc *JobLifecycle) cancelAndMarkCancelled() {
	lc.mu.Lock()
	if lc.cancelled {
		// Already cancelled — another goroutine won the race.
		lc.mu.Unlock()
		return
	}
	lc.cancelled = true
	cancelFunc := lc.CancelFunc
	if lc.Status == models.JobStatusCompleted || lc.Status == models.JobStatusFailed || lc.Status == models.JobStatusOrganized || lc.Status == models.JobStatusReverted {
		lc.mu.Unlock()
		if cancelFunc != nil {
			cancelFunc()
		}
		return
	}
	lc.Status = models.JobStatusCancelled
	lc.CompletedAt = nowTimePtr()
	lc.closeDoneLocked()
	lc.mu.Unlock()

	if cancelFunc != nil {
		cancelFunc()
	}
}

func (lc *JobLifecycle) markDeleted() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.deleted = true
}

// Done returns a channel that is closed when the job reaches a terminal state.
// Callers can select on this to wait for a job to finish after requesting cancellation,
// matching the idiomatic Go context.Done pattern.
func (lc *JobLifecycle) Done() <-chan struct{} {
	return lc.done
}

func (lc *JobLifecycle) closeDoneLocked() {
	select {
	case <-lc.done:
	default:
		close(lc.done)
	}
}

func (lc *JobLifecycle) Cancel() {
	lc.cancelAndMarkCancelled()
}

func (lc *JobLifecycle) MarkFailed() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.Status == models.JobStatusCompleted || lc.Status == models.JobStatusCancelled || lc.Status == models.JobStatusOrganized || lc.Status == models.JobStatusReverted {
		return
	}
	lc.Status = models.JobStatusFailed
	lc.CompletedAt = nowTimePtr()
	lc.closeDoneLocked()
}

func (lc *JobLifecycle) MarkCancelled() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.Status == models.JobStatusCompleted || lc.Status == models.JobStatusFailed || lc.Status == models.JobStatusOrganized || lc.Status == models.JobStatusReverted {
		return
	}
	lc.Status = models.JobStatusCancelled
	lc.CompletedAt = nowTimePtr()
	lc.closeDoneLocked()
}

func (lc *JobLifecycle) MarkOrganized() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	// Guard terminal non-success states: a Cancelled/Failed job must not be
	// clobbered by a later Organized. (Organized/Reverted already short-circuit
	// here.) Completed→Organized remains a valid success upgrade.
	if lc.Status == models.JobStatusOrganized || lc.Status == models.JobStatusReverted ||
		lc.Status == models.JobStatusCancelled || lc.Status == models.JobStatusFailed {
		return
	}
	lc.Status = models.JobStatusOrganized
	lc.OrganizedAt = nowTimePtr()
	lc.closeDoneLocked()
}

func (lc *JobLifecycle) MarkReverted() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.Status == models.JobStatusReverted {
		return
	}
	lc.Status = models.JobStatusReverted
	lc.RevertedAt = nowTimePtr()
	lc.closeDoneLocked()
}

func (lc *JobLifecycle) GetJobStatus() models.JobStatus {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.Status
}

func (lc *JobLifecycle) SetDeleted(deleted bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.deleted = deleted
}

// MarkCompleted transitions the job to completed state and invokes the
// markCompletedFn callback (set by BatchJob) for cross-boundary operations
// like progress recalculation. Per ADR-0041: this lets JobLifecycle satisfy
// PhaseLifecycle while keeping the results-aware logic in the callback.
func (lc *JobLifecycle) MarkCompleted() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	// Guard terminal non-success states: a Cancelled/Failed job must not be
	// clobbered by a later Completed. (Organized/Reverted already short-circuit
	// here.) Re-Completed on an already-Completed job stays idempotent.
	if lc.Status == models.JobStatusOrganized || lc.Status == models.JobStatusReverted ||
		lc.Status == models.JobStatusCancelled || lc.Status == models.JobStatusFailed {
		return
	}
	lc.Status = models.JobStatusCompleted
	lc.CompletedAt = nowTimePtr()
	lc.closeDoneLocked()
	if lc.markCompletedFn != nil {
		lc.markCompletedFn()
	}
}

// LifecycleSnapshot holds a point-in-time copy of the lifecycle fields
// needed for batch job status snapshots. Per ADR-0041/0042: BatchJob consumes
// its own sub-manager interfaces instead of reaching into internals.
type LifecycleSnapshot struct {
	Status      models.JobStatus
	CompletedAt *time.Time
	OrganizedAt *time.Time
	RevertedAt  *time.Time
	IsDeleted   bool
}

// StatusSnapshot returns a point-in-time copy of the lifecycle fields needed
// for batch job status snapshots. The caller must NOT be holding lifecycle.mu
// when calling this method (it acquires the lock internally).
func (lc *JobLifecycle) StatusSnapshot() LifecycleSnapshot {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return LifecycleSnapshot{
		Status:      lc.Status,
		CompletedAt: cloneTimePtr(lc.CompletedAt),
		OrganizedAt: cloneTimePtr(lc.OrganizedAt),
		RevertedAt:  cloneTimePtr(lc.RevertedAt),
		IsDeleted:   lc.deleted,
	}
}

// statusSnapshotLocked returns a point-in-time copy of the lifecycle fields.
// The caller MUST be holding lifecycle.mu when calling this method.
func (lc *JobLifecycle) statusSnapshotLocked() LifecycleSnapshot {
	return LifecycleSnapshot{
		Status:      lc.Status,
		CompletedAt: cloneTimePtr(lc.CompletedAt),
		OrganizedAt: cloneTimePtr(lc.OrganizedAt),
		RevertedAt:  cloneTimePtr(lc.RevertedAt),
		IsDeleted:   lc.deleted,
	}
}

// Compile-time assertion: JobLifecycle satisfies JobCanceller.
var _ JobCanceller = (*JobLifecycle)(nil)

func nowTimePtr() *time.Time {
	now := time.Now()
	return &now
}
