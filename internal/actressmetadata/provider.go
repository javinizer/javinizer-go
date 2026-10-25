// Package actressmetadata resolves read-only metadata evidence independently of
// movie scrapers. Results are not identity verification or mutation authority.
package actressmetadata

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

const Skip = "__skip__"
const MaxProviders = 32

var ErrBusy = errors.New("metadata resolver is busy")

// Reference is an exact source-owned ID, never a name or inferred cross-source ID.
type Reference struct{ Source, ID string }

// Query requires exactly one explicit reference or authoritative external-ID
// claim supplied by a reviewed caller. This boundary does not establish that
// caller's authority, infer claims from digits, or accept name lookup.
type Query struct {
	Reference *Reference
	Claim     *actresscache.ExternalID
}

// clone freezes pointer-bearing input and gives each adapter its own query.
func (q Query) clone() Query {
	if q.Reference != nil {
		reference := *q.Reference
		q.Reference = &reference
	}
	if q.Claim != nil {
		claim := *q.Claim
		q.Claim = &claim
	}
	return q
}
func cloneEvidence(record *actresscache.Evidence) *actresscache.Evidence {
	if record == nil {
		return nil
	}
	owned := *record
	owned.Metadata.Aliases = append([]string(nil), record.Metadata.Aliases...)
	owned.ExternalIDs = append([]actresscache.ExternalID(nil), record.ExternalIDs...)
	return &owned
}
func (q Query) validate() error {
	if (q.Reference == nil) == (q.Claim == nil) {
		return fmt.Errorf("exactly one reference or claim is required")
	}
	if q.Reference != nil {
		_, err := actresscache.SourceKey(q.Reference.Source, q.Reference.ID)
		return err
	}
	_, err := actresscache.SourceKey(q.Claim.Namespace, q.Claim.Value)
	return err
}

type Outcome string

const (
	NoSnapshot Outcome = "no_snapshot"
	Unmatched  Outcome = "unmatched"
	Ambiguous  Outcome = "ambiguous"
	Available  Outcome = "evidence_available"
	Skipped    Outcome = "skipped"
)

// Result retains provenance and the exact artifact generation used for a read.
// Evidence is an owned copy, never an authorized field patch.
type Result struct {
	Outcome        Outcome
	SnapshotDigest string
	Evidence       *actresscache.Evidence
}

// Provider intentionally has no movie search/scrape, mutation, or refresh capability.
// Implementations are trusted adapters: they must validate input provenance,
// obey context cancellation, and return owned evidence from a pinned artifact.
type Provider interface {
	LookupEvidence(context.Context, Query) (Result, error)
}

// SnapshotProvider pins one immutable generation at construction. Replacing a
// store cannot alter an active provider. Create a new provider to adopt a refresh.
type SnapshotProvider struct {
	snapshot *actresscache.Snapshot
	source   string
}

func NewSnapshotProvider(snapshot *actresscache.Snapshot, source string) (*SnapshotProvider, error) {
	if _, err := actresscache.SourceKey(source, "validation"); err != nil {
		return nil, err
	}
	return &SnapshotProvider{snapshot: snapshot, source: source}, nil
}
func (p *SnapshotProvider) LookupEvidence(ctx context.Context, q Query) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := q.validate(); err != nil {
		return Result{}, err
	}
	if p.snapshot == nil {
		return Result{Outcome: NoSnapshot}, nil
	}
	result := Result{Outcome: Unmatched, SnapshotDigest: p.snapshot.Digest()}
	var candidate *actresscache.Evidence
	if q.Reference != nil {
		if q.Reference.Source != p.source {
			return result, nil
		}
		key, _ := actresscache.SourceKey(q.Reference.Source, q.Reference.ID)
		if record, ok := p.snapshot.Get(key); ok {
			candidate = &record
		}
	} else {
		// Check all owners BEFORE source selection; never erase competing claims.
		match, err := p.snapshot.LookupUnique(q.Claim.Namespace, q.Claim.Value)
		if err != nil {
			return Result{}, err
		}
		if match.Ambiguous {
			result.Outcome = Ambiguous
			return result, nil
		}
		if match.Candidate != nil && match.Candidate.Source == p.source {
			candidate = match.Candidate
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if candidate != nil {
		result.Outcome = Available
		result.Evidence = candidate
	}
	return result, nil
}

// Clock permits deterministic throttling tests. Wait must honor ctx. The runtime
// implementation uses a cancellable timer; no worker or background queue exists.
type Clock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
func (SystemClock) Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Registration has explicit enabled presence: false disables, with no defaults.
// MinInterval zero explicitly disables throttling; negatives are rejected.
type Registration struct {
	Name        string
	Provider    Provider
	Enabled     bool
	MinInterval time.Duration
}
type entry struct {
	Registration
	next time.Time
}

// Registry admits one synchronous resolution at a time (ErrBusy, no waiting
// queue). Rate limits span calls. Construct once and do not mutate providers.
type Registry struct {
	mu      sync.Mutex
	entries map[string]*entry
	clock   Clock
	timeout time.Duration
}

func NewRegistry(registrations []Registration, clock Clock, timeout time.Duration) (*Registry, error) {
	if clock == nil || timeout <= 0 || len(registrations) > MaxProviders {
		return nil, fmt.Errorf("invalid resolver clock, timeout or provider budget")
	}
	r := &Registry{entries: make(map[string]*entry, len(registrations)), clock: clock, timeout: timeout}
	for _, registration := range registrations {
		if _, err := actresscache.SourceKey(registration.Name, "validation"); err != nil {
			return nil, err
		}
		if registration.Provider == nil || registration.MinInterval < 0 || r.entries[registration.Name] != nil {
			return nil, fmt.Errorf("invalid or duplicate metadata provider")
		}
		r.entries[registration.Name] = &entry{Registration: registration}
	}
	return r, nil
}

// Observation retains provider diagnostics instead of conflating missing/empty.
type Observation struct {
	Provider string
	Result   Result
}
type Resolution struct {
	Outcome      Outcome
	Observations []Observation
}

// Resolve queries every enabled priority entry in caller order. Priority orders
// observations, not authority: it never short-circuits or selects a winner from
// competing source keys. __skip__ anywhere disables the entire request. Unknown
// or duplicate priorities are errors before any calls. Empty priorities skip.
// Errors and cancellation return no partial resolution. Providers must cooperate
// with the bounded context; no goroutine is leaked to mask blocking providers.
// Callers must not mutate inputs concurrently during entry. Each adapter receives
// independent query values; returned evidence is copied before validation/storage.
// Adapters must not mutate returned evidence concurrently with that copy.
func (r *Registry) Resolve(ctx context.Context, q Query, priorities []string) (*Resolution, error) {
	q = q.clone()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := q.validate(); err != nil {
		return nil, err
	}
	if len(priorities) > MaxProviders {
		return nil, fmt.Errorf("metadata priority budget exceeded")
	}
	priorities = append([]string(nil), priorities...)
	if !r.mu.TryLock() {
		return nil, ErrBusy
	}
	defer r.mu.Unlock()
	seen := make(map[string]bool, len(priorities))
	skip := len(priorities) == 0
	for _, name := range priorities {
		if seen[name] {
			return nil, fmt.Errorf("duplicate metadata priority")
		}
		seen[name] = true
		if name == Skip {
			skip = true
			continue
		}
		if r.entries[name] == nil {
			return nil, fmt.Errorf("unknown metadata provider: %s", name)
		}
	}
	result := &Resolution{Outcome: Skipped}
	if skip {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	owners := make(map[string]*actresscache.Evidence)
	ambiguous := false
	digest := ""
	for _, name := range priorities {
		e := r.entries[name]
		if !e.Enabled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if delay := e.next.Sub(r.clock.Now()); delay > 0 {
			if err := r.clock.Wait(ctx, delay); err != nil {
				return nil, err
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		e.next = r.clock.Now().Add(e.MinInterval)
		observation, err := e.Provider.LookupEvidence(ctx, q.clone())
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		observation.Evidence = cloneEvidence(observation.Evidence)
		// A request must not mix artifact generations, even when the source key
		// happens to agree. Conflicting metadata must not look like one owner.
		if observation.Outcome != NoSnapshot {
			decoded, err := hex.DecodeString(observation.SnapshotDigest)
			if err != nil || len(decoded) != 32 {
				return nil, fmt.Errorf("provider returned invalid snapshot digest")
			}
		} else if observation.SnapshotDigest != "" || observation.Evidence != nil {
			return nil, fmt.Errorf("missing snapshot carried evidence")
		}
		if observation.Outcome != Available && observation.Evidence != nil {
			return nil, fmt.Errorf("nonavailable provider result carried evidence")
		}
		if observation.SnapshotDigest != "" {
			if digest != "" && digest != observation.SnapshotDigest {
				return nil, fmt.Errorf("metadata providers used different snapshot generations")
			}
			digest = observation.SnapshotDigest
		}
		result.Observations = append(result.Observations, Observation{Provider: name, Result: observation})
		switch observation.Outcome {
		case Ambiguous:
			ambiguous = true
		case Available:
			if observation.Evidence == nil {
				return nil, fmt.Errorf("provider returned available without evidence")
			}
			key, err := actresscache.SourceKey(observation.Evidence.Source, observation.Evidence.SourceID)
			if err != nil {
				return nil, err
			}
			record := observation.Evidence
			if q.Reference != nil {
				if record.Source != q.Reference.Source || record.SourceID != q.Reference.ID {
					return nil, fmt.Errorf("provider evidence does not match source reference")
				}
			} else {
				matched := false
				for _, id := range record.ExternalIDs {
					if id == *q.Claim {
						matched = true
						break
					}
				}
				if !matched {
					return nil, fmt.Errorf("provider evidence does not contain exact claim")
				}
			}
			if record.Provenance.SourceURL == "" || record.Provenance.DatasetSHA256 == "" || record.Provenance.ParserRevision == "" || record.Provenance.ObservedAt.IsZero() {
				return nil, fmt.Errorf("provider evidence lacks provenance")
			}
			if previous := owners[key]; previous != nil && !reflect.DeepEqual(previous, record) {
				return nil, fmt.Errorf("metadata providers returned conflicting evidence for the same source key")
			}
			owners[key] = record
		case NoSnapshot, Unmatched:
		default:
			return nil, fmt.Errorf("invalid metadata provider outcome")
		}
	}
	if len(result.Observations) == 0 {
		return result, nil
	}
	result.Outcome = Unmatched
	if len(owners) > 0 {
		result.Outcome = Available
	}
	for _, observation := range result.Observations {
		if observation.Result.Outcome == NoSnapshot {
			result.Outcome = NoSnapshot
		}
	}
	if ambiguous || len(owners) > 1 {
		result.Outcome = Ambiguous
	}
	return result, nil
}
