package worker

import (
	"context"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// JobLifecycle manages the lifecycle state and transitions of a batch job.
//
// Lock ordering: lifecycle.mu → results.mu → job.mu. Never acquire in reverse order.
type JobLifecycle struct {
	mu     sync.RWMutex
	Status models.JobStatus
	// currentPhase records which phase ("scrape"/"apply") owns a Running job.
	// Persisted on the job envelope at phase entry so a restart can tell an
	// apply-phase Running job (still editable) from a scrape-phase one
	// (not editable). "" when no phase is running (POSTER-WRITE-HARDENING D16).
	currentPhase string
	CompletedAt  *time.Time
	OrganizedAt  *time.Time
	RevertedAt   *time.Time
	done         chan struct{}
	// phaseDone closes only when the phase goroutine fully RETURNS — after
	// Run's deferred persistence, unlike done, which terminal Mark* calls close
	// mid-Run. Wait() prefers phaseDone so callers join the phase (all DB
	// writes finished), not merely observe its terminal status. nil when no
	// phase is running (fresh/reconstructed jobs), in which case Wait falls
	// back to done. Created alongside done by markStarted.
	phaseDone  chan struct{}
	CancelFunc context.CancelFunc
	deleted    bool
	cancelled  bool // prevents double-invocation of cancelFunc in cancelAndMarkCancelled

	// markCompletedFn is a callback set by BatchJob during construction.
	// MarkCompleted crosses the lifecycle/results boundary
	// (it sets status AND recalculates progress). The callback lets
	// JobLifecycle satisfy PhaseLifecycle without knowing about ResultTracker.
	//
	// Lock ordering contract: markCompletedFn is called while holding lifecycle.mu,
	// and the callback acquires results.mu and then job.mu. The required order is:
	//
	//	lifecycle.mu → results.mu → job.mu
	//
	// Any callback assigned here MUST NOT acquire locks in the reverse order,
	// or a deadlock will result.
	markCompletedFn func()
}

// IsJobActive reports whether the job is active in the rescrape-management sense (Pending or Completed).
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

// IsDeleted reports whether the job has been marked deleted.
func (lc *JobLifecycle) IsDeleted() bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.deleted
}

func (lc *JobLifecycle) cancelAndMarkCancelled() {
	lc.mu.Lock()
	if lc.cancelled {
		// Already cancelled — another goroutine won the race.
		lc.mu.Unlock()
		return
	}
	cancelFunc := lc.CancelFunc
	if lc.Status == models.JobStatusCompleted || lc.Status == models.JobStatusFailed || lc.Status == models.JobStatusOrganized || lc.Status == models.JobStatusReverted {
		// codex r48 P2: a cancel RACING terminal completion must not pin
		// cancelled=true on a COMPLETED job — the flag would permanently
		// reject later phase launches (Cancel() semantics are live-phase
		// scoped; the job already settled cleanly).
		lc.clearCurrentPhaseLocked()
		lc.mu.Unlock()
		if cancelFunc != nil {
			cancelFunc()
		}
		return
	}
	lc.cancelled = true
	lc.Status = models.JobStatusCancelled
	// codex r38: keep currentPhase set while the phase goroutine drains —
	// clearing it here would admit edits mid-drain and let late scrape
	// writes overwrite them. The phase goroutine's final persist clears it.
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

// Cancel marks the job cancelled and invokes its context cancel function.
func (lc *JobLifecycle) Cancel() {
	lc.cancelAndMarkCancelled()
}

// MarkFailed transitions the job to failed unless it has already reached a terminal state.
func (lc *JobLifecycle) MarkFailed() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.Status == models.JobStatusCompleted || lc.Status == models.JobStatusCancelled || lc.Status == models.JobStatusOrganized || lc.Status == models.JobStatusReverted {
		return
	}
	lc.Status = models.JobStatusFailed
	lc.CompletedAt = nowTimePtr()
	lc.clearCurrentPhaseLocked()
	lc.closeDoneLocked()
}

// MarkCancelled transitions the job to cancelled unless it has already reached a terminal state.
func (lc *JobLifecycle) MarkCancelled() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.Status == models.JobStatusCompleted || lc.Status == models.JobStatusFailed || lc.Status == models.JobStatusOrganized || lc.Status == models.JobStatusReverted {
		lc.clearCurrentPhaseLocked()
		return
	}
	lc.Status = models.JobStatusCancelled
	lc.CompletedAt = nowTimePtr()
	lc.clearCurrentPhaseLocked()
	lc.closeDoneLocked()
}

// MarkOrganized transitions the job to organized unless it has reached a terminal non-success state.
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
	lc.clearCurrentPhaseLocked()
	lc.closeDoneLocked()
}

// MarkReverted transitions the job to reverted, idempotent when already reverted.
func (lc *JobLifecycle) MarkReverted() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.Status == models.JobStatusReverted {
		return
	}
	lc.Status = models.JobStatusReverted
	lc.RevertedAt = nowTimePtr()
	lc.clearCurrentPhaseLocked()
	lc.closeDoneLocked()
}

// GetJobStatus returns the current job status.
func (lc *JobLifecycle) GetJobStatus() models.JobStatus {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.Status
}

// SetDeleted sets the job's deleted flag.
func (lc *JobLifecycle) SetDeleted(deleted bool) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.deleted = deleted
}

// MarkCompleted transitions the job to completed state and invokes the
// markCompletedFn callback (set by BatchJob) for cross-boundary operations
// like progress recalculation. this lets JobLifecycle satisfy
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
	lc.clearCurrentPhaseLocked()
	lc.closeDoneLocked()
	if lc.markCompletedFn != nil {
		lc.markCompletedFn()
	}
}

// LifecycleSnapshot holds a point-in-time copy of the lifecycle fields
// needed for batch job status snapshots. BatchJob consumes
// its own sub-manager interfaces instead of reaching into internals.
type LifecycleSnapshot struct {
	Status       models.JobStatus
	CurrentPhase string
	CompletedAt  *time.Time
	OrganizedAt  *time.Time
	RevertedAt   *time.Time
	IsDeleted    bool
}

// StatusSnapshot returns a point-in-time copy of the lifecycle fields needed
// for batch job status snapshots. The caller must NOT be holding lifecycle.mu
// when calling this method (it acquires the lock internally).
func (lc *JobLifecycle) StatusSnapshot() LifecycleSnapshot {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return LifecycleSnapshot{
		Status:       lc.Status,
		CurrentPhase: lc.currentPhase,
		CompletedAt:  cloneTimePtr(lc.CompletedAt),
		OrganizedAt:  cloneTimePtr(lc.OrganizedAt),
		RevertedAt:   cloneTimePtr(lc.RevertedAt),
		IsDeleted:    lc.deleted,
	}
}

// statusSnapshotLocked returns a point-in-time copy of the lifecycle fields.
// The caller MUST be holding lifecycle.mu when calling this method.
func (lc *JobLifecycle) statusSnapshotLocked() LifecycleSnapshot {
	return LifecycleSnapshot{
		Status:       lc.Status,
		CurrentPhase: lc.currentPhase,
		CompletedAt:  cloneTimePtr(lc.CompletedAt),
		OrganizedAt:  cloneTimePtr(lc.OrganizedAt),
		RevertedAt:   cloneTimePtr(lc.RevertedAt),
		IsDeleted:    lc.deleted,
	}
}

// SetCurrentPhase records the phase that owns the Running state. Call at
// phase entry (markStarted) before the phase-entry envelope persist so the
// marker is durable before any phase work begins (D16 fail-closed).
func (lc *JobLifecycle) SetCurrentPhase(phase string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.currentPhase = phase
}

// CurrentPhase returns the phase that owns the Running state ("" when idle).
func (lc *JobLifecycle) CurrentPhase() string {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.currentPhase
}

// clearCurrentPhaseLocked resets the phase marker. Callers MUST hold lc.mu.
func (lc *JobLifecycle) clearCurrentPhaseLocked() { lc.currentPhase = "" }

// Compile-time assertion: JobLifecycle satisfies JobCanceller.
var _ JobCanceller = (*JobLifecycle)(nil)

func nowTimePtr() *time.Time {
	now := time.Now()
	return &now
}
