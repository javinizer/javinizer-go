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
	"github.com/javinizer/javinizer-go/internal/worker/fscase"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
)

// BatchJobDeps, JobConfig, BatchJob, and setDepsFromConfig are defined in batch_job.go.

// JobStore manages batch jobs with persistence to the database.
// It replaces the former JobQueue type, collapsing the job map,
// CreateJob, loadFromDatabase, and persistToDatabase into a single type.
// Per P-8: temp dir cleanup is delegated to TempDirCleaner rather than
// implemented directly on JobStore.
type JobStore struct {
	jobs             map[models.JobID]*BatchJob
	jobRepo          database.JobRepositoryInterface
	batchFileOpRepo  database.BatchFileOperationRepositoryInterface
	movieRepo        database.MovieRepositoryInterface
	actressRepo      database.ActressRepositoryInterface
	historyRepo      database.HistoryRepositoryInterface
	persistence      JobPersistencer
	envLocks         *keyedMutexRegistry // POSTER-WRITE-HARDENING D2: per-job envelope persist lock
	persistFlightsMu sync.Mutex
	persistFlights   map[models.JobID]*jobPersistFlight
	movieLocks       *keyedMutexRegistry // POSTER-WRITE-HARDENING D15: process-wide family lock registry shared by every job
	// codex r38 P2: actress-row keys live on a DISJOINT registry — a movie
	// rekeyed to a colliding ID like "actress:123" must never share a mutex
	// with the actress-rename leg (same-registry re-lock deadlocks).
	actressLocks      *keyedMutexRegistry
	tombstones        *tombstoneRegistry // POSTER-WRITE-HARDENING D3: deleted-job 410 registry
	editTx            EditTransactor     // POSTER-WRITE-HARDENING D4: composite tx seam (nil ⇒ legacy best-effort persists)
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

// WithEditTransactor wires the composite SQLite transaction seam used by
// review-edit commits (POSTER-WRITE-HARDENING D4): movie-row writes, actress
// renames, and the job envelope upsert land in ONE transaction. Typically
// satisfied by *database.DB (its WithEditTx method builds tx-scoped repos).
func WithEditTransactor(tx EditTransactor) JobStoreOption {
	return func(s *JobStore) {
		s.editTx = tx
	}
}

// WithActressRepo sets the actress repository used to persist explicit actress
// name edits made on the review page (rename by primary key). When unset, the
// edit path skips the overwrite and behaves as before (edits are discarded by
// the shared upserter's fill-missing merge).
func WithActressRepo(r database.ActressRepositoryInterface) JobStoreOption {
	return func(s *JobStore) {
		s.actressRepo = r
	}
}

// WithHistoryRepo sets the history repository used to record operation
// history during scrape and organize. When unset, history writes are skipped.
func WithHistoryRepo(r database.HistoryRepositoryInterface) JobStoreOption {
	return func(s *JobStore) {
		s.historyRepo = r
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
		jobs:           make(map[models.JobID]*BatchJob),
		persistence:    noopJobPersistence{},
		envLocks:       newKeyedMutexRegistry(),
		persistFlights: make(map[models.JobID]*jobPersistFlight),
		movieLocks:     newKeyedMutexRegistry(),
		actressLocks:   newKeyedMutexRegistry(),
		tombstones:     newTombstoneRegistry(0),
		tempCleaner:    NewTempDirCleaner(nil, "", nil),
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
		envLocks:       newKeyedMutexRegistry(),
		persistFlights: make(map[models.JobID]*jobPersistFlight),
		movieLocks:     newKeyedMutexRegistry(),
		actressLocks:   newKeyedMutexRegistry(),
		tombstones:     newTombstoneRegistry(0),
		tempDir:        tempDir,
		templateEngine: engine,
		fs:             filesystem,
		tempCleaner:    nil, // built below with the admission probe attached
	}

	s.tempCleaner = NewTempDirCleaner(filesystem, tempDir, jobRepo, WithAdmissionProbe(s.admissionBusy))

	// Apply options, which may override the default persistence.
	for _, opt := range opts {
		opt(s)
	}

	// codex r41 P2: reconcile rekey witnesses SYNCHRONOUSLY before job
	// reconstruction — ClearMissingTempPosters runs inside reconstruction
	// and would clear the old crop URL while the relocated bytes still sit
	// at the new ID. The periodic stale-cleanup goroutine is not started by
	// the production bootstrap, so this must not ride on
	// StartStaleTempCleanup.
	if n, err := s.tempCleaner.ReconcileRekeyWitnesses(context.Background()); err != nil {
		logging.Warnf("rekey witness reconciliation failed at startup: %v", err)
	} else if n > 0 {
		logging.Infof("reversed %d orphaned poster rekey relocation(s) at startup", n)
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
		if s.historyRepo != nil {
			job.deps.HistoryRepo = s.historyRepo
		}
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
			s.tombstones.Unmark(batchJob.ID.String()) // live row wins (codex r36)
			s.bindPersistFlight(batchJob)
			s.jobs[batchJob.ID] = batchJob
		}
	}

	s.recoverOrphanedJobs()
}

// recoverOrphanedJobs marks jobs stuck in 'running' or 'pending' status as
// 'failed' on startup. When the server restarts after a crash or kill, these
// jobs have no live worker goroutine — they are orphaned zombies that can
// neither be cancelled (no worker to process the context cancellation) nor
// deleted (DeleteJob refuses running jobs). Marking them failed restores
// normal Cancel/Delete functionality and gives the user a clear signal.
// attachEditDeps wires POSTER-WRITE-HARDENING edit-path dependencies onto a
// job (composite tx seam + candidate envelope builder + persistence
// fallback). Idempotent: safe to call after the editor is rebuilt.
func (s *JobStore) attachEditDeps(job *BatchJob) {
	if job == nil || job.posterEditor == nil {
		return
	}
	var committer *EditCommitter
	if s.editTx != nil {
		committer = NewEditCommitter(s.editTx, s.envLocks, job.ID.String(), s.actressLocks)
	}
	job.posterEditor.setLockRegistry(s.movieLocks)
	job.posterEditor.attachEnv(&posterEditEnv{
		committer: committer,
		jobID:     job.ID.String(),
		tempDir:   job.GetTempDir(),
		fs:        job.fs,
		// Per-job override wins (a JobConfig-supplied ActressRepo outranks the
		// store-level repo) so job-bound actress edits are never dropped in
		// the legacy fallback path (codex r16).
		actressRepo: func() database.ActressRepositoryInterface {
			if job.deps.ActressRepo != nil {
				return job.deps.ActressRepo
			}
			return s.actressRepo
		}(),
		envelope: func(overrides map[string]*resultstore.MovieResult, provOverrides map[string]*resultstore.ProvenanceData, excluded map[string]bool) (*models.Job, error) {
			return s.candidateEnvelope(job, overrides, provOverrides, excluded)
		},
		generationCommitted: func(generation uint64) {
			job.mu.Lock()
			if generation > job.envelopeGeneration {
				job.envelopeGeneration = generation
			}
			job.mu.Unlock()
		},
		persistFn: func() error { return s.PersistJobByID(job.ID.String()) },
		lifecycle: job.lifecycle,
	})
}

// SyncEnvelopeGeneration updates an already-loaded live job after a repository
// metadata mutation (for example, an API revert) advances the durable generation.
// It never lowers an in-memory generation, so a concurrent accepted envelope
// remains the stronger fence. The optional method is consumed by API handlers
// without widening JobStoreInterface or legacy test doubles.
func (s *JobStore) SyncEnvelopeGeneration(id string, generation uint64) bool {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok || job == nil {
		return false
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if generation < job.envelopeGeneration {
		return false
	}
	job.envelopeGeneration = generation
	return true
}

// IsTombstoned reports whether the job was explicitly deleted recently.
func (s *JobStore) IsTombstoned(id string) bool { return s.tombstones.Contains(id) }

// JobGone distinguishes 410-gone (explicitly deleted) from 404-unknown for
// edit admission (POSTER-WRITE-HARDENING D3).
func (s *JobStore) JobGone(id string) bool {
	if s.tombstones.Contains(id) {
		return true
	}
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	return ok && job.admission.IsGone()
}

// acquireAdmission is the shared spine of the admission gates: resolve the
// job (410-gone via tombstone / 404-unknown), take a shared lease (delete
// drain), then apply the caller's lifecycle gate. The returned lease MUST be
// released by the caller (D1/D16).
func (s *JobStore) acquireAdmission(id string, gate func(status models.JobStatus, phase string) error) (BatchJobInterface, func(), error) {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok {
		if s.tombstones.Contains(id) {
			return nil, nil, fmt.Errorf("%w: %s", ErrJobGone, id)
		}
		return nil, nil, fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	release, err := job.admission.AdmitShared()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrJobGone, id)
	}
	// A job flagged deleted in its lifecycle is gone regardless of tombstone
	// state — test fixtures and direct SetDeleted paths bypass the store's
	// delete protocol but mean the same thing semantically.
	if job.lifecycle.IsDeleted() {
		release()
		return nil, nil, fmt.Errorf("%w: %s", ErrJobGone, id)
	}
	if gate != nil {
		if err := gate(job.lifecycle.GetJobStatus(), job.lifecycle.CurrentPhase()); err != nil {
			release()
			return nil, nil, err
		}
	}
	jobAdapter, _ := s.GetBatchJob(id)
	return jobAdapter, release, nil
}

// AcquireEditAccess admits review-edit operations (PATCH/crop/poster-from-URL/
// field-override, D16): 409 while Pending or Running-with-scrape-phase; Running
// with an unpersisted/unknown phase marker conservatively rejects. Completed,
// Organized, Failed, and Running-with-apply accept.
func (s *JobStore) AcquireEditAccess(id string) (BatchJobInterface, func(), error) {
	return s.acquireAdmission(id, func(status models.JobStatus, phase string) error {
		return editAdmissionError(id, status, phase)
	})
}

// AcquireRescrapeAccess admits rescrape operations ATOMICALLY (codex P1-A):
// job resolution, tombstone check, admission lease, and the lifecycle gate
// all happen under one acquisition, so a concurrent StartScrape/StartApply
// can never slip a Running transition past the status check. Rescrape is
// admitted for Pending and Completed (legacy set) plus Running-with-apply;
// rejected while Running-with-scrape/unknown-phase, or in terminal failure
// states.
//
// Phase starts use TryBeginPhase, which fails busy while any shared lease
// (including this rescrape's) is held — the admission is mutually exclusive
// in both directions.
func (s *JobStore) AcquireRescrapeAccess(id string) (ControlledJob, func(), error) {
	job, release, err := s.acquireAdmission(id, func(status models.JobStatus, phase string) error {
		switch status {
		case models.JobStatusPending, models.JobStatusCompleted:
			return nil
		default:
			// Running (any phase) rejects rescrape in Phase 1: apply-phase
			// rescrape admission is deferred to Phase 2's merged write-back
			// machinery (D5) — without it the apply worker's unconditional
			// per-file write-back can clobber the rescrape's commit
			// (codex P4-C).
			return &EditPhaseBusyError{JobID: id, Status: status, Phase: phase}
		}
	})
	if err != nil {
		return nil, nil, err
	}
	return job, release, nil
}

// AcquireSharedLease takes a gone-checked shared admission lease without a
// lifecycle gate (phases and long-running operations like rescrape use this
// so DeleteJob's exclusive drain cannot reclaim a job mid-operation, D3).
// The returned release MUST run when the operation completes.
func (s *JobStore) AcquireSharedLease(id string) (func(), error) {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok {
		if s.tombstones.Contains(id) {
			return nil, fmt.Errorf("%w: %s", ErrJobGone, id)
		}
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	release, err := job.admission.AdmitShared()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrJobGone, id)
	}
	return release, nil
}

// AcquireExclusionAccess admits exclude operations (D16): 410/404 as above,
// 409 while ANY phase is Running (both currentPhase values). Pending
// exclusions stay admitted so the legacy all-excluded auto-cancel flow keeps
// working.
func (s *JobStore) AcquireExclusionAccess(id string) (BatchJobInterface, func(), error) {
	return s.acquireAdmission(id, func(status models.JobStatus, phase string) error {
		if status == models.JobStatusRunning {
			return &EditPhaseBusyError{JobID: id, Status: status, Phase: phase}
		}
		return nil
	})
}

func (s *JobStore) recoverOrphanedJobs() {
	recovered := 0
	for id, job := range s.jobs {
		job.lifecycle.mu.Lock()
		status := job.lifecycle.Status
		if status != models.JobStatusRunning && status != models.JobStatusPending {
			job.lifecycle.mu.Unlock()
			continue
		}
		job.lifecycle.mu.Unlock()

		job.lifecycle.MarkFailed()
		// audit F4: this row's phase goroutine died with the old process — the
		// marker would otherwise persist on a terminal row and 409 edits
		// forever. Clear it BEFORE persisting the recovered state.
		job.lifecycle.SetCurrentPhase("")
		if err := s.persistence.PersistJob(job); err != nil {
			logging.Warnf("Failed to persist recovered job %s: %v", id, err)
		}
		recovered++
		logging.Warnf("Recovered orphaned job %s (was %s, marked failed)", id, status)
	}
	if recovered > 0 {
		logging.Warnf("Recovered %d orphaned job(s) on startup", recovered)
	}
}

// jobAdapters holds pre-built adapter components for a BatchJob.
// Constructed once by buildAdapters and consumed by the public JobStore
// methods (CreateJob, GetJobForEdit, GetJobForControl) to eliminate
// duplicated closure wiring.
type jobAdapters struct {
	reader          JobReader
	movieLookup     resultstore.MovieLookup
	phaseController PhaseController
	canceller       JobCanceller
	editor          JobEditor
}

// buildAdapters constructs all adapter components for a BatchJob.
// Each public method assembles its return value from a subset of these
// components, avoiding the ~20 lines of duplicated closure wiring that
// previously appeared in CreateJob, GetJobForEdit, and GetJobForControl.
// Per the result-store extraction: job.results is a resultstore.Store, which
// satisfies MovieLookup, so movieLookup is wired directly from job.results.
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
		movieLookup: job.results,
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
			store:        job.results,
			lifecycle:    job.lifecycle,
			posterEditor: job.posterEditor,
			movieRepo:    job.deps.MovieRepo,
			actressRepo:  job.deps.ActressRepo,
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
		job.fsCaseCache = fscase.NewFSCaseCache(s.fs)
	}
	if s.templateEngine != nil {
		job.templateEngine = s.templateEngine
	}

	// Swap in the store-scoped movie repo on the SAME editor instance —
	// replacing the editor would orphan the keyed-lock registry + env (D13).
	if s.movieRepo != nil {
		job.posterEditor.setMovieRepo(s.movieRepo)
	}

	// Set persistFn after job is constructed so the closure captures the correct pointer
	// Route phase persists through the envelope-locked, tombstone-aware store
	// entry point (D2: both persist entry points hold the section).
	job.deps.PersistFn = func() error { return s.PersistJob(job) }

	// Fallback: if JobConfig didn't provide these repos, use JobStore's
	if job.deps.BatchFileOpRepo == nil {
		job.deps.BatchFileOpRepo = s.batchFileOpRepo
	}
	if job.deps.MovieRepo == nil {
		job.deps.MovieRepo = s.movieRepo
	}
	if job.deps.ActressRepo == nil {
		job.deps.ActressRepo = s.actressRepo
	}
	if job.deps.HistoryRepo == nil {
		job.deps.HistoryRepo = s.historyRepo
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()
	// Clear any stale tombstone when an explicit-ID job is recreated
	// (deleted-job-recreation window codex r36). Bind its own flight after
	// replacing any sealed coordinator left by the deleted instance.
	s.tombstones.Unmark(job.ID.String())
	s.bindPersistFlight(job)
	s.attachEditDeps(job)

	if err := s.persistence.PersistJob(job); err != nil {
		logging.Warnf("Failed to persist new job %s: %v", job.ID.String(), err)
	}

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
// returns an editableJobAdapter composing jobReaderImpl +
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
// returns a controlledJobAdapter composing jobReaderImpl +
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
// DeleteJob implements the POSTER-WRITE-HARDENING D3 delete protocol:
//
//  1. Acquire the job's exclusive admission lease — in-flight edits hold
//     shared leases, so this drains them (an edit either fully commits or
//     fails cleanly before we proceed; never half-applied against
//     unregistered state). Phases also hold shared leases, so a Running job's
//     lease would block here — the status check below rejects Running first.
//  2. DB row delete FIRST, inside the per-job envelope lock, so a concurrent
//     persist either lands before the delete (delete wins) or after
//     (tombstone blocks the upsert — no resurrection).
//  3. Only on DB success: register the tombstone, mark the barrier gone,
//     remove from the map, cancel/markDeleted, clean temps.
//
// Any DB failure leaves the job fully usable and unchanged.
func (s *JobStore) DeleteJob(id string) error {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok {
		if s.tombstones.Contains(id) {
			return fmt.Errorf("%w: %s", ErrJobGone, id)
		}
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}

	// Fast-fail Running BEFORE taking the exclusive lease: the phase goroutine
	// holds a shared lease through its final persist, so AdmitExclusive would
	// stall for the entire phase (blocking every edit admission for that job)
	// instead of returning "cannot delete running job" promptly. Still
	// re-checked under the exclusive lease below — a phase may transition
	// between the two.
	preSnap := job.lifecycle.StatusSnapshot()
	if preSnap.Status == models.JobStatusRunning {
		return fmt.Errorf("cannot delete running job")
	}

	// Deadline-bounded exclusive acquisition (codex r13 delete-phase race):
	// re-converge instead of parking: a Running holder fails fast; edit /
	// rescrape holders drain right away on TryLock; a phase STARTING mid-wait
	// flips the lifecycle and is caught on the next probe (never parks behind
	// a phase).
	// Writer-preference delete loop (codex r14-A): register delete intent
	// BEFORE polling so new shared admissions park during the drain —
	// sibling edits already in flight complete in milliseconds; Running
	// transitions still fast-fail through the re-check below.
	job.admission.EnterExclusiveWait()
	var release func()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if rel, ok := job.admission.TryAdmitExclusive(); ok {
			job.admission.CancelExclusiveWait()
			release = rel
			break
		}
		if rel, ok := job.admission.PollExclusiveWait(); ok {
			release = rel
			break
		}
		if job.lifecycle.GetJobStatus() == models.JobStatusRunning {
			job.admission.CancelExclusiveWait()
			return fmt.Errorf("cannot delete running job")
		}
		if job.lifecycle.IsDeleted() || job.admission.IsGone() {
			job.admission.CancelExclusiveWait()
			return fmt.Errorf("%w: %s", ErrJobGone, id)
		}
		if time.Now().After(deadline) {
			job.admission.CancelExclusiveWait()
			return fmt.Errorf("delete timed out waiting for an in-flight operation on job %s", id)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if job.admission.IsGone() || job.lifecycle.IsDeleted() {
		release()
		return fmt.Errorf("%w: %s", ErrJobGone, id)
	}
	defer release()

	// Fence the per-job persistence flight before taking the envelope lock:
	// active/coalesced persists drain first, and new requests cannot start
	// while the row delete is being committed. Failed deletes reopen the
	// flight; successful deletes leave the tombstone fence closed.
	persistFlight := s.persistFlightForJob(job)
	flightRelease, flightErr := persistFlight.acquireExclusive(context.Background())
	if flightErr != nil {
		return flightErr
	}
	keepFlightClosed := false
	defer func() {
		if !keepFlightClosed {
			flightRelease()
		}
	}()

	lcSnap := job.lifecycle.StatusSnapshot()
	if lcSnap.IsDeleted {
		return fmt.Errorf("%w: %s", ErrJobGone, id)
	}
	if lcSnap.Status == models.JobStatusRunning {
		return fmt.Errorf("cannot delete running job")
	}

	// Envelope-locked: [row delete + tombstone] serialize against persists.
	envRelease := s.envLocks.Acquire(id)
	if err := s.persistence.DeleteJobFromDB(id); err != nil {
		envRelease()
		return err
	}
	s.tombstones.Mark(id)
	persistFlight.sealExclusive(ErrJobGone)
	keepFlightClosed = true
	// The tombstone is now the durable short-lived gone fence. Remove the
	// ID-map entry to avoid retaining one sealed flight per deleted job; old
	// BatchJob pointers retain their sealed instance flight, while recreated
	// jobs bind a distinct coordinator.
	s.resetPersistFlight(job.ID)
	envRelease()

	// Row is gone: fence the job, then remove + cancel + clean up.
	job.admission.MarkGone()
	s.mu.Lock()
	delete(s.jobs, models.JobID(id))
	s.mu.Unlock()

	if lcSnap.Status == models.JobStatusPending {
		job.lifecycle.Cancel()
	}
	job.lifecycle.markDeleted()

	// Wait only for jobs that were cancelled from Pending here — terminal or
	// never-started jobs have no worker goroutine to join (their done channel
	// stays open, so an unconditional wait would stall 5s per delete).
	if lcSnap.Status == models.JobStatusPending {
		select {
		case <-job.lifecycle.done:
		case <-time.After(5 * time.Second):
			logging.Warnf("DeleteJob: timed out waiting for job %s to finish, proceeding with cleanup", id)
		}
	}

	s.getTempCleaner().CleanJobTempDir(id)

	return nil
}

// persistFlightFor returns the per-job persistence coordinator, lazily
// initializing the map for direct test-constructed JobStores as well as the
// normal constructors. The map lifetime follows the store lifetime.
func (s *JobStore) persistFlightFor(id models.JobID) *jobPersistFlight {
	s.persistFlightsMu.Lock()
	defer s.persistFlightsMu.Unlock()
	if s.persistFlights == nil {
		s.persistFlights = make(map[models.JobID]*jobPersistFlight)
	}
	if flight := s.persistFlights[id]; flight != nil {
		return flight
	}
	flight := newJobPersistFlight()
	s.persistFlights[id] = flight
	return flight
}

// persistFlightForJob returns the coordinator bound to this BatchJob instance.
// The lazy fallback keeps direct test-constructed jobs safe; normal jobs receive
// their own flight at construction. This identity binding prevents a deleted
// pointer from joining a newly-created job that reuses the same ID.
func (s *JobStore) persistFlightForJob(job *BatchJob) *jobPersistFlight {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.persistFlight == nil {
		job.persistFlight = s.persistFlightFor(job.ID)
	}
	return job.persistFlight
}

// bindPersistFlight makes the compatibility ID map point at the instance
// flight. The BatchJob-owned pointer remains authoritative for persistence,
// so resetting this map cannot make an old pointer join a recreated job.
func (s *JobStore) bindPersistFlight(job *BatchJob) {
	flight := s.persistFlightForJob(job)
	s.persistFlightsMu.Lock()
	if s.persistFlights == nil {
		s.persistFlights = make(map[models.JobID]*jobPersistFlight)
	}
	s.persistFlights[job.ID] = flight
	s.persistFlightsMu.Unlock()
}

func (s *JobStore) resetPersistFlight(id models.JobID) {
	s.persistFlightsMu.Lock()
	delete(s.persistFlights, id)
	s.persistFlightsMu.Unlock()
}

// PersistJob saves a job to the database.
// this is the public persistence method. The former PersistManagedJob
// is removed because it type-asserted to *BatchJob internally — callers that hold
// a composite should use PersistJobByID instead.
// PersistJob saves a job to the database under the per-job envelope lock
// (POSTER-WRITE-HARDENING D2): snapshot + upsert serialize, so the last
// committed envelope can never regress a concurrently committed edit. A
// tombstoned/deleted job row refuses the upsert so an in-flight persist
// racing DeleteJob cannot resurrect the row (D3).
func (s *JobStore) PersistJob(job *BatchJob) error {
	return s.persistFlightForJob(job).do(context.Background(), func() error {
		release := s.envLocks.Acquire(job.ID.String())
		defer release()
		if s.tombstones.Contains(job.ID.String()) || job.Lifecycle().IsDeleted() {
			return fmt.Errorf("%w: %s", ErrJobGone, job.ID.String())
		}
		// The callback snapshots only after acquiring the existing envelope lock;
		// a coalesced follow-up therefore sees the latest completed mutation.
		return s.persistence.PersistJob(job)
	})
}

// PersistJobByID persists a job by its ID.
// callers that hold a composite (EditableJob, ControlledJob)
// use this instead of PersistJob — no type assertion needed. The store holds
// the concrete *BatchJob internally. No-op if the job is not found.
func (s *JobStore) PersistJobByID(id string) error {
	s.mu.RLock()
	job, ok := s.jobs[models.JobID(id)]
	s.mu.RUnlock()
	if !ok {
		if s.tombstones.Contains(id) {
			return fmt.Errorf("%w: %s", ErrJobGone, id)
		}
		return fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return s.PersistJob(job)
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
			s.tempCleaner = NewTempDirCleaner(s.fs, s.tempDir, s.jobRepo, WithAdmissionProbe(s.admissionBusy))
		} else if s.tempCleaner.admissionProbe == nil {
			// Eagerly-constructed cleaners (NewJobStore) must carry the same
			// lease protection — attaching here covers both paths (codex P3 R2-2).
			s.tempCleaner.admissionProbe = s.admissionBusy
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
