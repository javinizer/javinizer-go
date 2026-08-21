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
	"github.com/spf13/afero"
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

	released atomic.Bool // wave-53: signaled by releaseForReclaim once both holds are freed
	wedged   atomic.Bool // wave-57: admission timeout — holds retained, reverter skips (busy-class)

	// completed is flipped by the worker's untrack closure under the ledger
	// mutex (sweepBusyClaimLedger.mu) BEFORE the delete, so the wave-58
	// wedged-claim reinsertion (same lock) reinserts ONLY while the worker is
	// still running; a worker that already finished (untrack + releases) is
	// not resurrected (the dest would stay pinned wedged until restart).
	completed bool

	fs    afero.Fs // wave-55: filesystem the marker was claimed on, for the ownership-attestation gate
	token string   // wave-55: the busy-marker token this claim wrote ("" for markerless test claims)

	// Wave-56 (codex P1, PR#215 finding F1): per-claim admission gate. The
	// sweep goroutine holds admitMu across each attested mutation (admit →
	// mutation → done), so reclaim — which frees the dest lock and takes the
	// marker aside — can only finish AFTER an in-flight admitted stage
	// completes. admitHeld tracks whether THIS sweep goroutine currently owns
	// admitMu (sweep-goroutine-only; reclaim touches admitMu solely through
	// TryLock, never admitHeld), so releaseAdmit unlocks exactly the holds it
	// owns and never a gate reclaim acquired for its release.
	admitMu   sync.Mutex
	admitHeld bool
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
// true means the claim was reclaimed (or its marker swapped out from under
// it) and the caller abandons WITHOUT any further mutation, leaving
// dest/journal/backup exactly as its current pre-mutation state classified
// them. A worker parked INSIDE a wedged fs call (uninterruptible; the
// wave-8 shape) whose arbitration was reclaimed meanwhile resumes here and
// stops silently apart from the seam log.
//
// Wave-55 (codex P1, PR#215 finding 1 — full close): the gate now ALSO
// attests ownership of the on-disk marker at every mutating stage, not just
// the in-process revocation flag. The reclaim frees the dest lock (so the
// reverter can mutate) and takes the marker aside (so the reverter
// re-acquires it under its own token) WITHOUT waiting for a bounded worker
// ack — a deadline was the wave-53 safety bound, but a worker wedged
// mid-mutation between the flag check and the syscall could still complete
// that mutation after the reverter started. The airtight fix is to make the
// sweep's MUTATIONS themselves ownership-conditional: at every gate the
// worker re-reads the marker and requires it still names THIS claim's token.
// A marker taken aside by reclaim (or re-acquired by the reverter, or gone)
// reads a different token and the worker abandons before mutating. The ack
// is subsumed: no fixed deadline is needed once every mutating stage is
// attested, so the wave-53 worker-ack bound is removed (the reclaim frees the
// marker directly; the reverter never bypasses a still-owned marker).
//
// Wave-56 (codex P1, PR#215 finding F1 — hold arbitration across each attested
// mutation): the attestation above ran as a SEPARATE check ahead of the
// caller's mutation — a reclaim landing in that gap freed the dest lock and
// marker while the already-admitted mutation had not yet run, so the reverter
// mutated concurrently with the sweep's mutation. The gate now wraps the
// attestation and the mutation in ONE in-process admission: admit releases
// the PREVIOUS stage's gate, try-locks a fresh one, and attests ownership
// UNDER the lock; the gate stays held until the next admit (or releaseAdmit,
// deferred by every gated entry point) — i.e. across the mutation. Reclaim's
// releaseForReclaim polls TryLock on admitMu (bounded by
// sweepReclaimAdmitGrace) and only then frees the dest lock, so the reverter
// never mutates while an admitted sweep mutation is in flight. A TryLock
// failure means reclaim holds the gate for its release — the stage abandons.
func (c *sweepBusyMarkerClaim) abandonIfRevoked(phase, backup, dest string) bool {
	if c == nil {
		return false
	}
	// Release the previous stage's admit gate, then try-lock a fresh one and
	// attest ownership UNDER it: reclaim cannot take the marker aside between
	// the attestation and the mutation that follows this return.
	c.releaseAdmit()
	if !c.admitMu.TryLock() {
		// reclaim holds the gate for its release — abandon without mutating.
		sweepClaimRevokedLogFn(phase, c.epoch, backup, dest)
		return true
	}
	if c.isRevoked() || (c.token != "" && c.fs != nil && !fsutil.ReplacementBusyMarkerIsOurs(c.fs, dest, c.token)) {
		c.admitMu.Unlock()
		sweepClaimRevokedLogFn(phase, c.epoch, backup, dest)
		return true
	}
	c.admitHeld = true // the gate is held across the caller's mutation
	return false
}

// releaseAdmit frees the admit gate held by the last successful
// abandonIfRevoked. It is nil-safe and a no-op when no gate is held, so
// every gated entry point can `defer claim.releaseAdmit()` unconditionally.
// Sweep-goroutine-only: reclaim never calls it (it unlocks admitMu directly
// from releaseForReclaim when waitAdmitted acquired it).
func (c *sweepBusyMarkerClaim) releaseAdmit() {
	if c == nil || !c.admitHeld {
		return
	}
	c.admitHeld = false
	c.admitMu.Unlock()
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

// sweepReclaimReleaseGrace is the bounded grace the reverter's reclaim waits
// for the detached releaseForReclaim to finish freeing BOTH holds (wave-53,
// finding 4). On a healthy filesystem the releases complete in microseconds;
// a wedged marker take-aside outlasts the grace and the reverter proceeds —
// the dest lock is already freed (pure in-process work, never wedged), so the
// reverter's dest-lock acquisition is never unbounded. Tests shorten this
// through TestMain.
var sweepReclaimReleaseGrace = 250 * time.Millisecond

// sweepReclaimReleasePoll is the released-signal polling interval.
var sweepReclaimReleasePoll = 25 * time.Millisecond

// sweepReclaimAdmitGrace bounds how long reclaim's releaseForReclaim waits for
// an IN-FLIGHT admitted sweep mutation to finish before freeing the dest lock
// (wave-56, finding F1). The dest lock is pure in-process work (never wedged),
// so freeing it lets the reverter's dest-lock acquisition proceed; the bound
// caps the residual risk that a mutation wedged past the grace completes
// after the reverter starts. Normal fs stages (publish, quarantine, unlink,
// journal tx) finish well within the grace; a truly wedged fs call outlasts
// it and reclaim proceeds (the dest lock is freed in-process regardless).
// Tests shorten this through TestMain.
var sweepReclaimAdmitGrace = 100 * time.Millisecond

// sweepReclaimAdmitPoll is the admit-gate TryLock polling interval.
var sweepReclaimAdmitPoll = 5 * time.Millisecond

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
		if c.wedged.Load() {
			return false // wave-57: release wedged — holds retained, reclaim must not signal success
		}
		if !time.Now().Before(deadline) {
			return c.released.Load()
		}
		time.Sleep(sweepReclaimReleasePoll)
	}
}

// releaseForReclaim frees the dest lock FIRST (in-process, can never wedge),
// then the marker's once-guarded take-aside release (wave-38/49/50/52 posture).
// Wave-55 (codex P1, PR#215 finding 1 — full close): the marker is freed
// directly — NO bounded worker-ack wait gates the release. The wave-54
// tombstone/grace design let the reverter proceed past a still-owned marker
// while a wedged worker could complete a mutation after the reverter started;
// the airtight fix makes the sweep's mutations ownership-conditional
// (abandonIfRevoked attests the on-disk marker token at every mutating stage),
// so the reclaim taking the marker aside is safe without a deadline: a worker
// resuming reads a swapped/gone marker and abandons before mutating. The ack
// is subsumed as an optimization, not the safety bound. Detached by reclaim.
// Wave-56 (finding F1): before freeing the dest lock, reclaim polls TryLock
// on the admit gate (bounded by sweepReclaimAdmitGrace) so an in-flight
// admitted mutation finishes first — the reverter never mutates concurrently
// with an already-admitted sweep mutation. On success reclaim HOLDS the gate
// across the in-process dest-lock free (new sweep admits TryLock-fail and
// abandon), then unlocks it; the marker take-aside runs after (may wedge). A
// timeout (wedged mutation) proceeds without holding — the dest lock is
// freed in-process regardless, the residual risk bounded by the grace.
func (c *sweepBusyMarkerClaim) releaseForReclaim() {
	c.mu.Lock()
	c.reclaimed = true
	destRelease := c.destRelease
	c.mu.Unlock()
	admitted := c.waitAdmitted() // bounded TryLock poll; true = reclaim holds the admit gate
	if !admitted && destRelease != nil {
		// Wave-57 (codex P1, PR#215 — "keep arbitration held when admission times
		// out"): an admitted mutation is still in flight and could not be drained
		// within the admit grace. Freeing the dest lock + marker would let the
		// reverter mutate concurrently — UNSAFE. Retain both holds; the revocation
		// flag (set by reclaim before detaching) gates the worker's next stage, and
		// when the wedge unblocks the worker abandons via abandonIfRevoked and its
		// deferred releases self-fire. The claim stays wedged in the ledger so the
		// reverter's consult skips this dest (busy-class); reclaim still reports
		// true (the revoke landed) — the reverter consults sweepClaimIsWedged, not
		// the boolean, to skip. The wedge fires only for a bound claim
		// (bindDestLock): a markerless test claim frees nothing real, so its
		// reclaim keeps the ordinary released posture; in production a claim always
		// binds its dest lock before any admitted mutation.
		c.wedged.Store(true)
		return
	}
	if destRelease != nil {
		destRelease() // free the dest lock (in-process, fast) — while reclaim holds the admit gate
	}
	if admitted {
		c.admitMu.Unlock() // new sweep admits now TryLock-fail and read revoked → abandon
	}
	c.release()            // wave-55: free the marker directly — the attestation gates the worker
	c.released.Store(true) // wave-53, finding 4: signal the detached releases completed
}

// waitAdmitted polls TryLock on the admit gate up to sweepReclaimAdmitGrace:
// true means no admitted mutation is in flight (reclaim holds the gate across
// the dest-lock free); false means a mutation outlasted the grace (reclaim
// proceeds without holding — the residual, bounded risk). The poll is
// non-blocking so a wedged worker never strands the reverter's reclaim.
func (c *sweepBusyMarkerClaim) waitAdmitted() bool {
	deadline := time.Now().Add(sweepReclaimAdmitGrace)
	for {
		if c.admitMu.TryLock() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(sweepReclaimAdmitPoll)
	}
}

// sweepBusyClaimLedger is the in-process ledger of sweep-owned busy markers.
// Every key derivation runs through the ledger's ONE DestKeyResolver under
// the ledger mutex (wave-50, F2): the resolver freezes each present root's
// probe posture at first use for the ledger's lifetime, so a drifting probe
// (transient failure vs definitive recovery between record and reclaim) can
// never split one claim across two keys. DestKeyResolver caches postures in a
// plain map, hence the mutex-scoped derivations. The epoch counter (wave-51)
// issues each claim's monotonic epoch under the same mutex. Wave-55 (finding 1):
// a reclaimed claim's marker is taken aside immediately (no tombstone/grace);
// the reverter re-acquires the freed marker under its own token, and the
// worker's attestation gates refuse every further mutation.
type sweepBusyClaimLedger struct {
	mu       sync.Mutex
	resolver *fsutil.DestKeyResolver
	byDest   map[string]*sweepBusyMarkerClaim

	epoch uint64
}

func newSweepBusyClaimLedger() *sweepBusyClaimLedger {
	return &sweepBusyClaimLedger{
		resolver: fsutil.NewDestKeyResolver(),
		byDest:   make(map[string]*sweepBusyMarkerClaim),
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
// one firing. Wave-55 (finding 1): the claim also records the filesystem and
// the marker token it just wrote, so every mutating stage gate can re-prove
// the on-disk marker still names this claimant's token. Wave-56 (finding F2):
// the token MUST ride the acquisition API (fsutil.AcquireReplacementBusyEx)
// — the ledger never re-reads the marker by pathname here, so a racing swap
// or a transient read failure after the acquire cannot adopt an empty or
// foreign token and silently disarm the attestation gate. The caller refuses
// to record a claim whose provenance is unavailable (empty token with a
// non-nil fs): it treats that as a failed acquire. The returned untrack
// removes ONLY this exact record (pointer identity — a re-recorded claim for
// the same dest is never retracted by a stale holder) and is always deferred
// by the claimer ahead of the releases.
func recordSweepBusyClaim(ctx context.Context, fs afero.Fs, dest, token string, release func()) (*sweepBusyMarkerClaim, func()) {
	return sweepBusyClaims.record(ctx, fs, dest, token, release)
}

func (l *sweepBusyClaimLedger) record(ctx context.Context, fs afero.Fs, dest, token string, release func()) (*sweepBusyMarkerClaim, func()) {
	rec := &sweepBusyMarkerClaim{ctx: ctx, fs: fs, token: token, release: release}
	l.mu.Lock()
	l.epoch++
	rec.epoch = l.epoch
	key := l.resolver.Key(dest)
	l.byDest[key] = rec
	l.mu.Unlock()
	return rec, func() {
		l.mu.Lock()
		rec.completed = true // wave-58: mark the worker finished before untrack, under the ledger lock
		if l.byDest[key] == rec {
			delete(l.byDest, key)
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

// sweepClaimIsWedged reports whether dest has a wedged sweep claim whose holds
// are retained after an admission timeout (wave-57). The reverter consults this
// after the pre-acquisition reclaim and skips the destination (busy-class) so it
// never blocks on the still-held dest lock nor mutates concurrently with the
// sweep's in-flight syscall — the wedged claim self-releases when the fs
// unblocks and a later retry succeeds.
func sweepClaimIsWedged(dest string) bool {
	return sweepBusyClaims.isWedged(dest)
}

func (l *sweepBusyClaimLedger) isWedged(dest string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec := l.byDest[l.resolver.Key(dest)]
	return rec != nil && rec.wedged.Load()
}

func (l *sweepBusyClaimLedger) reclaim(dest string) bool {
	l.mu.Lock()
	key := l.resolver.Key(dest)
	rec := l.byDest[key]
	if rec == nil || rec.ctx.Err() == nil {
		l.mu.Unlock()
		return false
	}
	if rec.wedged.Load() {
		// Already wedged (a prior consult hit the admission timeout): holds are
		// retained — signal not-reclaimed so the reverter skips this dest instead
		// of blocking on the still-held dest lock.
		l.mu.Unlock()
		return false
	}
	delete(l.byDest, key)
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
	// dest-lock acquisition is never unbounded). Wave-55 (finding 1):
	// releaseForReclaim frees the marker directly (no bounded worker-ack wait);
	// the on-disk attestation gates the worker, so the reverter re-acquires the
	// freed marker under its own token and never bypasses a still-owned marker.
	go rec.releaseForReclaim()
	rec.waitReleased()
	if rec.wedged.Load() {
		// Wave-57: the release wedged (admission timeout) — holds retained. Re-insert
		// so the reverter's consult skips this dest; the stranded worker's untrack
		// removes it once it self-releases. The revoke already landed, so report
		// true — the reverter consults sweepClaimIsWedged (not this boolean) to skip.
		// Wave-58: reinsert ONLY while the worker is still running — its untrack
		// flips `completed` under this same lock before releasing, so a worker that
		// finished between the ledger-unlock and here is not resurrected (it would
		// pin the dest wedged until restart). `nil` keeps the pointer-scoped guard.
		l.mu.Lock()
		if l.byDest[key] == nil && !rec.completed {
			l.byDest[key] = rec
		}
		l.mu.Unlock()
	}
	return true
}
