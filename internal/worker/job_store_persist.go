package worker

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker/fscase"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// JobResultsEnvelope and the JSON marshal/unmarshal logic now live in the
// internal/worker/jobpersist package (codec.go). Legacy format parsing for the
// Results column lives in jobpersist/result_parse.go (ParseResultsJSON).
// DataTypeMovie (dead code) has been deleted.

// wireJobDeps attaches shared infrastructure to a BatchJob that both
// newBatchJob and reconstructBatchJob require. Per P-2: this eliminates the
// divergence where the two construction paths wired attachLifecycleCallback,
// posterEditor, controller, and PersistFn differently.
//
// movieRepo may be nil (for newBatchJob, the caller sets it later via
// JobStore.createJob; for reconstructed jobs it comes from JobStore).
// When non-nil, it is set on job.deps.MovieRepo so that jobEditorImpl
// (created via getAdapters/buildAdapters) can persist movie edits to the
// database. Without this, reconstructed jobs have nil MovieRepo and
// UpdateMovie() silently skips DB persistence.
func wireJobDeps(job *BatchJob, movieRepo database.MovieRepositoryInterface, actressRepo database.ActressRepositoryInterface, persistFn func()) {
	job.attachLifecycleCallback()
	job.posterEditor = NewPosterEditor(job.results, job.results, movieRepo)
	job.controller = newJobController(job)
	if movieRepo != nil {
		job.deps.MovieRepo = movieRepo
	}
	if actressRepo != nil {
		job.deps.ActressRepo = actressRepo
	}
	if persistFn != nil {
		job.deps.PersistFn = persistFn
	}
}

// reconstructBatchJob reconstructs a BatchJob from a database Job model.
// It calls jobpersist.Decode once to deserialize all JSON columns into a
// Snapshot, logs returned errors (incrementing s.deserializeErrors once per
// error to preserve the prior error metric), then constructs the
// resultstore.Store from the decoded Snapshot and wires the live BatchJob.
// Job ID parsing (malformed ID fallback) stays here as a worker concern —
// Snapshot.ID is a string matching models.Job.ID.
func (s *JobStore) reconstructBatchJob(dbJob *models.Job) *BatchJob {
	snapshot, errs := jobpersist.Decode(dbJob)
	for _, err := range errs {
		logging.Warnf("reconstructBatchJob: %v", err)
		s.deserializeErrors.Add(1)
	}

	jobID, err := models.ParseJobID(snapshot.ID)
	if err != nil {
		logging.Errorf("reconstructBatchJob: invalid job ID %q from DB: %v", snapshot.ID, err)
		jobID = models.MustJobID(fmt.Sprintf("recovered-%s", snapshot.ID))
	}

	tracker := resultstore.NewFromSnapshot(
		snapshot.TotalFiles,
		snapshot.Files,
		snapshot.Results,
		snapshot.Provenance,
		snapshot.FileMatchInfo,
		snapshot.Excluded,
		snapshot.Completed,
		snapshot.Failed,
		snapshot.Progress,
	)

	batchJob := &BatchJob{
		ID:        jobID,
		StartedAt: snapshot.StartedAt,
		lifecycle: &JobLifecycle{
			Status:      snapshot.Status,
			CompletedAt: snapshot.CompletedAt,
			OrganizedAt: snapshot.OrganizedAt,
			RevertedAt:  snapshot.RevertedAt,
			done:        make(chan struct{}),
		},
		results: tracker,
		cfg: jobConfig{
			destination: snapshot.Destination,
			tempDir:     snapshot.TempDir,
			update:      snapshot.Update,
		},
		fs:                  s.fs,
		batchJobEventSource: newBatchJobEventSource(),
		rescrapePhase:       NewRescrapePhase(),
		scrapePhase:         NewScrapePhase(),
		applyPhase:          NewApplyPhase(),
		fsCaseCache:         fscase.NewFSCaseCache(s.fs),
	}

	wireJobDeps(batchJob, s.movieRepo, s.actressRepo, func() { s.persistence.PersistJob(batchJob) })

	batchJob.mu.Lock()
	if s.reconMatcher != nil {
		batchJob.deps.Matcher = s.reconMatcher
	}
	if s.reconPosterGen != nil {
		batchJob.deps.PosterGen = s.reconPosterGen
	}
	if s.reconBatchCfg.MaxWorkers > 0 || s.reconBatchCfg.WorkerTimeout > 0 || len(s.reconBatchCfg.ScraperPriority) > 0 || s.reconBatchCfg.NFOEnabled {
		batchJob.deps.BatchCfg = s.reconBatchCfg
	}
	batchJob.mu.Unlock()

	if snapshot.OperationModeOverride != "" && !snapshot.OperationModeOverride.IsValid() {
		logging.Warnf("setOperationModeFromDB: invalid DB mode %q, leaving operationMode empty", snapshot.OperationModeOverride)
	} else {
		mode := snapshot.OperationModeOverride
		if mode == "" {
			mode = operationmode.OperationModeOrganize
		}
		batchJob.mu.Lock()
		batchJob.cfg.operationMode = mode
		batchJob.mu.Unlock()
	}

	ClearMissingTempPosters(s.fs, batchJob.cfg.tempDir, dbJob.ID, batchJob.results.RawResults())

	select {
	case <-batchJob.lifecycle.done:
	default:
		close(batchJob.lifecycle.done)
	}

	return batchJob
}

// snapshotForPersist delegates to snapshotFull, which takes separate snapshots
// from each sub-manager (lifecycle, results, job) rather than holding all locks
// simultaneously. The result snapshot is from Store.SnapshotForStatus() which
// acquires its own read lock independently. The batchJobSnapshot is converted
// to a jobpersist.Snapshot (dropping worker-only fields PersistError, IsDeleted,
// ResultIndex) and encoded via jobpersist.Encode. Returns (nil, false) if the
// job is deleted or if any JSON marshal fails.
func snapshotForPersist(job *BatchJob) (*models.Job, bool) {
	snapshot := job.snapshotFull()
	if snapshot.IsDeleted {
		logging.Debugf("[Job %s] Skipping persist - job marked as deleted", snapshot.ID)
		return nil, false
	}

	persistSnapshot := jobpersist.Snapshot{
		ID:                    snapshot.ID.String(),
		Status:                snapshot.Status,
		TotalFiles:            snapshot.TotalFiles,
		Completed:             snapshot.Completed,
		Failed:                snapshot.Failed,
		Progress:              snapshot.Progress,
		Files:                 snapshot.Files,
		Results:               snapshot.results,
		Provenance:            snapshot.provenance,
		Excluded:              snapshot.Excluded,
		FileMatchInfo:         snapshot.FileMatchInfo,
		Destination:           snapshot.Destination,
		TempDir:               snapshot.TempDir,
		OperationModeOverride: snapshot.OperationModeOverride,
		StartedAt:             snapshot.StartedAt,
		CompletedAt:           snapshot.CompletedAt,
		OrganizedAt:           snapshot.OrganizedAt,
		RevertedAt:            snapshot.RevertedAt,
		Update:                snapshot.Update,
	}

	dbJob, err := jobpersist.Encode(persistSnapshot)
	if err != nil {
		logging.Warnf("%v", err)
		return nil, false
	}
	return dbJob, true
}
