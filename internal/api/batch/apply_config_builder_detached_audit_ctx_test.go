package batch

// PR #249 codex follow-up (F5) — the API PostApplyFunc audit emits must ride a
// DETACHED bounded ctx, not the per-file task ctx: interpretApplyResult
// (worker/apply_phase.go) invokes the hook with a ctx that is ALREADY expired
// when apply exhausts WorkerTimeout, and eventlog.emit drops events on a
// canceled ctx (eventlog/emitter.go checks ctx.Err()), so the timed-out
// apply's failure/warning audit rows vanished — the same drop the CLI hook
// fixed in commandutil/batch_command.go cliBatchPostApply. The pins below
// mimic the real eventlog's drop-on-canceled-ctx semantics and invoke the
// hooks with a pre-canceled ctx synchronously; a regression back to the task
// ctx would drop every event and fail these tests.

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ctxEnforcingEmitter mirrors eventlog.emit: an emit on an already-canceled
// ctx is DROPPED (recorded in drops), exactly like the production emitter —
// so these pins fail if PostApplyFunc ever forwards the (canceled) task ctx.
type ctxEnforcingEmitter struct {
	calls []applyConfigEventCall
	drops []string
}

func (e *ctxEnforcingEmitter) EmitScraperEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return nil
}

func (e *ctxEnforcingEmitter) EmitOrganizeEvent(ctx context.Context, source, message string, severity models.EventSeverity, eventContext map[string]any) error {
	if ctx.Err() != nil {
		e.drops = append(e.drops, message)
		return fmt.Errorf("event emitter: caller context cancelled: %w", ctx.Err())
	}
	e.calls = append(e.calls, applyConfigEventCall{source: source, message: message, severity: severity, context: eventContext})
	return nil
}

func (e *ctxEnforcingEmitter) EmitSystemEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return nil
}

func (e *ctxEnforcingEmitter) Stats() (emitted, failed int64) {
	return int64(len(e.calls)), int64(len(e.drops))
}

var _ eventlog.EventEmitter = (*ctxEnforcingEmitter)(nil)

func preCanceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestPostApplyDetachedAuditCtx_Organize pins both organize lanes: invoked
// with a pre-canceled task ctx (the WorkerTimeout-exhausted shape), the
// success info + per-warning audit entries AND the failure + per-warning
// audit entries all still land — detached from the dead ctx.
func TestPostApplyDetachedAuditCtx_Organize(t *testing.T) {
	emitter := &ctxEnforcingEmitter{}
	rt := core.NewAPIRuntime(&core.APIDeps{EventEmitter: emitter})
	snapshot := core.NewSnapshotForTesting(rt, core.APIConfig{})
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	organize, err := resolveOrganizeApplyConfig(snapshot, factory, job, contracts.OrganizeRequest{
		OperationMode: string(operationmode.OperationModeInPlace),
	})
	require.NoError(t, err)
	require.NotNil(t, organize.PostApplyFunc)
	require.NotNil(t, organize.ApplyGenerationRef)
	*organize.ApplyGenerationRef = 44

	warning := "duplicate destination within batch: /dest/movie.mp4 already claimed (overwrite authorized)"

	// Success lane, pre-canceled task ctx.
	organize.PostApplyFunc(preCanceledCtx(), &worker.ApplyFileContext{
		FilePath: "/source/movie.mp4",
		Movie:    &models.Movie{ID: "MOV-44"},
	}, &worker.ApplyFileResult{Result: &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/movie.mp4", Warnings: []string{warning}},
	}})

	// Failure lane, pre-canceled task ctx.
	organize.PostApplyFunc(preCanceledCtx(), &worker.ApplyFileContext{
		FilePath: "/source/other.mp4",
		Movie:    &models.Movie{ID: "MOV-45"},
	}, &worker.ApplyFileResult{
		Err:    fmt.Errorf("organization failed: copy timed out"),
		Result: &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/other.mp4", Warnings: []string{warning}}},
	})

	assert.Empty(t, emitter.drops, "no audit event may be dropped on the canceled task ctx")
	require.Len(t, emitter.calls, 4, "info + warning on success, failure + warning on failure")

	assert.Equal(t, models.SeverityInfo, emitter.calls[0].severity)
	assert.Contains(t, emitter.calls[0].message, "Organized MOV-44")

	assert.Equal(t, models.SeverityWarn, emitter.calls[1].severity)
	assert.Contains(t, emitter.calls[1].message, warning)

	assert.Equal(t, models.SeverityError, emitter.calls[2].severity)
	assert.Contains(t, emitter.calls[2].message, "Organize failed for MOV-45")
	assert.Equal(t, []string{warning}, emitter.calls[2].context["warnings"])

	assert.Equal(t, models.SeverityWarn, emitter.calls[3].severity)
	assert.Equal(t, warning, emitter.calls[3].context["warning"])
}

// erringEmitter fails every emit so the postApplyAuditEmit failure branch
// (Warn-logged, not fatal) is exercised.
type erringEmitter struct {
	attempts atomic.Int64
}

func (e *erringEmitter) EmitScraperEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return nil
}

func (e *erringEmitter) EmitOrganizeEvent(_ context.Context, _, _ string, _ models.EventSeverity, _ map[string]any) error {
	e.attempts.Add(1)
	return fmt.Errorf("event repo write failed")
}

func (e *erringEmitter) EmitSystemEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return nil
}

func (e *erringEmitter) Stats() (emitted, failed int64) {
	return 0, e.attempts.Load()
}

var _ eventlog.EventEmitter = (*erringEmitter)(nil)

// TestPostApplyAuditEmitFailure_WarnLoggedNotFatal pins that a persist-level
// emission failure is Warn-logged and swallowed (mirroring the CLI hook) —
// the post-apply hook must never mask the original apply outcome.
func TestPostApplyAuditEmitFailure_WarnLoggedNotFatal(t *testing.T) {
	emitter := &erringEmitter{}
	rt := core.NewAPIRuntime(&core.APIDeps{EventEmitter: emitter})
	snapshot := core.NewSnapshotForTesting(rt, core.APIConfig{})
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	organize, err := resolveOrganizeApplyConfig(snapshot, factory, job, contracts.OrganizeRequest{
		OperationMode: string(operationmode.OperationModeInPlace),
	})
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
			FilePath: "/source/movie.mp4",
			Movie:    &models.Movie{ID: "MOV-47"},
		}, &worker.ApplyFileResult{Result: &workflow.ApplyResult{
			OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/movie.mp4"},
		}})
	})
	assert.Equal(t, int64(1), emitter.attempts.Load(), "the emit was attempted and its failure absorbed")
}

// TestPostApplyDetachedAuditCtx_Update pins the update resolver's nfo_gen
// failure emit: with a pre-canceled task ctx the "Update failed" audit entry
// must still land on the detached bounded ctx.
func TestPostApplyDetachedAuditCtx_Update(t *testing.T) {
	emitter := &ctxEnforcingEmitter{}
	rt := core.NewAPIRuntime(&core.APIDeps{EventEmitter: emitter})
	snapshot := core.NewSnapshotForTesting(rt, core.APIConfig{})
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	update, err := resolveUpdateApplyConfig(snapshot, factory, job, contracts.UpdateRequest{})
	require.NoError(t, err)
	require.NotNil(t, update.PostApplyFunc)

	update.PostApplyFunc(preCanceledCtx(), &worker.ApplyFileContext{
		FilePath: "/source/movie.mp4",
		Movie:    &models.Movie{ID: "MOV-46"},
	}, &worker.ApplyFileResult{Err: fmt.Errorf("update failed: nfo write timed out")})

	assert.Empty(t, emitter.drops, "no audit event may be dropped on the canceled task ctx")
	require.Len(t, emitter.calls, 1)
	assert.Equal(t, "nfo_gen", emitter.calls[0].source)
	assert.Equal(t, models.SeverityError, emitter.calls[0].severity)
	assert.Contains(t, emitter.calls[0].message, "Update failed for MOV-46")
}
