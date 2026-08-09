package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/progress"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/timeout"
	"github.com/javinizer/javinizer-go/internal/worker/fanout"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ScrapePhase runs the batch scrape step across a set of files.
type ScrapePhase interface {
	Run(ctx context.Context, inputs scrapePhaseInputs, files []string, cfg ScrapePhaseConfig)
}

type scrapePhase struct{}

// NewScrapePhase returns the default ScrapePhase implementation.
func NewScrapePhase() ScrapePhase {
	return &scrapePhase{}
}

// scrapeFileOutcome captures the result of scraping a single file.
// Collected by the errgroup goroutine, then aggregated by trackScrapeResults.
//
// Result carries the un-persisted scrape output (cmd.SkipPersist=true in the
// batch path). The dedicated persist pool reads Result.Movie to persist off
// the per-goroutine critical path. It is nil for the failed/error/panic paths.
type scrapeFileOutcome struct {
	FilePath  string
	MovieID   string
	Success   bool
	Failed    bool // true if scrape failed (not panic)
	Panic     bool // true if goroutine panicked
	Cancelled bool // true if scrape failed due to context.Canceled
	PanicMsg  string
	ErrorMsg  string
	Result    *scrape.ScrapeResult
	Meta      *workflow.OrchestrationMeta
}

// Run executes the scrape phase: setup errgroup → iterate files → dispatch
// scrapeFile → collect outcomes → track results → mark lifecycle.
func (p *scrapePhase) Run(ctx context.Context, inputs scrapePhaseInputs, files []string, cfg ScrapePhaseConfig) {
	defer func() {
		if r := recover(); r != nil {
			panicErr := panicutil.FormatRecover(r)
			logging.Errorf("BatchJob.StartScrape %s %v", inputs.JobID.String(), panicErr)
			inputs.Lifecycle.MarkFailed()
		}
		if !inputs.KeepBroadcasterOpen {
			inputs.Broadcaster.Close()
		}
		if inputs.persister != nil {
			if err := inputs.persister.Persist(); err != nil {
				logging.Warnf("[Scrape] envelope persist failed: %v", err)
			}
		}
	}()

	outcomes := fanout.BoundedFanOut(ctx, inputs.Concurrency.MaxWorkers, files,
		func(egCtx context.Context, filePath string) scrapeFileOutcome {
			// scrapeFile does NOT persist to the database: buildScrapeCmd sets
			// cmd.SkipPersist=true when inputs.MovieRepo is wired, so the workflow's
			// scrape orchestrator skips its inline DB persist (step 4). Persistence
			// runs after all scrape goroutines drain (see persistScrapeOutcomes
			// below) — off the errgroup-gated critical path, so SQLite's
			// single-writer lock never serializes the per-file scrape workers
			// (root cause of the 5→1 worker degradation).
			fmi := inputs.FileMatchInfo[filePath]
			cmd, fromMatcher := buildScrapeCmd(filePath, fmi, inputs, cfg)
			outcome := scrapeFile(egCtx, filePath, fmi, cmd, fromMatcher, inputs, cfg)
			// Broadcast per-file scrape progress over WebSocket so the frontend's
			// messagesByFile populates and ProgressModal shows live per-file status.
			// Mirrors main's realtime.ProgressAdapter which forwarded per-task
			// scrape updates to the WS hub (deleted in this refactor with no
			// replacement — restored here via the hook seam).
			if outcome.Success && cfg.OnFileScraped != nil {
				cfg.OnFileScraped(filePath, outcome.MovieID, fmt.Sprintf("Scraped %s successfully", outcome.MovieID))
			} else if outcome.Failed && cfg.OnFileScrapeFailed != nil {
				cfg.OnFileScrapeFailed(filePath, outcome.MovieID, outcome.ErrorMsg)
			}
			return outcome
		},
	)

	if err := ctx.Err(); err != nil {
		trackScrapeResults(inputs, outcomes, nil)
		inputs.Lifecycle.MarkCancelled()
		return
	}

	// Persist successful scrape results OFF the per-goroutine critical path.
	// This runs AFTER all errgroup-gated scrape goroutines have drained, so the
	// scrape workers never blocked on the DB write during scraping. A small
	// dedicated pool (independent of eg.SetLimit(MaxWorkers)) bounds total
	// persist latency. Only the batch scrape path opts out of the workflow's
	// inline persist (cmd.SkipPersist=true via buildScrapeCmd); single-scrape
	// callers (CLI/API/rescrape) still persist inline inside Workflow.Scrape.
	// Must complete before MarkCompleted so job-state persistence (deferred at
	// the top of Run) captures Persisted=true and any surfacable persist errors.
	var recorded map[string]bool
	if inputs.MovieRepo != nil {
		// Pass cfg.OnFileScrapeFailed so a persist failure can correct the
		// per-file WS status: the scrape worker already emitted a terminal
		// "success" ProgressMessage for this file (OnFileScraped), but persist
		// runs later in a separate pool and can fail. Re-firing the per-file
		// failure hook overwrites messagesByFile[filePath] so the frontend
		// never shows a stale "success" for a file whose persist failed.
		recorded = persistScrapeOutcomePool(ctx, outcomes, inputs, cfg.OnFileScrapeFailed)
	}

	// ctx can be canceled while the persist pool is draining. After it returns,
	// re-check cancellation before MarkCompleted so a canceled job finishes as
	// Cancelled rather than being marked Completed with a partially-persisted set.
	if err := ctx.Err(); err != nil {
		trackScrapeResults(inputs, outcomes, recorded)
		inputs.Lifecycle.MarkCancelled()
		return
	}

	trackScrapeResults(inputs, outcomes, recorded)

	inputs.Lifecycle.MarkCompleted()
}

// isManualURLInput reports whether a manual input looks like an http(s) URL.
// Mirrors rescrape_phase.go's manual-search URL detection. Post Phase-2
// validation only http(s) URLs reach the scrape path (non-http schemes and
// unhandleable URLs are rejected with 400), so this prefix check is a safe
// proxy for matcher.ParseInput's IsURL at this seam.
func isManualURLInput(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// buildScrapeCmd constructs a scrape.ScrapeCmd for a single file.
// It resolves the movie ID (from override or matcher), determines scrapers
// to use, and builds the command.
func buildScrapeCmd(
	filePath string,
	fmi models.FileMatchInfo,
	inputs scrapePhaseInputs,
	cfg ScrapePhaseConfig,
) (scrape.ScrapeCmd, bool) {
	var movieID string
	var rawInput string
	var manualURL bool
	movieIDFromMatcher := false
	if raw, ok := cfg.RawInputOverride[filePath]; ok && strings.TrimSpace(raw) != "" {
		trimmed := strings.TrimSpace(raw)
		rawInput = trimmed
		// MovieID is surfaced in persisted job state, WebSocket events, and
		// per-file progress messages. A manual URL may carry a query token
		// (e.g. ?token=secret); redact it here so the raw URL never reaches
		// those sinks. RawInput stays unredacted so resolveScrapeInput/ScrapeURL
		// still see the real URL. RedactURLQuery passes plain IDs through
		// unchanged, so manual ID inputs are unaffected.
		movieID = scrape.RedactURLQuery(trimmed)
		manualURL = isManualURLInput(trimmed)
	}
	if movieID == "" {
		// MovieIDOverride (per-file rescrape override) takes precedence over the
		// validated FileMatchInfo.MovieID, then the matcher fallback.
		if override, ok := cfg.MovieIDOverride[filePath]; ok {
			movieID = override
		} else if fmi.MovieID != "" {
			// Prefer the directory-validated FileMatchInfo.MovieID (set by the scan/match
			// phase, e.g. a restored E/Z catalog suffix) over the matcher's unvalidated
			// heuristic — keeps the scrape ID consistent with the validated match result.
			movieID = fmi.MovieID
			movieIDFromMatcher = true
		} else {
			movieID = ""
			if inputs.Matcher != nil {
				movieID = inputs.Matcher.MatchString(filepath.Base(filePath))
			}
			if movieID == "" {
				movieID = filepath.Base(filePath)
				ext := filepath.Ext(movieID)
				if ext != "" {
					movieID = movieID[:len(movieID)-len(ext)]
				}
			} else {
				movieIDFromMatcher = true
			}
		}
	}

	scrapersToUse := cfg.SelectedScrapers
	if manualURL {
		scrapersToUse = nil
	} else if len(scrapersToUse) == 0 && len(cfg.PriorityOverride) > 0 {
		scrapersToUse = cfg.PriorityOverride
	}
	if len(scrapersToUse) == 0 {
		scrapersToUse = nil
	}

	return scrape.ScrapeCmd{
		MovieID:          movieID,
		RawInput:         rawInput,
		ForceRefresh:     cfg.Force,
		SelectedScrapers: scrapersToUse,
		PriorityOverride: cfg.PriorityOverride,
		// Batch scrape opts out of the workflow's inline DB persist so the
		// errgroup-gated scrape workers don't block on SQLite's single-writer
		// lock. Persistence runs in a dedicated pool off the critical path —
		// see Run(). Single-scrape callers (CLI/API/rescrape) leave this false.
		SkipPersist: inputs.MovieRepo != nil,
	}, movieIDFromMatcher
}

// interpretScrapeResult processes the workflow.Scrape result/error into a
// scrapeFileOutcome. It handles error and nil-result cases, poster generation,
// result tracking, provenance, and broadcast events.
func interpretScrapeResult(
	filePath string,
	fmi models.FileMatchInfo,
	cmd scrape.ScrapeCmd,
	startTime time.Time,
	taskCtx context.Context,
	inputs scrapePhaseInputs,
	result *scrape.ScrapeResult,
	meta *workflow.OrchestrationMeta,
	err error,
	preserveMovieID bool,
) (outcome scrapeFileOutcome) {
	outcome = scrapeFileOutcome{
		FilePath: filePath,
		MovieID:  cmd.MovieID,
	}

	now := time.Now()

	if err != nil {
		fileStatus := models.JobStatusFailed
		if errors.Is(err, context.Canceled) {
			fileStatus = models.JobStatusCancelled
			outcome.Cancelled = true
		}
		errMsg, errorCode := classifyFileScrapeError(err)
		inputs.Updater.UpdateFileResult(filePath, &resultstore.MovieResult{
			FileMatchInfo: fmi,
			Status:        fileStatus,
			Error:         errMsg,
			ErrorCode:     errorCode,
			StartedAt:     startTime,
			EndedAt:       &now,
		})
		inputs.Broadcaster.Send(JobEvent{
			JobID:     inputs.JobID,
			MovieID:   cmd.MovieID,
			Phase:     JobEventPhaseScrape,
			Step:      StepFailed,
			Message:   fmt.Sprintf("Scrape failed: %s", errMsg),
			Timestamp: time.Now(),
		})
		outcome.Failed = true
		outcome.ErrorMsg = errMsg
		return
	}
	if result == nil || result.Movie == nil {
		// The scrape package populates result.Message with a verbose,
		// per-scraper failure summary (e.g. "No results from any scraper:
		// fc2: movie PPV-2856053 not found on FC2; javdb: ...") via
		// buildNoResultsError. When result is nil there is no scrape payload
		// to lift a message from, so fall back to a generic "no result".
		errorMsg := "no result"
		errorCode := string(models.ScraperErrorKindUnknown)
		if result != nil {
			if strings.TrimSpace(result.Message) != "" {
				errorMsg = result.Message
			}
			if result.FailureKind != "" {
				errorCode = string(result.FailureKind)
			}
		}
		inputs.Updater.UpdateFileResult(filePath, &resultstore.MovieResult{
			FileMatchInfo: fmi,
			Status:        models.JobStatusFailed,
			Error:         errorMsg,
			ErrorCode:     errorCode,
			StartedAt:     startTime,
			EndedAt:       &now,
		})
		inputs.Broadcaster.Send(JobEvent{
			JobID:     inputs.JobID,
			MovieID:   cmd.MovieID,
			Phase:     JobEventPhaseScrape,
			Step:      StepFailed,
			Message:   fmt.Sprintf("Scrape produced no result: %s", errorMsg),
			Timestamp: time.Now(),
		})
		outcome.Failed = true
		outcome.ErrorMsg = errorMsg
		return outcome
	}

	fileResult, prov := scrapeResultToMovieResult(fmi, result, meta, preserveMovieID)
	fileResult.StartedAt = startTime

	// Poster generation — moved from the workflow's scrape orchestrator
	// to the worker phase so that ScrapeCmd stays a pure query and
	// the side-effect (filesystem write) is owned by the orchestration layer.
	if inputs.PosterGen != nil && fileResult.Movie != nil {
		posterErr := inputs.PosterGen.GeneratePoster(taskCtx, inputs.JobID.String(), fileResult.Movie)
		if posterErr != nil {
			s := posterErr.Error()
			fileResult.PosterError = &s
		}
		fileResult.PosterGenerated = true
	}

	// Establish the scraped poster state as the Reset baseline so the review
	// UI's Reset returns to what this scrape produced. Done after poster
	// generation so the generated CroppedPosterURL is captured too. Mirrors the
	// rescrape path (establishScrapedBaseline) for full symmetry — without it,
	// Original* stays empty until the first manual edit snapshots it lazily via
	// backupPosterOriginals, which is inconsistent with the rescrape baseline.
	if fileResult.Movie != nil {
		establishScrapedBaseline(fileResult.Movie, fileResult.Movie)
	}

	inputs.Updater.UpdateFileResult(filePath, fileResult)
	if prov != nil {
		inputs.Updater.SetProvenance(filePath, prov)
	}

	inputs.Broadcaster.Send(JobEvent{
		JobID:     inputs.JobID,
		MovieID:   result.Movie.ID,
		Phase:     JobEventPhaseScrape,
		Step:      StepComplete,
		Progress:  1.0,
		Message:   fmt.Sprintf("Scraped %s successfully", result.Movie.ID),
		Timestamp: *fileResult.EndedAt,
	})
	outcome.Success = true
	outcome.Result = result
	outcome.Meta = meta
	return outcome
}

// scrapeFile handles the per-file scrape logic: build ScrapeCmd, execute workflow.Scrape,
// interpret result. Error handling, panic recovery, and result tracking are performed here.
func scrapeFile(
	egCtx context.Context,
	filePath string,
	fmi models.FileMatchInfo,
	cmd scrape.ScrapeCmd,
	fromMatcher bool,
	inputs scrapePhaseInputs,
	cfg ScrapePhaseConfig,
) (outcome scrapeFileOutcome) {
	outcome = scrapeFileOutcome{
		FilePath: filePath,
		MovieID:  cmd.MovieID,
	}

	movieIDFromMatcher := fromMatcher || fmi.MovieID != ""
	if fmi.MovieID == "" && cmd.MovieID != "" {
		fmi.MovieID = cmd.MovieID
	}

	// Mirror main's newFailedFileResult(filePath, ...): the scrape argument is
	// the authoritative file path. When the in-memory FileMatchInfo map lacks an
	// entry for this file (scanner miss, nil map, or path-normalization mismatch),
	// fmi.Path is empty and the API response's file_path field comes back blank —
	// so the frontend's failed-files list (UnidentifiedFilesCard renders
	// basename(result.file_path)) shows the error ("no result") with no filename.
	// Backfill Path so every derived MovieResult (running, failed, no-result,
	// panic-recovered) carries it. No-op when the scanner already populated it.
	if fmi.Path == "" {
		fmi.Path = filePath
	}
	// Backfill Name + Extension alongside Path. The organizer's resolveFileName
	// builds the target filename as `templateOutput + match.Extension`; when
	// the scanner map misses this file, fmi.Extension is empty and the video
	// preview row renders as `ABF-346` (no `.mp4` appended) — even though NFO /
	// poster / fanart rows look correct because they derive from movie.ID, not
	// from the source extension. Mirror scanner.go's own construction
	// (Name: filepath.Base(path); Extension: filepath.Ext(path)).
	if fmi.Name == "" {
		fmi.Name = filepath.Base(filePath)
	}
	if fmi.Extension == "" {
		fmi.Extension = filepath.Ext(filePath)
	}

	taskCtx := egCtx
	if inputs.Concurrency.WorkerTimeout > 0 {
		var taskCancel context.CancelFunc
		taskCtx, taskCancel = context.WithTimeout(egCtx, inputs.Concurrency.WorkerTimeout)
		defer taskCancel()
	}

	startTime := time.Now()

	rc := recoveryContext{
		filePath:  filePath,
		fmi:       fmi,
		updater:   inputs.Updater,
		broadcast: broadcastFailure(inputs.Broadcaster, inputs.JobID, cmd.MovieID, JobEventPhaseScrape, "Scrape"),
		startTime: startTime,
	}
	defer withFileRecovery(rc, &outcome)()

	inputs.Updater.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: fmi,
		Status:        models.JobStatusRunning,
		StartedAt:     startTime,
	})

	// Step 2: Execute the scrape.
	reporter := makeProgressReporter(inputs.Broadcaster, inputs.JobID, cmd.MovieID, JobEventPhaseScrape)
	// Wrap the in-process reporter so each step update also reaches the WS
	// hub (with FilePath), restoring main's realtime.ProgressAdapter live
	// per-file step text in ProgressModal. The base reporter still drives the
	// in-process Broadcaster (TUI/CLI).
	if cfg.OnScrapeStepProgress != nil {
		wsHook := cfg.OnScrapeStepProgress
		base := reporter
		reporter = progress.ReporterFunc(func(step progress.ProgressStep, pct float64, msg string) {
			base.Report(step, pct, msg)
			wsHook(filePath, string(step), pct, msg)
		})
	}
	// Inject the reporter into taskCtx (which carries the worker timeout /
	// errgroup cancellation) so downstream emitters resolve it via
	// progress.FromContext. Use taskCtx, not the parent egCtx.
	taskCtx = progress.WithReporter(taskCtx, reporter)

	// Nest the overall scrape operation timeout (scrapers.request_timeout_seconds)
	// inside the worker_timeout task context. The sooner deadline wins via
	// min(worker_timeout, request_timeout_seconds) — see design D3.
	scrapeCtx := taskCtx
	if inputs.Concurrency.RequestTimeout > 0 {
		resolved := timeout.FromDuration(inputs.Concurrency.RequestTimeout, "config:scrapers.request_timeout_seconds")
		logging.Debugf("Scrape: applying request timeout %s (nested within worker_timeout)", resolved)
		var scrapeCancel context.CancelFunc
		scrapeCtx, scrapeCancel = context.WithTimeout(taskCtx, resolved.Duration)
		defer scrapeCancel()
	}

	result, meta, err := inputs.WF.Scrape(scrapeCtx, cmd)

	// Step 3: Interpret the result.
	// If the request timeout fired, surface it as a failure rather than
	// persisting a partial/successful result from an expired context.
	if scrapeCtx.Err() != nil && err == nil {
		err = scrapeCtx.Err()
		if result == nil {
			result = &scrape.ScrapeResult{Status: scrape.StatusFailed, Message: "scrape timed out"}
		} else {
			result.Status = scrape.StatusFailed
			result.Message = "scrape timed out"
		}
	}
	return interpretScrapeResult(filePath, fmi, cmd, startTime, taskCtx, inputs, result, meta, err, movieIDFromMatcher)
}

// trackScrapeResults processes collected scrapeFileOutcomes.
// The actual Updater/Broadcaster calls are already done inside scrapeFile;
// this function is a seam for future aggregation (e.g., counters, logging).
func trackScrapeResults(inputs scrapePhaseInputs, outcomes []scrapeFileOutcome, recordedSuccesses map[string]bool) {
	for _, o := range outcomes {
		if o.Cancelled {
			continue
		}
		if o.Success && !recordedSuccesses[o.FilePath] {
			auditScrapeSuccess(inputs, o)
			continue
		}
		if o.Panic || o.Failed {
			auditScrapeFailure(inputs, o)
		}
	}
}

// persistScrapeOutcomePool fans persist work for a batch of scrape outcomes out
// across a small dedicated goroutine pool. The pool is sized independently of
// eg.SetLimit(MaxWorkers) (the scrape worker limit) and runs AFTER the scrape
// goroutines have drained, so the SQLite single-writer lock never serializes
// the per-file scrape workers — the root cause of the 5→1 worker degradation
// reported by QuickLion (see /tmp/concurrency-investigation-results.md).
//
// Only successful scrapes with a movie are persisted; the failed/no-result/panic
// paths are already reflected on the in-memory result and have nothing to write.
func persistScrapeOutcomePool(ctx context.Context, outcomes []scrapeFileOutcome, inputs scrapePhaseInputs, onFileFailed func(filePath, movieID, errMsg string)) map[string]bool {
	recorded := make(map[string]bool)
	var recordedMu sync.Mutex
	work := make(chan scrapeFileOutcome, len(outcomes))
	for _, o := range outcomes {
		work <- o
	}
	close(work)

	const persistPoolSize = 2
	var wg sync.WaitGroup
	for i := 0; i < persistPoolSize; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logging.Errorf("persist worker panic recovered: %v", r)
				}
			}()
			for k, v := range persistScrapeOutcomes(ctx, work, inputs, onFileFailed) {
				recordedMu.Lock()
				recorded[k] = v
				recordedMu.Unlock()
			}
		}()
	}
	wg.Wait()
	return recorded
}

// persistScrapeOutcomes drains a channel of scrape outcomes and persists each
// successful one. Used by persistScrapeOutcomePool to fan persist work across
// the pool goroutines.
func persistScrapeOutcomes(ctx context.Context, ch <-chan scrapeFileOutcome, inputs scrapePhaseInputs, onFileFailed func(filePath, movieID, errMsg string)) map[string]bool {
	recorded := make(map[string]bool)
	for o := range ch {
		if !o.Success || o.Result == nil || o.Result.Movie == nil || inputs.MovieRepo == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					logging.Errorf("persistScrapeOutcome panic recovered: %v", r)
					recorded[o.FilePath] = true
					_ = inputs.Updater.AtomicUpdateFileResult(o.FilePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
						current.Status = models.JobStatusFailed
						current.Error = fmt.Sprintf("persist panic: %v", r)
						return current, nil
					})
					if onFileFailed != nil {
						onFileFailed(o.FilePath, o.MovieID, fmt.Sprintf("persist panic: %v", r))
					}
					auditCtx, auditCancel := historyAuditContext()
					defer auditCancel()
					recordHistory(auditCtx, inputs.HistoryRepo, models.History{
						MovieID:      o.MovieID,
						BatchJobID:   jobIDPtr(inputs.JobID),
						Operation:    models.HistoryOpScrape,
						OriginalPath: o.FilePath,
						Status:       models.HistoryStatusFailed,
						ErrorMessage: fmt.Sprintf("persist panic: %v", r),
					})
				}
			}()
			if persistScrapeOutcome(ctx, o, inputs, onFileFailed) {
				recorded[o.FilePath] = true
			}
		}()
	}
	return recorded
}

// persistScrapeOutcome persists a single successful scrape's movie off the
// per-goroutine critical path. The in-memory MovieResult.Movie is already
// set (by interpretScrapeResult) before this runs; persist updates the
// Persisted flag (and refreshes the movie with the DB-saved version) via
// AtomicUpdateFileResult so API/UI readers observe a consistent snapshot.
// Persist failures surface on the MovieResult, preserving the original
// error semantics (persist error → Status=Failed).
func persistScrapeOutcome(ctx context.Context, o scrapeFileOutcome, inputs scrapePhaseInputs, onFileFailed func(filePath, movieID, errMsg string)) (handled bool) {
	// Clone before persisting: UpsertWithTranslations mutates its input movie in
	// place (resets association slices to reapply associations). The in-memory
	// MovieResult.Movie shares the result.Movie pointer, so mutating it here
	// would race with concurrent API/UI readers under -race.
	cloned := o.Result.Movie.Clone()
	var genreTrans []models.GenreTranslationData
	var actressTrans []models.ActressTranslationData
	if o.Result != nil && o.Result.TranslationOutput != nil {
		genreTrans = o.Result.TranslationOutput.GenreTranslations
		actressTrans = o.Result.TranslationOutput.ActressTranslations
	}
	saved, err := inputs.MovieRepo.UpsertWithTranslations(ctx, cloned, genreTrans, actressTrans)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false
		}
		logging.Warnf("[scrape-phase] Failed to persist %s: %v", o.MovieID, err)
		_ = inputs.Updater.AtomicUpdateFileResult(o.FilePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
			current.Status = models.JobStatusFailed
			current.Error = fmt.Sprintf("persist failed: %v", err)
			return current, nil
		})
		inputs.Broadcaster.Send(JobEvent{
			JobID:     inputs.JobID,
			MovieID:   o.MovieID,
			Phase:     JobEventPhaseScrape,
			Step:      StepFailed,
			Message:   fmt.Sprintf("Scrape persist failed: %v", err),
			Timestamp: time.Now(),
		})
		// Correct the per-file WS status: the scrape worker already emitted a
		// terminal "success" ProgressMessage (OnFileScraped) for this file before
		// persist ran. The JobEvent broadcast above is job-level (no FilePath),
		// so it never reaches the frontend's messagesByFile — re-fire the
		// per-file failure hook so messagesByFile[filePath] flips from success
		// to error instead of leaving a stale "success".
		if onFileFailed != nil {
			onFileFailed(o.FilePath, o.MovieID, fmt.Sprintf("persist failed: %v", err))
		}
		recordAuditCtx, recordAuditCancel := historyAuditContext()
		defer recordAuditCancel()
		recordHistory(recordAuditCtx, inputs.HistoryRepo, models.History{
			MovieID:      o.MovieID,
			BatchJobID:   jobIDPtr(inputs.JobID),
			Operation:    models.HistoryOpScrape,
			OriginalPath: o.FilePath,
			Status:       models.HistoryStatusFailed,
			ErrorMessage: fmt.Sprintf("persist failed: %v", err),
		})
		return true
	}
	// Refresh the in-memory movie with the DB-saved version (DB-assigned IDs,
	// normalized associations) and flip Persisted. AtomicUpdateFileResult clones
	// under lock, so no shared-pointer mutation leaks to readers.
	_ = inputs.Updater.AtomicUpdateFileResult(o.FilePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		current.Persisted = true
		if saved != nil {
			current.Movie = saved.Clone()
		}
		return current, nil
	})
	movieID := o.MovieID
	if saved != nil {
		movieID = saved.ID
	}
	recordAuditCtx, recordAuditCancel := historyAuditContext()
	defer recordAuditCancel()
	recordHistory(recordAuditCtx, inputs.HistoryRepo, models.History{
		MovieID:      movieID,
		BatchJobID:   jobIDPtr(inputs.JobID),
		Operation:    models.HistoryOpScrape,
		OriginalPath: o.FilePath,
		Status:       models.HistoryStatusSuccess,
	})
	return true
}

func classifyFileScrapeError(err error) (errMsg, errorCode string) {
	if err == nil {
		return "", string(models.ScraperErrorKindUnknown)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "scrape timed out", string(models.ScraperErrorKindUnavailable)
	}
	if errors.Is(err, context.Canceled) {
		return "scrape canceled", string(models.ScraperErrorKindUnavailable)
	}
	if se, ok := models.AsScraperError(err); ok {
		code := string(se.Kind)
		if code == "" {
			code = string(models.ScraperErrorKindUnknown)
		}
		msg := se.Message
		if strings.TrimSpace(msg) == "" {
			msg = err.Error()
		}
		return msg, code
	}
	return err.Error(), string(models.ScraperErrorKindUnknown)
}
