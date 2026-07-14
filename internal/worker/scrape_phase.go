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
	"github.com/javinizer/javinizer-go/internal/scrape"
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
	FilePath string
	MovieID  string
	Success  bool
	Failed   bool // true if scrape failed (not panic)
	Panic    bool // true if goroutine panicked
	PanicMsg string
	ErrorMsg string
	Result   *scrape.ScrapeResult
	Meta     *workflow.OrchestrationMeta
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
			inputs.persister.Persist()
		}
	}()

	outcomes := boundedFanOut(ctx, inputs.Concurrency.MaxWorkers, files,
		func(egCtx context.Context, filePath string) scrapeFileOutcome {
			// scrapeFile does NOT persist to the database: buildScrapeCmd sets
			// cmd.SkipPersist=true when inputs.MovieRepo is wired, so the workflow's
			// scrape orchestrator skips its inline DB persist (step 4). Persistence
			// runs after all scrape goroutines drain (see persistScrapeOutcomes
			// below) — off the errgroup-gated critical path, so SQLite's
			// single-writer lock never serializes the per-file scrape workers
			// (root cause of the 5→1 worker degradation).
			cmd, fromMatcher := buildScrapeCmd(filePath, inputs, cfg)
			fmi := inputs.FileMatchInfo[filePath]
			outcome := scrapeFile(egCtx, filePath, fmi, cmd, fromMatcher, inputs, cfg)
			// Broadcast per-file scrape progress over WebSocket so the frontend's
			// messagesByFile populates and ProgressModal shows live per-file status.
			// Mirrors main's realtime.ProgressAdapter which forwarded per-task
			// scrape updates to the WS hub (deleted in this refactor with no
			// replacement — restored here via the hook seam).
			if outcome.Success && cfg.OnFileScraped != nil {
				cfg.OnFileScraped(filePath, fmt.Sprintf("Scraped %s successfully", outcome.MovieID))
			} else if outcome.Failed && cfg.OnFileScrapeFailed != nil {
				cfg.OnFileScrapeFailed(filePath, outcome.ErrorMsg)
			}
			return outcome
		},
	)

	if err := ctx.Err(); err != nil {
		inputs.Lifecycle.MarkCancelled()
		// On cancellation, skip persist + MarkCompleted — the job is cancelled,
		// not completed. Any outcomes collected before cancellation are already
		// reflected on the in-memory result via UpdateFileResult inside each
		// worker goroutine.
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
	if inputs.MovieRepo != nil {
		// Pass cfg.OnFileScrapeFailed so a persist failure can correct the
		// per-file WS status: the scrape worker already emitted a terminal
		// "success" ProgressMessage for this file (OnFileScraped), but persist
		// runs later in a separate pool and can fail. Re-firing the per-file
		// failure hook overwrites messagesByFile[filePath] so the frontend
		// never shows a stale "success" for a file whose persist failed.
		persistScrapeOutcomePool(ctx, outcomes, inputs, cfg.OnFileScrapeFailed)
	}

	// ctx can be canceled while the persist pool is draining. After it returns,
	// re-check cancellation before MarkCompleted so a canceled job finishes as
	// Cancelled rather than being marked Completed with a partially-persisted set.
	if err := ctx.Err(); err != nil {
		inputs.Lifecycle.MarkCancelled()
		return
	}

	trackScrapeResults(outcomes)

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
		if override, ok := cfg.MovieIDOverride[filePath]; ok {
			movieID = override
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
) scrapeFileOutcome {
	outcome := scrapeFileOutcome{
		FilePath: filePath,
		MovieID:  cmd.MovieID,
	}

	now := time.Now()

	if err != nil {
		fileStatus := models.JobStatusFailed
		if errors.Is(err, context.Canceled) {
			fileStatus = models.JobStatusCancelled
		}
		inputs.Updater.UpdateFileResult(filePath, &MovieResult{
			FileMatchInfo: fmi,
			Status:        fileStatus,
			Error:         err.Error(),
			StartedAt:     startTime,
			EndedAt:       &now,
		})
		inputs.Broadcaster.Send(JobEvent{
			JobID:     inputs.JobID,
			MovieID:   cmd.MovieID,
			Phase:     JobEventPhaseScrape,
			Step:      StepFailed,
			Message:   fmt.Sprintf("Scrape failed: %v", err),
			Timestamp: time.Now(),
		})
		outcome.Failed = true
		outcome.ErrorMsg = err.Error()
		return outcome
	}
	if result == nil || result.Movie == nil {
		// The scrape package populates result.Message with a verbose,
		// per-scraper failure summary (e.g. "No results from any scraper:
		// fc2: movie PPV-2856053 not found on FC2; javdb: ...") via
		// buildNoResultsError. When result is nil there is no scrape payload
		// to lift a message from, so fall back to a generic "no result".
		errorMsg := "no result"
		if result != nil && strings.TrimSpace(result.Message) != "" {
			errorMsg = result.Message
		}
		inputs.Updater.UpdateFileResult(filePath, &MovieResult{
			FileMatchInfo: fmi,
			Status:        models.JobStatusFailed,
			Error:         errorMsg,
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
) scrapeFileOutcome {
	outcome := scrapeFileOutcome{
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

	inputs.Updater.UpdateFileResult(filePath, &MovieResult{
		FileMatchInfo: fmi,
		Status:        models.JobStatusRunning,
		StartedAt:     startTime,
	})

	// Step 2: Execute the scrape.
	progressFn := makeProgressFn(inputs.Broadcaster, inputs.JobID, cmd.MovieID, JobEventPhaseScrape)
	// Wrap the in-process progress fn so each step update also reaches the WS
	// hub (with FilePath), restoring main's realtime.ProgressAdapter live
	// per-file step text in ProgressModal. The base fn still drives the
	// in-process Broadcaster (TUI/CLI).
	if cfg.OnScrapeStepProgress != nil {
		wsHook := cfg.OnScrapeStepProgress
		baseFn := progressFn
		progressFn = func(step scrape.ProgressStep, pct float64, msg string) {
			baseFn(step, pct, msg)
			wsHook(filePath, string(step), pct, msg)
		}
	}

	result, meta, err := inputs.WF.Scrape(taskCtx, cmd, progressFn)

	// Step 3: Interpret the result.
	return interpretScrapeResult(filePath, fmi, cmd, startTime, taskCtx, inputs, result, meta, err, movieIDFromMatcher)
}

// trackScrapeResults processes collected scrapeFileOutcomes.
// The actual Updater/Broadcaster calls are already done inside scrapeFile;
// this function is a seam for future aggregation (e.g., counters, logging).
func trackScrapeResults(outcomes []scrapeFileOutcome) {
	// Currently a no-op seam — all per-file tracking is done inline in scrapeFile.
	// Future: aggregate counters, emit summary events, etc.
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
func persistScrapeOutcomePool(ctx context.Context, outcomes []scrapeFileOutcome, inputs scrapePhaseInputs, onFileFailed func(filePath, errMsg string)) {
	// Seed a buffered channel (closed up-front) so persist workers can drain it
	// concurrently without coordination. Buffer == outcome count guarantees the
	// sends never block.
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
			// Recover panics inside the persist worker. The top-level Run defer
			// cannot catch panics from these goroutines; an unrecovered panic from
			// repository persistence would crash the process and bypass lifecycle
			// accounting. Log and swallow so the pool drains and the job resolves
			// through its normal failure path instead of taking down the binary.
			defer func() {
				if r := recover(); r != nil {
					logging.Errorf("persist worker panic recovered: %v", r)
				}
			}()
			persistScrapeOutcomes(ctx, work, inputs, onFileFailed)
		}()
	}
	wg.Wait()
}

// persistScrapeOutcomes drains a channel of scrape outcomes and persists each
// successful one. Used by persistScrapeOutcomePool to fan persist work across
// the pool goroutines.
func persistScrapeOutcomes(ctx context.Context, ch <-chan scrapeFileOutcome, inputs scrapePhaseInputs, onFileFailed func(filePath, errMsg string)) {
	for o := range ch {
		if !o.Success || o.Result == nil || o.Result.Movie == nil || inputs.MovieRepo == nil {
			continue
		}
		persistScrapeOutcome(ctx, o, inputs, onFileFailed)
	}
}

// persistScrapeOutcome persists a single successful scrape's movie off the
// per-goroutine critical path. The in-memory MovieResult.Movie is already
// set (by interpretScrapeResult) before this runs; persist updates the
// Persisted flag (and refreshes the movie with the DB-saved version) via
// AtomicUpdateFileResult so API/UI readers observe a consistent snapshot.
// Persist failures surface on the MovieResult, preserving the original
// error semantics (persist error → Status=Failed).
func persistScrapeOutcome(ctx context.Context, o scrapeFileOutcome, inputs scrapePhaseInputs, onFileFailed func(filePath, errMsg string)) {
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
		logging.Warnf("[scrape-phase] Failed to persist %s: %v", o.MovieID, err)
		_ = inputs.Updater.AtomicUpdateFileResult(o.FilePath, func(current *MovieResult) (*MovieResult, error) {
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
			onFileFailed(o.FilePath, fmt.Sprintf("persist failed: %v", err))
		}
		return
	}
	// Refresh the in-memory movie with the DB-saved version (DB-assigned IDs,
	// normalized associations) and flip Persisted. AtomicUpdateFileResult clones
	// under lock, so no shared-pointer mutation leaks to readers.
	_ = inputs.Updater.AtomicUpdateFileResult(o.FilePath, func(current *MovieResult) (*MovieResult, error) {
		current.Persisted = true
		if saved != nil {
			current.Movie = saved.Clone()
		}
		return current, nil
	})
}
