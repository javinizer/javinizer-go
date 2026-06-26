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
	// PersistJob persists a concrete *BatchJob to the database.
	PersistJob(job *BatchJob)

	// PersistJobByID persists a job by its ID (no-op if not found).
	PersistJobByID(id string)

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

func (noopJobPersistence) PersistJob(_ *BatchJob)                           {}
func (noopJobPersistence) PersistJobByID(_ string)                          {}
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

func (p *dbJobPersistence) PersistJob(job *BatchJob) {
	persistToDatabase(p.jobRepo, job)
}

func (p *dbJobPersistence) PersistJobByID(id string) {
	// dbJobPersistence only holds the job repo, not the store's id→*BatchJob
	// map, so it cannot resolve a job by ID. ID→job resolution lives in
	// JobStore.PersistJobByID (which looks up s.jobs then calls PersistJob).
	// Rather than silently dropping the update, report the inability to persist
	// so callers using the JobPersistencer contract get a signal.
	logging.Warnf("dbJobPersistence.PersistJobByID(%s) cannot resolve job by ID without the store map; update not persisted", id)
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
func persistToDatabase(jobRepo database.JobRepositoryInterface, job *BatchJob) {
	if jobRepo == nil {
		return
	}

	dbJob, ok := snapshotForPersist(job)
	if !ok {
		return
	}

	var persistMsg string
	if err := jobRepo.Upsert(context.Background(), dbJob); err != nil {
		logging.Warnf("Failed to upsert job %s in database: %v", job.ID.String(), err)
		persistMsg = fmt.Sprintf("upsert failed: %v", err)
	}
	job.controller.SetPersistError(persistMsg)
}
