package worker

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/applyplan"
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
func wireJobDeps(job *BatchJob, movieRepo database.MovieRepositoryInterface, actressRepo database.ActressRepositoryInterface, historyRepo database.HistoryRepositoryInterface, persistFn func() error) {
	job.attachLifecycleCallback()
	if job.editLocks == nil {
		job.editLocks = newKeyedMutexRegistry()
	}
	if job.admission == nil {
		job.admission = newAdmissionBarrier()
	}
	job.posterEditor = NewPosterEditor(job.results, job.results, movieRepo)
	job.controller = newJobController(job)
	if movieRepo != nil {
		job.deps.MovieRepo = movieRepo
	}
	if actressRepo != nil {
		job.deps.ActressRepo = actressRepo
	}
	if historyRepo != nil {
		job.deps.HistoryRepo = historyRepo
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
		ID:                 jobID,
		pruneVersion:       snapshot.PruneVersion,
		envelopeGeneration: snapshot.EnvelopeGeneration,
		StartedAt:          snapshot.StartedAt,
		persistFlight:      newJobPersistFlight(),
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
			applyPlan:   applyplan.Clone(snapshot.ApplyPlan),
		},
		fs:                  s.fs,
		batchJobEventSource: newBatchJobEventSource(),
		rescrapePhase:       NewRescrapePhase(),
		scrapePhase:         NewScrapePhase(),
		applyPhase:          NewApplyPhase(),
		fsCaseCache:         fscase.NewFSCaseCache(s.fs),
	}

	wireJobDeps(batchJob, s.movieRepo, s.actressRepo, s.historyRepo, func() error { return s.PersistJob(batchJob) })
	// Restore the durable phase marker (D16): legacy envelopes predating the
	// marker decode "" and read conservatively-busy for edits on Running rows.
	batchJob.lifecycle.currentPhase = snapshot.CurrentPhase
	// audit F4: a TERMINAL row with a marker is poison across restarts — no
	// phase goroutine survives the process boundary to clear it, so every
	// future edit 409s forever (EditPhaseBusyError). Clear + re-persist once.
	if snapshot.CurrentPhase != "" {
		switch snapshot.Status {
		case models.JobStatusCompleted, models.JobStatusOrganized, models.JobStatusFailed, models.JobStatusCancelled, models.JobStatusReverted:
			batchJob.lifecycle.currentPhase = ""
			if s.persistence != nil {
				if perr := s.persistence.PersistJob(batchJob); perr != nil {
					logging.Warnf("reconstructBatchJob: clearing stale phase marker for %s failed: %v", jobID, perr)
				}
			}
		}
	}

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

	// D13: reconstructed jobs get the same edit seams as fresh jobs — without
	// this the composite-tx path is silently inert after a restart (envelope
	// never persisted on edits, renames dropped) because handlers no longer
	// run post-op PersistJobByID.
	s.attachEditDeps(batchJob)

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
func snapshotForPersist(job *BatchJob) (*models.Job, error) {
	return s_candidateEnvelope(job, nil, nil, nil)
}

// candidateEnvelope builds the encoded DB row for job, substituting candidate
// (unpublished) results/provenance/excluded for the store's live values
// (POSTER-WRITE-HARDENING D1/D4): review-edit transactions commit an envelope
// reflecting the CANDIDATE-MERGED state without first publishing it in
// memory, so a mid-transaction crash can never leave the envelope ahead of or
// behind the movie-row writes. Snapshot maps are clones (SnapshotForStatus),
// so substitution is allocation-only. Both nil-overrides paths collapse to
// the plain snapshot flow.
//
// The envelope lock is held by JobStore callers around this build+upsert, so
// the snapshot is taken inside the locked section per D2.
func (s *JobStore) candidateEnvelope(job *BatchJob, overrides map[string]*resultstore.MovieResult, provOverrides map[string]*resultstore.ProvenanceData, excluded map[string]bool) (*models.Job, error) {
	return s_candidateEnvelope(job, overrides, provOverrides, excluded)
}

func s_candidateEnvelope(job *BatchJob, overrides map[string]*resultstore.MovieResult, provOverrides map[string]*resultstore.ProvenanceData, excluded map[string]bool) (*models.Job, error) {
	snapshot := job.snapshotFull()
	if snapshot.IsDeleted {
		logging.Debugf("[Job %s] Skipping persist - job marked as deleted", snapshot.ID)
		return nil, nil
	}

	if len(overrides) > 0 {
		merged := make(map[string]*resultstore.MovieResult, len(snapshot.results))
		for k, v := range snapshot.results {
			if cand, ok := overrides[k]; ok && cand != nil {
				merged[k] = cand
			} else {
				merged[k] = v
			}
		}
		snapshot.results = merged
		// Rekeyed identity PATCHes must keep the top-level FileMatchInfo map
		// consistent with the candidate's identity (codex P6-A): a stale
		// match entry would otherwise resurrect the old ID after restart.
		for k, cand := range merged {
			if _, was := overrides[k]; !was || cand == nil {
				continue
			}
			if fm, ok := snapshot.FileMatchInfo[k]; ok && fm.MovieID != cand.FileMatchInfo.MovieID {
				fm.MovieID = cand.FileMatchInfo.MovieID
				snapshot.FileMatchInfo[k] = fm
			}
		}
	}
	if len(provOverrides) > 0 {
		mergedProv := make(map[string]*resultstore.ProvenanceData, len(snapshot.provenance))
		for k, v := range snapshot.provenance {
			mergedProv[k] = v
		}
		for k, v := range provOverrides {
			mergedProv[k] = v
		}
		snapshot.provenance = mergedProv
	}
	if excluded != nil {
		snapshot.Excluded = excluded
		// Project MarkExcluded's full effect into the candidate envelope
		// (codex r17): terminal counters recomputed against the substituted
		// exclusion map, in-flight excluded entries flip Running→Cancelled,
		// and progress mirrors stateRecalculateProgress (denominator shrinks
		// by the excluded count, terminal files removed from num/denom).
		completed, failed, excludedCount := 0, 0, 0
		for _, v := range excluded {
			if v {
				excludedCount++
			}
		}
		// Snapshot boundaries (stateCloneResultsLocked) never emit nil-valued
		// rows; no nil guard is needed here.
		for k, r := range snapshot.results {
			if excluded[k] && r.Status == models.JobStatusRunning {
				r.Status = models.JobStatusCancelled // clone — safe to mutate
			}
			if excluded[k] {
				continue
			}
			switch r.Status {
			case models.JobStatusCompleted:
				completed++
			case models.JobStatusFailed:
				failed++
			}
		}
		snapshot.Completed = completed
		snapshot.Failed = failed
		activeTotal := snapshot.TotalFiles - excludedCount
		if activeTotal <= 0 {
			snapshot.Progress = 100
		} else {
			snapshot.Progress = float64(completed+failed) / float64(activeTotal) * 100
		}
	}

	persistSnapshot := jobpersist.Snapshot{
		ID:                    snapshot.ID.String(),
		PruneVersion:          snapshot.pruneVersion,
		EnvelopeGeneration:    snapshot.envelopeGeneration,
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
		ApplyPlan:             applyplan.Clone(snapshot.ApplyPlan),
		StartedAt:             snapshot.StartedAt,
		CompletedAt:           snapshot.CompletedAt,
		OrganizedAt:           snapshot.OrganizedAt,
		RevertedAt:            snapshot.RevertedAt,
		Update:                snapshot.Update,
		CurrentPhase:          snapshot.CurrentPhase,
	}

	dbJob, err := jobpersist.Encode(persistSnapshot)
	if err != nil {
		return nil, fmt.Errorf("encode job %s envelope: %w", snapshot.ID.String(), err)
	}
	return dbJob, nil
}
