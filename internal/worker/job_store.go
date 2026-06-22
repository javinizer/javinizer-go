package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// BatchJobDeps, JobConfig, BatchJob, and setDepsFromConfig are defined in batch_job.go.

// JobStore manages batch jobs with persistence to the database.
// It replaces the former JobQueue type, collapsing the job map,
// CreateJob, loadFromDatabase, and persistToDatabase into a single type.
// Per P-8: temp dir cleanup is delegated to TempDirCleaner rather than
// implemented directly on JobStore.
type JobStore struct {
	jobs              map[models.JobID]*BatchJob
	jobRepo           database.JobRepositoryInterface
	batchFileOpRepo   database.BatchFileOperationRepositoryInterface
	movieRepo         database.MovieRepositoryInterface
	persistence       JobPersistencer
	tempDir           string
	templateEngine    template.EngineInterface
	fs                afero.Fs
	mu                sync.RWMutex
	deserializeErrors atomic.Int64    // count of JSON deserialization failures in reconstructBatchJob
	tempCleaner       *TempDirCleaner // Per P-8: owns CleanupStaleTempDirs and StartStaleTempCleanup
	tempCleanerOnce   sync.Once       // Guards tempCleaner lazy-init against concurrent RLock callers

	// reconstructionDeps are infrastructure dependencies that reconstructed jobs
	// (loaded from DB on startup) need for apply/rescrape phases. They are set
	// after JobStore construction via SetReconstructionDeps, once the
	// BatchJobFactory (which owns matcher, posterGen, batchCfg) is built.
	// New jobs created via createJob get these from JobConfig.BatchJobDeps instead.
	reconMatcher   matcher.MatcherInterface
	reconPosterGen poster.PosterGenerator
	reconBatchCfg  BatchJobConfig
}

// JobStoreOption configures a JobStore during construction.
type JobStoreOption func(*JobStore)

// WithPersistence sets the JobPersistencer for the JobStore.
// When provided, it overrides the default persistence implementation.
// For NewJobStore, the default is dbJobPersistence constructed from the repos.
// For NewInMemoryJobStore, the default is noopJobPersistence.
// Use this option to inject a mock persistencer in tests.
func WithPersistence(p JobPersistencer) JobStoreOption {
	return func(s *JobStore) {
		s.persistence = p
	}
}

// NewInMemoryJobStore creates a JobStore without a database.
// It provides the full JobStore construction path (createJob) including
// JobStore registration and PersistFn wiring, but skips database persistence
// since jobRepo is nil. Use this for CLI/TUI usage where persistence is
// not needed — it replaces the former NewBatchJob direct-construction path.
//
// Per NEW-2: this is the single construction path for non-persistent jobs.
// All job creation (persistent and in-memory) routes through JobStore.createJob,
// ensuring that adding a new initialization step changes only createJob, not
// two separate functions.
func NewInMemoryJobStore(opts ...JobStoreOption) *JobStore {
	s := &JobStore{
		jobs:        make(map[models.JobID]*BatchJob),
		persistence: noopJobPersistence{},
		tempCleaner: NewTempDirCleaner(nil, "", nil),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewJobStore creates a new job store with the given repositories and temp directory.
// If fs is nil, the real OS filesystem is used.
// By default, it constructs a dbJobPersistence from the provided repos.
// Use WithPersistence to inject a custom JobPersistencer (e.g., a mock in tests).
func NewJobStore(jobRepo database.JobRepositoryInterface, batchFileOpRepo database.BatchFileOperationRepositoryInterface, movieRepo database.MovieRepositoryInterface, tempDir string, engine template.EngineInterface, fs afero.Fs, opts ...JobStoreOption) *JobStore {
	if engine == nil {
		engine = template.NewEngine()
	}
	var filesystem afero.Fs
	if fs != nil {
		filesystem = fs
	} else {
		filesystem = afero.NewOsFs()
	}
	s := &JobStore{
		jobs:            make(map[models.JobID]*BatchJob),
		jobRepo:         jobRepo,
		batchFileOpRepo: batchFileOpRepo,
		movieRepo:       movieRepo,
		persistence: &dbJobPersistence{
			jobRepo: jobRepo,
		},
		tempDir:        tempDir,
		templateEngine: engine,
		fs:             filesystem,
		tempCleaner:    NewTempDirCleaner(filesystem, tempDir, jobRepo),
	}

	// Apply options, which may override the default persistence.
	for _, opt := range opts {
		opt(s)
	}

	s.loadFromDatabase()

	return s
}

// SetReconstructionDeps sets the infrastructure dependencies (matcher, posterGen,
// batchCfg) used when reconstructing jobs from the database. These are not
// available at NewJobStore time because they require the WorkflowFactory
// (which is built later, lazily, by APIRuntime). APIRuntime.buildBatchJobFactory
// calls this once the factory deps are ready.
//
// The method also re-hydrates all already-loaded in-memory jobs so that jobs
// reconstructed during NewJobStore.loadFromDatabase (before this call) get
// the same deps as jobs reconstructed afterwards.
func (s *JobStore) SetReconstructionDeps(m matcher.MatcherInterface, pg poster.PosterGenerator, batchCfg BatchJobConfig) {
	s.mu.Lock()
	s.reconMatcher = m
	s.reconPosterGen = pg
	s.reconBatchCfg = batchCfg
	for _, job := range s.jobs {
		job.mu.Lock()
		if m != nil {
			job.deps.Matcher = m
		}
		if pg != nil {
			job.deps.PosterGen = pg
		}
		// BatchCfg is a value type (not a pointer), so we always overwrite to
		// pick up the latest config snapshot.
		job.deps.BatchCfg = batchCfg
		job.mu.Unlock()
	}
	s.mu.Unlock()
}

// loadFromDatabase loads existing jobs from the database on startup
func (s *JobStore) loadFromDatabase() {
	jobs, err := s.persistence.LoadJobs(context.Background())
	if err != nil {
		logging.Warnf("Failed to load jobs from database: %v", err)
		return
	}

	for i := range jobs {
		batchJob := s.reconstructBatchJob(&jobs[i])
		if batchJob != nil {
			s.jobs[batchJob.ID] = batchJob
		}
	}
}

// jobAdapters holds pre-built adapter components for a BatchJob.
// Constructed once by buildAdapters and consumed by the public JobStore
// methods (CreateJob, GetJobForEdit, GetJobForControl) to eliminate
// duplicated closure wiring.
type jobAdapters struct {
	reader          JobReader
	movieLookup     MovieLookup
	phaseController PhaseController
	canceller       JobCanceller
	editor          JobEditor
}

// buildAdapters constructs all adapter components for a BatchJob.
// Each public method assembles its return value from a subset of these
// components, avoiding the ~20 lines of duplicated closure wiring that
// previously appeared in CreateJob, GetJobForEdit, and GetJobForControl.
func buildAdapters(job *BatchJob) *jobAdapters {
	jr := &jobReaderImpl{
		id:          job.ID.String(),
		lifecycle:   job.lifecycle,
		results:     job.results,
		snapshotFn:  job.GetStatus,
		subscribeFn: job.Subscribe,
		resultsFn:   job.results.GetResults,
	}
	return &jobAdapters{
		reader:      jr,
		movieLookup: job.resultIndex,
		phaseController: &phaseControllerImpl{
			startScrape:      job.controller.StartScrape,
			startApply:       job.controller.StartApply,
			wait:             job.controller.Wait,
			rescrape:         job.controller.Rescrape,
			setWorkflow:      job.controller.SetWorkflow,
			setBatchCfg:      job.controller.SetBatchCfg,
			setJobStatus:     job.controller.SetJobStatus,
			setOperationMode: job.controller.SetOperationModeOverride,
			setPersistError:  job.controller.SetPersistError,
		},
		canceller: job.lifecycle,
		editor: &jobEditorImpl{
			updater:      job.results,
			accessor:     job.results,
			lifecycle:    job.lifecycle,
			posterEditor: job.posterEditor,
			movieRepo:    job.deps.MovieRepo,
		},
	}
}

// CreateJob creates a new batch job and returns it as a BatchJobInterface.
// Per DEEP-1: returns the unified lifecycle seam instead of ControlledJob,
// giving API handlers a single interface for the full batch job lifecycle.
func (s *JobStore) CreateJob(files []string, jobCfg ...*JobConfig) BatchJobInterface {
	job := s.createJob(files, jobCfg...)
	a := job.getAdapters()
	return &batchJobAdapter{
		JobReader:       a.reader,
		MovieLookup:     a.movieLookup,
		PhaseController: a.phaseController,
		JobCanceller:    a.canceller,
		JobEditor:       a.editor,
	}
}

// CreateJobBatch creates a new batch job and returns the concrete *BatchJob.
// For internal use and tests that need direct access to BatchJob fields.
// External callers should use CreateJob which returns the ControlledJob composite.
func (s *JobStore) CreateJobBatch(files []string, jobCfg ...*JobConfig) *BatchJob {
	return s.createJob(files, jobCfg...)
}

// createJob is the shared implementation for CreateJob and CreateJobBatch.
// Per NEW-2: delegates to newBatchJob for base construction, then adds
// JobStore-specific initialization (PersistFn, fallback repos, job map
// registration, database persistence). This ensures a single construction
// path — newBatchJob handles the common init, createJob adds the JobStore layer.
func (s *JobStore) createJob(files []string, jobCfg ...*JobConfig) *BatchJob {
	job := newBatchJob(files, jobCfg...)

	// JobStore-specific: set tempDir and fs from the store
	if s.tempDir != "" {
		job.mu.Lock()
		job.cfg.tempDir = s.tempDir
		job.mu.Unlock()
	}
	if s.fs != nil {
		job.fs = s.fs
		job.fsCaseCache = NewFSCaseCache(s.fs)
	}
	if s.templateEngine != nil {
		job.templateEngine = s.templateEngine
	}

	// Override posterEditor with the one that has movieRepo access
	if s.movieRepo != nil {
		job.posterEditor = NewPosterEditor(job.resultIndex, job.results, s.movieRepo)
	}

	// Set persistFn after job is constructed so the closure captures the correct pointer
	job.deps.PersistFn = func() { s.persistence.PersistJob(job) }

	// Fallback: if JobConfig didn't provide these repos, use JobStore's
	if job.deps.BatchFileOpRepo == nil {
		job.deps.BatchFileOpRepo = s.batchFileOpRepo
	}
	if job.deps.MovieRepo == nil {
		job.deps.MovieRepo = s.movieRepo
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	s.persistence.PersistJob(job)

	return job
}

// GetJob retrieves a thread-safe snapshot of a job by ID
// Returns a read-only BatchJobStatus to prevent external mutations of internal state
func (s *JobStore) GetJob(id string) (*BatchJobStatus, bool) {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()

	if !ok {
		return nil, false
	}

	// Return a safe snapshot using GetStatus
	return job.GetStatus(), true
}

// GetJobForEdit retrieves an EditableJob for movie editing operations.
// Per ADR-0041: returns an editableJobAdapter composing jobReaderImpl +
// ResultTracker + jobEditorImpl. This decouples the edit path from *BatchJob.
func (s *JobStore) GetJobForEdit(id string) (EditableJob, bool) {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	a := job.getAdapters()
	return &editableJobAdapter{
		JobReader:   a.reader,
		MovieLookup: a.movieLookup,
		JobEditor:   a.editor,
	}, true
}

// GetJobForControl retrieves a ControlledJob for phase execution operations.
// Per ADR-0041: returns a controlledJobAdapter composing jobReaderImpl +
// ResultTracker + *BatchJob (for PhaseController) + JobLifecycle.
// For the unified seam, prefer GetBatchJob which returns BatchJobInterface.
func (s *JobStore) GetJobForControl(id string) (ControlledJob, bool) {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	a := job.getAdapters()
	return &controlledJobAdapter{
		JobReader:       a.reader,
		MovieLookup:     a.movieLookup,
		PhaseController: a.phaseController,
		JobCanceller:    a.canceller,
	}, true
}

// GetBatchJob retrieves a BatchJobInterface for full lifecycle operations.
// Per DEEP-1: returns the unified seam for batch handlers that need both
// edit and control access, eliminating the need to juggle GetJobForEdit
// and GetJobForControl separately.
func (s *JobStore) GetBatchJob(id string) (BatchJobInterface, bool) {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	a := job.getAdapters()
	return &batchJobAdapter{
		JobReader:       a.reader,
		MovieLookup:     a.movieLookup,
		PhaseController: a.phaseController,
		JobCanceller:    a.canceller,
		JobEditor:       a.editor,
	}, true
}

// deleteJobFromDB deletes a job from the database via the job repository.
// Extracted from DeleteJob per S-9 so that DB logic is separated from lifecycle logic.
func deleteJobFromDB(jobRepo database.JobRepositoryInterface, id string) error {
	if jobRepo == nil {
		return nil
	}
	if err := jobRepo.Delete(context.Background(), id); err != nil {
		logging.Warnf("Failed to delete job %s from database: %v", id, err)
		return fmt.Errorf("database deletion failed: %w", err)
	}
	return nil
}

// DeleteJob removes a job from the store and cleans up associated temp files.
// Cancels the job first and waits for it to fully finish before removing files.
// tempDir is the base temp directory (e.g., "data/temp").
// The status check and job removal are performed under the store lock to prevent
// a TOCTOU race where the job transitions to Running between the check and deletion.
// Returns error if job not found, job is running, or database deletion fails.
// Per S-9: temp cleanup delegated to TempDirCleaner, DB deletion to deleteJobFromDB;
// DeleteJob is now a thin lifecycle orchestrator.
func (s *JobStore) DeleteJob(id string) error {
	s.mu.Lock()
	job, ok := s.jobs[models.JobID(id)]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("job %s not found", id)
	}

	// Check status under the store lock to prevent TOCTOU race:
	// without holding the store lock, a concurrent StartScrape could transition
	// the job to Running between the status check and the deletion below.
	job.lifecycle.mu.RLock()
	status := job.lifecycle.Status
	deleted := job.lifecycle.deleted
	job.lifecycle.mu.RUnlock()

	if deleted {
		s.mu.Unlock()
		return fmt.Errorf("job %s already deleted", id)
	}
	if status == models.JobStatusRunning {
		s.mu.Unlock()
		return fmt.Errorf("cannot delete running job")
	}

	if status == models.JobStatusPending {
		job.lifecycle.Cancel()
	}

	// Remove from the map while still holding the store lock so no concurrent
	// caller can observe or transition this job after we've decided to delete it.
	delete(s.jobs, models.JobID(id))
	s.mu.Unlock()

	// Wait for the job to finish outside the store lock — the job is already
	// removed from the map so no new callers can reach it.
	select {
	case <-job.lifecycle.done:
	case <-time.After(5 * time.Second):
		logging.Warnf("DeleteJob: timed out waiting for job %s to finish, proceeding with cleanup", id)
	}

	job.lifecycle.markDeleted()

	// Clean up temp files and delete from DB
	s.getTempCleaner().CleanJobTempDir(id)

	if err := s.persistence.DeleteJobFromDB(id); err != nil {
		return err
	}

	return nil
}

// PersistJob saves a job to the database.
// Per ADR-0032: this is the public persistence method. The former PersistManagedJob
// is removed because it type-asserted to *BatchJob internally — callers that hold
// a composite should use PersistJobByID instead.
func (s *JobStore) PersistJob(job *BatchJob) {
	s.persistence.PersistJob(job)
}

// PersistJobByID persists a job by its ID.
// Per ADR-0032: callers that hold a composite (EditableJob, ControlledJob)
// use this instead of PersistJob — no type assertion needed. The store holds
// the concrete *BatchJob internally. No-op if the job is not found.
func (s *JobStore) PersistJobByID(id string) {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok {
		return
	}
	s.persistence.PersistJob(job)
}

// ListJobs returns thread-safe snapshots of all jobs
// Returns read-only BatchJobStatus snapshots to prevent external mutations of internal state
func (s *JobStore) ListJobs() []*BatchJobStatus {
	s.mu.RLock()
	// Create a snapshot of job pointers while holding the lock
	jobSnapshots := make([]*BatchJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobSnapshots = append(jobSnapshots, job)
	}
	s.mu.RUnlock()

	// Create safe snapshots of each job (releases lock before expensive copying)
	jobs := make([]*BatchJobStatus, 0, len(jobSnapshots))
	for _, job := range jobSnapshots {
		jobs = append(jobs, job.GetStatus())
	}
	return jobs
}

// CleanupStaleTempDirs removes temp poster directories for jobs that are either:
//   - In a terminal state (Organized/Failed/Cancelled/Reverted/Completed) and have been so for >24 hours
//   - Orphaned (the job ID no longer exists in the database)
//
// Returns the count of removed directories. This prevents unbounded disk growth
// from temp poster files that are only cleaned up on explicit DeleteJob calls.
// Per P-8: delegates to TempDirCleaner.
func (s *JobStore) CleanupStaleTempDirs(ctx context.Context) (int, error) {
	return s.getTempCleaner().CleanupStaleTempDirs(ctx)
}

// getTempCleaner returns the TempDirCleaner, initializing one lazily from
// the JobStore's own fields if it was not set during construction (e.g.,
// direct struct literal construction in tests). Thread-safe: a sync.Once
// guards the lazy init, so concurrent callers (which hold the store's RLock,
// allowing multiple readers) can all enter getTempCleaner simultaneously
// without racing the tempCleaner write. If construction already set a
// non-nil tempCleaner, the Once.Do callback observes it and does nothing.
// After the first call returns, subsequent calls read the stable pointer
// with no lock contention.
func (s *JobStore) getTempCleaner() *TempDirCleaner {
	s.tempCleanerOnce.Do(func() {
		if s.tempCleaner == nil {
			s.tempCleaner = NewTempDirCleaner(s.fs, s.tempDir, s.jobRepo)
		}
	})
	return s.tempCleaner
}

// isPastActiveStatus returns true if the job is no longer actively running.
// This includes both true terminal states (Failed, Cancelled, Organized, Reverted)
// and Completed (which can transition to Running/Organized but is not currently active).
// Used by CleanupStaleTempDirs to determine which jobs' temp directories are safe to clean.
func isPastActiveStatus(status models.JobStatus) bool {
	switch status {
	case models.JobStatusOrganized, models.JobStatusFailed,
		models.JobStatusCancelled, models.JobStatusReverted,
		models.JobStatusCompleted:
		return true
	}
	return false
}

// latestInactiveTime returns the most recent past-active timestamp from a Job.
// Returns nil if no inactive timestamp is set.
func latestInactiveTime(job *models.Job) *time.Time {
	var latest *time.Time
	if job.OrganizedAt != nil {
		latest = job.OrganizedAt
	}
	if job.CompletedAt != nil {
		if latest == nil || job.CompletedAt.After(*latest) {
			latest = job.CompletedAt
		}
	}
	if job.RevertedAt != nil {
		if latest == nil || job.RevertedAt.After(*latest) {
			latest = job.RevertedAt
		}
	}
	return latest
}

// StartStaleTempCleanup starts a background goroutine that periodically cleans
// up stale temp poster directories. Returns a stop channel that should be closed
// on shutdown to stop the cleanup loop.
// Per P-8: delegates to TempDirCleaner.
func (s *JobStore) StartStaleTempCleanup() chan struct{} {
	return s.getTempCleaner().StartStaleTempCleanup()
}
