package websocket

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgressStatus_String(t *testing.T) {
	assert.Equal(t, "success", ProgressStatusSuccess.String())
	assert.Equal(t, "error", ProgressStatusError.String())
	assert.Equal(t, "pending", ProgressStatusPending.String())
}

func TestProgressStatus_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(ProgressStatusSuccess)
	require.NoError(t, err)
	assert.Equal(t, `"success"`, string(data))
}

func TestProgressStatus_UnmarshalJSON(t *testing.T) {
	var s ProgressStatus
	err := json.Unmarshal([]byte(`"error"`), &s)
	require.NoError(t, err)
	assert.Equal(t, ProgressStatusError, s)
}

func TestProgressStatus_UnmarshalJSON_Invalid(t *testing.T) {
	var s ProgressStatus
	err := json.Unmarshal([]byte(`123`), &s)
	assert.Error(t, err)
}
