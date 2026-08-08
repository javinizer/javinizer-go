package worker

import (
	"context"
	"fmt"

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

	var persistMsg string
	if err := jobRepo.Upsert(context.Background(), dbJob); err != nil {
		logging.Warnf("Failed to upsert job %s in database: %v", job.ID.String(), err)
		persistMsg = fmt.Sprintf("upsert failed: %v", err)
		job.controller.SetPersistError(persistMsg)
		return fmt.Errorf("persist job %s: %w", job.ID.String(), err)
	}
	job.controller.SetPersistError("")
	return nil
}
