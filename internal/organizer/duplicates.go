package organizer

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// duplicateClaim records one batch file's plan-time claim on a canonical
// destination key.
type duplicateClaim struct {
	source string
	target string
}

// claimEntry tracks one canonical destination key's ownership lifecycle.
// Observations are terminal-gated (codex P2, PR #241): a claimant observing
// a key still owned by a DIFFERENT source waits on done instead of
// conflicting against a snapshot — the owner's terminal outcome then
// arbitrates. success=true (owner published / dry-run-planned) keeps the
// key: waiters wake to the unchanged duplicate verdict. success=false
// (owner released: validation failure, vanished source, recovered panic,
// and every execute failure whose destination was never touched) frees the
// key: the next claimant is promoted to owner and ITS observe falls through
// to organize. A partial-publish execute failure instead lands on the
// success=true side — the destination already carries the owner's bytes
// (the fsutil.PublishCompleted class), so the claim settles and the waiter's
// duplicate verdict stands (codex P1, PR #241). done closes exactly once at
// the terminal transition; success and settled are only meaningful under mu.
//
// Promotion order (codex P2, PR #241 F1): the ordered STANDBY queue — every
// primed claimant beyond the owner, retained in priming (= sorted) order —
// always wins, WHETHER OR NOT its members have started observing, so an
// owner failing before the other workers reach observe still hands the key
// to the deterministic sorted-next claimant instead of deleting the claim
// and letting the survivors race for a first-come re-registration. Only
// once the standby queue is drained do blocked waiters beyond the primed
// set race, resolved by the existing sorted-first rule.
type claimEntry struct {
	claim   duplicateClaim
	standby []string      // later primed claimants awaiting their turn, in priming (sorted) order
	waiters []string      // sources blocked in observe, sorted on promotion
	done    chan struct{} // closed once, at the terminal transition
	settled bool          // owner reached its terminal outcome
	success bool          // terminal outcome kept the key
}

// DuplicatePriming is one batch file's plan-time destination claim, computed
// by the apply phase's pre-fan-out planning pass and registered in sorted
// order so each canonical key's owner is pre-assigned before any apply worker
// starts (#240 finding A).
type DuplicatePriming struct {
	SourcePath string
	TargetPath string
	WillMove   bool
}

// DuplicateTracker performs intra-batch duplicate-destination preflight
// (#224 phase E). One tracker is shared across every file of a batch apply
// run; each plan's destination registers against the proven-equal canonical
// grouping keys of fsutil.DestKeyResolver — the folded-key discipline whose
// per-root case/normalization postures freeze once for a grouping pass — so
// spelling variants of one destination group together exactly when the
// destination volume proves them aliases, and byte-distinct names on
// case-sensitive volumes never do.
//
// Ownership is DETERMINISTIC (#240 finding A): the apply phase plans each
// sorted batch item exactly once BEFORE worker fan-out and primes the
// tracker via PrimeBatch in that sorted order, so the first sorted item wins
// its canonical key regardless of goroutine arrival order — and every later
// primed claimant is retained as an ordered standby (codex P2, PR #241 F1),
// so an owner that fails before its siblings observe still hands the key to
// the sorted-next claimant rather than reopening a scheduler-timing race.
// Identical batches therefore reject and move identical files, and under
// ForceUpdate only the primed winner's bytes can land — the loser demotes to
// a warning and skips execution instead of racing the winner onto one
// destination.
//
// Claims are TERMINAL-GATED (codex P2, PR #241): observe on a key owned by
// another source waits for the owner's terminal outcome rather than
// conflicting against a mid-flight snapshot. settle (owner success) leaves
// the key owned — waiters keep their duplicate verdict unchanged; release
// (owner failure/release) promotes the sorted-first waiter to owner so a
// valid later claimant organizes instead of the destination ending empty
// purely by scheduling. The owner goroutine's every exit leg closes out its
// claim (organizer error legs release — except a PARTIAL-publish execute
// failure, which settles because the destination already carries the
// owner's bytes (codex P1, PR #241) — success legs settle, panics release
// and re-panic, and the apply phase's recovery boundary abandons anything
// left open), so waiters can never deadlock behind a dead owner.
//
// A nonProbing tracker (dry runs, #240 finding B) derives keys through
// fsutil.DestKeyResolver.KeyNonProbing: postures come ONLY from previously
// frozen or process-cached postures, with an uncached root falling back to
// the conservative distinction-preserving posture — a fresh-process dry run
// performs zero probe writes and leaves no probe artifacts on disk.
type DuplicateTracker struct {
	mu         sync.Mutex
	resolver   *fsutil.DestKeyResolver
	claims     map[string]*claimEntry
	nonProbing bool
	primed     bool
}

// NewDuplicateTracker returns an empty tracker for one batch run. nonProbing
// MUST be true whenever the run must not write (dry runs): key derivation
// then consults only frozen/cached postures with a conservative fallback.
func NewDuplicateTracker(nonProbing bool) *DuplicateTracker {
	return &DuplicateTracker{
		resolver:   fsutil.NewDestKeyResolver(),
		claims:     make(map[string]*claimEntry),
		nonProbing: nonProbing,
	}
}

// PrimeBatch pre-assigns each canonical key's winner from the run's sorted
// plan-time claims (#240 finding A). It MUST be called exactly once per run,
// before any worker observes — a run plans once, so later calls are ignored.
// The first registered claim per key owns it until its terminal outcome, and
// every later primed claimant of the same key is retained as an ordered
// standby (codex P2, PR #241 F1): primings arrive in sorted order, so the
// standby queue IS the sorted fallback order an owner failure promotes from.
// WillMove=false and empty-target primings register nothing, mirroring
// observe's guard.
func (t *DuplicateTracker) PrimeBatch(primings []DuplicatePriming) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.primed {
		return
	}
	t.primed = true
	for _, p := range primings {
		if !p.WillMove || p.TargetPath == "" {
			continue
		}
		key := t.keyLocked(p.TargetPath)
		if entry, ok := t.claims[key]; ok {
			// Ordered standby (codex P2, PR #241 F1): retain this later primed
			// claimant as the sorted-next fallback rather than discarding it —
			// an owner failing before the other workers reach observe otherwise
			// deletes the claim and the survivors re-register by scheduler
			// timing. Re-primings of the owner itself stay claim-nothing no-ops.
			if filepath.Clean(p.SourcePath) != filepath.Clean(entry.claim.source) && !containsClaimant(entry.standby, p.SourcePath) {
				entry.standby = append(entry.standby, p.SourcePath)
			}
			continue
		}
		t.claims[key] = &claimEntry{
			claim: duplicateClaim{source: p.SourcePath, target: p.TargetPath},
			done:  make(chan struct{}),
		}
	}
}

// containsClaimant reports whether queue already names source.
func containsClaimant(queue []string, source string) bool {
	for _, s := range queue {
		if s == source {
			return true
		}
	}
	return false
}

// removeClaimant drops src from a claim queue (waiters or standby), keeping
// the relative order of every remaining claimant. Comparisons clean both
// spellings; callers pass an already-cleaned src.
func removeClaimant(queue []string, src string) []string {
	kept := queue[:0]
	for _, s := range queue {
		if filepath.Clean(s) != src {
			kept = append(kept, s)
		}
	}
	return kept
}

// keyLocked derives the canonical key under the tracker's probing policy.
// Callers hold t.mu below; DestKeyResolver deliberately carries NO internal
// locking (it is built for single-goroutine grouping passes), so key
// resolution always happens under the tracker mutex. First-key-per-root
// probes single-flight inside fsutil; later keys are pure map reads, and
// nonProbing keys never touch the filesystem at all.
func (t *DuplicateTracker) keyLocked(target string) string {
	if t.nonProbing {
		return t.resolver.KeyNonProbing(target)
	}
	return t.resolver.Key(target)
}

// observe reports the plan's earlier claimant when the proven-equal canonical
// target key is terminally owned by a DIFFERENT source. Mid-flight ownership
// blocks (codex P2, PR #241): while the owner is still applying, the observer
// Waits for the terminal outcome instead of snapshot-conflicting — a settled
// owner keeps the key and the observer returns the unchanged duplicate
// verdict, while a released owner hands the key to the next claimant in
// line (ordered standby first, sorted-first waiter behind it — see
// claimEntry), whose observe then falls through as the new owner so ITS
// bytes land. An unclaimed key registers first-come — covering primed owners
// claiming their own key, plans that diverged from the priming pass, and
// unprimed single-file trackers. Plans that move nothing (WillMove=false)
// are never registered: their target is the already-occupied source
// location, and the destination-occupation conflict checks own that class.
// Re-observing the same source file is idempotent so retried or re-planned
// files never self-conflict — including a waiter that wakes to find itself
// promoted.
//
// The wait honors the claimant's context (codex P2, PR #241 F2): a deadline
// or batch cancel landing mid-wait makes the observer LEAVE the claim
// queues — waiter and standby slots alike — and fall through as no-duplicate
// instead of blocking on (or being promoted into) post-cancellation
// execution. The caller's promotion/execute boundary recheck turns the fall
// through into the context error before any filesystem work.
func (t *DuplicateTracker) observe(ctx context.Context, plan *OrganizePlan) (duplicateClaim, bool) {
	if t == nil || plan == nil || !plan.WillMove || plan.TargetPath == "" {
		return duplicateClaim{}, false
	}
	src := filepath.Clean(plan.SourcePath)
	t.mu.Lock()
	key := t.keyLocked(plan.TargetPath)
	for {
		entry, ok := t.claims[key]
		if !ok {
			t.claims[key] = &claimEntry{
				claim: duplicateClaim{source: plan.SourcePath, target: plan.TargetPath},
				done:  make(chan struct{}),
			}
			t.mu.Unlock()
			return duplicateClaim{}, false
		}
		if filepath.Clean(entry.claim.source) == src {
			// Own key: first registration of a primed owner, idempotent
			// re-observe, or this waiter's promotion just landed.
			t.mu.Unlock()
			return duplicateClaim{}, false
		}
		if entry.settled {
			claim := entry.claim
			t.mu.Unlock()
			return claim, true
		}
		// Owner mid-flight: register as a waiter (idempotent — a promoted key
		// carries its remaining waiters forward) and sleep until terminal.
		registered := false
		for _, w := range entry.waiters {
			if w == plan.SourcePath {
				registered = true
				break
			}
		}
		if !registered {
			entry.waiters = append(entry.waiters, plan.SourcePath)
		}
		done := entry.done
		t.mu.Unlock()
		// codex P2 (PR #241 F2): wake on the owner's terminal outcome OR the
		// claimant's own cancellation, whichever lands first. A cancelled
		// waiter leaves the queue — a mid-wait promotion it may have just won
		// is handed onward by the caller's ctx recheck releasing its plan —
		// so the claim bookkeeping never leaks a dead claimant's slots.
		select {
		case <-done:
			t.mu.Lock()
		case <-ctx.Done():
			t.mu.Lock()
			t.cancelWaiterLocked(key, src)
			t.mu.Unlock()
			return duplicateClaim{}, false
		}
	}
}

// cancelWaiterLocked drops a claimant whose context died mid-wait from every
// queue it holds on key (codex P2, PR #241 F2): both the waiter slot it just
// vacated and any STANDBY slot it earned at priming time, so it can never be
// promoted into post-cancellation execution. A settled entry is final — the
// scrub then only tidies its inert queues and every remaining waiter still
// wakes to the recorded verdict; a key the (dead) claimant already OWNS is
// handed onward by the organize-side recheck releasing its plan.
func (t *DuplicateTracker) cancelWaiterLocked(key, src string) {
	entry, ok := t.claims[key]
	if !ok {
		return
	}
	entry.waiters = removeClaimant(entry.waiters, src)
	entry.standby = removeClaimant(entry.standby, src)
}

// settle marks the plan's OWNED claim terminal-success: the owner published
// (or dry-run-planned) its destination, so every waiting claimant's
// duplicate verdict is now final. Non-owner, unregistered, register-nothing,
// and already-settled calls are no-ops.
func (t *DuplicateTracker) settle(plan *OrganizePlan) {
	if t == nil || plan == nil || !plan.WillMove || plan.TargetPath == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := t.keyLocked(plan.TargetPath)
	entry, ok := t.claims[key]
	if !ok || entry.settled || filepath.Clean(entry.claim.source) != filepath.Clean(plan.SourcePath) {
		return
	}
	entry.settled = true
	entry.success = true
	close(entry.done)
}

// release drops an OWNED claim whose plan proved inexecutable mid-apply
// (codex r2 P2) or whose owner reached any other failed terminal outcome:
// claims live for the whole run — the tracker never times them out — so a
// primed winner whose source vanished between priming and apply, whose
// validation/execution failed, or whose worker panicked must explicitly
// release the canonical key. The terminal transition wakes waiting
// claimants: with none registered, the key frees outright so the next
// observe falls through; with waiters registered, the sorted-first one is
// PROMOTED to owner so ITS organize proceeds (codex P2, PR #241). Only the
// recorded owner of an UNSETTLED claim releases its key — losers, foreign
// sources, register-nothing plans, and claims already settled (success or
// failure) release nothing.
func (t *DuplicateTracker) release(plan *OrganizePlan) {
	if t == nil || plan == nil || !plan.WillMove || plan.TargetPath == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := t.keyLocked(plan.TargetPath)
	entry, ok := t.claims[key]
	if !ok || entry.settled || filepath.Clean(entry.claim.source) != filepath.Clean(plan.SourcePath) {
		return
	}
	t.failEntryLocked(key, entry)
}

// ReleaseAbandonedBy closes out every OPEN claim owned by source whose owner
// worker exited without settling (codex P2, PR #241): a panic before the
// organizer's own close-out ran, or an apply canceled at Organize's entry,
// must not hold the canonical key open — waiting claimants promote instead
// of deadlocking behind a dead owner. It is the apply phase's recovery-
// boundary safety net; settled claims are final and untouched, so runs in
// which the owner settled normally are no-ops. The abandoned source also
// leaves every open claim's STANDBY and waiter queues (codex P2, PR #241 F1):
// a dead claimant still listed as a fallback would otherwise be promoted
// later and hold the key with nobody left to close it out.
func (t *DuplicateTracker) ReleaseAbandonedBy(source string) {
	if t == nil || source == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	src := filepath.Clean(source)
	for key, entry := range t.claims {
		if entry.settled {
			continue
		}
		entry.waiters = removeClaimant(entry.waiters, src)
		entry.standby = removeClaimant(entry.standby, src)
		if filepath.Clean(entry.claim.source) == src {
			t.failEntryLocked(key, entry)
		}
	}
}

// failEntryLocked records the owned key's terminal FAILURE: waiters wake,
// the next claimant takes over as an open claim, or — with nobody left — the
// key frees outright. Promotion order is DETERMINISTIC (codex P2, PR #241
// F1): the ordered standby queue wins FIRST, promote-before-delete even when
// its sorted-next claimant has not reached observe yet (its observation
// later falls through as owner); the cancelled/abandoned scrub keeps dead
// claimants out of both queues, so a promoted entry never inherits a corpse.
// Only with the standby drained does the sorted-first blocked waiter take
// the key, carrying the remaining waiters forward.
func (t *DuplicateTracker) failEntryLocked(key string, entry *claimEntry) {
	entry.settled = true
	entry.success = false
	close(entry.done)
	if len(entry.standby) > 0 {
		next := entry.standby[0]
		t.claims[key] = &claimEntry{
			claim:   duplicateClaim{source: next, target: entry.claim.target},
			standby: entry.standby[1:],
			waiters: removeClaimant(entry.waiters, next),
			done:    make(chan struct{}),
		}
		return
	}
	if len(entry.waiters) == 0 {
		delete(t.claims, key)
		return
	}
	sort.Strings(entry.waiters)
	t.claims[key] = &claimEntry{
		claim:   duplicateClaim{source: entry.waiters[0], target: entry.claim.target},
		waiters: entry.waiters[1:],
		done:    make(chan struct{}),
	}
}

// applyDuplicatePreflight registers the freshly computed plan against the
// batch's duplicate tracker (#224 phase E). An unauthorized duplicate
// appends a ConflictDuplicate to plan.Conflicts — the identical failure
// pipeline as destination-occupation conflicts ("conflicts detected: …"),
// in dry-run and live runs alike. An authorized duplicate returns a per-file
// warning instead: authorization may demote it, never hide it, so the caller
// persists the warning and emits the audit event. skip=true on the authorized
// path is the #240 finding A contract: the duplicate NEVER executes its move,
// so only the primed winner's bytes can land on a claimed destination.
func applyDuplicatePreflight(ctx context.Context, plan *OrganizePlan, tracker *DuplicateTracker, overwriteAuthorized bool) (warnings []string, skip bool) {
	prior, dup := tracker.observe(ctx, plan)
	if !dup {
		return nil, false
	}
	if overwriteAuthorized {
		return []string{fmt.Sprintf(
			"duplicate destination within batch: %s already claimed by %s (overwrite authorized)",
			plan.TargetPath, prior.source)}, true
	}
	plan.Conflicts = append(plan.Conflicts, PlanConflict{Path: plan.TargetPath, Kind: ConflictDuplicate})
	return nil, false
}
