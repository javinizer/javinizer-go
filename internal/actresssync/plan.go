// Package actresssync prepares read-only, exact-ID offline metadata previews.
// A preview is not identity verification or permission to mutate an actor.
package actresssync

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

// MaxSelection bounds SQL parameters and result size independently of caller limits.
const MaxSelection = 500

// Options requires an explicit clock and finite freshness/selection budgets.
// Exactly MaxAge old evidence is fresh; future observations fail closed.
type Options struct {
	Now       time.Time
	MaxAge    time.Duration
	MaxActors int
}

// ValidateSelection checks admission before database reads or allocations.
// Empty, zero and duplicate IDs are rejected, not interpreted as 'all actors'.
func (o Options) ValidateSelection(ids []uint) error {
	if o.Now.IsZero() || o.MaxAge <= 0 || o.MaxActors <= 0 || o.MaxActors > MaxSelection || len(ids) == 0 || len(ids) > o.MaxActors {
		return fmt.Errorf("invalid offline sync clock, freshness budget or selection size")
	}
	seen := make(map[uint]bool, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			return fmt.Errorf("offline sync selection contains a zero or duplicate actor ID")
		}
		seen[id] = true
	}
	return nil
}

// Target is a transaction-consistent read projection, never client-supplied authority.
// Protected includes user ownership, revoked management and legacy/unknown origins.
// No names, aliases or URLs participate in matching.
type Target struct {
	ID          uint
	Exists      bool
	DMMID       int
	Verified    bool
	Quarantined bool
	Protected   bool
	UpdatedAt   time.Time
}

type Outcome string

const (
	Missing           Outcome = "missing"
	Quarantined       Outcome = "quarantined"
	Unverified        Outcome = "unverified"
	Protected         Outcome = "protected"
	MissingIdentity   Outcome = "missing_identity"
	NoSnapshot        Outcome = "no_snapshot"
	Unmatched         Outcome = "unmatched"
	Ambiguous         Outcome = "ambiguous"
	Stale             Outcome = "stale"
	FutureObservation Outcome = "future_observation"
	EvidenceAvailable Outcome = "evidence_available"
)

// Item retains the selected local ID and the observed identity preconditions.
// Evidence is an owned copy, not a patch; aliases/URLs remain untrusted claims.
type Item struct {
	Target   Target
	Outcome  Outcome
	Evidence *actresscache.Evidence
}

// Plan binds every result to one immutable snapshot and caller clock. UpdatedAt
// is diagnostic only, not a fencing token or a substitute for write-time checks.
type Plan struct {
	SnapshotDigest string
	EvaluatedAt    time.Time
	Items          []Item
}

// Preview evaluates one bounded selection against a pinned immutable snapshot.
// Targets must be in selection order and include explicit missing placeholders.
// No partial plan is returned on cancellation or invalid input. The only identity
// bridge currently supported is an existing positive DMM ID to an exact 'dmm'
// external-ID claim. SourceID, names, URLs and leading-zero variants never match.
func Preview(ctx context.Context, targets []Target, snapshot *actresscache.Snapshot, options Options) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(targets) > MaxSelection {
		return nil, fmt.Errorf("offline sync selection too large")
	}
	ids := make([]uint, len(targets))
	for i, target := range targets {
		ids[i] = target.ID
	}
	if err := options.ValidateSelection(ids); err != nil {
		return nil, err
	}
	plan := &Plan{EvaluatedAt: options.Now.UTC().Round(0), Items: make([]Item, 0, len(targets))}
	if snapshot != nil {
		plan.SnapshotDigest = snapshot.Digest()
	}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item := Item{Target: target}
		switch {
		case !target.Exists:
			item.Outcome = Missing
		case target.Quarantined:
			item.Outcome = Quarantined
		case !target.Verified:
			item.Outcome = Unverified
		case target.Protected:
			item.Outcome = Protected
		case target.DMMID <= 0:
			item.Outcome = MissingIdentity
		case snapshot == nil:
			item.Outcome = NoSnapshot
		default:
			match, err := snapshot.LookupUnique("dmm", strconv.Itoa(target.DMMID))
			if err != nil {
				return nil, err
			}
			switch {
			case match.Ambiguous:
				item.Outcome = Ambiguous
			case match.Candidate == nil:
				item.Outcome = Unmatched
			case match.Candidate.Provenance.ObservedAt.After(options.Now):
				item.Outcome = FutureObservation
			case match.Candidate.Provenance.ObservedAt.Before(options.Now.Add(-options.MaxAge)):
				item.Outcome = Stale
			default:
				item.Outcome, item.Evidence = EvidenceAvailable, match.Candidate
			}
		}
		plan.Items = append(plan.Items, item)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return plan, nil
}
