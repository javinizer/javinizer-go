package worker

import (
	"encoding/json"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// DataTypeMovie identifies that a result's Data field contains a Movie.
// Kept for backward compatibility during DB reconstruction of legacy FileResult JSON.
const DataTypeMovie = "movie"

// JobResultsEnvelope wraps domain results and provenance data for persistence
// per ADR-0027. The Results text column stores this envelope instead of
// a raw map[string]*MovieResult.
type JobResultsEnvelope struct {
	Domain     map[string]*MovieResult    `json:"domain"`
	Provenance map[string]*ProvenanceData `json:"provenance,omitempty"`
}

// Legacy format parsing has been centralized in ParseJobResultsJSON
// (internal/worker/result_parse.go). The legacyFileResult type and
// convertLegacyFileResult function have been removed from this file.

// reconstructResultTracker builds a ResultTracker from a database Job model,
// deserializing Files, Results, Excluded, and FileMatchInfo JSON fields.
// Per P-2: extracted from reconstructBatchJob so that deserialization and tracker
// construction are a single responsibility, independent of BatchJob wiring.
func (s *JobStore) reconstructResultTracker(dbJob *models.Job) *ResultTracker {
	rt := NewResultTracker(dbJob.TotalFiles, nil)
	rt.Completed = dbJob.Completed
	rt.Failed = dbJob.Failed
	rt.Progress = dbJob.Progress
	rt.Provenance = make(map[string]*ProvenanceData)

	// Parse Files JSON
	if dbJob.Files != "" {
		if err := json.Unmarshal([]byte(dbJob.Files), &rt.Files); err != nil {
			logging.Warnf("Failed to parse files for job %s: %v", dbJob.ID, err)
			s.deserializeErrors.Add(1)
		}
	}

	// Parse Results JSON — uses the shared ParseJobResultsJSON function
	// that handles all three persistence formats (envelope, legacy, old MovieResult).
	if dbJob.Results != "" {
		raw := []byte(dbJob.Results)
		parsed, err := ParseJobResultsJSON(raw)
		if err != nil {
			logging.Warnf("Failed to parse results for job %s: %v", dbJob.ID, err)
			s.deserializeErrors.Add(1)
		} else {
			rt.Results = parsed.Results
			rt.Provenance = parsed.Provenance
		}
	}

	// Parse Excluded JSON
	if dbJob.Excluded != "" {
		if err := json.Unmarshal([]byte(dbJob.Excluded), &rt.Excluded); err != nil {
			logging.Warnf("Failed to parse excluded for job %s: %v", dbJob.ID, err)
			s.deserializeErrors.Add(1)
		}
	}

	// Parse models.FileMatchInfo JSON
	if dbJob.FileMatchInfo != "" {
		if err := json.Unmarshal([]byte(dbJob.FileMatchInfo), &rt.FileMatchInfo); err != nil {
			logging.Warnf("Failed to parse file match info for job %s: %v", dbJob.ID, err)
			s.deserializeErrors.Add(1)
		}
	}

	// Rebuild movieID secondary index after bulk-loading results from DB.
	// The index is not persisted — it is reconstructed from the Results map.
	rt.rebuildMovieIDIndexLocked()

	return rt
}

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
func wireJobDeps(job *BatchJob, movieRepo database.MovieRepositoryInterface, persistFn func()) {
	job.attachLifecycleCallback()
	job.posterEditor = NewPosterEditor(job.resultIndex, job.results, movieRepo)
	job.controller = newJobController(job)
	if movieRepo != nil {
		job.deps.MovieRepo = movieRepo
	}
	if persistFn != nil {
		job.deps.PersistFn = persistFn
	}
}

// reconstructBatchJob reconstructs a BatchJob from a database Job model.
// It handles both the new MovieResult format and the legacy FileResult format
// for backward compatibility with existing database records.
// Per P-2: decomposed into reconstructResultTracker (deserialization + tracker
// construction) → BatchJob construction → wireJobDeps (shared wiring) →
// DB-specific finalization (operation mode, temp poster validation, done channel).
func (s *JobStore) reconstructBatchJob(dbJob *models.Job) *BatchJob {
	jobID, err := models.ParseJobID(dbJob.ID)
	if err != nil {
		logging.Errorf("reconstructBatchJob: invalid job ID %q from DB: %v", dbJob.ID, err)
		jobID = models.MustJobID(fmt.Sprintf("recovered-%s", dbJob.ID))
	}

	// Step 1: Deserialize and construct tracker
	tracker := s.reconstructResultTracker(dbJob)

	// Step 2: Construct BatchJob
	batchJob := &BatchJob{
		ID:        jobID,
		StartedAt: dbJob.StartedAt,
		lifecycle: &JobLifecycle{
			Status:      dbJob.Status,
			CompletedAt: dbJob.CompletedAt,
			OrganizedAt: dbJob.OrganizedAt,
			RevertedAt:  dbJob.RevertedAt,
			done:        make(chan struct{}),
		},
		results: tracker,
		cfg: jobConfig{
			destination: dbJob.Destination,
			tempDir:     dbJob.TempDir,
			update:      dbJob.Update,
		},
		fs:                  s.fs,
		batchJobEventSource: newBatchJobEventSource(),
		rescrapePhase:       NewRescrapePhase(),
		scrapePhase:         NewScrapePhase(),
		applyPhase:          NewApplyPhase(),
		fsCaseCache:         NewFSCaseCache(s.fs),
	}

	// Step 3: Wire shared dependencies
	wireJobDeps(batchJob, s.movieRepo, func() { s.persistence.PersistJob(batchJob) })

	// Step 3b: Restore infrastructure deps that are not persisted in the DB
	// but are required for apply/rescrape phases. These are set on the JobStore
	// via SetReconstructionDeps after the BatchJobFactory is built. They may
	// be nil if reconstruction runs before SetReconstructionDeps is called
	// (loadFromDatabase at startup) — SetReconstructionDeps re-hydrates them.
	batchJob.mu.Lock()
	if s.reconMatcher != nil {
		batchJob.deps.Matcher = s.reconMatcher
	}
	if s.reconPosterGen != nil {
		batchJob.deps.PosterGen = s.reconPosterGen
	}
	// BatchCfg is a value type — only overwrite if non-zero to avoid writing
	// a zero-value config before SetReconstructionDeps has been called.
	// SetReconstructionDeps always overwrites BatchCfg unconditionally.
	if s.reconBatchCfg.MaxWorkers > 0 || s.reconBatchCfg.WorkerTimeout > 0 || len(s.reconBatchCfg.ScraperPriority) > 0 || s.reconBatchCfg.NFOEnabled {
		batchJob.deps.BatchCfg = s.reconBatchCfg
	}
	batchJob.mu.Unlock()

	// Inline setOperationModeFromDB: DB reconstruction must not fail on corrupted data.
	if dbJob.OperationModeOverride != "" && !dbJob.OperationModeOverride.IsValid() {
		logging.Warnf("setOperationModeFromDB: invalid DB mode %q, leaving operationMode empty", dbJob.OperationModeOverride)
	} else {
		mode := dbJob.OperationModeOverride
		if mode == "" {
			mode = operationmode.OperationModeOrganize
		}
		batchJob.mu.Lock()
		batchJob.cfg.operationMode = mode
		batchJob.mu.Unlock()
	}

	// Drop cropped_poster_url values whose temp artifact is missing on disk
	// so the detail view stays consistent with the list view (see
	// ClearMissingTempPosters). This clears URLs only; it never deletes files.
	// Safe to mutate results unlocked: the job is not yet registered in s.jobs,
	// so no concurrent reader can observe it (see loadFromDatabase / NewJobStore).
	ClearMissingTempPosters(s.fs, batchJob.cfg.tempDir, dbJob.ID, batchJob.results.Results)

	// Close Done channel for all states — reconstructed jobs are snapshots
	// from the database and should not block Wait() callers.
	// Use select-guard to prevent double-close panic if Done is already closed.
	select {
	case <-batchJob.lifecycle.done:
		// Already closed — safe
	default:
		close(batchJob.lifecycle.done)
	}

	return batchJob
}

// snapshotForPersist acquires all 3 sub-manager RLocks simultaneously in
// documented order and returns a *models.Job populated from a consistent
// point-in-time snapshot. Returns (nil, false) if the job is deleted or
// if any JSON marshal fails.
//
// Lock ordering: lifecycle.mu → results.mu → job.mu (never reverse).
// All 3 are read-locks so holding them simultaneously does not block
// other readers. Writers (e.g., MarkCompleted) block until all 3 are released.
func snapshotForPersist(job *BatchJob) (*models.Job, bool) {
	snapshot := job.snapshotFull()
	if snapshot.IsDeleted {
		logging.Debugf("[Job %s] Skipping persist - job marked as deleted", snapshot.ID)
		return nil, false
	}

	filesJSON, err := json.Marshal(snapshot.Files)
	if err != nil {
		logging.Warnf("Failed to marshal files for job %s: %v", snapshot.ID, err)
		return nil, false
	}

	envelope := JobResultsEnvelope{
		Domain:     snapshot.results,
		Provenance: snapshot.provenance,
	}
	resultsJSON, err := json.Marshal(envelope)
	if err != nil {
		logging.Warnf("Failed to marshal results for job %s: %v", snapshot.ID, err)
		return nil, false
	}

	excludedJSON, err := json.Marshal(snapshot.Excluded)
	if err != nil {
		logging.Warnf("Failed to marshal excluded for job %s: %v", snapshot.ID, err)
		return nil, false
	}

	fileMatchInfoJSON, err := json.Marshal(snapshot.FileMatchInfo)
	if err != nil {
		logging.Warnf("Failed to marshal file match info for job %s: %v", snapshot.ID, err)
		return nil, false
	}

	return &models.Job{
		ID:                    snapshot.ID.String(),
		Status:                snapshot.Status,
		TotalFiles:            snapshot.TotalFiles,
		Completed:             snapshot.Completed,
		Failed:                snapshot.Failed,
		Progress:              snapshot.Progress,
		Destination:           snapshot.Destination,
		TempDir:               snapshot.TempDir,
		OperationModeOverride: snapshot.OperationModeOverride,
		Files:                 string(filesJSON),
		Results:               string(resultsJSON),
		Excluded:              string(excludedJSON),
		FileMatchInfo:         string(fileMatchInfoJSON),
		StartedAt:             snapshot.StartedAt,
		CompletedAt:           snapshot.CompletedAt,
		OrganizedAt:           snapshot.OrganizedAt,
		RevertedAt:            snapshot.RevertedAt,
		Update:                snapshot.Update,
	}, true
}
