package batch

import (
	"context"
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

// TestPostApplyDuplicateWarningAudit_MultiWarningDeterministic pins the #224
// phase E duplicate-warning audit loop in resolveOrganizeApplyConfig's
// PostApplyFunc (apply_config_builder.go ~L130-133) with a fully synchronous,
// goroutine-free invocation. Codecov flapped this cluster (1 miss + 1 partial)
// because the full-suite profile covers the loop body only when the async
// apply path happens to carry warnings before the instrumented binary exits —
// an all-or-nothing race per run. This pin invokes the resolved PostApplyFunc
// directly with a fabricated worker result carrying TWO warnings, so every
// block of the cluster (guard, range header, loop backedge, body emit) is
// covered deterministically on every run, independent of any worker timing.
func TestPostApplyDuplicateWarningAudit_MultiWarningDeterministic(t *testing.T) {
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
	*organize.ApplyGenerationRef = 21

	warnings := []string{
		"duplicate destination within batch: /dest/movie.mp4 already claimed by /source/alpha.mp4 (overwrite authorized)",
		"duplicate destination within batch: /dest/movie.mp4 also claimed by /source/beta.mp4 (overwrite authorized)",
	}
	organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/movie.mp4",
		Movie:    &models.Movie{ID: "MOV-21"},
	}, &worker.ApplyFileResult{Result: &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:  "/dest/movie.mp4",
			Warnings: warnings,
		},
	}})

	// One info emit + one audit emit per warning, in order; the loop body must
	// iterate once per warning — pinning the range backedge, not just entry.
	require.Len(t, emitter.calls, 3)

	info := emitter.calls[0]
	assert.Equal(t, "file_move", info.source)
	assert.Equal(t, models.SeverityInfo, info.severity)
	assert.Equal(t, uint64(21), info.context["apply_generation"])

	for i, warning := range warnings {
		warn := emitter.calls[1+i]
		assert.Equal(t, "file_move", warn.source, "warning %d source", i)
		assert.Equal(t, models.SeverityWarn, warn.severity, "warning %d severity", i)
		assert.Contains(t, warn.message, "Organize warning for MOV-21:", "warning %d message prefix", i)
		assert.Contains(t, warn.message, warning, "warning %d message carries its own text", i)
		assert.Equal(t, "stub-job", warn.context["job_id"], "warning %d job id", i)
		assert.Equal(t, "MOV-21", warn.context["movie_id"], "warning %d movie id", i)
		assert.Equal(t, "/source/movie.mp4", warn.context["file"], "warning %d file", i)
		assert.Equal(t, "/dest/movie.mp4", warn.context["new_path"], "warning %d new path", i)
		assert.Equal(t, warning, warn.context["warning"], "warning %d audit payload", i)
		assert.Equal(t, uint64(21), warn.context["apply_generation"], "warning %d generation", i)
	}
}

// TestPostApplyDuplicateWarningAudit_EmptyAndNilResultDeterministic pins the
// no-warning edges of the same cluster so they too never hinge on async apply
// timing: an OrganizeResult with an EMPTY (non-nil) warnings slice walks the
// range header with zero iterations, and a nil OrganizeResult takes the
// guard's false branch. Both must emit exactly the info event and no audit
// warnings — synchronously, via direct PostApplyFunc invocation.
func TestPostApplyDuplicateWarningAudit_EmptyAndNilResultDeterministic(t *testing.T) {
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

	organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/a.mp4",
		Movie:    &models.Movie{ID: "MOV-22"},
	}, &worker.ApplyFileResult{Result: &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:  "/dest/a.mp4",
			Warnings: []string{},
		},
	}})
	organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/b.mp4",
		Movie:    &models.Movie{ID: "MOV-23"},
	}, &worker.ApplyFileResult{Err: nil, Result: nil})

	require.Len(t, emitter.calls, 2, "one info emit per success call, zero audit warnings")
	for i, info := range emitter.calls {
		assert.Equal(t, "file_move", info.source, "call %d source", i)
		assert.Equal(t, models.SeverityInfo, info.severity, "call %d severity", i)
		assert.NotContains(t, info.message, "warning", "call %d is not an audit emit", i)
	}
	assert.Equal(t, "/dest/a.mp4", emitter.calls[0].context["new_path"])
	assert.Equal(t, "", emitter.calls[1].context["new_path"], "nil OrganizeResult yields empty new_path")
}
