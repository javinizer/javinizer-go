package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Typed errors for edit admission and lifecycle fencing
// (POSTER-WRITE-HARDENING D3/D16). The API layer maps these via errors.Is to
// stable HTTP codes (mapBatchEditError): UnknownJob → 404, Gone → 410,
// PhaseBusy/JobBusy → 409.
var (
	// ErrJobNotFound — the job ID is unknown to the store.
	ErrJobNotFound = errors.New("job not found")
	// ErrJobGone — the job was explicitly deleted recently (tombstone), or its
	// admission barrier reports gone. Distinct from never-existed so clients
	// can stop retrying.
	ErrJobGone = errors.New("job was deleted")
	// ErrEditNotAdmitted — the job's lifecycle does not accept review edits
	// right now (Pending or Running with an active scrape phase).
	ErrEditNotAdmitted = errors.New("job does not accept edits in its current state")
	// ErrMovieFamilyEmpty — no in-memory results resolve to the requested movie
	// inside the keyed edit section (e.g. concurrent rescrape rekey removed it).
	ErrMovieFamilyEmpty = errors.New("movie result not found for family")
	// ErrJobBusy — another operation (delete-drain or phase transition) holds
	// the barrier non-sharably right now. Callers retry or surface 409.
	ErrJobBusy = errors.New("another operation is in progress on this job")
	// ErrFamilyRekeyed — an override's family assignment changed between the
	// pre-lock resolution and the locked section; the operation retries on the
	// correct key (codex P1-B). Should never escape the retry loop.
	ErrFamilyRekeyed = errors.New("result's movie family changed during edit")
)

// EditPhaseBusyError carries the rejecting lifecycle detail for 409 responses.
type EditPhaseBusyError struct {
	JobID  string
	Status models.JobStatus
	Phase  string
}

func (e *EditPhaseBusyError) Error() string {
	if e.Phase != "" {
		return fmt.Sprintf("job %s is %s (%s phase in progress); edits are not accepted", e.JobID, e.Status, e.Phase)
	}
	return fmt.Sprintf("job %s is %s; edits are not accepted", e.JobID, e.Status)
}

// Is anchors errors.Is on ErrEditNotAdmitted so handlers classify admission rejections without the concrete type.
func (e *EditPhaseBusyError) Is(target error) bool { return target == ErrEditNotAdmitted }

// admissionBarrier is a per-job shared/exclusive lease gate with writer
// preference, built on sync.Cond so the exclusive→shared "phase entry
// downgrade" is genuinely atomic (Go's sync.RWMutex CANNOT downgrade when a
// writer is pending). POSTER-WRITE-HARDENING D1/D3 semantics:
//
//   - edit/exclusion/rescrape ops take SHARED leases
//   - DeleteJob takes the EXCLUSIVE lease (drains all shared leases first;
//     pending exclusive admissions block new shared ones — no stall-through)
//   - phase starts use BeginPhase: they QUEUE behind in-flight ops (never
//     silently discarded) then atomically downgrade into the shared lease held for the phase,
//     closing the admission-vs-transition TOCTOU against concurrent rescrape
//     or edit admissions.
type admissionBarrier struct {
	mu               sync.Mutex
	cond             *sync.Cond
	shared           int
	exclusive        bool
	pendingExclusive int
	pendingPhase     int
	gone             atomic.Bool
}

func newAdmissionBarrier() *admissionBarrier {
	b := &admissionBarrier{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

// AdmitShared acquires a shared lease. Blocks while an exclusive lease is
// held OR pending (writer preference: a waiting delete drains the job).
// Returns ErrJobGone when the barrier has been marked gone.
func (b *admissionBarrier) AdmitShared() (release func(), err error) {
	b.mu.Lock()
	if b.gone.Load() {
		b.mu.Unlock()
		return nil, ErrJobGone
	}
	for b.exclusive || b.pendingExclusive > 0 || b.pendingPhase > 0 {
		b.cond.Wait()
	}
	// A waiter that queued BEFORE MarkGone must not proceed with the lease —
	// re-check after the wait loop (codex r12: delete-during-wait window).
	if b.gone.Load() {
		b.mu.Unlock()
		return nil, ErrJobGone
	}
	b.shared++
	b.mu.Unlock()
	return b.releaseShared, nil
}

func (b *admissionBarrier) releaseShared() {
	b.mu.Lock()
	b.shared--
	if b.shared == 0 {
		b.cond.Broadcast()
	}
	b.mu.Unlock()
}

// AdmitExclusive acquires the exclusive lease (delete-drain): waits for all
// shared leases to be released; new shared admissions wait behind it.
func (b *admissionBarrier) AdmitExclusive() (release func(), err error) {
	b.mu.Lock()
	b.pendingExclusive++
	for b.shared > 0 || b.exclusive {
		b.cond.Wait()
	}
	b.pendingExclusive--
	b.exclusive = true
	b.mu.Unlock()
	return b.releaseExclusive, nil
}

// TryAdmitExclusive attempts the exclusive lease without blocking. Used by
// DeleteJob's fail-fast discrimination (Running-holder ⇒ busy immediately).
func (b *admissionBarrier) TryAdmitExclusive() (release func(), ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Queued phase starts and queued deletes CARRY intent — a cleanup-style
	// exclusive grab must not cut in front of them (codex P3 R6-5), and a
	// blocked try can never strand an indecisive waiter either.
	if b.shared > 0 || b.exclusive || b.pendingExclusive > 0 || b.pendingPhase > 0 || b.gone.Load() {
		return nil, false
	}
	b.exclusive = true
	return b.releaseExclusive, true
}

// EnterExclusiveWait registers delete-writer intent WITHOUT blocking: while
// pendingExclusive > 0 new shared admissions park in AdmitShared, so a
// delete polling under sustained edit traffic drains deterministically
// (codex r14-A). Pair with PollExclusiveWait / CancelExclusiveWait.
func (b *admissionBarrier) EnterExclusiveWait() {
	b.mu.Lock()
	b.pendingExclusive++
	b.mu.Unlock()
}

// PollExclusiveWait attempts the exclusive grab for a waiter that entered via
// EnterExclusiveWait. On success the pending counter is consumed.
func (b *admissionBarrier) PollExclusiveWait() (release func(), ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.shared > 0 || b.exclusive || b.gone.Load() {
		return nil, false
	}
	if b.pendingExclusive == 0 {
		// contract bug anchor: Poll without Enter.
		return nil, false
	}
	b.pendingExclusive--
	b.exclusive = true
	return b.releaseExclusive, true
}

// CancelExclusiveWait deregisters a waiter that gave up (running-reject path
// or timeout).
func (b *admissionBarrier) CancelExclusiveWait() {
	b.mu.Lock()
	if b.pendingExclusive > 0 {
		b.pendingExclusive--
		b.cond.Broadcast()
	}
	b.mu.Unlock()
}

func (b *admissionBarrier) releaseExclusive() {
	b.mu.Lock()
	b.exclusive = false
	b.cond.Broadcast()
	b.mu.Unlock()
}

// phaseEntry is the exclusive token from BeginPhase. Exactly one of
// Fail (start rejected) or Downgrade (start committed) must run.
type phaseEntry struct{ b *admissionBarrier }

// BeginPhase takes the exclusive lease for the phase transition window,
// WAITING for in-flight operations (edit/rescrape) to finish rather than
// failing busy: a phase launch is a queued decision, never a silently
// discarded one (codex P3-A). Returns ErrJobGone when the job was deleted
// while waiting (MarkGone wakes the queue as well). The wait honors ctx:
// caller cancellation drains the pendingPhase registration (codex P2-B) and
// returns the context error instead of starting an abandoned phase.
func (b *admissionBarrier) BeginPhase(ctx context.Context) (*phaseEntry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// Writer-preference accounting: the queued phase start blocks new shared
	// admissions (pendingPhase) and yields to queued deletes — an exclusive
	// waiter must starve a phase, never the reverse (codex P4-A).
	b.pendingPhase++
	// codex r36 P2: the wait must honor caller cancellation — a client that
	// disconnects while queued behind a long shared op (rescrape / poster
	// download) would otherwise leave pendingPhase registered (blocking every
	// new shared admission) and could start a phase with an already-cancelled
	// context. Wake on ctx.Done() and drain the counter on the way out.
	if done := ctx.Done(); done != nil {
		stop := context.AfterFunc(ctx, func() {
			b.mu.Lock()
			b.cond.Broadcast()
			b.mu.Unlock()
		})
		defer stop()
	}
	for {
		if b.gone.Load() {
			b.pendingPhase--
			return nil, ErrJobGone
		}
		if err := ctx.Err(); err != nil {
			b.pendingPhase--
			b.cond.Broadcast()
			return nil, err
		}
		if b.shared == 0 && !b.exclusive && b.pendingExclusive == 0 {
			b.pendingPhase--
			b.exclusive = true
			return &phaseEntry{b: b}, nil
		}
		b.cond.Wait()
	}
}

// Fail releases the start-window exclusive lease without transitioning.
func (p *phaseEntry) Fail() { p.b.releaseExclusive() }

// Downgrade atomically converts the exclusive start-window lease into the
// shared phase lease (safe with a condvar barrier). Returns the shared
// release function held for the phase duration.
func (p *phaseEntry) Downgrade() (release func()) {
	b := p.b
	b.mu.Lock()
	b.exclusive = false
	b.shared++
	b.cond.Broadcast()
	b.mu.Unlock()
	return b.releaseShared
}

// MarkGone flags the barrier as gone. It MUST be called while holding the
// exclusive lease so no new shared lease can slip in after the flag. Wakes
// queued phase-start waiters so they can exit with ErrJobGone instead of
// parking on the cond var forever.
func (b *admissionBarrier) MarkGone() {
	b.mu.Lock()
	b.gone.Store(true)
	b.cond.Broadcast()
	b.mu.Unlock()
}

// IsGone reports whether the barrier has been marked gone.
func (b *admissionBarrier) IsGone() bool { return b.gone.Load() }

// tombstoneRegistry tracks recently deleted job IDs so the store can answer
// 410-gone (deleted) instead of 404-unknown for a bounded window
// (POSTER-WRITE-HARDENING D3).
type tombstoneRegistry struct {
	mu   sync.Mutex
	byID map[string]time.Time
	ttl  time.Duration
}

const defaultTombstoneTTL = 10 * time.Minute

func newTombstoneRegistry(ttl time.Duration) *tombstoneRegistry {
	if ttl <= 0 {
		ttl = defaultTombstoneTTL
	}
	return &tombstoneRegistry{byID: make(map[string]time.Time), ttl: ttl}
}

// Unmark removes a tombstone — a fresh job with the same ID makes the old
// deletion marker moot (codex r36): without it every PersistJob for the new
// job would skip silently while its edits hang in memory.
func (t *tombstoneRegistry) Unmark(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byID, id)
}

func (t *tombstoneRegistry) Mark(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.byID[id] = time.Now()
}

// Contains reports whether id is a live tombstone. Expired entries are swept
// lazily inside the registry lock.
func (t *tombstoneRegistry) Contains(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	exp, ok := t.byID[id]
	if !ok {
		return false
	}
	if time.Since(exp) > t.ttl {
		delete(t.byID, id)
		return false
	}
	return true
}

// editAdmissionError gates review edits (POSTER-WRITE-HARDENING D16):
// admitted: terminal statuses, Running-with-apply-phase; rejected 409 otherwise.
// Pending rejects regardless of the marker; a non-empty phase marker always
// rejects — even a terminal-looking status who still has its marker set is a
// cancelling-but-not-yet-drained phase whose late write-back would clobber
// the incoming edit (codex P1-D).
func editAdmissionError(jobID string, status models.JobStatus, currentPhase string) error {
	if status == models.JobStatusRunning {
		if currentPhase == string(JobPhaseApply) {
			return nil
		}
		// scrape marker, unknown/legacy marker, or no marker at all — an
		// in-flight or unlabelled running phase never admits edits.
		return &EditPhaseBusyError{JobID: jobID, Status: status, Phase: currentPhase}
	}
	if currentPhase != "" {
		// Terminal-looking status + a lingering phase marker = a cancelled
		// phase still draining (codex P1-D): its late write-back would clobber
		// an admitted edit.
		return &EditPhaseBusyError{JobID: jobID, Status: status, Phase: currentPhase}
	}
	if status == models.JobStatusPending {
		return &EditPhaseBusyError{JobID: jobID, Status: status}
	}
	return nil
}
