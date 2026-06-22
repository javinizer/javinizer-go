package jobs

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestBuildRevertResponse_AllReverted(t *testing.T) {
	result := &history.RevertBatchResult{
		Total:     3,
		Succeeded: 3,
		Skipped:   0,
		Failed:    0,
		Outcomes: []history.RevertFileResult{
			{OperationID: 1, MovieID: "M1", Outcome: models.RevertOutcomeReverted},
			{OperationID: 2, MovieID: "M2", Outcome: models.RevertOutcomeReverted},
			{OperationID: 3, MovieID: "M3", Outcome: models.RevertOutcomeReverted},
		},
	}

	resp := buildRevertResponse("job-1", models.JobStatusReverted, result)
	assert.Equal(t, "job-1", resp.JobID)
	assert.Equal(t, models.JobStatusReverted, resp.Status)
	assert.Equal(t, 3, resp.Total)
	assert.Equal(t, 3, resp.Succeeded)
	assert.Equal(t, 0, resp.Skipped)
	assert.Equal(t, 0, resp.Failed)
	assert.Empty(t, resp.Errors, "no errors when all reverted successfully")
}

func TestBuildRevertResponse_WithFailures(t *testing.T) {
	result := &history.RevertBatchResult{
		Total:     3,
		Succeeded: 1,
		Skipped:   1,
		Failed:    1,
		Outcomes: []history.RevertFileResult{
			{OperationID: 1, MovieID: "M1", Outcome: models.RevertOutcomeReverted},
			{OperationID: 2, MovieID: "M2", Outcome: models.RevertOutcomeSkipped, Reason: "already reverted"},
			{OperationID: 3, MovieID: "M3", Outcome: models.RevertOutcomeFailed, Error: "file not found"},
		},
	}

	resp := buildRevertResponse("job-2", models.JobStatusOrganized, result)
	assert.Equal(t, 2, len(resp.Errors), "non-reverted outcomes produce errors")
	assert.Equal(t, uint(2), resp.Errors[0].OperationID)
	assert.Equal(t, uint(3), resp.Errors[1].OperationID)
	assert.Equal(t, "file not found", resp.Errors[1].Error)
}

func TestBuildRevertResponse_EmptyOutcomes(t *testing.T) {
	result := &history.RevertBatchResult{
		Total:     0,
		Succeeded: 0,
		Skipped:   0,
		Failed:    0,
		Outcomes:  nil,
	}

	resp := buildRevertResponse("job-empty", models.JobStatusOrganized, result)
	assert.Empty(t, resp.Errors)
}

func TestBuildRevertResponse_PartialFailure(t *testing.T) {
	result := &history.RevertBatchResult{
		Total:     2,
		Succeeded: 1,
		Skipped:   0,
		Failed:    1,
		Outcomes: []history.RevertFileResult{
			{OperationID: 1, MovieID: "M1", Outcome: models.RevertOutcomeReverted},
			{OperationID: 2, MovieID: "M2", Outcome: models.RevertOutcomeFailed, Error: "permission denied"},
		},
	}

	resp := buildRevertResponse("job-partial", models.JobStatusOrganized, result)
	assert.Equal(t, 1, len(resp.Errors))
	assert.Equal(t, uint(2), resp.Errors[0].OperationID)
	assert.Equal(t, "permission denied", resp.Errors[0].Error)
	assert.Equal(t, models.RevertOutcomeFailed, resp.Errors[0].Outcome)
}

func TestEmitRevertEvent_NilEmitter(t *testing.T) {
	// Should not panic when emitter is nil
	result := &history.RevertBatchResult{
		Total:     1,
		Succeeded: 1,
		Failed:    0,
	}
	assert.NotPanics(t, func() {
		emitRevertEvent(context.Background(), nil, "test message", "job-1", result, nil)
	})
}

func TestEmitRevertEvent_SeverityAllSuccess(t *testing.T) {
	// Pure success should emit info severity
	var capturedSev models.EventSeverity
	mockEmitter := &captureSeverityEmitter{capture: &capturedSev}

	result := &history.RevertBatchResult{
		Total:     2,
		Succeeded: 2,
		Failed:    0,
		Skipped:   0,
	}

	emitRevertEvent(context.Background(), mockEmitter, "revert completed", "job-1", result, nil)
	assert.Equal(t, models.SeverityInfo, capturedSev)
}

func TestEmitRevertEvent_SeverityPartialFailure(t *testing.T) {
	// Mixed success/failure should emit warn severity
	var capturedSev models.EventSeverity
	mockEmitter := &captureSeverityEmitter{capture: &capturedSev}

	result := &history.RevertBatchResult{
		Total:     2,
		Succeeded: 1,
		Failed:    1,
		Skipped:   0,
	}

	emitRevertEvent(context.Background(), mockEmitter, "revert partial", "job-1", result, nil)
	assert.Equal(t, models.SeverityWarn, capturedSev)
}

func TestEmitRevertEvent_SeverityAllFailed(t *testing.T) {
	// All failures should emit error severity
	var capturedSev models.EventSeverity
	mockEmitter := &captureSeverityEmitter{capture: &capturedSev}

	result := &history.RevertBatchResult{
		Total:     2,
		Succeeded: 0,
		Failed:    2,
		Skipped:   0,
	}

	emitRevertEvent(context.Background(), mockEmitter, "revert failed", "job-1", result, nil)
	assert.Equal(t, models.SeverityError, capturedSev)
}

func TestEmitRevertEvent_ExtraFields(t *testing.T) {
	var capturedFields map[string]any
	mockEmitter := &captureFieldsEmitter{capture: &capturedFields}

	result := &history.RevertBatchResult{
		Total:     1,
		Succeeded: 1,
		Failed:    0,
	}

	emitRevertEvent(context.Background(), mockEmitter, "test", "job-1", result, map[string]any{"movie_id": "M1"})
	assert.NotNil(t, capturedFields)
	assert.Equal(t, "M1", capturedFields["movie_id"])
	assert.Equal(t, "job-1", capturedFields["job_id"])
}

// captureSeverityEmitter captures the severity of emitted events.
type captureSeverityEmitter struct {
	capture *models.EventSeverity
}

func (e *captureSeverityEmitter) EmitScraperEvent(ctx context.Context, eventType, message string, severity models.EventSeverity, fields map[string]any) error {
	return nil
}

func (e *captureSeverityEmitter) EmitOrganizeEvent(ctx context.Context, eventType, message string, severity models.EventSeverity, fields map[string]any) error {
	*e.capture = severity
	return nil
}

func (e *captureSeverityEmitter) EmitSystemEvent(ctx context.Context, eventType, message string, severity models.EventSeverity, fields map[string]any) error {
	return nil
}

func (e *captureSeverityEmitter) Stats() (int64, int64) {
	return 0, 0
}

// captureFieldsEmitter captures the fields of emitted events.
type captureFieldsEmitter struct {
	capture *map[string]any
}

func (e *captureFieldsEmitter) EmitScraperEvent(ctx context.Context, eventType, message string, severity models.EventSeverity, fields map[string]any) error {
	return nil
}

func (e *captureFieldsEmitter) EmitOrganizeEvent(ctx context.Context, eventType, message string, severity models.EventSeverity, fields map[string]any) error {
	*e.capture = fields
	return nil
}

func (e *captureFieldsEmitter) EmitSystemEvent(ctx context.Context, eventType, message string, severity models.EventSeverity, fields map[string]any) error {
	return nil
}

func (e *captureFieldsEmitter) Stats() (int64, int64) {
	return 0, 0
}
