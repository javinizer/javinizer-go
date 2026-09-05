package organizer

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// duplicateClaim records one batch file's plan-time claim on a canonical
// destination key.
type duplicateClaim struct {
	source string
	target string
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
// case-sensitive volumes never do. Detection is plan-only: execution keeps
// the atomic no-replace publish composites as the backstop for collisions the
// preflight cannot see (cross-batch writers, post-plan plants).
//
// Ownership is DETERMINISTIC (#240 finding A): the apply phase plans each
// sorted batch item exactly once BEFORE worker fan-out and primes the
// tracker via PrimeBatch in that sorted order, so the first sorted item wins
// its canonical key regardless of goroutine arrival order. Identical batches
// therefore reject and move identical files, and under ForceUpdate only the
// primed winner's bytes can land — the loser demotes to a warning and skips
// execution instead of racing the winner onto one destination.
//
// A nonProbing tracker (dry runs, #240 finding B) derives keys through
// fsutil.DestKeyResolver.KeyNonProbing: postures come ONLY from previously
// frozen or process-cached postures, with an uncached root falling back to
// the conservative distinction-preserving posture — a fresh-process dry run
// performs zero probe writes and leaves no probe artifacts on disk.
type DuplicateTracker struct {
	mu         sync.Mutex
	resolver   *fsutil.DestKeyResolver
	claims     map[string]duplicateClaim
	nonProbing bool
	primed     bool
}

// NewDuplicateTracker returns an empty tracker for one batch run. nonProbing
// MUST be true whenever the run must not write (dry runs): key derivation
// then consults only frozen/cached postures with a conservative fallback.
func NewDuplicateTracker(nonProbing bool) *DuplicateTracker {
	return &DuplicateTracker{
		resolver:   fsutil.NewDestKeyResolver(),
		claims:     make(map[string]duplicateClaim),
		nonProbing: nonProbing,
	}
}

// PrimeBatch pre-assigns each canonical key's winner from the run's sorted
// plan-time claims (#240 finding A). It MUST be called exactly once per run,
// before any worker observes — a run plans once, so later calls are ignored.
// The first registered claim per key owns it for the whole run UNLESS the
// owner's plan later proves inexecutable and releases it (codex r2 P2);
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
		if _, ok := t.claims[key]; ok {
			continue
		}
		t.claims[key] = duplicateClaim{source: p.SourcePath, target: p.TargetPath}
	}
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
// target key is owned by a DIFFERENT source. Primed runs consult the
// pre-assigned winner map, so the sorted-first owner wins even when a loser
// observes first; only an unprimed key (a plan that diverged from the run's
// priming pass, or a tracker that was never primed) falls back to first-come
// registration, which keeps single-file and unprimed callers functional.
// Plans that move nothing (WillMove=false) are never registered: their target
// is the already-occupied source location, and the destination-occupation
// conflict checks own that class. Re-observing the same source file is
// idempotent so retried or re-planned files never self-conflict.
func (t *DuplicateTracker) observe(plan *OrganizePlan) (duplicateClaim, bool) {
	if t == nil || plan == nil || !plan.WillMove || plan.TargetPath == "" {
		return duplicateClaim{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := t.keyLocked(plan.TargetPath)
	prior, ok := t.claims[key]
	if !ok {
		t.claims[key] = duplicateClaim{source: plan.SourcePath, target: plan.TargetPath}
		return duplicateClaim{}, false
	}
	if filepath.Clean(prior.source) == filepath.Clean(plan.SourcePath) {
		return duplicateClaim{}, false
	}
	return prior, true
}

// release drops an OWNED claim whose plan proved inexecutable mid-apply
// (codex r2 P2): claims live for the whole run — the tracker never times
// them out — so a primed winner whose source vanished between priming and
// apply, or whose validation/execution failed for any reason, must
// explicitly release the canonical key before observe can fall through to a
// later valid claimant. Only the recorded owner releases its key (losers
// and foreign sources release nothing), and plans that register nothing
// (WillMove=false, empty target — observe's guard) release nothing.
func (t *DuplicateTracker) release(plan *OrganizePlan) {
	if t == nil || plan == nil || !plan.WillMove || plan.TargetPath == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := t.keyLocked(plan.TargetPath)
	if prior, ok := t.claims[key]; ok && filepath.Clean(prior.source) == filepath.Clean(plan.SourcePath) {
		delete(t.claims, key)
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
func applyDuplicatePreflight(plan *OrganizePlan, tracker *DuplicateTracker, overwriteAuthorized bool) (warnings []string, skip bool) {
	prior, dup := tracker.observe(plan)
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
