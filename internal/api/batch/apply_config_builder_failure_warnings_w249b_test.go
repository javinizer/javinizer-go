package batch

// PR #249 codex follow-up (F4) — the API organize PostApplyFunc must surface
// an Err!=nil lane's OrganizeResult.Warnings: the failure event carries them
// in its context AND each warning gets its own audit entry, exactly like the
// success lane's duplicate-warning loop. The organizer binds the displacement
// crumb at the destruction (F2) so failed organize results carry warnings the
// pre-F4 hook discarded behind the `afr.Err != nil` early-emit/return.

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

// TestPostApplyFailureCarriesWarnings pins the F4 failure-lane warning
// surface deterministically (synchronous direct PostApplyFunc invocation —
// the same all-or-nothing async-timing hazard the w241 pin retired).
func TestPostApplyFailureCarriesWarnings(t *testing.T) {
	emitter := &applyConfigEventEmitter{}
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
	*organize.ApplyGenerationRef = 33

	warnings := []string{
		"overwrite authorized: replaced existing destination /dest/movie.mp4",
		"overwrite authorized: replaced existing destination /dest/subtitle.srt",
	}
	applyErr := fmt.Errorf("organization failed: failed to create hard link (source and destination must be on the same filesystem)")
	organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/movie.mp4",
		Movie:    &models.Movie{ID: "MOV-33"},
	}, &worker.ApplyFileResult{
		Err: applyErr,
		Result: &workflow.ApplyResult{
			OrganizeResult: &organizer.OrganizeResult{
				NewPath:  "/dest/movie.mp4",
				Warnings: warnings,
			},
		},
	})

	// One error emit carrying the warnings in its context + one audit emit
	// per warning, in order.
	require.Len(t, emitter.calls, 3)

	failure := emitter.calls[0]
	assert.Equal(t, "file_move", failure.source)
	assert.Equal(t, models.SeverityError, failure.severity)
	assert.Contains(t, failure.message, "Organize failed for MOV-33")
	assert.Equal(t, applyErr.Error(), failure.context["error"])
	assert.Equal(t, warnings, failure.context["warnings"],
		"the failure event itself carries the crumbs — no correlation needed")
	assert.Equal(t, uint64(33), failure.context["apply_generation"])

	for i, warning := range warnings {
		ev := emitter.calls[1+i]
		assert.Equal(t, "file_move", ev.source, "warning %d source", i)
		assert.Equal(t, models.SeverityWarn, ev.severity, "warning %d severity", i)
		assert.Equal(t, ev.message, fmt.Sprintf("Organize warning for MOV-33: %s", warning), "warning %d message", i)
		assert.Equal(t, "stub-job", ev.context["job_id"], "warning %d job id", i)
		assert.Equal(t, "MOV-33", ev.context["movie_id"], "warning %d movie id", i)
		assert.Equal(t, "/source/movie.mp4", ev.context["file"], "warning %d file", i)
		assert.Equal(t, "/dest/movie.mp4", ev.context["new_path"], "warning %d new path", i)
		assert.Equal(t, warning, ev.context["warning"], "warning %d audit payload", i)
		assert.Equal(t, applyErr.Error(), ev.context["error"], "warning %d failure context", i)
		assert.Equal(t, uint64(33), ev.context["apply_generation"], "warning %d generation", i)
	}
}

// TestPostApplyFailureWithoutWarnings_KeepsSingleSurface pins the unchanged
// failure lane: no OrganizeResult (or an empty Warnings slice) emits exactly
// the failure event with no warnings key and zero audit entries.
func TestPostApplyFailureWithoutWarnings_KeepsSingleSurface(t *testing.T) {
	emitter := &applyConfigEventEmitter{}
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
		{name: "empty warnings", result: &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/movie.mp4"}}},
		{name: "nil organize result", result: &workflow.ApplyResult{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emitter.calls = nil
			organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
				FilePath: "/source/movie.mp4",
				Movie:    &models.Movie{ID: "MOV-34"},
			}, &worker.ApplyFileResult{Err: fmt.Errorf("organization failed"), Result: tc.result})

			require.Len(t, emitter.calls, 1, "exactly the failure event")
			failure := emitter.calls[0]
			assert.Equal(t, models.SeverityError, failure.severity)
			assert.NotContains(t, failure.context, "warnings")
		})
	}
}
