package batch

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// resolveOrganizeApplyConfig takes a parsed OrganizeRequest and returns
// the fully-validated ApplyPhaseConfig. Handlers become thin:
// parse request → call builder → launch. Builder is testable without gin.
func resolveOrganizeApplyConfig(
	rt *core.APIRuntime,
	factory worker.BatchJobFactoryInterface,
	job worker.BatchJobInterface,
	req contracts.OrganizeRequest,
) (worker.ApplyPhaseConfig, error) {
	deps := rt.Deps()
	apiCfg := rt.GetAPIConfig()
	batchCfg := apiCfg.BatchConfig()
	secCfg := apiCfg.SecurityConfig()

	// Re-resolve seam strings with the typed request's fields (overrides generic map).
	resolved, seamErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		OperationMode: func() string {
			if req.OperationMode != "" {
				return req.OperationMode
			}
			return batchCfg.OperationMode
		}(),
		LinkMode: req.LinkMode,
	})
	if seamErr != nil {
		return worker.ApplyPhaseConfig{}, seamErr
	}

	effectiveMode := resolved.OperationMode

	if effectiveMode == operationmode.OperationModePreview {
		return worker.ApplyPhaseConfig{}, fmt.Errorf("Preview mode should use the preview endpoint, not organize") //nolint:staticcheck // intentional: matches existing test expectations
	}

	if effectiveMode == operationmode.OperationModeOrganize {
		if req.Destination == "" {
			return worker.ApplyPhaseConfig{}, fmt.Errorf("destination is required for organize mode")
		}
		if !isDirAllowed(deps.GetFs(), req.Destination, secCfg) {
			return worker.ApplyPhaseConfig{}, fmt.Errorf("Access denied to requested directory") //nolint:staticcheck // intentional: matches existing test expectations
		}
	}

	logging.Infof("Organize job %s: copy_only=%v operation_mode=%q link_mode=%q destination=%q", job.GetID(), req.CopyOnly, req.OperationMode, req.LinkMode, req.Destination)

	applyOpts := factory.NewApplyConfig(
		workflow.OrganizeOptions{
			MoveFiles:   !req.CopyOnly,
			LinkMode:    resolved.LinkMode,
			ForceUpdate: true,
			Skip:        effectiveMode != operationmode.OperationModeOrganize,
		},
		workflow.MergeOptions{
			ForceOverwrite: true,
		},
		req.Destination,
	)
	applyOpts.GenerateNFO = !req.SkipNFO
	applyOpts.Download = !req.SkipDownload
	applyOpts.OperationModeOverride = resolved.OperationMode
	sink := newOrganizeBroadcastSink(rt)
	applyOpts.OnPhaseComplete = makeOrganizeCompleteBroadcaster(job, false /* isUpdate */, sink)
	applyOpts.OnFileProgress = makeOrganizeProgressBroadcaster(job, false /* isUpdate */, sink)
	applyOpts.OnFileOrganizeStart = makeOrganizeFileStartBroadcaster(job, false /* isUpdate */, sink)
	applyOpts.OnFileOrganized = makeOrganizeFileOrganizedBroadcaster(job, false /* isUpdate */, sink)
	applyOpts.OnFileFailed = makeOrganizeFileFailedBroadcaster(job, false /* isUpdate */, sink)
	applyOpts.PostApplyFunc = func(ctx context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
		// Guard: never dereference a nil payload. If the apply context or
		// result is missing required fields, skip emitting this secondary
		// event so the original apply error is preserved instead of being
		// masked by a nil-panic here.
		if afc == nil || afc.Movie == nil || afr == nil {
			return
		}
		emitter := deps.GetEventEmitter()
		if afr.Err != nil && emitter != nil {
			_ = emitter.EmitOrganizeEvent(ctx, "file_move", fmt.Sprintf("Organize failed for %s", afc.Movie.ID), models.SeverityError, map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "error": afr.Err.Error()})
		} else if emitter != nil {
			var newPath string
			if afr.Result != nil && afr.Result.OrganizeResult != nil {
				newPath = afr.Result.OrganizeResult.NewPath
			}
			_ = emitter.EmitOrganizeEvent(ctx, "file_move", fmt.Sprintf("Organized %s", afc.Movie.ID), models.SeverityInfo, map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "file": afc.FilePath, "new_path": newPath})
		}
	}

	return applyOpts, nil
}

// resolveUpdateApplyConfig takes a parsed UpdateRequest and returns
// the fully-validated ApplyPhaseConfig. Handlers become thin:
// parse request → call builder → launch. Builder is testable without gin.
func resolveUpdateApplyConfig(
	rt *core.APIRuntime,
	factory worker.BatchJobFactoryInterface,
	job worker.BatchJobInterface,
	req contracts.UpdateRequest,
) (worker.ApplyPhaseConfig, error) {
	deps := rt.Deps()
	resolvedUpdate, seamErr := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		Preset:         req.Preset,
		ScalarStrategy: req.ScalarStrategy,
		ArrayStrategy:  req.ArrayStrategy,
	})
	if seamErr != nil {
		return worker.ApplyPhaseConfig{}, seamErr
	}

	applyOpts := factory.NewApplyConfig(
		workflow.OrganizeOptions{
			Skip: true,
		},
		workflow.MergeOptions{
			ForceOverwrite: req.ForceOverwrite,
			PreserveNFO:    req.PreserveNFO,
			ScalarStrategy: resolvedUpdate.ScalarStrategy,
			ArrayStrategy:  resolvedUpdate.ArrayStrategy,
		},
		"", // no destination for update
	)
	applyOpts.GenerateNFO = !req.SkipNFO
	applyOpts.Download = !req.SkipDownload
	sink := newOrganizeBroadcastSink(rt)
	applyOpts.OnPhaseComplete = makeOrganizeCompleteBroadcaster(job, true /* isUpdate */, sink)
	applyOpts.OnFileProgress = makeOrganizeProgressBroadcaster(job, true /* isUpdate */, sink)
	applyOpts.OnFileOrganizeStart = makeOrganizeFileStartBroadcaster(job, true /* isUpdate */, sink)
	applyOpts.OnFileOrganized = makeOrganizeFileOrganizedBroadcaster(job, true /* isUpdate */, sink)
	applyOpts.OnFileFailed = makeOrganizeFileFailedBroadcaster(job, true /* isUpdate */, sink)
	applyOpts.PostApplyFunc = func(ctx context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
		// Guard: never dereference a nil payload; skip the secondary event so
		// the original apply error is preserved.
		if afc == nil || afc.Movie == nil || afr == nil {
			return
		}
		emitter := deps.GetEventEmitter()
		if afr.Err != nil && emitter != nil {
			_ = emitter.EmitOrganizeEvent(ctx, "nfo_gen", fmt.Sprintf("Update failed for %s", afc.Movie.ID), models.SeverityError, map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "error": afr.Err.Error()})
		}
	}

	return applyOpts, nil
}

// stampJobCounts enriches a WebSocket ProgressMessage with AUTHORITATIVE
// job-level TotalFiles/Completed/Failed read from job.GetStatus() (a lock-
// protected snapshot — see BatchJob.GetStatus → snapshotFull). It is stamped on
// every emitted message so any latest-message read carries totals.
//
// Frontend consumers (Home "Current Activity" bar, BackgroundJobIndicator,
// ProgressModal) use these instead of inferring totals from message counts —
// that proxy was the iter-6 MAJOR regression (revert 30e6e53f): for organize,
// messagesByFile holds only terminal per-file 'organized'/'updated' messages
// (Progress:100), so finished/total pegged at 100% after the first file. With
// authoritative totals, completed+failed ≤ totalFiles always, so the bar is
// ≤100 and only reaches 100 at completion.
//
// Returns msg (mutated in place) for chaining; nil-safe on both msg and job.
// A nil job (or nil status snapshot) leaves the fields zero (omitted on the
// wire via omitempty), so older/tests paths that pass a stub returning nil are
// unaffected.
func stampJobCounts(msg *websocket.ProgressMessage, job worker.BatchJobInterface) *websocket.ProgressMessage {
	if msg == nil || job == nil {
		return msg
	}
	if status := job.GetStatus(); status != nil {
		msg.TotalFiles = status.TotalFiles
		msg.Completed = status.Completed
		msg.Failed = status.Failed
	}
	return msg
}

// makeOrganizeCompleteBroadcaster returns an OnPhaseComplete hook that emits
// the {status:"organization_completed"|"update_completed", progress:100}
// WebSocket progress message at the end of the apply phase. Restores the
// real-time completion signal dropped when organize moved from main's
// process_organize.go (which called broadcastProgress inline with runtime
// hub access) into the worker apply_phase (which only has the in-process
// jobEventBroadcaster). The frontend's organize-controller primarily
// finalizes on this WS status (handleWebSocketMessage line: '"organization_completed"').
// isUpdate picks the wire status string matching main's contract.
//
// Takes an injected sink (like makeOrganizeProgressBroadcaster) so the closure
// is unit-testable with a recording sink and the sibling factories share a
// uniform sink-injected signature. The resolver supplies the production sink
// via newOrganizeBroadcastSink(rt).
func makeOrganizeCompleteBroadcaster(job worker.BatchJobInterface, isUpdate bool, sink progressSink) func(organized, failed int) {
	return func(organized, failed int) {
		status := websocket.ProgressStatusOrganizeCompleted
		action := "Organized"
		if isUpdate {
			status = websocket.ProgressStatusUpdateCompleted
			action = "Updated"
		}
		sink(stampJobCounts(&websocket.ProgressMessage{
			JobID:    job.GetID(),
			Status:   status,
			Progress: 100,
			Message:  fmt.Sprintf("%s %d files, %d failed", action, organized, failed),
		}, job))
	}
}

// makeOrganizeFileOrganizedBroadcaster returns an OnFileOrganized hook that
// emits a per-file WebSocket ProgressMessage with Status "organized" (or
// "updated" in update mode) and FilePath set, so the frontend's fileStatuses
// map populates per file and OrganizeStatusCard renders live per-file rows.
// Mirrors main's process_organize.go per-file success WS message. Takes an
// injected sink so the closure is unit-testable with a recording sink.
func makeOrganizeFileOrganizedBroadcaster(job worker.BatchJobInterface, isUpdate bool, sink progressSink) func(filePath string) {
	status := websocket.ProgressStatus("organized")
	if isUpdate {
		status = websocket.ProgressStatus("updated")
	}
	return func(filePath string) {
		sink(stampJobCounts(&websocket.ProgressMessage{
			JobID:    job.GetID(),
			FilePath: filePath,
			Status:   status,
			Progress: 100,
		}, job))
	}
}

// makeOrganizeFileStartBroadcaster returns an OnFileOrganizeStart hook that
// emits a per-file WebSocket ProgressMessage at the TOP of applyFile, BEFORE
// any work begins on the file, so the Home "Current Activity" card and
// OrganizeStatusCard show which file is currently being organized (verbose
// organize progress) — not just the aggregate "Organized N of M files" count.
//
// Safety (the certified double-count-safe pattern scrape already uses): the
// message is non-terminal (pending) with Progress 0, so it enters the
// frontend's messagesByFile and counts in computeJobProgress's activeProgress
// (contributing 0) — keeping the bar = finished/total (monotonic). When the
// file completes, the terminal OnFileOrganized/OnFileFailed message
// (Progress:100, status organized/updated/failed) OVERWRITES it in
// messagesByFile (dedup-latest by file_path — see websocket.ts). NEVER set
// Progress:100 here (would falsely peg the bar / break the overwrite contract).
//
// isUpdate selects the action verb ("Updating" vs "Organizing") so the live
// text matches the job mode; the basename is always included. Also stamps
// authoritative job-level counts via stampJobCounts. Takes an injected sink so
// the closure is unit-testable with a recording sink (mirrors the sibling
// per-file broadcasters).
func makeOrganizeFileStartBroadcaster(job worker.BatchJobInterface, isUpdate bool, sink progressSink) func(filePath string) {
	action := "Organizing"
	if isUpdate {
		action = "Updating"
	}
	return func(filePath string) {
		sink(stampJobCounts(&websocket.ProgressMessage{
			JobID:    job.GetID(),
			FilePath: filePath,
			Status:   websocket.ProgressStatusPending,
			Progress: 0,
			Message:  fmt.Sprintf("%s %s", action, filepath.Base(filePath)),
		}, job))
	}
}

// makeOrganizeFileFailedBroadcaster returns an OnFileFailed hook that emits a
// per-file WebSocket ProgressMessage with Status "failed", FilePath set, and
// Error populated, so the frontend's fileStatuses map records the failure and
// OrganizeStatusCard can offer a "Retry Failed" path. Mirrors main's
// process_organize.go per-file failure WS message. Takes an injected sink so
// the closure is unit-testable with a recording sink.
func makeOrganizeFileFailedBroadcaster(job worker.BatchJobInterface, _ bool, sink progressSink) func(filePath, errMsg string) {
	return func(filePath, errMsg string) {
		sink(stampJobCounts(&websocket.ProgressMessage{
			JobID:    job.GetID(),
			FilePath: filePath,
			Status:   websocket.ProgressStatus("failed"),
			Progress: 100,
			Error:    errMsg,
		}, job))
	}
}

// makeScrapeFileScrapedBroadcaster returns an OnFileScraped hook that emits a
// per-file WebSocket ProgressMessage with FilePath set and a success status,
// so the frontend's messagesByFile populates during scrape and ProgressModal
// shows live per-file status. Mirrors main's realtime.ProgressAdapter success
// forwarding. Takes an injected sink so the closure is unit-testable.
func makeScrapeFileScrapedBroadcaster(job worker.BatchJobInterface, sink progressSink) func(filePath, message string) {
	return func(filePath, message string) {
		sink(stampJobCounts(&websocket.ProgressMessage{
			JobID:    job.GetID(),
			FilePath: filePath,
			Status:   websocket.ProgressStatusSuccess,
			Progress: 100,
			Message:  message,
		}, job))
	}
}

// makeScrapeFileFailedBroadcaster returns an OnFileScrapeFailed hook that
// emits a per-file WebSocket ProgressMessage with FilePath + Error set and a
// failure status, so the frontend's messagesByFile records scrape failures.
// Mirrors main's realtime.ProgressAdapter failure forwarding. Takes an injected
// sink so the closure is unit-testable.
func makeScrapeFileFailedBroadcaster(job worker.BatchJobInterface, sink progressSink) func(filePath, errMsg string) {
	return func(filePath, errMsg string) {
		sink(stampJobCounts(&websocket.ProgressMessage{
			JobID:    job.GetID(),
			FilePath: filePath,
			Status:   websocket.ProgressStatusError,
			Progress: 100,
			Error:    errMsg,
		}, job))
	}
}

// makeScrapeStepProgressBroadcaster returns an OnScrapeStepProgress hook that
// emits an incremental per-file WebSocket ProgressMessage with FilePath, a
// non-terminal 'pending' status, partial progress, and the step message, so the
// frontend's messagesByFile updates live per step and ProgressModal active rows
// show step text during scraping (e.g. "Querying scrapers..."). Mirrors main's
// realtime.ProgressAdapter which forwarded every step update to the WS hub.
// Takes an injected sink so the closure is unit-testable.
//
// Scale note: the scrape ProgressFunc reports pct on a 0-1 fraction
// (internal/scrape/scrape.go: 0.2 "Querying scrapers...", 0.7 "Aggregating...",
// 1.0 "Completed"), whereas the WS ProgressMessage.progress and the frontend's
// computeJobProgress both expect a 0-100 percentage (matching main, which
// forwarded overall job progress 0-100). Scale pct*100 here so in-flight
// partials contribute their intended weight to the progress bar and the Home
// "Current Activity" card, instead of ~1/100th.
func makeScrapeStepProgressBroadcaster(job worker.BatchJobInterface, sink progressSink) func(filePath, step string, pct float64, msg string) {
	return func(filePath, step string, pct float64, msg string) {
		sink(stampJobCounts(&websocket.ProgressMessage{
			JobID:    job.GetID(),
			FilePath: filePath,
			Status:   websocket.ProgressStatusPending,
			Progress: pct * 100,
			Message:  msg,
		}, job))
	}
}

// newOrganizeBroadcastSink is the production progressSink used by both
// makeOrganizeProgressBroadcaster and makeOrganizeCompleteBroadcaster: it
// forwards a message to the WebSocket hub via broadcastProgress. Extracted to a
// named helper (rather than an inline closure at each resolver call site) so
// the production broadcast path is a single, unit-testable seam — a regression
// that replaced it with a no-op closure would be caught by the e2e wiring test
// that drives this sink through a real hub to a real client.
func newOrganizeBroadcastSink(rt *core.APIRuntime) progressSink {
	return func(m *websocket.ProgressMessage) {
		broadcastProgress(rt.GetRuntime(), m)
	}
}

// progressSink is the broadcast action makeOrganizeProgressBroadcaster and
// makeOrganizeCompleteBroadcaster drive. It is injected so each closure is
// testable end-to-end without a live WebSocket hub: tests pass a recording sink
// and assert the exact messages that would reach the hub. The production caller
// supplies newOrganizeBroadcastSink(rt), which forwards to broadcastProgress.
//
// This is an internal callback parameter, deliberately a bare function type
// rather than an interface: it lets tests pass `func(m){ got = m }` directly
// with no adapter struct. The package's ProgressBroadcaster interface
// (rescrape_orchestrator.go) is the cross-component contract injected as a
// struct dependency into RescrapeOrchestrator; progressSink is the narrower
// same-package callback for the organize broadcast closures. The two model the
// same action at different layers and coexist intentionally.
//
// Contract: a sink MUST be non-blocking. makeOrganizeProgressBroadcaster holds
// its mutex across the sink call, so a blocking sink would stall every worker
// goroutine in the apply phase. The production sink satisfies this —
// broadcastProgress → hub.Broadcast uses a buffered channel with a
// non-blocking select/default-drop send.
type progressSink func(m *websocket.ProgressMessage)

// makeOrganizeProgressBroadcaster returns an OnFileProgress hook that emits an
// incremental WebSocket ProgressMessage after each file's apply completes, so
// the frontend progress bar advances smoothly across files (0→100) instead of
// jumping straight to 100 on the terminal organization_completed message.
//
// This closes the gap left by makeOrganizeCompleteBroadcaster: the apply phase
// produces granular per-file completion, but only the terminal hook reached the
// WS hub, so the bar sat at 0% for the whole run then snapped to 100%. The
// per-file step progress (organize/download/NFO) still flows only to the
// in-process jobEventBroadcaster (consumed by TUI/CLI); the web flow gets
// per-file aggregate throughput here.
//
// Scope note: this hook drives the progress BAR only. Per-file success/failure
// STATUS (the frontend's organized/updated/failed branches keyed on file_path)
// still reaches the web UI via the polling fallback in organize-controller, not
// the WS hub — the per-file JobEvent{StepComplete/Failed} goes to the in-process
// broadcaster only. Do not assume the WS path is exhaustive for row status.
//
// Monotonic delivery: worker goroutines invoke this concurrently and the WS hub
// delivers from a buffered channel, so without serialization a higher count
// could be delivered before a lower one, momentarily regressing the bar. The
// high-water check and the sink call are both performed under a single mutex,
// so a message is only emitted when its processed count strictly exceeds every
// previously-emitted count, AND emits happen in that increasing order — the sink
// never observes a regression. (An atomic filter alone would not guarantee this:
// the filter can win while a concurrent higher count is already in flight to the
// sink.) The terminal 100% from makeOrganizeCompleteBroadcaster fires on the
// non-cancelled completion path — after boundedFanOut returns, i.e. after every
// OnFileProgress call has returned. It is skipped on cancellation (the apply
// phase returns before OnPhaseComplete), where the frontend finalizes via its
// poll loop detecting the 'cancelled' job status.
//
// Status is ProgressStatusPending (non-terminal) so the frontend's
// handleWebSocketMessage only updates the bar (msg.progress) without tripping
// the organized/updated/failed/organization_completed branches. Progress is on
// a 0-100 scale consumed by the frontend progress card. isUpdate picks the
// human-readable action verb in the message text. The message construction is
// delegated to buildOrganizeProgressMessage so the wire-format contract is
// unit-testable without a live WebSocket hub.
func makeOrganizeProgressBroadcaster(job worker.BatchJobInterface, isUpdate bool, sink progressSink) func(processed, total int) {
	var mu sync.Mutex
	maxSent := 0
	return func(processed, total int) {
		msg, ok := buildOrganizeProgressMessage(job.GetID(), isUpdate, processed, total)
		if !ok {
			return
		}
		// Hold the lock across both the high-water check and the sink call so emits
		// are serialized in increasing order: a goroutine may emit only if its
		// processed strictly exceeds the max already emitted, and the emit happens
		// before the lock is released, so no later lower count can overtake it.
		mu.Lock()
		if processed <= maxSent {
			mu.Unlock()
			return
		}
		maxSent = processed
		sink(stampJobCounts(msg, job))
		mu.Unlock()
	}
}

// buildOrganizeProgressMessage constructs the WebSocket ProgressMessage for a
// per-file progress update. Returns (nil, false) when there is nothing to
// report (total <= 0), signalling the caller to skip the broadcast. Extracted
// from makeOrganizeProgressBroadcaster so the wire-format contract — Status is
// non-terminal ProgressStatusPending, Progress is on the 0-100 scale, JobID is
// set, and the message names the action verb and processed/total counts — is
// unit-testable without a live WebSocket hub or a real job.
func buildOrganizeProgressMessage(jobID string, isUpdate bool, processed, total int) (*websocket.ProgressMessage, bool) {
	pct := organizeProgressPercent(processed, total)
	if pct < 0 {
		return nil, false
	}
	action := "Organized"
	if isUpdate {
		action = "Updated"
	}
	return &websocket.ProgressMessage{
		JobID:    jobID,
		Status:   websocket.ProgressStatusPending,
		Progress: pct,
		Message:  fmt.Sprintf("%s %d of %d files", action, processed, total),
	}, true
}

// organizeProgressPercent maps a running processed count and total file count
// to the 0-100 scale the frontend progress card consumes. Returns -1 when
// there is nothing to report (total <= 0), signalling the caller to skip the
// broadcast. Clamps to 100 in case processed ever exceeds total (defensive —
// the production caller's counter is bounded by total, but this helper is also
// exercised directly by tests with arbitrary inputs). Extracted from
// makeOrganizeProgressBroadcaster so the scale and clamping are unit-testable
// without a live WebSocket hub.
func organizeProgressPercent(processed, total int) float64 {
	if total <= 0 {
		return -1
	}
	pct := float64(processed) / float64(total) * 100
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}
