package batch

// PR #249 codex P2 — canceled-error lane. interpretApplyResult
// (worker/apply_phase.go) classifies an apply error satisfying
// errors.Is(err, context.Canceled) as Cancelled, NOT failed (fileStatus =
// models.JobStatusCancelled; OnFileFailed deliberately not invoked). The
// PostApplyFunc hooks must match that taxonomy: no SeverityError
// "Organize failed"/"Update failed" event for a canceled file, because the
// audit trail would mislabel a cancelled item as erasable/retriable whilst
// the job row says Cancelled. The buffered OrganizeResult.Warnings events
// STILL emit — they are truthful evidence of work done (bytes destroyed,
// partial publish) before the cancellation landed, and the success-history
// row the worker writes for a canceled partial move reads the same slice.

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPostApplyCanceled_Organize_NoFailureEvent_WarningsStillEmitted pins the
// organize lane with errors.Is(err, context.Canceled): bare AND wrapped
// cancellation errors each suppress the "Organize failed" SeverityError event
// while every buffered warning still lands as its own SeverityWarn audit row.
func TestPostApplyCanceled_Organize_NoFailureEvent_WarningsStillEmitted(t *testing.T) {
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
	*organize.ApplyGenerationRef = 55

	warning := "overwrite authorized: replaced existing destination /dest/MOV-55/MOV-55.mp4"

	for _, tc := range []struct {
		name     string
		applyErr error
	}{
		{name: "bare context.Canceled", applyErr: context.Canceled},
		{name: "wrapped context.Canceled", applyErr: fmt.Errorf("organization aborted: %w", context.Canceled)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emitter.calls = nil
			emitter.drops = nil

			organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
				FilePath: "/source/MOV-55.mp4",
				Movie:    &models.Movie{ID: "MOV-55"},
			}, &worker.ApplyFileResult{
				Err: tc.applyErr,
				Result: &workflow.ApplyResult{
					OrganizeResult: &organizer.OrganizeResult{
						NewPath:  "/dest/MOV-55/MOV-55.mp4",
						Warnings: []string{warning},
					},
				},
			})

			require.Equal(t, 1, len(emitter.calls), "exactly the warning event — no failure event for a canceled apply")
			ev := emitter.calls[0]
			assert.Equal(t, "file_move", ev.source)
			assert.Equal(t, models.SeverityWarn, ev.severity, "canceled applies never produce SeverityError rows")
			assert.Equal(t, fmt.Sprintf("Organize warning for MOV-55: %s", warning), ev.message)
			assert.Equal(t, warning, ev.context["warning"])
			assert.Equal(t, tc.applyErr.Error(), ev.context["error"], "the warning row still records the cancellation cause verbatim")
			assert.Equal(t, uint64(55), ev.context["apply_generation"])
			assert.Empty(t, emitter.drops, "audit events ride the detached ctx even here")
		})
	}
}

// TestPostApplyCanceled_Organize_NoWarnings_NoEvents pins the canceled lane
// with no buffered crumbs: NOTHING is emitted — no failure event to mislabel
// the Cancelled file, and no warning loop iterations on an empty slice.
func TestPostApplyCanceled_Organize_NoWarnings_NoEvents(t *testing.T) {
	emitter := &ctxEnforcingEmitter{}
	rt := core.NewAPIRuntime(&core.APIDeps{EventEmitter: emitter})
	snapshot := core.NewSnapshotForTesting(rt, core.APIConfig{})
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	organize, err := resolveOrganizeApplyConfig(snapshot, factory, job, contracts.OrganizeRequest{
		OperationMode: string(operationmode.OperationModeInPlace),
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		result *workflow.ApplyResult
	}{
		{name: "nil result", result: nil},
		{name: "organize result without warnings", result: &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/movie.mp4"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emitter.calls = nil
			emitter.drops = nil

			organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
				FilePath: "/source/movie.mp4",
				Movie:    &models.Movie{ID: "MOV-56"},
			}, &worker.ApplyFileResult{Err: context.Canceled, Result: tc.result})

			assert.Empty(t, emitter.calls, "a canceled apply with no crumbs emits no events at all")
			assert.Empty(t, emitter.drops)
		})
	}
}

// TestPostApplyCanceled_Update_NoEvents pins the update resolver's nfo_gen
// lane: errors.Is(err, context.Canceled) suppresses the "Update failed"
// SeverityError event entirely — the worker records the file Cancelled, so a
// failure row would mislabel it (update has no warning loop to preserve).
func TestPostApplyCanceled_Update_NoEvents(t *testing.T) {
	emitter := &ctxEnforcingEmitter{}
	rt := core.NewAPIRuntime(&core.APIDeps{EventEmitter: emitter})
	snapshot := core.NewSnapshotForTesting(rt, core.APIConfig{})
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	update, err := resolveUpdateApplyConfig(snapshot, factory, job, contracts.UpdateRequest{})
	require.NoError(t, err)
	require.NotNil(t, update.PostApplyFunc)

	update.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/MOV-57.mp4",
		Movie:    &models.Movie{ID: "MOV-57"},
	}, &worker.ApplyFileResult{Err: fmt.Errorf("update aborted: %w", context.Canceled)})

	assert.Empty(t, emitter.calls, "a canceled update emits no failure event")
	assert.Empty(t, emitter.drops)

	// Control: a NON-canceled update failure still lands the failure event —
	// the canceled predicate does not suppress real failures.
	update.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/MOV-58.mp4",
		Movie:    &models.Movie{ID: "MOV-58"},
	}, &worker.ApplyFileResult{Err: fmt.Errorf("update failed: nfo write timed out")})
	require.Len(t, emitter.calls, 1)
	assert.Equal(t, models.SeverityError, emitter.calls[0].severity)
	assert.Contains(t, emitter.calls[0].message, "Update failed for MOV-58")
}
