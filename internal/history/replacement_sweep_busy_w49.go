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
// The sweep now journals every busy-marker claim into an in-process,
// context-scoped ledger BEFORE the claim can outlive its waiter: sweepOne
// (and the full-sweep's ledger leg) record (dest → {ctx, release}) the
// instant a claim lands. The reverter's busy leg consults the ledger
// (reclaimAbandonedSweepBusyMarker): a record whose sweep context is DONE
// belongs to an abandoned sweep, and its marker is revoked BEST-EFFORT by
// invoking the claim's own release closure — the wave-38 once-guarded,
// token-bound take-aside unlink — which:
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

import (
	"context"
	"sync"
)

// sweepBusyMarkerClaim records ONE claimed .dlbusy marker: the sweep
// invocation's context (abandonment = ctx done) and the acquisition's
// once-guarded, token-bound release closure.
type sweepBusyMarkerClaim struct {
	ctx     context.Context
	release func()
}

// sweepBusyMarkerClaims is the in-process ledger of sweep-owned busy
// markers, keyed by the probe-aware destination key (sweepSlash — the same
// normalization the sweep's journal comparisons use, so a revert-side
// spelling of the same destination resolves identically).
var sweepBusyMarkerClaims = struct {
	mu     sync.Mutex
	byDest map[string]*sweepBusyMarkerClaim
}{byDest: make(map[string]*sweepBusyMarkerClaim)}

// recordSweepBusyClaim journals the sweep's freshly acquired busy marker for
// dest. The returned untrack removes ONLY this exact record (pointer
// identity — a re-recorded claim for the same dest is never retracted by a
// stale holder) and is always deferred by the claimer ahead of the marker
// release.
func recordSweepBusyClaim(ctx context.Context, dest string, release func()) func() {
	rec := &sweepBusyMarkerClaim{ctx: ctx, release: release}
	key := sweepSlash(dest)
	sweepBusyMarkerClaims.mu.Lock()
	sweepBusyMarkerClaims.byDest[key] = rec
	sweepBusyMarkerClaims.mu.Unlock()
	return func() {
		sweepBusyMarkerClaims.mu.Lock()
		if sweepBusyMarkerClaims.byDest[key] == rec {
			delete(sweepBusyMarkerClaims.byDest, key)
		}
		sweepBusyMarkerClaims.mu.Unlock()
	}
}

// reclaimAbandonedSweepBusyMarker revokes a recorded sweep busy marker whose
// owning sweep's context is already DONE (the wave-8 deadline leg: the
// caller stopped waiting, the goroutine is stranded mid-op because fs calls
// are uninterruptible). The revocation invokes the claim's OWN release
// closure — once-guarded and token-bound (wave-38 take-aside discipline,
// never a pathname delete) — frees only the abandoned sweep's marker, and
// leaves the stranded goroutine's later deferred release a no-op. True is
// returned when a revocation ran; the caller retries its acquisition once.
// A live-ctx record (an in-flight sweep someone still waits on) is never
// reclaimed: the caller keeps the ordinary ErrReplacementBusy posture.
func reclaimAbandonedSweepBusyMarker(dest string) bool {
	key := sweepSlash(dest)
	sweepBusyMarkerClaims.mu.Lock()
	rec := sweepBusyMarkerClaims.byDest[key]
	if rec == nil || rec.ctx.Err() == nil {
		sweepBusyMarkerClaims.mu.Unlock()
		return false
	}
	delete(sweepBusyMarkerClaims.byDest, key)
	sweepBusyMarkerClaims.mu.Unlock()
	rec.release()
	return true
}
