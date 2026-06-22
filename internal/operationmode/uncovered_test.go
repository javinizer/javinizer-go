package operationmode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationMode_MarshalJSON_Uncovered(t *testing.T) {
	m := OperationModeInPlace
	data, err := json.Marshal(m)
	require.NoError(t, err)
	assert.Equal(t, `"in-place"`, string(data))
}

func TestOperationMode_UnmarshalJSON_Uncovered(t *testing.T) {
	var m OperationMode
	err := json.Unmarshal([]byte(`"metadata-artwork"`), &m)
	require.NoError(t, err)
	assert.Equal(t, OperationModeMetadataArtwork, m)
}

func TestOperationMode_UnmarshalJSON_InvalidUncovered(t *testing.T) {
	var m OperationMode
	err := json.Unmarshal([]byte(`123`), &m)
	assert.Error(t, err)
}

func TestOperationMode_Scan_NilUncovered(t *testing.T) {
	var m OperationMode
	err := m.Scan(nil)
	require.NoError(t, err)
	assert.Equal(t, OperationMode(""), m)
}

func TestOperationMode_Scan_BytesUncovered(t *testing.T) {
	var m OperationMode
	err := m.Scan([]byte("preview"))
	require.NoError(t, err)
	assert.Equal(t, OperationModePreview, m)
}

func TestOperationMode_ValueUncovered(t *testing.T) {
	m := OperationModeOrganize
	val, err := m.Value()
	require.NoError(t, err)
	assert.Equal(t, "organize", val)
}

func TestOperationMode_StringUncovered(t *testing.T) {
	assert.Equal(t, "organize", string(OperationModeOrganize))
	assert.Equal(t, "in-place", OperationModeInPlace.String())
}

func TestParseOperationMode_CaseInsensitiveUncovered(t *testing.T) {
	m, err := ParseOperationMode("IN-PLACE")
	require.NoError(t, err)
	assert.Equal(t, OperationModeInPlace, m)
}

func TestParseOperationMode_WithWhitespaceUncovered(t *testing.T) {
	m, err := ParseOperationMode("  preview  ")
	require.NoError(t, err)
	assert.Equal(t, OperationModePreview, m)
}
