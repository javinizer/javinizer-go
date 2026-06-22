package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobStatus_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "pending", JobStatusPending.String())
	assert.Equal(t, "running", JobStatusRunning.String())
	assert.Equal(t, "completed", JobStatusCompleted.String())
	assert.Equal(t, "failed", JobStatusFailed.String())
	assert.Equal(t, "cancelled", JobStatusCancelled.String())
	assert.Equal(t, "organized", JobStatusOrganized.String())
	assert.Equal(t, "reverted", JobStatusReverted.String())
}

func TestJobStatus_MarshalJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(JobStatusRunning)
	require.NoError(t, err)
	assert.Equal(t, `"running"`, string(data))
}

func TestJobStatus_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	var s JobStatus
	require.NoError(t, json.Unmarshal([]byte(`"completed"`), &s))
	assert.Equal(t, JobStatusCompleted, s)
}

func TestJobStatus_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()
	var s JobStatus
	err := json.Unmarshal([]byte(`42`), &s)
	assert.Error(t, err)
}

func TestJobStatus_Scan_String(t *testing.T) {
	t.Parallel()
	var s JobStatus
	require.NoError(t, s.Scan("failed"))
	assert.Equal(t, JobStatusFailed, s)
}

func TestJobStatus_Scan_Bytes(t *testing.T) {
	t.Parallel()
	var s JobStatus
	require.NoError(t, s.Scan([]byte("pending")))
	assert.Equal(t, JobStatusPending, s)
}

func TestJobStatus_Scan_Nil(t *testing.T) {
	t.Parallel()
	var s JobStatus
	require.NoError(t, s.Scan(nil))
	assert.Equal(t, JobStatus(""), s)
}

func TestJobStatus_Value(t *testing.T) {
	t.Parallel()
	v, err := JobStatusOrganized.Value()
	require.NoError(t, err)
	assert.Equal(t, "organized", v)
}

func TestJobStatus_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	statuses := []JobStatus{
		JobStatusPending, JobStatusRunning, JobStatusCompleted,
		JobStatusFailed, JobStatusCancelled, JobStatusOrganized, JobStatusReverted,
	}
	for _, s := range statuses {
		data, err := json.Marshal(s)
		require.NoError(t, err)
		var got JobStatus
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, s, got)
	}
}

func TestJobStatus_ScanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []JobStatus{JobStatusPending, JobStatusRunning, JobStatusCompleted} {
		v, err := s.Value()
		require.NoError(t, err)
		var got JobStatus
		require.NoError(t, got.Scan(v))
		assert.Equal(t, s, got)
	}
}

// ---------------------------------------------------------------------------
// Job struct methods
// ---------------------------------------------------------------------------

func TestJob_TableName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "jobs", Job{}.TableName())
}

func TestJob_ParseResults_Empty(t *testing.T) {
	t.Parallel()
	j := &Job{Results: ""}
	var v []string
	require.NoError(t, j.ParseResults(&v))
	assert.Nil(t, v)
}

func TestJob_ParseResults_Valid(t *testing.T) {
	t.Parallel()
	j := &Job{Results: `["a","b"]`}
	var v []string
	require.NoError(t, j.ParseResults(&v))
	assert.Equal(t, []string{"a", "b"}, v)
}

func TestJob_ParseResults_Invalid(t *testing.T) {
	t.Parallel()
	j := &Job{ID: "test-job", Results: `{invalid}`}
	var v any
	err := j.ParseResults(&v)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse results for job test-job")
}

func TestJob_ParseExcluded_Empty(t *testing.T) {
	t.Parallel()
	j := &Job{Excluded: ""}
	var v []string
	require.NoError(t, j.ParseExcluded(&v))
	assert.Nil(t, v)
}

func TestJob_ParseExcluded_Valid(t *testing.T) {
	t.Parallel()
	j := &Job{Excluded: `["x","y"]`}
	var v []string
	require.NoError(t, j.ParseExcluded(&v))
	assert.Equal(t, []string{"x", "y"}, v)
}

func TestJob_ParseExcluded_Invalid(t *testing.T) {
	t.Parallel()
	j := &Job{ID: "test-job", Excluded: `{bad}`}
	var v any
	err := j.ParseExcluded(&v)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse excluded for job test-job")
}
