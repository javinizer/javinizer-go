package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/panicutil"
	"github.com/javinizer/javinizer-go/internal/progress"
	"github.com/javinizer/javinizer-go/internal/worker/fanout"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// ApplyPhase runs the apply (organize) step over a batch job's scraped files.
type ApplyPhase interface {
	Run(ctx context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig)
}

type applyPhase struct{}

// NewApplyPhase returns the default ApplyPhase implementation.
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
	DryRun    bool          // true if apply was a dry-run
}

// recoverRunPanic marks the job failed only when a panic recovered in Run's
// defer counciled the main body (never nil on normal exit).
func recoverRunPanic(inputs applyPhaseInputs, r any) {
	if r == nil {
		return
	}
	panicErr := panicutil.FormatRecover(r)
	logging.Errorf("BatchJob.StartApply %s %v", inputs.JobID.String(), panicErr)
	inputs.Lifecycle.MarkFailed()
}

// Run executes the apply phase: setup errgroup → iterate files → dispatch
// applyFile → collect outcomes → track results → report status.
func (p *applyPhase) Run(ctx context.Context, inputs applyPhaseInputs, cfg ApplyPhaseConfig) {
	wf := inputs.WF
	persister := inputs.persister

	defer func() {
		// Extraction keeps the recovery arm testable: fanout workers forward
		// panics, so ONLY main-body panics reach this defer — and a named fn is
		// the honest seam for exercising MarkFailed on phase panic (codex P2-E).
		recoverRunPanic(inputs, recover())
		if persister != nil {
			if err := persister.Persist(); err != nil {
				logging.Warnf("[Apply] envelope persist failed: %v", err)
			}
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
		fileResult *resultstore.MovieResult
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
	inputs.Dedup = &sync.Map{}
	outcomes := fanout.BoundedFanOut(ctx, inputs.Concurrency.MaxWorkers, items,
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
		var org, fail int64
		trackApplyResults(inputs, outcomes, &org, &fail)
		inputs.Lifecycle.MarkCancelled()
		return
	}

	var organized int64
	var failed int64
	trackApplyResults(inputs, outcomes, &organized, &failed)

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
// applyFamilyLock serializes an apply write-back against review edits on ALL
// identity keys (codex r37 P2): edits lock the MATCHER alias
// (FileMatchInfo.MovieID), while buildApplyCmd rewrote afc.Match.MovieID to
// the canonical Movie.ID — applying under the canonical key alone lets the
// atomic write-back run between an edit's candidate snapshot and its
// post-transaction publication on the alias key.
//
// codex r42 P2: the lock acquisition MUST route through the registry's ONE
// total order (AcquireMany folds keys uppercase then sorts). Any caller-side
// ordering rule reproduces a DIFFERENT comparison (e.g. lowercase sort
// orders "z" AFTER "_" while the fold orders "Z" BEFORE "_") — a concurrent
// multi-key review edit then deadlocks against this write-back.
func applyFamilyLock(inputs applyPhaseInputs, aliasID, canonicalID string) func() {
	if inputs.EditLockFn == nil {
		return func() {}
	}
	return inputs.EditLockFn(aliasID, canonicalID)
}

// applyFamilyKeyIDs extracts the matcher alias (edit-lock identity) and the
// canonical Movie.ID (apply-rewritten identity) for a write-back.
func applyFamilyKeyIDs(afc *ApplyFileContext) (aliasID, canonicalID string) {
	canonicalID = afc.Match.MovieID
	if afc.MovieResult != nil {
		aliasID = afc.MovieResult.FileMatchInfo.MovieID
	}
	return aliasID, canonicalID
}

func buildApplyCmd(
	filePath string,
	movie *models.Movie,
	fileResult *resultstore.MovieResult,
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
	// In-place modes (organize runs but no destination is required) must fall
	// back to the source dir so downstream steps (download/NFO) resolve a real
	// directory. The previous gate on cfg.OrganizeOptions.Skip is stale: the
	// skip-gate fix (!RequiresOrganize()) means in-place modes now run organize
	// with Skip=false, so an empty destination would otherwise stay empty.
	// Gate on the effective override mode requiring organize instead of Skip.
	if destPath == "" {
		if mode := cfg.OperationModeOverride; mode != "" && mode.RequiresOrganize() {
			destPath = sourceDir
		} else if cfg.OrganizeOptions.Skip {
			destPath = sourceDir
		}
	}

	applyCmd := workflow.ApplyCmd{
		Movie:                  movie,
		Match:                  match,
		DestPath:               destPath,
		DryRun:                 cfg.DryRun,
		Organize:               cfg.OrganizeOptions,
		Merge:                  cfg.MergeOptions,
		Download:               cfg.Download,
		DisplayTitleSrc:        movie,
		DownloadExtrafanart:    cfg.DownloadExtrafanart,
		OverwriteExistingMedia: cfg.OverwriteExistingMedia,
		Dedup:                  inputs.Dedup,
		OperationMode:          cfg.OperationModeOverride,
	}

	applyCmd.GenerateNFO = cfg.GenerateNFO && (inputs.NFOEnabled || cfg.ForceNFO)

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
		// Live-session merge (not whole-struct replace): the review-editable
		// set keeps the LIVE value when the user edited it mid-phase
		// (codex P6-B); phase-computed fields still come from the frozen
		// movie. Falls back to whole-struct write when no result exists yet
		// (early-fail paths in phase tests create rows on demand).
		// Serialize with concurrent review edits (codex r11 P1): publication of
		// the merged write-back happens under the movie family key.
		// codex r37 P2: lock BOTH identity keys — edits hold the matcher
		// alias while afc.Match.MovieID was rewritten to the canonical ID.
		aliasID, canonicalID := applyFamilyKeyIDs(afc)
		unlock := applyFamilyLock(inputs, aliasID, canonicalID)
		defer unlock()

		// codex P2-C/D: settled-rekey skip runs UNDER the family key; the
		// callback's mismatch return is the net for rekeys landing mid-write.
		// codex P2: fence the FAILURE write-back (and panic-converted failures
		// land here too) behind outstanding promote witnesses — its revision
		// bump would make startup arbitrate the failed refresh as committed.
		if mid := strings.TrimSpace(movie.ID); mid != "" && inputs.PromoteWitnessFn != nil && inputs.PromoteWitnessFn(mid) {
			logging.Warnf("[Apply] skipping failure write-back for %s — promote witness for %s unresolved; restart reconciles", filePath, mid)
		} else if !writebackPreSkipped(inputs.Updater, movie, filePath, "Apply") {
			// R10-6: state+provenance publish under ONE acquisition — a persist
			// snapshot between the two never observes mismatched halves.
			errUp := inputs.Updater.AtomicUpdateFileResultWithProvenance(filePath, func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
				if applyWritebackIdentityMismatch(movie, current) {
					logging.Warnf("[Apply] skipping write-back for %s — result rekeyed to %s mid-phase", filePath, current.FileMatchInfo.MovieID)
					return current, prov, nil
				}
				fm := applyMatchFollowedByLiveIdentity(afc.Match, current)
				current.FileMatchInfo = fm
				current.Movie = mergeLiveReviewEdits(movie, movie, current.Movie)
				current.Status = fileStatus
				current.Error = errMsg
				current.StartedAt = startTime
				current.EndedAt = &now
				return current, mergeWriteBackProvenance(nil, prov), nil
			})
			if errUp != nil {
				inputs.Updater.UpdateFileResult(filePath, &resultstore.MovieResult{
					FileMatchInfo: afc.Match,
					Movie:         movie,
					Status:        fileStatus,
					Error:         errMsg,
					StartedAt:     startTime,
					EndedAt:       &now,
				})
			}
		}

		if isCancelled {
			if result != nil && result.OrganizeResult != nil {
				auditOrganizeSuccess(inputs, movie, filePath, result, cfg)
			}
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
		if !cfg.OrganizeOptions.Skip || (result != nil && result.OrganizeResult != nil) {
			auditOrganizeFailure(inputs, movie, filePath, result, applyErr, cfg)
		}
		return outcome
	}

	if result != nil && result.Movie != nil {
		// codex r37 P2: lock BOTH identity keys — edits hold the matcher alias.
		aliasID, canonicalID := applyFamilyKeyIDs(afc)
		unlock := applyFamilyLock(inputs, aliasID, canonicalID)
		defer unlock()
		// codex P2-C/D: settled-rekey skip runs UNDER the family key.
		if !writebackPreSkipped(inputs.Updater, movie, filePath, "Apply") {
			if mid := result.Movie.ID; mid != "" && inputs.PromoteWitnessFn != nil && inputs.PromoteWitnessFn(mid) {
				// codex P2: an unresolved promote witness arbitrates at startup by
				// revision — this write-back bumps it, which would declare a
				// failed refresh committed and discard its recovery state.
				logging.Warnf("[Apply] skipping success write-back for %s — promote witness for %s unresolved; restart reconciles", filePath, mid)
			} else {
				err2 := inputs.Updater.AtomicUpdateFileResultWithProvenance(filePath, func(current *resultstore.MovieResult, prov *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
					if applyWritebackIdentityMismatch(movie, current) {
						logging.Warnf("[Apply] skipping success write-back for %s — result rekeyed to %s mid-phase", filePath, current.FileMatchInfo.MovieID)
						return current, prov, nil
					}
					current.Movie = mergeLiveReviewEdits(movie, result.Movie, current.Movie)
					return current, mergeWriteBackProvenance(nil, prov), nil
				})
				if err2 != nil {
					logging.Warnf("Failed to update movie result for %s after apply: %v", filePath, err2)
				}
			}
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
	auditOrganizeSuccess(inputs, movie, filePath, result, cfg)
	return outcome
}

// applyFile handles the per-file apply logic: build ApplyCmd, execute workflow.Apply,
// interpret result. Error handling, panic recovery, and result tracking are performed here.
func applyFile(
	egCtx context.Context,
	wf workflow.WorkflowInterface,
	filePath string,
	fileResult *resultstore.MovieResult,
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
		DryRun:   cfg.DryRun,
	}

	rc := recoveryContext{
		filePath: filePath,
		// Preserve the existing FileMatchInfo (incl. IsMultiPart / PartNumber /
		// PartSuffix set by the earlier discovery/scrape phases) on the panic
		// path. Constructing a fresh struct here would silently zero multipart
		// metadata for any file that panicked mid-apply, so /review/[jobId]
		// would then show the file as single-part.
		fmi:              fileResult.FileMatchInfo,
		movie:            fileResult.Movie,
		updater:          inputs.Updater,
		broadcast:        broadcastFailure(inputs.Broadcaster, inputs.JobID, movie.ID, jobEventPhaseApply, "Apply"),
		startTime:        startTime,
		editLockFn:       inputs.EditLockFn,
		promoteWitnessFn: inputs.PromoteWitnessFn,
	}
	defer withFileRecovery(rc, &outcome)()

	applyTimeout := inputs.Concurrency.WorkerTimeout
	taskCtx := egCtx
	var taskCancel context.CancelFunc
	if applyTimeout > 0 {
		taskCtx, taskCancel = context.WithTimeout(egCtx, applyTimeout)
		defer taskCancel()
	}

	// codex r51 P2c: freeze the phase-entry baseline BEFORE the workflow
	// sees the pointer — stepDisplayTitle & friends mutate cmd.Movie/afc.Movie
	// (= the same movie), and a mutated "baseline" misclassifies merges as
	// concurrent review edits, restoring stale fields over the computed ones.
	frozenBaseline := movie.Clone()

	// Step 1: Build the ApplyCmd.
	applyCmd, afc, shouldExecute := buildApplyCmd(filePath, movie, fileResult, inputs, cfg, taskCtx)
	if !shouldExecute {
		return outcome
	}

	// Step 2: Execute the workflow.Apply.
	reporter := makeProgressReporter(inputs.Broadcaster, inputs.JobID, movie.ID, jobEventPhaseApply)
	// Inject the reporter into taskCtx (which carries the worker timeout /
	// errgroup cancellation) so downstream emitters resolve it via
	// progress.FromContext. Use taskCtx, not the parent egCtx.
	taskCtx = progress.WithReporter(taskCtx, reporter)

	result, applyErr := wf.Apply(taskCtx, applyCmd)

	// Step 3: Interpret the result against the FROZEN baseline (workflow
	// permutations may have rewritten fields on the live pointer mid-apply).
	return interpretApplyResult(filePath, frozenBaseline, startTime, applyTimeout, inputs, cfg, taskCtx, afc, result, applyErr)
}

// trackApplyResults processes collected applyFileOutcomes: increments counters
// for organized/failed. The actual Updater/Broadcaster calls are already done
// inside applyFile; this function only handles the aggregate counters.
func trackApplyResults(inputs applyPhaseInputs, outcomes []applyFileOutcome, organized *int64, failed *int64) {
	for _, o := range outcomes {
		if o.Success {
			atomic.AddInt64(organized, 1)
		}
		if o.Failed || o.Panic {
			atomic.AddInt64(failed, 1)
		}
		if o.Panic && !o.Cancelled && !inputs.OrganizeSkipped {
			auditOrganizePanic(inputs, o)
		}
	}
}
