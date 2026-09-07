package batch

// Post-apply audit emission: the secondary eventlog rows each apply file
// result produces (success/failure + one row per warning) and the factories
// that build the PostApplyFunc hooks the apply phase invokes per file.
// Split out of apply_config_builder.go to keep it under the API file-size
// cap (scripts/check_api_file_size.sh) — the reviewer-driven audit additions
// (detached ctx, shared deadline, canceled checks, per-warning rows) grew it
// over the 700-line limit.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// makeOrganizePostApplyAuditHook returns the organize lane's PostApplyFunc:
// it emits the file's audit rows ("Organized" success or "Organize failed"
// plus one row per OrganizeResult.Warnings entry) under ONE detached, bounded
// ctx per invocation (see postApplyAuditEmit for why detachment matters).
func makeOrganizePostApplyAuditHook(deps *core.APIDeps, job worker.BatchJobInterface, applyGenerationRef *uint64) func(ctx context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
	return func(_ context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
		// Guard: never dereference a nil payload. If the apply context or
		// result is missing required fields, skip emitting this secondary
		// event so the original apply error is preserved instead of being
		// masked by a nil-panic here.
		if afc == nil || afc.Movie == nil || afr == nil {
			return
		}
		emitter := deps.GetEventEmitter()
		if emitter == nil {
			return
		}
		// One detached bounded ctx per PostApplyFunc invocation, REUSED
		// across the success/failure event + every warning event (PR #249
		// codex P2): scoping a fresh 5s ctx PER EVENT would let an organize
		// result with W warnings sit (1+W)×5s worst-case on a worker
		// goroutine after an apply timeout. Mirror the CLI hook
		// (cliBatchPostApply) and the worker history writer: budget once per
		// file, aggregate all of the file's audit rows under it.
		auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		emit := postApplyAuditEmit(auditCtx, emitter, job.GetID())
		if afr.Err != nil {
			// PR #249 codex follow-up (F4): the failed lane's result still
			// carries the displacement crumbs — the link lane binds the
			// force-overwrite crumb at the destruction (F2) and the partial-
			// publish/cross-device legs union their evidence into the failed
			// result (F3) — so the failure event itself carries the warnings,
			// and each warning additionally gets its own audit entry exactly
			// like the success lane below. Consumers must not have to
			// distinguish outcomes to see that resident bytes were destroyed.
			var warnings []string
			var newPath string
			if afr.Result != nil && afr.Result.OrganizeResult != nil {
				warnings = afr.Result.OrganizeResult.Warnings
				newPath = afr.Result.OrganizeResult.NewPath
			}
			// PR #249 codex P2 (canceled-error lane): context.Canceled is
			// checked BEFORE any failure emit. The worker classifies a canceled
			// apply as Cancelled, NOT failed (apply_phase.go interpretApplyResult:
			// fileStatus = models.JobStatusCancelled and OnFileFailed is
			// deliberately NOT invoked), so a SeverityError "Organize failed"
			// row for a canceled file would mislabel the audit trail — consumers
			// correlate failed events with erasable/retriable files. Skip it;
			// mirrors auditOrganizeFailure's canceled skip
			// (worker/history_writer.go) and the CLI hook (cliBatchPostApply).
			// The buffered warning events STILL emit: they are truthful evidence
			// (resident bytes destroyed, partial publish) recorded before the
			// cancellation landed, and the success-history row the worker writes
			// for a canceled partial move reads the same Warnings slice.
			if !errors.Is(afr.Err, context.Canceled) {
				failCtx := map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "error": afr.Err.Error(), "apply_generation": loadApplyGeneration(applyGenerationRef)}
				if len(warnings) > 0 {
					failCtx["warnings"] = warnings
				}
				emit("file_move", fmt.Sprintf("Organize failed for %s", afc.Movie.ID), models.SeverityError, failCtx)
			}
			for _, warning := range warnings {
				emit("file_move", fmt.Sprintf("Organize warning for %s: %s", afc.Movie.ID, warning), models.SeverityWarn, map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "file": afc.FilePath, "new_path": newPath, "warning": warning, "error": afr.Err.Error(), "apply_generation": loadApplyGeneration(applyGenerationRef)})
			}
			return
		}
		var newPath string
		if afr.Result != nil && afr.Result.OrganizeResult != nil {
			newPath = afr.Result.OrganizeResult.NewPath
		}
		emit("file_move", fmt.Sprintf("Organized %s", afc.Movie.ID), models.SeverityInfo, map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "file": afc.FilePath, "new_path": newPath, "apply_generation": loadApplyGeneration(applyGenerationRef)})
		// #224 phase E: authorized intra-batch duplicates are demoted from
		// conflicts to per-file warnings; each warning gets its own audit
		// event via the existing eventlog.
		if afr.Result != nil && afr.Result.OrganizeResult != nil {
			for _, warning := range afr.Result.OrganizeResult.Warnings {
				emit("file_move", fmt.Sprintf("Organize warning for %s: %s", afc.Movie.ID, warning), models.SeverityWarn, map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "file": afc.FilePath, "new_path": newPath, "warning": warning, "apply_generation": loadApplyGeneration(applyGenerationRef)})
			}
		}
	}
}

// makeUpdatePostApplyAuditHook returns the update lane's PostApplyFunc: it
// emits the "Update failed" audit row (skipped for canceled applies) under a
// single detached, bounded ctx per invocation.
func makeUpdatePostApplyAuditHook(deps *core.APIDeps, job worker.BatchJobInterface, applyGenerationRef *uint64) func(ctx context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
	return func(_ context.Context, afc *worker.ApplyFileContext, afr *worker.ApplyFileResult) {
		// Guard: never dereference a nil payload; skip the secondary event so
		// the original apply error is preserved.
		if afc == nil || afc.Movie == nil || afr == nil {
			return
		}
		emitter := deps.GetEventEmitter()
		if afr.Err != nil && emitter != nil && !errors.Is(afr.Err, context.Canceled) {
			// canceled check BEFORE the failure emit (PR #249 codex P2,
			// canceled-error lane): the worker records canceled files as
			// Cancelled, not failed, so a SeverityError "Update failed" row
			// would mislabel the audit trail — mirror the organize lane
			// (makeOrganizePostApplyAuditHook)
			// and the CLI hook. Same one-ctx-per-invocation shape as the
			// organize lane (update currently emits a single event, but keep
			// the pattern uniform so a future per-warning lane inherits the
			// aggregated budget).
			auditCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			postApplyAuditEmit(auditCtx, emitter, job.GetID())("nfo_gen", fmt.Sprintf("Update failed for %s", afc.Movie.ID), models.SeverityError, map[string]any{"job_id": job.GetID(), "movie_id": afc.Movie.ID, "error": afr.Err.Error(), "apply_generation": loadApplyGeneration(applyGenerationRef)})
		}
	}
}

// postApplyAuditEmit returns an emit closure that forwards organize audit
// events to the shared eventlog on the caller-supplied DETACHED, bounded
// auditCtx. The caller (a PostApplyFunc invocation) allocates that ctx ONCE
// and reuses it across the file's success/failure event plus every warning
// event, so a result with W warnings costs ONE 5s budget worst-case, not
// (1+W) — the same aggregation shape as the worker's history writer
// (worker/history_writer.go historyAuditContext) and the CLI hook
// (commandutil/batch_command.go cliBatchPostApply).
//
// Detachment is required: the apply phase invokes PostApplyFunc via
// interpretApplyResult with the per-file task ctx, which is ALREADY expired
// when apply exhausts WorkerTimeout, and eventlog.emit drops events on a
// canceled ctx (emitter.go checks ctx.Err()). Routing the passed task ctx
// through here meant a timed-out apply lost its failure/warning audit rows
// entirely, while the worker's history writer still recorded the outcome
// because it audits with a fresh bounded ctx. A 5s background-timeout context
// keeps the audit writes bounded without blocking the apply pipeline.
//
// Emission failures are Warn-logged, not silently discarded (mirroring the
// CLI hook), so a persist-level outage surfaces in the logs instead of
// vanishing.
func postApplyAuditEmit(auditCtx context.Context, emitter eventlog.EventEmitter, jobID string) func(source, message string, severity models.EventSeverity, eventCtx map[string]any) {
	return func(source, message string, severity models.EventSeverity, eventCtx map[string]any) {
		if err := emitter.EmitOrganizeEvent(auditCtx, source, message, severity, eventCtx); err != nil {
			logging.Warnf("[api batch %s] eventlog audit emission failed (source=%s, severity=%s, message=%q): %v", jobID, source, severity, message, err)
		}
	}
}
