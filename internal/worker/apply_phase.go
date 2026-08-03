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
		Movie:               movie,
		Match:               match,
		DestPath:            destPath,
		DryRun:              cfg.DryRun,
		Organize:            cfg.OrganizeOptions,
		Merge:               cfg.MergeOptions,
		Download:            cfg.Download,
		DisplayTitleSrc:     movie,
		DownloadExtrafanart: cfg.DownloadExtrafanart,
		OperationMode:       cfg.OperationModeOverride,
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
// and runs the PostApply hook if configured. applyCmd is the command the
// physical apply ran with; the mid-apply poster-drift repair pass
// (Codex P2-B) re-issues it scoped down to the poster write.
func interpretApplyResult(
	filePath string,
	movie *models.Movie,
	startTime time.Time,
	applyTimeout time.Duration,
	inputs applyPhaseInputs,
	cfg ApplyPhaseConfig,
	taskCtx context.Context,
	afc *ApplyFileContext,
	applyCmd workflow.ApplyCmd,
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
		//
		// Preserve the LIVE result's poster state on the apply-failure
		// write-back too: the snapshot `movie` pre-dates any crop/source edit
		// taken while the organize ran, and replacing the whole result with
		// it would silently erase a mid-organize manual crop. The write
		// therefore runs UNDER the live movie's poster-source lock (so no
		// crop/edit can interleave between the key resolution and the write)
		// and the atomic read-modify-write merges the live poster fields
		// onto the snapshot under the store lock. On an identity mismatch —
		// a mid-apply edit RE-KEYED the live result (P2-5) — the write-back
		// updates only the non-movie fields the pipeline owns
		// (status/error/timestamps) and keeps the live movie wholesale:
		// stamping A's snapshot over B's live movie would build a
		// franken-result. If the result vanished mid-apply (atomic update
		// errors), fall back to the legacy wholesale write so the failure
		// status is still recorded (nothing live remains to clobber).
		snapshotID := ""
		if movie != nil {
			snapshotID = movie.ID
		}
		// The release is deferred INSIDE the closure: a panic in the store
		// callback would otherwise leak the refcounted lock entry permanently
		// (the map entry never evicts → every later writer on this key
		// deadlocks). The panic still propagates to withFileRecovery, which
		// re-acquires the same key — that only works because it is free.
		writeErr := func() error {
			releasePosterLock := AcquirePosterSourceLock(inputs.JobID.String(), liveWritebackLockKey(inputs.Updater, filePath, snapshotID))
			defer releasePosterLock()
			return inputs.Updater.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
				if movie == nil || (current.Movie != nil && current.Movie.ID != movie.ID) {
					// Identity mismatch (mid-apply rekey) or no snapshot movie: do
					// NOT overwrite the live movie. Only pipeline-owned non-movie
					// fields move.
					current.Status = fileStatus
					current.Error = errMsg
					current.StartedAt = startTime
					current.EndedAt = &now
					return current, nil
				}
				merged := movie.Clone()
				mergeLivePosterState(merged, current.Movie)
				return &resultstore.MovieResult{
					// UpdateFileResult preserved ResultID on wholesale writes;
					// the atomic callback must carry it over explicitly or review
					// lookups keyed by result_id break for failed applies.
					ResultID:      current.ResultID,
					FileMatchInfo: afc.Match,
					Movie:         merged,
					Status:        fileStatus,
					Error:         errMsg,
					StartedAt:     startTime,
					EndedAt:       &now,
				}, nil
			})
		}()
		if writeErr != nil {
			logging.Warnf("Failed to atomically update movie result for %s after apply failure: %v", filePath, writeErr)
			inputs.Updater.UpdateFileResult(filePath, &resultstore.MovieResult{
				FileMatchInfo: afc.Match,
				Movie:         movie,
				Status:        fileStatus,
				Error:         errMsg,
				StartedAt:     startTime,
				EndedAt:       &now,
			})
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
		merged, drifted := writeBackSuccessMovie(inputs, filePath, result.Movie)
		outcome.Movie = result.Movie

		// Mid-apply poster edit (Codex P2-B): a crop, poster-from-URL, or
		// source edit that committed after wf.Apply captured its snapshot but
		// before the write-back is MERGED above — the envelope now carries the
		// new poster state, while the physical pass just wrote the poster file
		// and NFO from the OLD snapshot. The file is about to be reported
		// organized with on-disk output no subsequent organize would fix (the
		// persisted state believes it is already applied). Holding the poster
		// lock across the whole physical apply was rejected as too coarse:
		// downloads run minutes under it while crop/edit HTTP requests wait.
		// Instead, repair BEFORE reporting success: re-run the apply pass with
		// organize skipped and the MERGED (live-poster) movie, so the poster
		// file and NFO are rewritten from the state the envelope persists —
		// then re-check drift under the lock (an edit committed mid-repair
		// re-triggers; the loop is bounded, see the helper).
		// merged is non-nil exactly when the write-back stored a movie; drift
		// can only be detected in that same critical section, so drifted
		// implies merged != nil here (a skipped/failed write-back reports no
		// drift and skips the repair).
		if drifted {
			result = repairMidApplyPosterDrift(taskCtx, inputs, cfg, filePath, applyCmd, afc, result, merged)
			outcome.Movie = result.Movie
		}
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
		jobID:    inputs.JobID,
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
	reporter := makeProgressReporter(inputs.Broadcaster, inputs.JobID, movie.ID, jobEventPhaseApply)
	// Inject the reporter into taskCtx (which carries the worker timeout /
	// errgroup cancellation) so downstream emitters resolve it via
	// progress.FromContext. Use taskCtx, not the parent egCtx.
	taskCtx = progress.WithReporter(taskCtx, reporter)

	result, applyErr := wf.Apply(taskCtx, applyCmd)

	// Step 3: Interpret the result.
	return interpretApplyResult(filePath, movie, startTime, applyTimeout, inputs, cfg, taskCtx, afc, applyCmd, result, applyErr)
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

// liveWritebackLockKey resolves the poster-source lock key a pipeline
// write-back must serialize under: the LIVE result's movie ID when readable
// through the updater (a mid-flight edit may have re-keyed the result since
// the pipeline's snapshot — the write-back would otherwise hold a lock that
// serializes against NOTHING the live movie's writers take) with the
// snapshot's ID as fallback for updater seams without read access (test
// stubs) or results without a movie. Callers then hold the lock across the
// atomic update and re-check identity inside it (both merge call sites
// guard on live-vs-write-back ID mismatch and keep the live movie then).
func liveWritebackLockKey(updater resultstore.ResultUpdater, filePath, snapshotID string) string {
	if reader, ok := updater.(resultstore.ResultMapAccessor); ok {
		if live, err := reader.GetMovieResult(filePath); err == nil && live != nil && live.Movie != nil && live.Movie.ID != "" {
			return live.Movie.ID
		}
	}
	return snapshotID
}

// mergeLivePosterState overlays the LIVE result's poster identity onto a
// write-back movie that was cloned from an older pipeline snapshot — the
// apply-phase write-backs (apply-start snapshot; F-C) and the scrape persist
// pool's DB round-trip (F-D; CropBounds is gorm:"-", so the saved movie lost
// the runtime-only bounds). A manual crop, poster-from-URL, or source edit
// that landed while the pipeline ran must not be erased by a stale-snapshot
// write-back: the apply-phase callers read `live` inside the store's
// AtomicUpdateFileResult callback, so the merge is atomic with every other
// result-store poster mutation and needs no additional lock.
//
// Only the mutable poster fields move: the effective source pair
// (PosterURL ?? CoverURL — the downloader's resolution), the recorded crop
// decision (ShouldCropPoster), the carried CropBounds, and the cached preview
// URL (CroppedPosterURL). Everything else on dst (organized/merged pipeline
// output: titles, paths, genres...) is authoritative and never regressed.
// The Original* reset-baseline group moves too: it is captured against the
// SAME stored movie the mutable group was edited on (lazily by the first
// manual edit via backupPosterOriginals/backupCoverOriginal, eagerly at
// scrape/rescrape), and no pipeline write-back path produces a newer one —
// the NFO format carries no original_* fields and dst's group is always the
// older snapshot's. Keeping dst's would let a mid-flight edit's freshly
// captured baseline be overwritten by the stale snapshot (Reset losing its
// restore target) while its edited poster fields survive — an inconsistent
// pairing.
//
// Lock ordering: this function takes NO poster-source lock — apply callers
// hold none of these locks, and the two-lock rule reserves multi-lock
// acquisition for the rescrape rekey path.
func mergeLivePosterState(dst, live *models.Movie) {
	if dst == nil || live == nil {
		return
	}
	// Identity guard: a mid-flight edit can RE-KEY the live result (a
	// rescrape or whole-movie edit committing a corrected match rewrites
	// Movie.ID from A to B while a pipeline write-back cloned from A's
	// snapshot is still in flight). Merging B's poster identity onto A's
	// write-back would build a franken-movie — A's metadata wearing B's
	// poster — so on mismatch skip the merge and let the write-back carry
	// its own (snapshot) poster identity.
	if live.ID != dst.ID {
		logging.Debugf("mergeLivePosterState: skipping live poster merge — live movie %q != write-back %q (mid-flight rekey)", live.ID, dst.ID)
		return
	}
	dst.Poster.PosterURL = live.Poster.PosterURL
	dst.Poster.CoverURL = live.Poster.CoverURL
	dst.Poster.ShouldCropPoster = live.Poster.ShouldCropPoster
	dst.Poster.CroppedPosterURL = live.Poster.CroppedPosterURL
	if live.Poster.CropBounds != nil {
		b := *live.Poster.CropBounds
		dst.Poster.CropBounds = &b
	} else {
		dst.Poster.CropBounds = nil
	}
	dst.Poster.OriginalPosterURL = live.Poster.OriginalPosterURL
	dst.Poster.OriginalCroppedPosterURL = live.Poster.OriginalCroppedPosterURL
	dst.Poster.OriginalCoverURL = live.Poster.OriginalCoverURL
	if live.Poster.OriginalShouldCropPoster != nil {
		b := *live.Poster.OriginalShouldCropPoster
		dst.Poster.OriginalShouldCropPoster = &b
	} else {
		dst.Poster.OriginalShouldCropPoster = nil
	}
}

// writeBackSuccessMovie persists the pipeline-updated movie of a SUCCESSFUL
// physical apply pass onto the file result, letting the LIVE result's poster
// state win over the pass snapshot (see mergeLivePosterState) — a manual crop
// or poster/source edit taken while this file was being applied must not be
// clobbered, and an edit that RE-KEYED the result mid-apply (live identity
// differs) keeps its whole movie: the write-back touches nothing then (P2-5).
//
// The write runs UNDER the live key's poster-source lock so key resolution
// and write form one critical section against crop/edit writers. It returns
// the merged movie that was stored (nil when the write-back was skipped) and
// whether the live poster state DRIFTED from the poster state `applied`
// carried into the physical pass — the drift signal Codex P2-B's repair pass
// keys on: drift means the poster file/NFO just written describe the OLD
// snapshot while the envelope now persists the NEW one.
//
// Deferred release INSIDE the closure (L1): a panic in the store callback
// must not leak the refcounted lock entry — a leaked entry never evicts and
// deadlocks every later writer on this key.
func writeBackSuccessMovie(inputs applyPhaseInputs, filePath string, applied *models.Movie) (merged *models.Movie, drifted bool) {
	releasePosterLock := AcquirePosterSourceLock(inputs.JobID.String(), liveWritebackLockKey(inputs.Updater, filePath, applied.ID))
	defer releasePosterLock()
	if err := inputs.Updater.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		if current.Movie != nil && current.Movie.ID != applied.ID {
			logging.Debugf("apply write-back skipped for %s — live movie %q re-keyed away from pipeline movie %q", filePath, current.Movie.ID, applied.ID)
			return current, nil
		}
		next := applied.Clone()
		drifted = posterStateDrifted(applied, current.Movie)
		mergeLivePosterState(next, current.Movie)
		current.Movie = next
		merged = next
		return current, nil
	}); err != nil {
		logging.Warnf("Failed to update movie result for %s after apply: %v", filePath, err)
	}
	return merged, drifted
}

// posterStateDrifted reports whether the LIVE movie's physically-applied
// poster state differs from the state the apply pass ran with. Only the
// fields that decide the on-disk poster bytes count: the effective source
// pair (PosterURL ?? CoverURL — the downloader's resolution), the crop intent
// (ShouldCropPoster), and the carried bounds. The envelope-only group
// (CroppedPosterURL preview pointer, Original* reset baseline) never reaches
// disk inside apply, so changes there alone do not repair-trigger. Identity
// mismatch is NOT drift: the live movie belongs to a different identity
// (mid-flight rekey) and the write-back already skipped the merge — the
// rekeyed family's own writers own its disk representation.
func posterStateDrifted(applied, live *models.Movie) bool {
	if applied == nil || live == nil || applied.ID != live.ID {
		return false
	}
	a, l := applied.Poster, live.Poster
	if a.PosterURL != l.PosterURL || a.CoverURL != l.CoverURL || a.ShouldCropPoster != l.ShouldCropPoster {
		return true
	}
	if a.CropBounds == nil || l.CropBounds == nil {
		return a.CropBounds != l.CropBounds
	}
	return *a.CropBounds != *l.CropBounds
}

// maxPosterDriftRepairPasses bounds the drift-repair loop: each pass re-runs
// the poster write and re-checks drift under the poster lock, so a crop/edit
// committed mid-repair triggers one more pass. The bound protects against
// edit storms (and merge configurations whose NFO-preserve strategy keeps
// resurrecting the older poster fields); past the bound the file is left
// organized with a warning — the persisted envelope is authoritative, and a
// re-organize converges the disk, exactly as with any post-organize edit.
const maxPosterDriftRepairPasses = 3

// repairMidApplyPosterDrift rewrites the drifted physical poster output of a
// just-organized file BEFORE the success is reported (Codex P2-B). The
// first, already-finished pass organized/DOWNLOADED/NFO'd the file from the
// apply-start snapshot; by write-back time a crop/poster edit had landed, so
// that output is stale. Each repair pass re-issues wf.Apply with organize
// skipped (the file is already at its destination; Match.Path is repointed at
// the moved path so NFO stream details still resolve) and the MERGED movie —
// pipeline fields authoritative, poster fields live — with ForcePosterReplace
// set: drift proves the installed poster predates the effective source, and
// with nil CropBounds the downloader's exists-skip would otherwise keep it
// (Codex P2-A). The pass's own
// snapshot→write-back window is closed by re-running writeBackSuccessMovie:
// an edit committed mid-repair re-detects drift and triggers another pass,
// up to maxPosterDriftRepairPasses.
//
// Nothing here runs under the poster-source lock (the lock spans only the
// write-backs), so repair passes never extend the critical section across
// network downloads — the rejected alternative (holding the lock across the
// physical apply) would have blocked crop/edit requests for the whole
// download window. The last ApplyResult is returned so outcome/broadcast
// state reflects the pass whose movie the envelope finally persisted; on a
// repair failure the EARLIER result stands (the file IS organized — only its
// poster output lags the live edit, logged for a re-organize).
func repairMidApplyPosterDrift(
	taskCtx context.Context,
	inputs applyPhaseInputs,
	cfg ApplyPhaseConfig,
	filePath string,
	applyCmd workflow.ApplyCmd,
	afc *ApplyFileContext,
	lastResult *workflow.ApplyResult,
	merged *models.Movie,
) *workflow.ApplyResult {
	if applyCmd.DryRun || inputs.WF == nil || (!cfg.Download && !applyCmd.GenerateNFO) {
		// Nothing physical to rewrite: a dry-run touched no bytes, and
		// without download/NFO generation the pass produced no poster-bearing
		// artifacts — the persist merge alone is the whole fix. (inputs.WF
		// nil only in unit harnesses that drive interpretApplyResult directly.)
		return lastResult
	}
	for pass := 1; pass <= maxPosterDriftRepairPasses; pass++ {
		repairCmd := applyCmd
		repairCmd.Movie = merged.Clone()
		repairCmd.Organize.Skip = true
		repairCmd.Match = afc.Match
		// Force the poster rewrite to REPLACE the destination the first pass
		// installed (Codex P2-A): drift with a changed effective source and
		// nil CropBounds would otherwise hit the downloader's exists-skip
		// and report success while leaving the organized poster on the OLD
		// image the envelope no longer references.
		repairCmd.ForcePosterReplace = true
		if lastResult.OrganizeResult != nil {
			if lastResult.OrganizeResult.NewPath != "" {
				repairCmd.Match.Path = lastResult.OrganizeResult.NewPath
			}
			if lastResult.OrganizeResult.FolderPath != "" {
				repairCmd.DestPath = lastResult.OrganizeResult.FolderPath
			}
		}
		logging.Infof("apply: poster state changed mid-apply for %s — re-running the poster write from the live state (repair pass %d/%d)", filePath, pass, maxPosterDriftRepairPasses)
		res, err := inputs.WF.Apply(taskCtx, repairCmd)
		if err != nil {
			logging.Warnf("apply: poster-drift repair pass %d for %s failed: %v (on-disk poster/NFO lag the mid-apply edit; re-organize to converge)", pass, filePath, err)
			return lastResult
		}
		if res == nil || res.Movie == nil {
			logging.Warnf("apply: poster-drift repair pass %d for %s returned no movie (on-disk poster/NFO lag the mid-apply edit; re-organize to converge)", pass, filePath)
			return lastResult
		}
		lastResult = res
		nextMerged, stillDrifted := writeBackSuccessMovie(inputs, filePath, res.Movie)
		if nextMerged == nil {
			// Live result re-keyed mid-repair (or the store write failed):
			// the rekeyed family owns its disk representation — stop.
			return lastResult
		}
		if !stillDrifted {
			return lastResult
		}
		merged = nextMerged
	}
	logging.Warnf("apply: poster state still drifting after %d repair passes for %s (on-disk poster/NFO lag the latest edit; re-organize to converge)", maxPosterDriftRepairPasses, filePath)
	return lastResult
}
