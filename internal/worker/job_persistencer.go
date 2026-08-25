package worker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// JobPersistencer abstracts the database persistence operations for batch jobs.
// JobStore takes this at construction instead of raw repos, eliminating nil-checks.
// Two implementations are provided: noopJobPersistence (no-op) and
// dbJobPersistence (delegates to real repos).
//
// Scope: persist + lifecycle only. DB queries (ListPersistedJobs, CountOperations*)
// were removed — callers use database repos directly via APIDeps.GetJobRepo() /
// GetBatchFileOpRepo(). See architecture-review.md candidate #1.
type JobPersistencer interface {
	// PersistJob persists a concrete *BatchJob to the database. Returns a
	// non-nil error when the envelope could not be encoded or the upsert
	// failed (POSTER-WRITE-HARDENING D2: encode failures ride the same typed
	// error channel as repo failures).
	PersistJob(job *BatchJob) error

	// PersistJobByID persists a job by its ID. Returns ErrJobNotFound when no
	// job resolves for id.
	PersistJobByID(id string) error

	// DeleteJobFromDB deletes a job from the database.
	DeleteJobFromDB(id string) error

	// LoadJobs loads all jobs from the database for store initialization.
	LoadJobs(ctx context.Context) ([]models.Job, error)

	// UpsertJob persists a models.Job to the database.
	UpsertJob(dbJob *models.Job) error
}

// NewNoopJobPersistence returns a no-op JobPersistencer.
// Useful when persistence is not needed, such as in CLI/TUI mode or tests
// that don't require database interaction.
func NewNoopJobPersistence() JobPersistencer {
	return noopJobPersistence{}
}

// noopJobPersistence is a no-op implementation of JobPersistencer.
// Used by NewInMemoryJobStore where database persistence is not needed.
type noopJobPersistence struct{}

func (noopJobPersistence) PersistJob(_ *BatchJob) error                     { return nil }
func (noopJobPersistence) PersistJobByID(_ string) error                    { return nil }
func (noopJobPersistence) DeleteJobFromDB(_ string) error                   { return nil }
func (noopJobPersistence) LoadJobs(_ context.Context) ([]models.Job, error) { return nil, nil }
func (noopJobPersistence) UpsertJob(_ *models.Job) error                    { return nil }

// NewDBJobPersistence returns a database-backed JobPersistencer.
// jobRepo may be nil; the implementation handles nil-checks internally
// (returning errors for operations that require a repo).
func NewDBJobPersistence(jobRepo database.JobRepositoryInterface) JobPersistencer {
	return &dbJobPersistence{
		jobRepo: jobRepo,
	}
}

// dbJobPersistence is the database-backed implementation of JobPersistencer.
type dbJobPersistence struct {
	jobRepo database.JobRepositoryInterface
}

func (p *dbJobPersistence) PersistJob(job *BatchJob) error {
	return persistToDatabase(p.jobRepo, job)
}

func (p *dbJobPersistence) PersistJobByID(id string) error {
	// dbJobPersistence only holds the job repo, not the store's id→*BatchJob
	// map, so it cannot resolve a job by ID. ID→job resolution lives in
	// JobStore.PersistJobByID (which looks up s.jobs then calls PersistJob).
	// Surface the inability as an error instead of silently dropping.
	return fmt.Errorf("%w: dbJobPersistence.PersistJobByID(%s) cannot resolve job by ID without the store map", ErrJobNotFound, id)
}

func (p *dbJobPersistence) DeleteJobFromDB(id string) error {
	return deleteJobFromDB(p.jobRepo, id)
}

func (p *dbJobPersistence) LoadJobs(ctx context.Context) ([]models.Job, error) {
	if p.jobRepo == nil {
		return nil, nil
	}
	return p.jobRepo.List(ctx)
}

func (p *dbJobPersistence) UpsertJob(dbJob *models.Job) error {
	if p.jobRepo == nil {
		return nil
	}
	if err := p.jobRepo.Upsert(context.Background(), dbJob); err != nil {
		logging.Warnf("Failed to upsert job %s in database: %v", dbJob.ID, err)
		return err
	}
	return nil
}

// persistToDatabase saves a BatchJob to the database via the job repository.
// Encode (marshal) failures surface the same way as repository upsert
// failures: typed error to the caller AND persisted onto persist_error state.
func samePersistEnvelope(a, b *models.Job) bool {
	if a == nil || b == nil || a.ID != b.ID {
		return false
	}
	left := *a
	right := *b
	left.PruneVersion = 0
	right.PruneVersion = 0
	left.EnvelopeGeneration = 0
	right.EnvelopeGeneration = 0
	left.StartedAt = left.StartedAt.Round(0).UTC()
	right.StartedAt = right.StartedAt.Round(0).UTC()
	left.CompletedAt = normalizedPersistTime(left.CompletedAt)
	right.CompletedAt = normalizedPersistTime(right.CompletedAt)
	left.OrganizedAt = normalizedPersistTime(left.OrganizedAt)
	right.OrganizedAt = normalizedPersistTime(right.OrganizedAt)
	left.RevertedAt = normalizedPersistTime(left.RevertedAt)
	right.RevertedAt = normalizedPersistTime(right.RevertedAt)
	return reflect.DeepEqual(left, right)
}

func normalizedPersistTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.Round(0).UTC()
	return &normalized
}

func persistToDatabase(jobRepo database.JobRepositoryInterface, job *BatchJob) error {
	if jobRepo == nil {
		return nil
	}

	dbJob, err := snapshotForPersist(job)
	if err != nil {
		job.controller.SetPersistError(err.Error())
		return err
	}
	if dbJob == nil {
		return nil // deleted job — persist intentionally skipped
	}

	var (
		persistMsg         string
		persistErr         error
		acceptedGeneration = dbJob.EnvelopeGeneration
	)
	commit := func(candidate *models.Job) (uint64, error) {
		if committer, ok := jobRepo.(database.EnvelopeCommitter); ok {
			return committer.CommitEnvelope(context.Background(), candidate, candidate.EnvelopeGeneration)
		}
		// Legacy/test repositories without the optional seam retain the old
		// upsert behavior; production JobRepository implements EnvelopeCommitter.
		return candidate.EnvelopeGeneration, jobRepo.Upsert(context.Background(), candidate)
	}
	if _, hasCommitter := jobRepo.(database.EnvelopeCommitter); hasCommitter && dbJob.EnvelopeGeneration == 0 {
		// New jobs are registered through the historical create path before
		// their first update-style envelope commit. Probe once so a missing row
		// is initialized without allowing a deleted/reconstructed row to be
		// recreated by CommitEnvelope.
		_, findErr := jobRepo.FindByID(context.Background(), job.ID.String())
		switch {
		case database.IsNotFound(findErr):
			persistErr = jobRepo.Upsert(context.Background(), dbJob)
			if persistErr == nil {
				acceptedGeneration = dbJob.EnvelopeGeneration
			}
		case findErr != nil:
			persistErr = findErr
		default:
			acceptedGeneration, persistErr = commit(dbJob)
		}
	} else {
		acceptedGeneration, persistErr = commit(dbJob)
	}
	if errors.Is(persistErr, database.ErrStaleEnvelopeGeneration) {
		// Ordinary phase/recovery persists may retry once only when the durable
		// envelope is unchanged and differs by fencing generation alone. A
		// different durable candidate fails closed; transactional edit candidates
		// never enter this function.
		latest, findErr := jobRepo.FindByID(context.Background(), job.ID.String())
		if findErr != nil {
			persistErr = findErr
		} else if latest == nil {
			persistErr = database.ErrNotFound
		} else if !samePersistEnvelope(dbJob, latest) {
			// A different durable envelope means another writer accepted newer
			// state. Replaying the old in-memory snapshot at the new generation
			// would turn the CAS into an overwrite loophole; fail closed and let
			// the caller reload/retry from the durable state.
			persistErr = database.ErrStaleEnvelopeGeneration
		} else {
			// The durable row differs only by generation/prune fencing, so it is
			// safe to commit that durable candidate once at its current base.
			dbJob = latest
			acceptedGeneration, persistErr = commit(dbJob)
		}
	}
	if persistErr != nil {
		logging.Warnf("Failed to persist job %s in database: %v", job.ID.String(), persistErr)
		persistMsg = fmt.Sprintf("upsert failed: %v", persistErr)
		job.controller.SetPersistError(persistMsg)
		return fmt.Errorf("persist job %s: %w", job.ID.String(), persistErr)
	}
	job.mu.Lock()
	if dbJob.PruneVersion > job.pruneVersion {
		job.pruneVersion = dbJob.PruneVersion
	}
	if acceptedGeneration > job.envelopeGeneration {
		job.envelopeGeneration = acceptedGeneration
	}
	job.mu.Unlock()
	job.controller.SetPersistError("")
	return nil
}
