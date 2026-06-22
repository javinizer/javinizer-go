package worker

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/poster"
)

// JobStoreInterface defines the contract for the in-memory batch job store.
// API handlers depend on this interface rather than the concrete *JobStore,
// enabling test mocks without requiring a real database.
//
// Per ADR-0032: GetMutableJob and GetLifecycleJob are replaced with
// purpose-named getters (GetJobForEdit, GetJobForControl) that return
// the appropriate handler-oriented composite.
//
// The interface includes only the methods actually consumed by API callers:
//   - Batch handlers (lifecycle, execute, rescrape, movie_edit) use
//     GetJob, GetJobForEdit, GetJobForControl, CreateJob, DeleteJob,
//     PersistJob, and ListJobs.
//   - The jobs query service uses GetJobForControl.
//   - The temp poster handler uses GetJob.
type JobStoreInterface interface {
	// GetJob retrieves a thread-safe snapshot of a job by ID.
	// Returns a read-only BatchJobStatus and true if found, nil and false otherwise.
	GetJob(id string) (*BatchJobStatus, bool)

	// GetJobForEdit retrieves an EditableJob for movie editing operations.
	// Per ADR-0032: returns the narrow composite (15 methods) for movie_edit,
	// exclude, and similar handlers. The compiler enforces that callers cannot
	// access phase control or rescrape methods through this interface.
	GetJobForEdit(id string) (EditableJob, bool)

	// GetJobForControl retrieves a ControlledJob for phase execution operations.
	// Per ADR-0032: returns the narrow composite (15 methods) for rescrape,
	// organize, scrape, cancel, and revert handlers.
	// For the unified seam, prefer GetBatchJob which returns BatchJobInterface.
	GetJobForControl(id string) (ControlledJob, bool)

	// GetBatchJob retrieves a BatchJobInterface for full lifecycle operations.
	// Per DEEP-1: handlers that need both edit and control access use this
	// instead of juggling GetJobForEdit and GetJobForControl separately.
	GetBatchJob(id string) (BatchJobInterface, bool)

	// CreateJob creates a new batch job with the given files and optional config.
	// Per DEEP-1: returns BatchJobInterface — the unified lifecycle seam for batch
	// handlers. Callers that need only a narrow view can type-assert to ControlledJob
	// or EditableJob, but the full interface eliminates the need to juggle composites.
	// If jobCfg is provided, the job is configured with workflow dependencies.
	CreateJob(files []string, jobCfg ...*JobConfig) BatchJobInterface

	// CreateJobBatch creates a new batch job and returns the concrete *BatchJob.
	// Use this in test code that needs direct access to BatchJob methods
	// (SetJobStatus, SetFileMatchInfo, etc.) for setup.
	// Production code should use CreateJob which returns the ControlledJob composite.
	CreateJobBatch(files []string, jobCfg ...*JobConfig) *BatchJob

	// DeleteJob removes a job from the store and cleans up associated temp files.
	// Returns error if job not found, job is running, or database deletion fails.
	DeleteJob(id string) error

	// PersistJob saves a job to the database.
	PersistJob(job *BatchJob)

	// PersistJobByID persists a job by its ID.
	// The store holds the concrete *BatchJob internally, so callers that only
	// have a composite (EditableJob, ControlledJob) can persist without
	// a type assertion. No-op if the job is not found in the store.
	PersistJobByID(id string)

	// ListJobs returns thread-safe snapshots of all jobs.
	// Returns read-only BatchJobStatus snapshots.
	//
	// Scope: this interface owns the *in-memory* live-job map (Get/Create/Delete/Persist/List
	// and the composite getters). Persisted-job DB queries (ListPersistedJobs, CountOperations*)
	// are intentionally NOT here — callers use APIDeps.GetJobRepo() / GetBatchFileOpRepo() directly,
	// matching the api/jobs package's established pattern. See architecture-review.md candidate #1.
	ListJobs() []*BatchJobStatus

	// CleanupStaleTempDirs removes temp poster directories for jobs that are in
	// a terminal state for >24 hours or orphaned (job no longer in the database).
	// Returns the count of removed directories.
	CleanupStaleTempDirs(ctx context.Context) (int, error)

	// SetReconstructionDeps sets the infrastructure dependencies (matcher,
	// posterGen, batchCfg) used when reconstructing jobs from the database.
	// Called by APIRuntime after the BatchJobFactory is built, since these deps
	// are not available at NewJobStore time. Also re-hydrates already-loaded jobs.
	SetReconstructionDeps(m matcher.MatcherInterface, pg poster.PosterGenerator, batchCfg BatchJobConfig)
}

// Compile-time assertion that JobStore satisfies JobStoreInterface.
var _ JobStoreInterface = (*JobStore)(nil)
