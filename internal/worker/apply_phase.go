package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

type ApplyPhase interface {
	Run(ctx context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig)
}

type applyPhase struct{}

func NewApplyPhase() ApplyPhase {
	return &applyPhase{}
}

// applyFileOutcome captures the result of applying a single file.
// Collected by the errgroup goroutine, then aggregated by trackApplyResults.
type applyFileOutcome struct {
	FilePath  string
	MovieID   string
	Success   bool
	Failed    bool // true if apply failed (not panic, not skip, not cancel)
	Cancelled bool // true if apply was interrupted by context cancellation
	Panic     bool // true if goroutine panicked
	PanicMsg  string
	ErrorMsg  string
	Movie     *models.Movie // updated movie after apply (nil if failed)
}

// Run executes the apply phase: setup errgroup → iterate files → dispatch
// applyFile → collect outcomes → track results → report status.
func (p *applyPhase) Run(ctx context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig) {
	wf := inputs.WF
	persister := inputs.persister

	defer func() {
		if r := recover(); r != nil {
			panicErr := panicutil.FormatRecover(r)
			logging.Errorf("BatchJob.StartApply %s %v", inputs.JobID.String(), panicErr)
			inputs.Lifecycle.MarkFailed()
		}
		if persister != nil {
			persister.Persist()
		}
	}()

	excludedSnapshot := make(map[string]bool, len(inputs.Results))
	for filePath := range inputs.Results {
		excludedSnapshot[filePath] = inputs.Excluded[filePath]
	}

	// Build the filtered list of files to apply. inputs.Results is a frozen
	// snapshot of result state at apply start time. This is intentional:
	// apply operates on scrape-time state, and mutations (e.g., rescrape,
	// exclusion) during apply go through the live tracker via inputs.Updater
	// but do not affect which files this apply iteration processes. This
	// prevents concurrent-modification bugs during iteration.
	type applyItem struct {
		filePath   string
		fileResult *MovieResult
		movie      *models.Movie
	}
	items := make([]applyItem, 0, len(inputs.Results))
	for filePath, fileResult := range inputs.Results {
		if fileResult.Status != models.JobStatusCompleted || fileResult.Movie == nil {
			continue
		}
		if excludedSnapshot[filePath] {
			logging.Infof("Skipping excluded file: %s", filePath)
			continue
		}
		items = append(items, applyItem{
			filePath:   filePath,
			fileResult: fileResult,
			movie:      fileResult.Movie,
		})
	}

	total := len(items)
	var processed int64
	outcomes := boundedFanOut(ctx, inputs.Concurrency.MaxWorkers, items,
		func(egCtx context.Context, item applyItem) applyFileOutcome {
			outcome := applyFile(egCtx, wf, item.filePath, item.fileResult, item.movie, inputs, cfg)
			// Report per-file progress so the frontend bar advances 0→100 across
			// files instead of jumping straight to 100 on OnPhaseComplete. A file
			// counts as processed whether it succeeded or failed — the bar tracks
			// throughput, not success rate (per-file success/failure is surfaced
			// separately via OnPhaseComplete + the controller's polling fallback).
			// This closure runs once per item, so total (= len(items)) is always > 0
			// here; the total<=0 case is handled at the broadcast boundary by
			// organizeProgressPercent and is unreachable from this call site.
			if cfg.OnFileProgress != nil {
				done := int(atomic.AddInt64(&processed, 1))
				cfg.OnFileProgress(done, total)
			}
			return outcome
		},
	)

	if err := ctx.Err(); err != nil {
		inputs.Lifecycle.MarkCancelled()
		// On cancellation, skip trackResults + OnPhaseComplete + MarkOrganized/
		// MarkCompleted — the job is cancelled, not completed. Any outcomes
		// collected before cancellation are already reflected on the in-memory
		// result via UpdateFileResult inside each worker goroutine.
		return
	}

	var organized int64
	var failed int64
	trackApplyResults(outcomes, &organized, &failed)

	orgCount := atomic.LoadInt64(&organized)
	failCount := atomic.LoadInt64(&failed)

	// Broadcast the final organization_completed / update_completed WebSocket
	// message BEFORE MarkOrganized / MarkCompleted so frontend clients
	// watching for that status (organize-controller.handleWebSocketMessage)
	// can finalize the apply flow in real time. Mirrors main's
	// process_organize.go which called broadcastProgress inline at end of
	// organize. API layer wires the hook via ApplyPhaseConfig.OnPhaseComplete.
	if cfg.OnPhaseComplete != nil {
		cfg.OnPhaseComplete(int(orgCount), int(failCount))
	}

	if failCount == 0 && orgCount > 0 && !cfg.OrganizeOptions.Skip {
		inputs.Lifecycle.MarkOrganized()
	} else {
		inputs.Lifecycle.MarkCompleted()
	}
}

// buildApplyCmd constructs the workflow.ApplyCmd for a single file apply.
// It resolves the destination path, builds the command, and runs the
// PreApply hook if configured (which may mutate the ApplyFileContext and
// thus the returned ApplyCmd fields).
func buildApplyCmd(
	filePath string,
	movie *models.Movie,
	fileResult *MovieResult,
	inputs applyPhaseInputs,
	cfg ApplyPhaseConfig,
	taskCtx context.Context,
) (workflow.ApplyCmd, *ApplyFileContext, bool) {
	sourceDir := filepath.Dir(filePath)
	match := fileResult.FileMatchInfo
	match.MovieID = movie.ID

	destPath := cfg.Destination
	if destPath == "" {
		destPath = inputs.Destination
	}
	if destPath == "" && cfg.OrganizeOptions.Skip {
		destPath = sourceDir
	}

	applyCmd := workflow.ApplyCmd{
		Movie:               movie,
		Match:               match,
		DestPath:            destPath,
		DryRun:              cfg.DryRun,
		Organize:            cfg.OrganizeOptions,
		Merge:               cfg.MergeOptions,
		Download:            cfg.Download,
		DisplayTitleSrc:     movie,
		DownloadExtrafanart: cfg.DownloadExtrafanart,
	}

	applyCmd.GenerateNFO = cfg.GenerateNFO && inputs.NFOEnabled

	afc := &ApplyFileContext{
		FilePath:    filePath,
		Movie:       movie,
		MovieResult: fileResult,
		Match:       match,
		Destination: destPath,
	}

	if cfg.PreApplyFunc != nil {
		if err := cfg.PreApplyFunc(taskCtx, afc); err != nil {
			logging.Warnf("PreApply hook skipped %s: %v", filePath, err)
			return applyCmd, afc, false // false = skip execution
		}
		applyCmd.Movie = afc.Movie
		applyCmd.Match = afc.Match
		applyCmd.DestPath = afc.Destination
	}

	return applyCmd, afc, true
}

// interpretApplyResult processes the workflow.Apply result/error into an
// applyFileOutcome. It updates the job result tracker, broadcasts events,
// and runs the PostApply hook if configured.
func interpretApplyResult(
	filePath string,
	movie *models.Movie,
	startTime time.Time,
	applyTimeout time.Duration,
	inputs applyPhaseInputs,
	cfg ApplyPhaseConfig,
	taskCtx context.Context,
	afc *ApplyFileContext,
	result *workflow.ApplyResult,
	applyErr error,
) applyFileOutcome {
	outcome := applyFileOutcome{
		FilePath: filePath,
		MovieID:  movie.ID,
	}

	afr := &ApplyFileResult{
		Result: result,
		Err:    applyErr,
	}

	if cfg.PostApplyFunc != nil {
		cfg.PostApplyFunc(taskCtx, afc, afr)
	}

	if applyErr != nil {
		errMsg := applyErr.Error()
		if errors.Is(applyErr, context.DeadlineExceeded) {
			errMsg = fmt.Sprintf("apply timed out after %v", applyTimeout)
		}
		// A mid-apply cancellation is not an organize failure: the file was
		// scraped successfully, just not organized. Mirror scrape_phase.go and
		// preserve the Cancelled status (old OrganizeTask returned the error to
		// the pool without mutating the per-file FileResult, so cancelled-but-
		// scraped files stayed Completed). Main's process_organize.go likewise
		// did not relabel them Failed.
		fileStatus := models.JobStatusFailed
		isCancelled := errors.Is(applyErr, context.Canceled)
		if isCancelled {
			fileStatus = models.JobStatusCancelled
			errMsg = "organization canceled"
		}
		now := time.Now()
		// Preserve the prior scrape-phase Movie on the apply-failure path.
		// Main's process_organize.go returned early on organizeErr WITHOUT
		// mutating the per-file FileResult, so the Movie that the scrape
		// phase populated survived for inspection/retry on failed-apply rows.
		// UpdateFileResult replaces the whole struct (preserving only ResultID
		// + Revision), so without Movie set here, the API response for the
		// failed-apply row loses its movie payload and /review/[jobId] can't
		// render the movie card / poster preview. Same dropped-on-failure-path
		// pattern fixed for FileMatchInfo in commit 6249de64.
		inputs.Updater.UpdateFileResult(filePath, &MovieResult{
			FileMatchInfo: afc.Match,
			Movie:         movie,
			Status:        fileStatus,
			Error:         errMsg,
			StartedAt:     startTime,
			EndedAt:       &now,
		})
		if isCancelled {
			// Cancellation is not a failure: broadcast a non-failure apply event
			// and do NOT invoke OnFileFailed, otherwise the review page records
			// the file as failed and offers a Retry path despite the persisted
			// result being Cancelled.
			inputs.Broadcaster.Send(JobEvent{
				JobID:     inputs.JobID,
				MovieID:   movie.ID,
				Phase:     jobEventPhaseApply,
				Step:      StepApply,
				Message:   errMsg,
				Timestamp: time.Now(),
			})
			outcome.Cancelled = true
			outcome.ErrorMsg = errMsg
			return outcome
		}
		inputs.Broadcaster.Send(JobEvent{
			JobID:     inputs.JobID,
			MovieID:   movie.ID,
			Phase:     jobEventPhaseApply,
			Step:      StepFailed,
			Message:   fmt.Sprintf("Apply failed: %v", applyErr),
			Timestamp: time.Now(),
		})
		// Broadcast per-file failure over WebSocket so the frontend's fileStatuses
		// map records the failure and OrganizeStatusCard can offer a Retry path.
		// Mirrors main's process_organize.go per-file 'failed' WS message.
		if cfg.OnFileFailed != nil {
			cfg.OnFileFailed(filePath, errMsg)
		}
		outcome.Failed = true
		outcome.ErrorMsg = errMsg
		return outcome
	}

	if result != nil && result.Movie != nil {
		if err := inputs.Updater.AtomicUpdateFileResult(filePath, func(current *MovieResult) (*MovieResult, error) {
			current.Movie = result.Movie.Clone()
			return current, nil
		}); err != nil {
			logging.Warnf("Failed to update movie result for %s after apply: %v", filePath, err)
		}
		outcome.Movie = result.Movie
	}

	inputs.Broadcaster.Send(JobEvent{
		JobID:     inputs.JobID,
		MovieID:   movie.ID,
		Phase:     jobEventPhaseApply,
		Step:      StepComplete,
		Progress:  1.0,
		Message:   fmt.Sprintf("Applied %s successfully", movie.ID),
		Timestamp: time.Now(),
	})
	// Broadcast per-file success over WebSocket so the frontend's fileStatuses
	// map populates per file and OrganizeStatusCard renders live per-file rows.
	// Mirrors main's process_organize.go per-file 'organized' WS message.
	if cfg.OnFileOrganized != nil {
		cfg.OnFileOrganized(filePath)
	}
	outcome.Success = true
	return outcome
}

// applyFile handles the per-file apply logic: build ApplyCmd, execute workflow.Apply,
// interpret result. Error handling, panic recovery, and result tracking are performed here.
func applyFile(
	egCtx context.Context,
	wf workflow.WorkflowInterface,
	filePath string,
	fileResult *MovieResult,
	movie *models.Movie,
	inputs applyPhaseInputs,
	cfg ApplyPhaseConfig,
) (outcome applyFileOutcome) {
	startTime := time.Now()

	// Fire the per-file organize-start hook BEFORE any work begins on this file,
	// so the frontend's "Current Activity" card shows which file is being
	// organized (verbose organize progress). Nil-guarded; safe to call
	// concurrently from worker goroutines (the WS broadcaster is goroutine-safe).
	if cfg.OnFileOrganizeStart != nil {
		cfg.OnFileOrganizeStart(filePath)
	}

	// outcome is a NAMED return so the deferred withFileRecovery(rc, &outcome)
	// mutates the value the caller actually receives. With an unnamed return,
	// a recovered panic would leave the caller with the zero-value outcome
	// (Failed/Panic both false), so a panicking file would be counted as
	// neither organized nor failed by trackApplyResults — and the job would
	// wrongly MarkOrganized. Naming the return closes that hole: setPanic now
	// writes Failed/Panic onto the returned value.
	outcome = applyFileOutcome{
		FilePath: filePath,
		MovieID:  movie.ID,
	}

	rc := recoveryContext{
		filePath: filePath,
		// Preserve the existing FileMatchInfo (incl. IsMultiPart / PartNumber /
		// PartSuffix set by the earlier discovery/scrape phases) on the panic
		// path. Constructing a fresh struct here would silently zero multipart
		// metadata for any file that panicked mid-apply, so /review/[jobId]
		// would then show the file as single-part.
		fmi:       fileResult.FileMatchInfo,
		movie:     fileResult.Movie,
		updater:   inputs.Updater,
		broadcast: broadcastFailure(inputs.Broadcaster, inputs.JobID, movie.ID, jobEventPhaseApply, "Apply"),
		startTime: startTime,
	}
	defer withFileRecovery(rc, &outcome)()

	applyTimeout := inputs.Concurrency.WorkerTimeout
	taskCtx := egCtx
	var taskCancel context.CancelFunc
	if applyTimeout > 0 {
		taskCtx, taskCancel = context.WithTimeout(egCtx, applyTimeout)
		defer taskCancel()
	}

	// Step 1: Build the ApplyCmd.
	applyCmd, afc, shouldExecute := buildApplyCmd(filePath, movie, fileResult, inputs, cfg, taskCtx)
	if !shouldExecute {
		return outcome
	}

	// Step 2: Execute the workflow.Apply.
	progressFn := makeProgressFn(inputs.Broadcaster, inputs.JobID, movie.ID, jobEventPhaseApply)

	result, applyErr := wf.Apply(taskCtx, applyCmd, progressFn)

	// Step 3: Interpret the result.
	return interpretApplyResult(filePath, movie, startTime, applyTimeout, inputs, cfg, taskCtx, afc, result, applyErr)
}

// trackApplyResults processes collected applyFileOutcomes: increments counters
// for organized/failed. The actual Updater/Broadcaster calls are already done
// inside applyFile; this function only handles the aggregate counters.
func trackApplyResults(outcomes []applyFileOutcome, organized *int64, failed *int64) {
	for _, o := range outcomes {
		if o.Success {
			atomic.AddInt64(organized, 1)
		}
		// Count panics as failures too. Currently setPanic() sets both Panic
		// and Failed, so the || o.Panic is defensive — it future-proofs against
		// changes to setPanic that might set only Panic without Failed.
		if o.Failed || o.Panic {
			atomic.AddInt64(failed, 1)
		}
	}
}
