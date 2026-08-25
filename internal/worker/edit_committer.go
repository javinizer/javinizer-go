package worker

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// EditTransactor is the composite SQLite transaction seam for review edits
// (POSTER-WRITE-HARDENING D4). *database.DB satisfies it via WithEditTx: the
// EditUnit handed to fn holds movie/actress/job repositories scoped to ONE
// gorm transaction, so movie-row writes, actress renames, and the job
// envelope upsert commit — or roll back — together.
type EditTransactor interface {
	WithEditTx(ctx context.Context, fn func(u database.EditUnit) error) error
}

// ActressRenamePlan describes one actress name-field rename by primary key.
// Names are snapshotted from the REQUEST payload BEFORE the movie Upsert runs
// — Upsert may normalize movie.Actresses in place (D4 preTx snapshot rule).
type ActressRenamePlan struct {
	ID           uint
	FirstName    string
	LastName     string
	JapaneseName string
}

// EditCommitPlan is the complete atomic unit of a review-edit commit.
// Build it entirely under the keyed edit lock, hand it to Commit, and keep
// all publication inside Publish: ANY leg failing rolls ALL DB writes back
// and Publish never runs — no half-state ever becomes observable (D4).
type EditCommitPlan struct {
	// UpsertMovie, when non-nil, is the candidate movie row to upsert FIRST
	// (PATCH ordering contract: movie upsert precedes actress renames).
	UpsertMovie *models.Movie

	// MutateMovieID + MutateMovie perform a find→mutate→upsert INSIDE the
	// transaction (e.g. poster-from-URL field mutation of the canonical row).
	// A missing row is a no-op with a warning, matching legacy best-effort.
	MutateMovieID string
	MutateMovie   func(m *models.Movie)

	// Renames are explicit actress name edits, executed after the upsert.
	Renames []ActressRenamePlan

	// EnvelopeFn builds the candidate-merged job envelope row. Returning a
	// nil row skips the envelope upsert leg gracefully.
	EnvelopeFn func() (*models.Job, error)

	// EnvelopeGenerationCommitted updates the owning BatchJob after the
	// generation-aware envelope transaction accepts and publication succeeds.
	// A failed publication leaves the in-memory generation stale, forcing a
	// later persist to fail closed rather than overwrite the committed row.
	EnvelopeGenerationCommitted func(uint64)

	// Publish commits the candidate to in-memory state. Executed only AFTER
	// the transaction commits — never inside it, never before it.
	Publish func() error
}

// HasDBLegs reports whether the plan carries any database writes. Plans with
// no DB legs (e.g. geometry-only crop edits whose only leg is the envelope)
// still route through Commit when an envelope is present.
func (p *EditCommitPlan) HasDBLegs() bool {
	if p == nil {
		return false
	}
	return p.UpsertMovie != nil || p.MutateMovie != nil || len(p.Renames) > 0 || p.EnvelopeFn != nil
}

// EditCommitter executes EditCommitPlans inside one composite transaction.
// It holds the job's envelope lock for the WHOLE commit (D2): candidate
// snapshot → tx legs → publish serialize against phase persists
// (PersistJob/ByID) and competing edit commits, so the last committed
// envelope can never regress committed edits.
type EditCommitter struct {
	tx     EditTransactor
	locks  *keyedMutexRegistry
	jobKey string
	// famLocks serializes cross-entity rows (actress renames by PK) on the
	// process-wide registry (D15): two different families sharing an actress
	// record otherwise interleave read/rename/write legs across their
	// composite transactions (codex r13).
	famLocks *keyedMutexRegistry
}

// NewEditCommitter binds the tx seam to one job's envelope lock key; famLocks
// gates the actress-rename leg per actress ID.
func NewEditCommitter(tx EditTransactor, locks *keyedMutexRegistry, jobKey string, famLocks *keyedMutexRegistry) *EditCommitter {
	return &EditCommitter{tx: tx, locks: locks, jobKey: jobKey, famLocks: famLocks}
}

// Commit runs the plan atomically:
//  1. Movie-row upsert (PATCH ordering contract)
//  2. find→mutate→upsert row mutation
//  3. Actress renames (preTx-snapshotted fields)
//  4. Candidate-merged envelope build + upsert
//  5. Publish (only after commit success)
//
// Any leg failing rolls ALL back and the caller turns it into a 5xx; nothing
// observable changes (no partial movie row, no stale-provenance envelope, no
// in-memory divergence).
func (c *EditCommitter) Commit(ctx context.Context, plan *EditCommitPlan) error {
	if c == nil || c.tx == nil {
		return fmt.Errorf("edit committer requires a transaction seam")
	}
	if c.locks != nil && c.jobKey != "" {
		release := c.locks.Acquire(c.jobKey)
		defer release()
	}
	if plan == nil || !plan.HasDBLegs() {
		if plan != nil && plan.Publish != nil {
			return plan.Publish()
		}
		return nil
	}
	// Acquire ALL actress-ID keys from the renames leg before the tx opens —
	// folded onto the process-wide registry so concurrent family edits on
	// two jobs sharing an actress serialize (codex r13). Sorted set keeps a
	// single global acquisition order.
	if c.famLocks != nil && len(plan.Renames) > 0 {
		keys := make([]string, 0, len(plan.Renames))
		for _, rn := range plan.Renames {
			keys = append(keys, fmt.Sprintf("actress:%d", rn.ID))
		}
		release := c.famLocks.AcquireMany(keys)
		defer release()
	}

	var acceptedGeneration uint64
	generationCommitted := false
	if err := c.tx.WithEditTx(ctx, func(u database.EditUnit) error {
		// Renames FIRST inside the tx: the movie upserter's fill-merge reads
		// renamed DB rows by ID/name back into the in-memory movie (edited
		// names must reach NFO generation). Atomicity is unaffected — any
		// failing leg rolls the whole transaction back.
		for _, rn := range plan.Renames {
			existing, err := u.Actresses.FindByID(ctx, rn.ID)
			switch {
			case err != nil && !database.IsNotFound(err):
				return fmt.Errorf("load actress for rename: %w", err)
			case err != nil || existing == nil:
				continue
			}
			if existing.FirstName == rn.FirstName && existing.LastName == rn.LastName && existing.JapaneseName == rn.JapaneseName {
				continue
			}
			if err := u.Actresses.RenameNameFields(ctx, rn.ID, rn.FirstName, rn.LastName, rn.JapaneseName); err != nil {
				return fmt.Errorf("persist actress name edit: %w", err)
			}
		}
		if plan.UpsertMovie != nil {
			if _, err := u.Movies.Upsert(ctx, plan.UpsertMovie); err != nil {
				return fmt.Errorf("persist movie update: %w", err)
			}
		}
		if plan.MutateMovie != nil && plan.MutateMovieID != "" {
			existing, err := u.Movies.FindByID(ctx, plan.MutateMovieID)
			switch {
			case err != nil && !database.IsNotFound(err):
				return fmt.Errorf("find movie %s: %w", plan.MutateMovieID, err)
			case err != nil || existing == nil:
				logging.Warnf("movie %s not found for in-tx mutation; skipping", plan.MutateMovieID)
			default:
				plan.MutateMovie(existing)
				if _, err := u.Movies.Upsert(ctx, existing); err != nil {
					return fmt.Errorf("persist mutated movie %s: %w", plan.MutateMovieID, err)
				}
			}
		}
		if plan.EnvelopeFn != nil {
			row, err := plan.EnvelopeFn()
			if err != nil {
				return fmt.Errorf("encode envelope: %w", err)
			}
			if row != nil {
				if committer, ok := u.Jobs.(database.EnvelopeCommitter); ok {
					accepted, err := committer.CommitEnvelope(ctx, row, row.EnvelopeGeneration)
					if err != nil {
						return fmt.Errorf("persist job envelope: %w", err)
					}
					acceptedGeneration = accepted
					generationCommitted = true
					row.EnvelopeGeneration = accepted
				} else if err := u.Jobs.Upsert(ctx, row); err != nil {
					// Legacy/test repositories without the optional seam retain
					// the pre-Phase-6 composite behavior.
					return fmt.Errorf("persist job envelope: %w", err)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if plan.Publish != nil {
		if err := plan.Publish(); err != nil {
			// The DB committed but in-memory publication failed — an extreme
			// edge (AtomicUpdate enforces invariants). Leave the live generation
			// unchanged so a later persist cannot overwrite the committed row.
			return fmt.Errorf("publish committed edit: %w", err)
		}
	}
	if generationCommitted && plan.EnvelopeGenerationCommitted != nil {
		plan.EnvelopeGenerationCommitted(acceptedGeneration)
	}
	return nil
}
