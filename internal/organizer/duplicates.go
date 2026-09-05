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
type DuplicateTracker struct {
	mu       sync.Mutex
	resolver *fsutil.DestKeyResolver
	claims   map[string]duplicateClaim
}

// NewDuplicateTracker returns an empty tracker for one batch run.
func NewDuplicateTracker() *DuplicateTracker {
	return &DuplicateTracker{
		resolver: fsutil.NewDestKeyResolver(),
		claims:   make(map[string]duplicateClaim),
	}
}

// observe registers the plan's source→target claim and reports the earlier
// claim when the proven-equal canonical target key was already claimed by a
// DIFFERENT source. Plans that move nothing (WillMove=false) are never
// registered: their target is the already-occupied source location, and the
// destination-occupation conflict checks own that class. Re-observing the
// same source file is idempotent so retried or re-planned files never
// self-conflict.
func (t *DuplicateTracker) observe(plan *OrganizePlan) (duplicateClaim, bool) {
	if t == nil || plan == nil || !plan.WillMove || plan.TargetPath == "" {
		return duplicateClaim{}, false
	}
	// DestKeyResolver deliberately carries NO internal locking (it is built
	// for single-goroutine grouping passes), so key resolution happens under
	// the tracker mutex alongside claim registration. First-key-per-root
	// probes single-flight inside fsutil; later keys are pure map reads.
	t.mu.Lock()
	defer t.mu.Unlock()
	key := t.resolver.Key(plan.TargetPath)
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

// applyDuplicatePreflight registers the freshly computed plan against the
// batch's duplicate tracker (#224 phase E). An unauthorized duplicate
// appends a ConflictDuplicate to plan.Conflicts — the identical failure
// pipeline as destination-occupation conflicts ("conflicts detected: …"),
// in dry-run and live runs alike. An authorized duplicate returns a per-file
// warning instead: authorization may demote it, never hide it, so the caller
// persists the warning and emits the audit event.
func applyDuplicatePreflight(plan *OrganizePlan, tracker *DuplicateTracker, overwriteAuthorized bool) []string {
	prior, dup := tracker.observe(plan)
	if !dup {
		return nil
	}
	if overwriteAuthorized {
		return []string{fmt.Sprintf(
			"duplicate destination within batch: %s already claimed by %s (overwrite authorized)",
			plan.TargetPath, prior.source)}
	}
	plan.Conflicts = append(plan.Conflicts, PlanConflict{Path: plan.TargetPath, Kind: ConflictDuplicate})
	return nil
}
