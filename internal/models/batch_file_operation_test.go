package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// OperationTypeEnum
// ---------------------------------------------------------------------------

func TestOperationTypeEnum_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		e    OperationTypeEnum
		want string
	}{
		{OperationTypeMove, "move"},
		{OperationTypeCopy, "copy"},
		{OperationTypeHardlink, "hardlink"},
		{OperationTypeSymlink, "symlink"},
		{OperationTypeUpdate, "update"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.e.String())
	}
}

func TestOperationTypeEnum_MarshalJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(OperationTypeMove)
	require.NoError(t, err)
	assert.Equal(t, `"move"`, string(data))
}

func TestOperationTypeEnum_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	var e OperationTypeEnum
	require.NoError(t, json.Unmarshal([]byte(`"copy"`), &e))
	assert.Equal(t, OperationTypeCopy, e)
}

func TestOperationTypeEnum_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()
	var e OperationTypeEnum
	err := json.Unmarshal([]byte(`123`), &e)
	assert.Error(t, err)
}

func TestOperationTypeEnum_Scan_String(t *testing.T) {
	t.Parallel()
	var e OperationTypeEnum
	require.NoError(t, e.Scan("move"))
	assert.Equal(t, OperationTypeMove, e)
}

func TestOperationTypeEnum_Scan_Bytes(t *testing.T) {
	t.Parallel()
	var e OperationTypeEnum
	require.NoError(t, e.Scan([]byte("copy")))
	assert.Equal(t, OperationTypeCopy, e)
}

func TestOperationTypeEnum_Scan_Nil(t *testing.T) {
	t.Parallel()
	var e OperationTypeEnum
	require.NoError(t, e.Scan(nil))
	assert.Equal(t, OperationTypeEnum(""), e)
}

func TestOperationTypeEnum_Value(t *testing.T) {
	t.Parallel()
	v, err := OperationTypeHardlink.Value()
	require.NoError(t, err)
	assert.Equal(t, "hardlink", v)
}

// ---------------------------------------------------------------------------
// RevertStatusEnum
// ---------------------------------------------------------------------------

func TestRevertStatusEnum_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "applied", RevertStatusApplied.String())
	assert.Equal(t, "reverted", RevertStatusReverted.String())
	assert.Equal(t, "failed", RevertStatusFailed.String())
}

func TestRevertStatusEnum_MarshalJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(RevertStatusApplied)
	require.NoError(t, err)
	assert.Equal(t, `"applied"`, string(data))
}

func TestRevertStatusEnum_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	var e RevertStatusEnum
	require.NoError(t, json.Unmarshal([]byte(`"reverted"`), &e))
	assert.Equal(t, RevertStatusReverted, e)
}

func TestRevertStatusEnum_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()
	var e RevertStatusEnum
	err := json.Unmarshal([]byte(`42`), &e)
	assert.Error(t, err)
}

func TestRevertStatusEnum_Scan_String(t *testing.T) {
	t.Parallel()
	var e RevertStatusEnum
	require.NoError(t, e.Scan("applied"))
	assert.Equal(t, RevertStatusApplied, e)
}

func TestRevertStatusEnum_Scan_Bytes(t *testing.T) {
	t.Parallel()
	var e RevertStatusEnum
	require.NoError(t, e.Scan([]byte("failed")))
	assert.Equal(t, RevertStatusFailed, e)
}

func TestRevertStatusEnum_Scan_Nil(t *testing.T) {
	t.Parallel()
	var e RevertStatusEnum
	require.NoError(t, e.Scan(nil))
	assert.Equal(t, RevertStatusEnum(""), e)
}

func TestRevertStatusEnum_Value(t *testing.T) {
	t.Parallel()
	v, err := RevertStatusReverted.Value()
	require.NoError(t, err)
	assert.Equal(t, "reverted", v)
}

// ---------------------------------------------------------------------------
// RevertOutcomeEnum
// ---------------------------------------------------------------------------

func TestRevertOutcomeEnum_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "reverted", RevertOutcomeReverted.String())
	assert.Equal(t, "skipped", RevertOutcomeSkipped.String())
	assert.Equal(t, "failed", RevertOutcomeFailed.String())
}

func TestRevertOutcomeEnum_MarshalJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(RevertOutcomeSkipped)
	require.NoError(t, err)
	assert.Equal(t, `"skipped"`, string(data))
}

func TestRevertOutcomeEnum_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	var e RevertOutcomeEnum
	require.NoError(t, json.Unmarshal([]byte(`"reverted"`), &e))
	assert.Equal(t, RevertOutcomeReverted, e)
}

func TestRevertOutcomeEnum_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()
	var e RevertOutcomeEnum
	err := json.Unmarshal([]byte(`true`), &e)
	assert.Error(t, err)
}

func TestRevertOutcomeEnum_Scan_String(t *testing.T) {
	t.Parallel()
	var e RevertOutcomeEnum
	require.NoError(t, e.Scan("reverted"))
	assert.Equal(t, RevertOutcomeReverted, e)
}

func TestRevertOutcomeEnum_Scan_Bytes(t *testing.T) {
	t.Parallel()
	var e RevertOutcomeEnum
	require.NoError(t, e.Scan([]byte("skipped")))
	assert.Equal(t, RevertOutcomeSkipped, e)
}

func TestRevertOutcomeEnum_Scan_Nil(t *testing.T) {
	t.Parallel()
	var e RevertOutcomeEnum
	require.NoError(t, e.Scan(nil))
	assert.Equal(t, RevertOutcomeEnum(""), e)
}

func TestRevertOutcomeEnum_Value(t *testing.T) {
	t.Parallel()
	v, err := RevertOutcomeFailed.Value()
	require.NoError(t, err)
	assert.Equal(t, "failed", v)
}

// ---------------------------------------------------------------------------
// RevertReasonEnum
// ---------------------------------------------------------------------------

func TestRevertReasonEnum_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "anchor_missing", RevertReasonAnchorMissing.String())
	assert.Equal(t, "destination_conflict", RevertReasonDestinationConflict.String())
	assert.Equal(t, "access_denied", RevertReasonAccessDenied.String())
	assert.Equal(t, "unexpected_path_state", RevertReasonUnexpectedPathState.String())
	assert.Equal(t, "nfo_restore_failed", RevertReasonNFORestoreFailed.String())
	assert.Equal(t, "generated_cleanup_failed", RevertReasonGeneratedCleanupFailed.String())
}

func TestRevertReasonEnum_MarshalJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(RevertReasonAnchorMissing)
	require.NoError(t, err)
	assert.Equal(t, `"anchor_missing"`, string(data))
}

func TestRevertReasonEnum_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	var e RevertReasonEnum
	require.NoError(t, json.Unmarshal([]byte(`"access_denied"`), &e))
	assert.Equal(t, RevertReasonAccessDenied, e)
}

func TestRevertReasonEnum_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()
	var e RevertReasonEnum
	err := json.Unmarshal([]byte(`{}`), &e)
	assert.Error(t, err)
}

func TestRevertReasonEnum_Scan_String(t *testing.T) {
	t.Parallel()
	var e RevertReasonEnum
	require.NoError(t, e.Scan("destination_conflict"))
	assert.Equal(t, RevertReasonDestinationConflict, e)
}

func TestRevertReasonEnum_Scan_Bytes(t *testing.T) {
	t.Parallel()
	var e RevertReasonEnum
	require.NoError(t, e.Scan([]byte("nfo_restore_failed")))
	assert.Equal(t, RevertReasonNFORestoreFailed, e)
}

func TestRevertReasonEnum_Scan_Nil(t *testing.T) {
	t.Parallel()
	var e RevertReasonEnum
	require.NoError(t, e.Scan(nil))
	assert.Equal(t, RevertReasonEnum(""), e)
}

func TestRevertReasonEnum_Value(t *testing.T) {
	t.Parallel()
	v, err := RevertReasonUnexpectedPathState.Value()
	require.NoError(t, err)
	assert.Equal(t, "unexpected_path_state", v)
}

// ---------------------------------------------------------------------------
// BatchFileOperation struct
// ---------------------------------------------------------------------------

func TestBatchFileOperation_TableName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "batch_file_operations", BatchFileOperation{}.TableName())
}

func TestBatchFileOperation_Defaults(t *testing.T) {
	t.Parallel()
	op := BatchFileOperation{
		BatchJobID:    "job-1",
		MovieID:       "IPX-123",
		OriginalPath:  "/original/file.mp4",
		NewPath:       "/new/file.mp4",
		OperationType: OperationTypeMove,
		RevertStatus:  RevertStatusApplied,
	}
	assert.Equal(t, "job-1", op.BatchJobID)
	assert.Equal(t, OperationTypeMove, op.OperationType)
	assert.Equal(t, RevertStatusApplied, op.RevertStatus)
	assert.False(t, op.InPlaceRenamed)
	assert.Nil(t, op.RevertedAt)
}

func TestBatchFileOperation_WithRevert(t *testing.T) {
	t.Parallel()
	now := time.Now()
	op := BatchFileOperation{
		BatchJobID:      "job-2",
		OriginalPath:    "/a/file.mp4",
		NewPath:         "/b/file.mp4",
		OperationType:   OperationTypeSymlink,
		RevertStatus:    RevertStatusReverted,
		RevertedAt:      &now,
		InPlaceRenamed:  true,
		OriginalDirPath: "/a",
	}
	assert.Equal(t, RevertStatusReverted, op.RevertStatus)
	assert.NotNil(t, op.RevertedAt)
	assert.True(t, op.InPlaceRenamed)
	assert.Equal(t, "/a", op.OriginalDirPath)
}

// ---------------------------------------------------------------------------
// JSON round-trip for all enum types
// ---------------------------------------------------------------------------

func TestOperationTypeEnum_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	ops := []OperationTypeEnum{
		OperationTypeMove, OperationTypeCopy, OperationTypeHardlink,
		OperationTypeSymlink, OperationTypeUpdate,
	}
	for _, op := range ops {
		data, err := json.Marshal(op)
		require.NoError(t, err)
		var got OperationTypeEnum
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, op, got)
	}
}

func TestRevertStatusEnum_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	statuses := []RevertStatusEnum{RevertStatusApplied, RevertStatusReverted, RevertStatusFailed}
	for _, s := range statuses {
		data, err := json.Marshal(s)
		require.NoError(t, err)
		var got RevertStatusEnum
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, s, got)
	}
}

func TestRevertOutcomeEnum_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	outcomes := []RevertOutcomeEnum{RevertOutcomeReverted, RevertOutcomeSkipped, RevertOutcomeFailed}
	for _, o := range outcomes {
		data, err := json.Marshal(o)
		require.NoError(t, err)
		var got RevertOutcomeEnum
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, o, got)
	}
}

func TestRevertReasonEnum_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	reasons := []RevertReasonEnum{
		RevertReasonAnchorMissing, RevertReasonDestinationConflict,
		RevertReasonAccessDenied, RevertReasonUnexpectedPathState,
		RevertReasonNFORestoreFailed, RevertReasonGeneratedCleanupFailed,
	}
	for _, r := range reasons {
		data, err := json.Marshal(r)
		require.NoError(t, err)
		var got RevertReasonEnum
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, r, got)
	}
}

// ---------------------------------------------------------------------------
// Scan round-trip for all enum types
// ---------------------------------------------------------------------------

func TestOperationTypeEnum_ScanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, op := range []OperationTypeEnum{OperationTypeMove, OperationTypeCopy, OperationTypeUpdate} {
		v, err := op.Value()
		require.NoError(t, err)
		var got OperationTypeEnum
		require.NoError(t, got.Scan(v))
		assert.Equal(t, op, got)
	}
}

func TestRevertStatusEnum_ScanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []RevertStatusEnum{RevertStatusApplied, RevertStatusReverted, RevertStatusFailed} {
		v, err := s.Value()
		require.NoError(t, err)
		var got RevertStatusEnum
		require.NoError(t, got.Scan(v))
		assert.Equal(t, s, got)
	}
}

func TestRevertOutcomeEnum_ScanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, o := range []RevertOutcomeEnum{RevertOutcomeReverted, RevertOutcomeSkipped, RevertOutcomeFailed} {
		v, err := o.Value()
		require.NoError(t, err)
		var got RevertOutcomeEnum
		require.NoError(t, got.Scan(v))
		assert.Equal(t, o, got)
	}
}

func TestRevertReasonEnum_ScanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, r := range []RevertReasonEnum{RevertReasonAnchorMissing, RevertReasonAccessDenied} {
		v, err := r.Value()
		require.NoError(t, err)
		var got RevertReasonEnum
		require.NoError(t, got.Scan(v))
		assert.Equal(t, r, got)
	}
}
