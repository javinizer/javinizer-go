package batch

import (
	"context"
	"fmt"

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
		if !isDirAllowed(deps.GetFs(), req.Destination, secCfg.AllowedDirectories, secCfg.DeniedDirectories) {
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
	applyOpts.OnPhaseComplete = makeOrganizeCompleteBroadcaster(rt, job, false /* isUpdate */)
	applyOpts.PostApplyFunc = func(ctx context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
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
) worker.ApplyPhaseConfig {
	deps := rt.Deps()
	resolvedUpdate, _ := workflow.ResolveSeamStrings(workflow.SeamStringsInput{
		Preset:         req.Preset,
		ScalarStrategy: req.ScalarStrategy,
		ArrayStrategy:  req.ArrayStrategy,
	})

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
	applyOpts.OnPhaseComplete = makeOrganizeCompleteBroadcaster(rt, job, true /* isUpdate */)
	applyOpts.PostApplyFunc = func(ctx context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
		emitter := deps.GetEventEmitter()
		if afr.Err != nil && emitter != nil {
			_ = emitter.EmitOrganizeEvent(ctx, "nfo_gen", fmt.Sprintf("Update failed for %s", afc.Movie.ID), models.SeverityError, map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "error": afr.Err.Error()})
		}
	}

	return applyOpts
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
func makeOrganizeCompleteBroadcaster(rt *core.APIRuntime, job worker.BatchJobInterface, isUpdate bool) func(organized, failed int) {
	return func(organized, failed int) {
		status := websocket.ProgressStatus("organization_completed")
		action := "Organized"
		if isUpdate {
			status = websocket.ProgressStatus("update_completed")
			action = "Updated"
		}
		broadcastProgress(rt.GetRuntime(), &websocket.ProgressMessage{
			JobID:    job.GetID(),
			Status:   status,
			Progress: 100,
			Message:  fmt.Sprintf("%s %d files, %d failed", action, organized, failed),
		})
	}
}
