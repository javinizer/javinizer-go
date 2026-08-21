package history

// POSTER-WRITE-HARDENING wave-49 (codex P2, PR#215 — "do not abandon a sweep
// while it owns a busy marker"): the wave-8 bounded pre-revert sweep abandons
// its goroutine at the deadline, and wave-46 gates sweepOne's ENTRY on the
// context — but a cancellation landing INSIDE sweepOne (the fs calls are
// uninterruptible) leaves the goroutine holding the destination's .dlbusy
// marker after the revert already continued. That marker carries this
// process's own live PID (created after boot), so the continued revert's
// AcquireReplacementBusy refused with ErrReplacementBusy for the whole
// stranding — a well-formed marker nobody still waits on busy-blocking the
// batch's own revert.
//
// The sweep journals every busy-marker claim into an in-process,
// context-scoped ledger BEFORE the claim can outlive its waiter: sweep callers
// record (dest → {ctx, marker release}) the instant a claim lands. The
// reverter's busy leg consults the ledger (reclaimAbandonedSweepBusyMarker): a
// record whose sweep context is DONE belongs to an abandoned sweep, and its
// marker is revoked BEST-EFFORT by invoking the claim's own release closure —
// the wave-38 once-guarded, token-bound take-aside unlink — which:
//
//   - frees ONLY the exact well-formed marker the abandoned sweep planted
//     (never a pathname delete of a successor's marker), and
//   - consumes the sync.Once, so the stranded goroutine's deferred release
//     that fires when its wedged fs call finally answers is a no-op — no
//     double-free, and a fresh claimant's marker is never removed.
//
// A record whose sweep context is still LIVE belongs to an in-flight sweep
// someone still waits on: the reclaim refuses and the revert keeps the
// pre-wave-49 busy posture. The ledger is in-process by construction —
// cross-process markers keep their PID-liveness arbitration unchanged.
//
// Wave-50 (codex P2, PR#215 findings F1+F2):
//
//   - F1 (reverter_replacements_p3.go dest-lock acquisition): the stranded
//     goroutine parks on its wedged fs call while ALSO holding the
//     SharedDestLocks destination lock (taken inside sweepOne's arbitration
//     legs / the full-sweep ledger leg), and the wave-49 marker-only reclaim
//     could never free THAT: the continued revert blocked on the dest lock
//     BEFORE its ledger consult and hung indefinitely for exactly the
//     stranding the reclaim existed to heal. The record binds BOTH
//     arbitration holds (the dest-lock release through the claim's pending
//     cell — wave-52 below). The reverter consults the ledger FIRST (before
//     the blocking acquisition) and retries the acquisition against the
//     freed arbitration; the reclaim runs the dest-lock release AHEAD of the
//     marker release because a keyed-registry release is pure in-process
//     work that cannot wedge on the stranded filesystem (the marker's
//     take-aside unlink may — the established wave-49 posture). Both releases
//     share once-guards with the stranded goroutine's own defers, so the
//     late defers are no-ops and no successor's marker or lock is ever freed
//     twice. A LIVE record still refuses: the revert keeps its ordinary
//     blocking-then-conditional posture for sweeps someone waits on.
//
//   - F2 (stable claim keys): recordSweepBusyClaim and
//     reclaimAbandonedSweepBusyMarker used to derive the ledger key through
//     two INDEPENDENT sweepSlash (fsutil.DestKey) calls, which re-probe the
//     destination root's case/normalization posture PER CALL. A transient
//     probe failure at record time followed by a definitive probe at reclaim
//     time split ONE claim across two keys — the reclaim missed the record
//     and the destination kept its busy refusal (ErrReplacementBusy) for the
//     whole stranding. The ledger now derives every key through ONE
//     ledger-lifetime fsutil.DestKeyResolver (the wave-25/wave-45
//     posture-freeze discipline): each root's posture freezes at first use,
//     so record and reclaim always agree regardless of probe drift. The
//     sweep-only legs (journal spelling comparisons through sweepSlash) are
//     unchanged.
//
// Wave-52 (codex local review round 7, PR#215 finding F2 — "register the
// claim at busy-marker acquire time"): the wave-50 order (acquire the dest
// lock, THEN record the claim) left a fundamentally unhealable interval — a
// sweep ctx expiring DURING the blocking dest-lock wait owned the marker
// with NO ledger record at all, so the reverter's reclaim consult found
// nothing and the destination kept busy-refusing (ErrReplacementBusy) for
// the whole stranding. The record now lands at MARKER ACQUIRE TIME — before
// the dest-lock wait — born binding the marker release and carrying a
// PENDING CELL for the dest-lock release (bindDestLock fills it the instant
// the wait completes). The cell handshake preserves the wave-50 F1
// guarantees without the window: an owned marker is ledger-visible for the
// entire wait; a held lock is never untracked (the cell is populated
// atomically with acquisition completion, under the claim mutex the reclaim
// also takes); and a reclaim landing mid-wait marks the cell reclaimed, so
// the wait's late completion hands the just-acquired lock back to the
// stranded goroutine as its SOLE release responsibility with the revocation
// flag already visible (the goroutine abandons before any further work).
//
// Wave-51 (codex P1, PR#215 — "do not revoke the lock while the sweep is
// still running"): the wave-49/50 reclaim fired the recorded releases even
// though the abandoned goroutine's wedged fs call cannot observe
// cancellation — the continued revert then mutated the destination while the
// stranded goroutine, on resume, ran post-call verification, quarantine,
// removal, and journal consumption UNCHECKED (two restore/consume sequences
// racing one backup). Every claim now carries a monotonic ledger epoch plus
// an IN-PROCESS REVOCATION FLAG the stranded goroutine checks itself: the
// reclaimer flips the flag while still holding the ledger mutex and ONLY
// THEN fires the releases, so from the instant the freed dest lock lets the
// reverter mutate, every sweep mutation surface (destination publish,
// backup-removal unit, journal consumption, pending persist) reads
// "revoked" through abandonIfRevoked and abandons WITHOUT further mutation —
// the journal entry keeps its pre-mutation classification (armed /
// restore-pending kind), never clobbered with partially-mutated data.

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// sweepBusyMarkerClaim records ONE sweep-owned arbitration stance: the sweep
// invocation's context (abandonment = ctx done), the .dlbusy acquisition's
// once-guarded, token-bound release closure, and the once-guarded
// destination-lock release — held in a PENDING CELL the caller fills the
// instant its blocking SharedDestLocks wait completes (wave-52, F2: the
// record is born at marker-acquire time, before the wait, so an owned marker
// is ledger-visible for the whole wait; nil only for marker-only claims
// staged by tests). Wave-51 adds the epoch-ownership gate fields: a
// monotonic ledger epoch (diagnostic ordering for the gate log) and the
// in-process revocation flag the claimant itself consults before every
// mutation surface after a resume. The claim mutex serializes cell
// population (bindDestLock) against the reclaim's releaseForReclaim so a
// reclaim mid-wait (empty cell) and a reclaim post-bind (filled cell) each
// fire exactly the releases somebody owns.
type sweepBusyMarkerClaim struct {
	ctx     context.Context
	release func()
	epoch   uint64      // ledger-wide monotonic claim epoch (assigned under the ledger mutex)
	revoked atomic.Bool // wave-51: flipped by the reclaimer BEFORE the releases fire

	mu          sync.Mutex // wave-52: guards the pending dest-lock cell handshake below
	destRelease func()     // once-guarded dest-lock release — nil until bindDestLock fills the cell
	reclaimed   bool       // releaseForReclaim already fired — a late bind keeps sole release ownership

	workerAcked atomic.Int32 // wave-53: incremented at each abandonIfRevoked gate the worker reaches after revocation (the worker-ack stage counter)
	released    atomic.Bool  // wave-53: signaled by releaseForReclaim once both holds are freed
}

// revoke flips the claim's in-process revocation flag. Only the ledger's
// reclaim path calls it, under the ledger mutex, strictly BEFORE
// releaseForReclaim fires: the freed dest lock lets the reverter start
// mutating immediately, so the stranded claimant must already read "revoked"
// at every mutation gate it can still reach.
func (c *sweepBusyMarkerClaim) revoke() { c.revoked.Store(true) }

// isRevoked reports whether the claim was reclaimed by the continued revert.
// A nil claim is the direct-caller (test/legacy) posture — the wave-50
// discipline — and is never revoked.
func (c *sweepBusyMarkerClaim) isRevoked() bool { return c != nil && c.revoked.Load() }

// sweepClaimRevokedLogFn is the wave-51 revocation-gate logging seam: a
// sweep claimant resuming past its sweep's abandonment reports each mutation
// surface it REFUSES to enter. Production logs at warn level, in tune with
// the neighboring conservative kept/leaves legs; tests capture the phase
// strings to prove WHICH gate caught the revoked claim.
var sweepClaimRevokedLogFn = func(phase string, epoch uint64, backup, dest string) {
	logging.Warnf("replacement sweep claim (epoch %d) for destination %s was reclaimed after its sweep was abandoned — abandoning at the %s without further mutation; the journal entry keeps its pre-mutation classification and %s stays as classified", epoch, dest, phase, backup)
}

// abandonIfRevoked is the epoch-ownership gate consulted by the sweep worker
// at each mutation surface after its claim can have outlived its waiter:
// true means the claim was reclaimed and the caller abandons WITHOUT any
// further mutation, leaving dest/journal/backup exactly as its current
// pre-mutation state classified them. A worker parked INSIDE a wedged fs
// call (uninterruptible; the wave-8 shape) whose arbitration was reclaimed
// meanwhile resumes here and stops silently apart from the seam log.
// Wave-53 (codex P1/P5, PR#215 findings 1+4): a reclaimed worker ACKS here —
// the worker-ack stage counter increments so the reclaim's bounded wait
// (releaseForReclaim.waitWorkerAck) can observe that the worker reached a
// gate and stopped before the holds are freed.
func (c *sweepBusyMarkerClaim) abandonIfRevoked(phase, backup, dest string) bool {
	if !c.isRevoked() {
		return false
	}
	c.workerAcked.Add(1) // the worker observed the revocation and is stopping
	sweepClaimRevokedLogFn(phase, c.epoch, backup, dest)
	return true
}

// bindDestLock fills the claim's pending dest-lock cell the instant the
// caller's blocking SharedDestLocks wait completes (wave-52, F2): the lock
// is ledger-trackable from the exact moment it is held, and the cell
// handshake against releaseForReclaim leaves no acquire→bind window a
// reclaim could race. False means the claim was reclaimed DURING the wait
// (the reclaim fired against the empty cell): the once-guarded release never
// became visible to the reclaim, the just-acquired lock belongs to the
// caller alone, and the caller must release it directly and abandon — the
// revocation flag is already set (revoke precedes the releases). True means
// the cell took the once-guarded release: the reclaim path and the caller's
// own defer now share its single firing.
func (c *sweepBusyMarkerClaim) bindDestLock(release func()) bool {
	var once sync.Once
	guarded := func() { once.Do(release) }
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reclaimed {
		return false
	}
	c.destRelease = guarded
	return true
}

// releaseDestLock frees the claim's bound destination lock (no-op for
// marker-only claims and for claims still waiting on the lock — an empty
// cell). The once-guard shared with the reclaim path makes the stranded
// goroutine's post-abandonment defer a no-op.
func (c *sweepBusyMarkerClaim) releaseDestLock() {
	c.mu.Lock()
	destRelease := c.destRelease
	c.mu.Unlock()
	if destRelease != nil {
		destRelease()
	}
}

// sweepReclaimWorkerAckBound is the bounded grace releaseForReclaim waits for
// the stranded worker to acknowledge the revocation (wave-53, codex P1/P5,
// PR#215 findings 1+4) before freeing the destination lock and the busy
// marker. The worker acks at its next abandonIfRevoked gate; a wedge parks it
// inside an uninterruptible fs call, so the wait is bounded — once it expires
// the holds are freed anyway (the small residual race is accepted over an
// unbounded reverter block that would defeat the sweep bound the reclaim
// exists to heal). Tests shorten this through swapReclaimTimers.
var sweepReclaimWorkerAckBound = 2 * time.Second

// sweepReclaimWorkerAckPoll is the worker-ack polling interval.
var sweepReclaimWorkerAckPoll = 25 * time.Millisecond

// sweepReclaimReleaseGrace is the bounded grace the reverter's reclaim waits
// for the detached releaseForReclaim to finish freeing BOTH holds (wave-53,
// finding 4). On a healthy filesystem the releases complete in microseconds;
// a wedged marker take-aside outlasts the grace and the reverter proceeds —
// the dest lock is already freed (pure in-process work, never wedged), so the
// reverter's dest-lock acquisition is never unbounded. Tests shorten this
// through swapReclaimTimers.
var sweepReclaimReleaseGrace = 250 * time.Millisecond

// sweepReclaimReleasePoll is the released-signal polling interval.
var sweepReclaimReleasePoll = 25 * time.Millisecond

// waitWorkerAck blocks up to sweepReclaimWorkerAckBound (polling
// sweepReclaimWorkerAckPoll) for the stranded worker to acknowledge the
// revocation through an abandonIfRevoked gate (wave-53, finding 1). Returns
// true if the worker acked within the bound, false on timeout (the holds are
// freed anyway — the bounded grace accepts the small residual race over an
// unbounded reverter block).
func (c *sweepBusyMarkerClaim) waitWorkerAck() bool {
	deadline := time.Now().Add(sweepReclaimWorkerAckBound)
	for {
		if c.workerAcked.Load() > 0 {
			return true
		}
		if !time.Now().Before(deadline) {
			return c.workerAcked.Load() > 0
		}
		time.Sleep(sweepReclaimWorkerAckPoll)
	}
}

// waitReleased blocks up to sweepReclaimReleaseGrace (polling
// sweepReclaimReleasePoll) for the detached releaseForReclaim to signal that
// both holds are freed (wave-53, finding 4). Returns true if the releases
// landed within the grace, false on timeout (the reverter proceeds — the dest
// lock is already freed; a wedged marker take-aside continues in the
// background and the reverter's marker acquisition retries through its bounded
// grace).
func (c *sweepBusyMarkerClaim) waitReleased() bool {
	deadline := time.Now().Add(sweepReclaimReleaseGrace)
	for {
		if c.released.Load() {
			return true
		}
		if !time.Now().Before(deadline) {
			return c.released.Load()
		}
		time.Sleep(sweepReclaimReleasePoll)
	}
}

// releaseForReclaim frees the dest lock FIRST (in-process, can never wedge),
// then the marker's once-guarded take-aside release (wave-38/49/50/52 posture).
// Wave-54 (finding 1): the dest lock is freed BEFORE the bounded worker-ack
// wait; the on-disk marker is freed ONLY after the worker acknowledges — a
// wedged worker keeps it write-protective until it wakes and self-releases,
// while the reverter proceeds graced (markerGraced). Detached by reclaim.
func (c *sweepBusyMarkerClaim) releaseForReclaim() {
	c.mu.Lock()
	c.reclaimed = true
	destRelease := c.destRelease
	c.mu.Unlock()
	if destRelease != nil {
		destRelease()
	}
	if c.waitWorkerAck() { // wave-54, finding 1: free the marker ONLY after the worker acks
		c.release()
	}
	c.released.Store(true) // wave-53, finding 4: signal the detached releases completed
}

// sweepBusyClaimLedger is the in-process ledger of sweep-owned busy markers.
// Every key derivation runs through the ledger's ONE DestKeyResolver under
// the ledger mutex (wave-50, F2): the resolver freezes each present root's
// probe posture at first use for the ledger's lifetime, so a drifting probe
// (transient failure vs definitive recovery between record and reclaim) can
// never split one claim across two keys. DestKeyResolver caches postures in a
// plain map, hence the mutex-scoped derivations. The epoch counter (wave-51)
// issues each claim's monotonic epoch under the same mutex. Wave-54 (finding 1):
// a reclaimed-but-unreleased claim lives on as a TOMBSTONE so the reverter's
// marker-busy consult can grace the revert past a wedged worker (the marker
// stays write-protective until the worker wakes and self-releases; untrack drops it).
type sweepBusyClaimLedger struct {
	mu         sync.Mutex
	resolver   *fsutil.DestKeyResolver
	byDest     map[string]*sweepBusyMarkerClaim
	tombstones map[string]*sweepBusyMarkerClaim

	epoch uint64
}

func newSweepBusyClaimLedger() *sweepBusyClaimLedger {
	return &sweepBusyClaimLedger{
		resolver:   fsutil.NewDestKeyResolver(),
		byDest:     make(map[string]*sweepBusyMarkerClaim),
		tombstones: make(map[string]*sweepBusyMarkerClaim),
	}
}

// sweepBusyClaims is the process-lifetime ledger shared by the sweeps and the
// reverter's reclaim consult.
var sweepBusyClaims = newSweepBusyClaimLedger()

// recordSweepBusyClaim journals the sweep's freshly acquired busy marker for
// dest AT MARKER ACQUIRE TIME (wave-52, codex local review round 7, PR#215
// finding F2): the record lands BEFORE the caller's blocking destination-lock
// wait, so the wave-49 ledger names the claim for the entire wait — a sweep
// ctx expiring mid-wait no longer leaves an owned marker the reverter's
// reclaim consult cannot find (the finding's report: the pre-wave-52
// lock-then-record order stranded exactly those claims into ErrReplacementBusy
// for their whole stranding). The record is born binding the marker release
// and carrying an EMPTY dest-lock cell; the caller fills it the instant the
// wait completes through bindDestLock, which wraps the release in a
// once-guard at cell time so the reclaim and the claimer's own defer share
// one firing. The returned untrack removes ONLY this exact record (pointer
// identity — a re-recorded claim for the same dest is never retracted by a
// stale holder) and is always deferred by the claimer ahead of the releases.
func recordSweepBusyClaim(ctx context.Context, dest string, release func()) (*sweepBusyMarkerClaim, func()) {
	return sweepBusyClaims.record(ctx, dest, release)
}

func (l *sweepBusyClaimLedger) record(ctx context.Context, dest string, release func()) (*sweepBusyMarkerClaim, func()) {
	rec := &sweepBusyMarkerClaim{ctx: ctx, release: release}
	l.mu.Lock()
	l.epoch++
	rec.epoch = l.epoch
	key := l.resolver.Key(dest)
	l.byDest[key] = rec
	l.mu.Unlock()
	return rec, func() {
		l.mu.Lock()
		if l.byDest[key] == rec {
			delete(l.byDest, key)
		}
		if l.tombstones[key] == rec {
			delete(l.tombstones, key)
		}
		l.mu.Unlock()
	}
}

// reclaimAbandonedSweepBusyMarker revokes a recorded sweep claim whose owning
// sweep's context is already DONE (the wave-8 deadline leg: the caller stopped
// waiting, the goroutine is stranded mid-op because fs calls are
// uninterruptible). The revocation invokes the claim's OWN once-guarded
// releases (releaseForReclaim: dest lock first, then the wave-38 token-bound
// marker release) — freeing only the abandoned sweep's arbitration holds and
// leaving the stranded goroutine's later deferred releases as no-ops. True is
// returned when a revocation ran; the caller retries its acquisition once
// against the freed dest lock and marker name. A live-ctx record (an
// in-flight sweep someone still waits on) is never reclaimed: the caller
// keeps the ordinary blocking/busy posture. Wave-51: the reclaim flips the
// claim's in-process revocation flag FIRST (see revoke/abandonIfRevoked), so
// the releases below hand the arbitration to the revert only after the
// stranded goroutine can already read "revoked" at every mutation gate.
func reclaimAbandonedSweepBusyMarker(dest string) bool {
	return sweepBusyClaims.reclaim(dest)
}

func (l *sweepBusyClaimLedger) reclaim(dest string) bool {
	l.mu.Lock()
	key := l.resolver.Key(dest)
	rec := l.byDest[key]
	if rec == nil || rec.ctx.Err() == nil {
		l.mu.Unlock()
		return false
	}
	delete(l.byDest, key)
	l.tombstones[key] = rec
	// Wave-51: flip the claim's OWN revocation flag under the ledger mutex and
	// ONLY THEN fire the releases — the stranded claimant must read "revoked"
	// at its mutation gates from the instant the freed arbitration lets the
	// reverter mutate, never in a window where its dest lock is gone but its
	// flag still reads live.
	rec.revoke()
	l.mu.Unlock()
	// Wave-53 (codex P5, PR#215 finding 4): detach the releases to a goroutine
	// so the reverter never blocks on the marker's take-aside fs work (a wedged
	// unlink could outlast the sweep bound the reclaim exists to heal). The
	// reverter waits only on the revocation flag (set above) plus this bounded
	// grace for the detached releases to land — waitReleased returns once both
	// holds are freed, or after the grace if a wedge outlasts it (the dest lock
	// is already freed by then — pure in-process work — so the reverter's
	// dest-lock acquisition is never unbounded). releaseForReclaim waits a
	// bounded worker-ack (finding 1) before freeing the holds.
	go rec.releaseForReclaim()
	rec.waitReleased()
	return true
}

// markerGraced reports whether dest carries a revoked-but-unreleased tombstone
// whose marker is still held (wave-54, finding 1). Called by the reverter only
// after waitReleased settled the ack outcome: a tombstone here means the worker
// never acked within the bound — the reverter proceeds graced under the dest lock.
func (l *sweepBusyClaimLedger) markerGraced(dest string) bool {
	l.mu.Lock()
	_, ok := l.tombstones[l.resolver.Key(dest)]
	l.mu.Unlock()
	return ok
}
