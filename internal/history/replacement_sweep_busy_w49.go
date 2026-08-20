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
//     stranding the reclaim existed to heal. The record is now born binding
//     BOTH arbitration holds: sweep callers acquire the dest lock FIRST and
//     record (dest → {ctx, marker release, dest-lock release}) atomically, so
//     no leg can ever hold the lock untracked (there is no acquire→bind
//     window a reclaim could race). The reverter consults the ledger FIRST
//     (before the blocking acquisition) and retries the acquisition against
//     the freed arbitration; the reclaim runs the dest-lock release AHEAD of
//     the marker release because a keyed-registry release is pure in-process
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

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// sweepBusyMarkerClaim records ONE sweep-owned arbitration stance: the sweep
// invocation's context (abandonment = ctx done), the .dlbusy acquisition's
// once-guarded, token-bound release closure, and (wave-50, F1) the
// once-guarded destination-lock release bound at record time — nil only for
// marker-only claims staged by tests. Wave-51 adds the epoch-ownership gate
// fields: a monotonic ledger epoch (diagnostic ordering for the gate log)
// and the in-process revocation flag the claimant itself consults before
// every mutation surface after a resume.
type sweepBusyMarkerClaim struct {
	ctx         context.Context
	release     func()
	destRelease func()
	epoch       uint64      // ledger-wide monotonic claim epoch (assigned under the ledger mutex)
	revoked     atomic.Bool // wave-51: flipped by the reclaimer BEFORE the releases fire
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
func (c *sweepBusyMarkerClaim) abandonIfRevoked(phase, backup, dest string) bool {
	if !c.isRevoked() {
		return false
	}
	sweepClaimRevokedLogFn(phase, c.epoch, backup, dest)
	return true
}

// releaseDestLock frees the claim's bound destination lock (no-op for
// marker-only claims). The once-guard shared with the reclaim path makes the
// stranded goroutine's post-abandonment defer a no-op.
func (c *sweepBusyMarkerClaim) releaseDestLock() {
	if c.destRelease != nil {
		c.destRelease()
	}
}

// releaseForReclaim frees EVERYTHING bound to the claim — the destination
// lock FIRST (wave-50, F1: a keyed-registry release is purely in-process and
// can never wedge on the stranded filesystem, so it must be freed even when
// the marker release wedges), then the marker's once-guarded, token-bound
// take-aside release (wave-38; a wedged unlink leaves only the inert scratch
// sibling behind — the established wave-49 posture).
func (c *sweepBusyMarkerClaim) releaseForReclaim() {
	c.releaseDestLock()
	c.release()
}

// sweepBusyClaimLedger is the in-process ledger of sweep-owned busy markers.
// Every key derivation runs through the ledger's ONE DestKeyResolver under
// the ledger mutex (wave-50, F2): the resolver freezes each present root's
// probe posture at first use for the ledger's lifetime, so a drifting probe
// (transient failure vs definitive recovery between record and reclaim) can
// never split one claim across two keys. DestKeyResolver caches postures in a
// plain map, hence the mutex-scoped derivations. The epoch counter (wave-51)
// issues each claim's monotonic epoch under the same mutex.
type sweepBusyClaimLedger struct {
	mu       sync.Mutex
	resolver *fsutil.DestKeyResolver
	byDest   map[string]*sweepBusyMarkerClaim
	epoch    uint64
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
// dest TOGETHER with the destination lock the caller already holds (wave-50,
// F1): callers take the dest lock BEFORE recording so the record is born
// binding both holds — no acquire→bind window a reclaim could race, and no
// leg can hold the lock untracked. The dest-lock release is wrapped in a
// once-guard at record time so the reclaim and the claimer's own defer share
// one firing. The returned untrack removes ONLY this exact record (pointer
// identity — a re-recorded claim for the same dest is never retracted by a
// stale holder) and is always deferred by the claimer ahead of the releases.
func recordSweepBusyClaim(ctx context.Context, dest string, release, destRelease func()) (*sweepBusyMarkerClaim, func()) {
	return sweepBusyClaims.record(ctx, dest, release, destRelease)
}

func (l *sweepBusyClaimLedger) record(ctx context.Context, dest string, release, destRelease func()) (*sweepBusyMarkerClaim, func()) {
	rec := &sweepBusyMarkerClaim{ctx: ctx, release: release}
	if destRelease != nil {
		var once sync.Once
		rec.destRelease = func() { once.Do(destRelease) }
	}
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
	// Wave-51: flip the claim's OWN revocation flag under the ledger mutex and
	// ONLY THEN fire the releases — the stranded claimant must read "revoked"
	// at its mutation gates from the instant the freed arbitration lets the
	// reverter mutate, never in a window where its dest lock is gone but its
	// flag still reads live.
	rec.revoke()
	l.mu.Unlock()
	rec.releaseForReclaim()
	return true
}
