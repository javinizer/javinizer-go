package batch

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/eventlog"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type applyConfigEventEmitter struct {
	calls []applyConfigEventCall
}

type applyConfigEventCall struct {
	source   string
	message  string
	severity models.EventSeverity
	context  map[string]any
}

func (e *applyConfigEventEmitter) EmitScraperEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return nil
}

func (e *applyConfigEventEmitter) EmitOrganizeEvent(_ context.Context, source, message string, severity models.EventSeverity, eventContext map[string]any) error {
	e.calls = append(e.calls, applyConfigEventCall{source: source, message: message, severity: severity, context: eventContext})
	return nil
}

func (e *applyConfigEventEmitter) EmitSystemEvent(context.Context, string, string, models.EventSeverity, map[string]any) error {
	return nil
}

func (e *applyConfigEventEmitter) Stats() (emitted, failed int64) {
	return int64(len(e.calls)), 0
}

var _ eventlog.EventEmitter = (*applyConfigEventEmitter)(nil)

func TestResolveApplyConfig_PostApplyEmitsEvents(t *testing.T) {
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
	*organize.ApplyGenerationRef = 7

	organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/movie.mp4",
		Movie:    &models.Movie{ID: "MOV-1"},
	}, &worker.ApplyFileResult{Err: errors.New("move failed")})
	organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/movie.mp4",
		Movie:    &models.Movie{ID: "MOV-1"},
	}, &worker.ApplyFileResult{Result: &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/movie.mp4"},
	}})

	require.Len(t, emitter.calls, 2)
	assert.Equal(t, "file_move", emitter.calls[0].source)
	assert.Equal(t, models.SeverityError, emitter.calls[0].severity)
	assert.Equal(t, "move failed", emitter.calls[0].context["error"])
	assert.Equal(t, uint64(7), emitter.calls[0].context["apply_generation"])
	assert.Equal(t, "file_move", emitter.calls[1].source)
	assert.Equal(t, models.SeverityInfo, emitter.calls[1].severity)
	assert.Equal(t, "/dest/movie.mp4", emitter.calls[1].context["new_path"])
	assert.Equal(t, uint64(7), emitter.calls[1].context["apply_generation"])
}

func TestResolveUpdateApplyConfig_PostApplyEmitsFailureEvent(t *testing.T) {
	emitter := &applyConfigEventEmitter{}
	rt := core.NewAPIRuntime(&core.APIDeps{EventEmitter: emitter})
	snapshot := core.NewSnapshotForTesting(rt, core.APIConfig{})
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)

	update, err := resolveUpdateApplyConfig(snapshot, factory, &stubControlledJob{}, contracts.UpdateRequest{})
	require.NoError(t, err)
	require.NotNil(t, update.PostApplyFunc)
	require.NotNil(t, update.ApplyGenerationRef)
	*update.ApplyGenerationRef = 11

	update.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/movie.mp4",
		Movie:    &models.Movie{ID: "MOV-2"},
	}, &worker.ApplyFileResult{Err: errors.New("nfo failed")})

	require.Len(t, emitter.calls, 1)
	assert.Equal(t, "nfo_gen", emitter.calls[0].source)
	assert.Equal(t, models.SeverityError, emitter.calls[0].severity)
	assert.Equal(t, "nfo failed", emitter.calls[0].context["error"])
	assert.Equal(t, uint64(11), emitter.calls[0].context["apply_generation"])
}

func TestApplyProgressHelpers_NilAndGenerationPaths(t *testing.T) {
	assert.Equal(t, uint64(0), loadApplyGeneration(nil))
	assert.Nil(t, stampJobCountsForApply(nil, nil))

	var got *websocket.ProgressMessage
	generation := uint64(13)
	bcast := makeOrganizeFileFailedBroadcaster(&stubControlledJob{}, false, func(msg *websocket.ProgressMessage) {
		got = msg
	}, &generation)
	bcast("/source/movie.mp4", "organize failed")

	require.NotNil(t, got)
	assert.Equal(t, "failed", string(got.Status))
	assert.Equal(t, "organize failed", got.Error)
	assert.Equal(t, uint64(13), got.ApplyGeneration)
}

// TestResolveApplyConfig_PostApplyEmitsDuplicateWarningAudit pins the #224
// phase E audit lane: authorized intra-batch duplicates surface on the
// per-file result as warnings, and each warning becomes its own SeverityWarn
// organize event through the existing eventlog.
func TestResolveApplyConfig_PostApplyEmitsDuplicateWarningAudit(t *testing.T) {
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
	*organize.ApplyGenerationRef = 3

	organize.PostApplyFunc(context.Background(), &worker.ApplyFileContext{
		FilePath: "/source/movie.mp4",
		Movie:    &models.Movie{ID: "MOV-9"},
	}, &worker.ApplyFileResult{Result: &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{
			NewPath: "/dest/movie.mp4",
			Warnings: []string{
				"duplicate destination within batch: /dest/movie.mp4 already claimed by /source/other.mp4 (overwrite authorized)",
			},
		},
	}})

	require.Len(t, emitter.calls, 2)
	assert.Equal(t, models.SeverityInfo, emitter.calls[0].severity)
	warn := emitter.calls[1]
	assert.Equal(t, "file_move", warn.source)
	assert.Equal(t, models.SeverityWarn, warn.severity)
	assert.Contains(t, warn.message, "duplicate destination within batch")
	assert.Equal(t, "MOV-9", warn.context["movie_id"])
	assert.Equal(t, "/dest/movie.mp4", warn.context["new_path"])
	assert.Contains(t, warn.context["warning"], "already claimed by /source/other.mp4")
	assert.Equal(t, uint64(3), warn.context["apply_generation"])
}
