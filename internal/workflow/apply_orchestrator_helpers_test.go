package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestCompleteRevertLog_NilRevertLog_DoesNothing(t *testing.T) {
	o := &applyOrchImpl{revertLog: nil}
	assert.NotPanics(t, func() {
		o.completeRevertLogWithState(context.Background(), "op-123", &applyPipelineState{})
	})
}

func TestCompleteRevertLog_EmptyOpID_DoesNothing(t *testing.T) {
	mock := &stubRevertLog{}
	o := &applyOrchImpl{revertLog: mock}
	assert.NotPanics(t, func() {
		o.completeRevertLogWithState(context.Background(), "", &applyPipelineState{})
	})
	assert.Empty(t, mock.completeCalls, "Complete should not be called with empty opID")
}

func TestBeginRevertLog_NilRevertLog_ReturnsEmpty(t *testing.T) {
	o := &applyOrchImpl{revertLog: nil}
	cmd := ApplyCmd{Movie: &models.Movie{ID: "TEST-001"}}
	opID := o.beginRevertLog(context.Background(), cmd)
	assert.Equal(t, "", opID)
}

func TestBeginRevertLog_BeginFails_ReturnsEmptyAndLogs(t *testing.T) {
	mock := &stubRevertLog{beginErr: errTestBegin}
	o := &applyOrchImpl{revertLog: mock}
	cmd := ApplyCmd{Movie: &models.Movie{ID: "TEST-001"}}
	opID := o.beginRevertLog(context.Background(), cmd)
	assert.Equal(t, "", opID)
	assert.Equal(t, 1, mock.beginCalls, "Begin should be called once")
}

// stubRevertLog is a test double for RevertLog that records calls.
type stubRevertLog struct {
	beginCalls    int
	beginErr      error
	completeCalls []string
}

var errTestBegin = errTestBeginKind("begin failed")

type errTestBeginKind string

func (e errTestBeginKind) Error() string { return string(e) }

func (s *stubRevertLog) Begin(_ context.Context, _ ApplyCmd) (OperationID, error) {
	s.beginCalls++
	if s.beginErr != nil {
		return "", s.beginErr
	}
	return "op-42", nil
}

func (s *stubRevertLog) CaptureSnapshot(_ context.Context, _ OperationID, _ ApplyCmd) {
	// no-op stub
}

func (s *stubRevertLog) Complete(_ context.Context, opID OperationID, _ *ApplyResult) error {
	s.completeCalls = append(s.completeCalls, opID)
	return nil
}

func (s *stubRevertLog) CompleteFailed(_ context.Context, opID OperationID, _ *ApplyResult) error {
	s.completeCalls = append(s.completeCalls, opID)
	return nil
}
