package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseJobResultsJSON_EnvelopeFormatNilDomain(t *testing.T) {
	raw := []byte(`{"domain": null, "provenance": null}`)
	parsed, err := ParseJobResultsJSON(raw)
	require.NoError(t, err)
	assert.NotNil(t, parsed.Results)
	assert.NotNil(t, parsed.Provenance)
	assert.Equal(t, 0, len(parsed.Results))
}

func TestParseJobResultsJSON_EnvelopeFormatEmpty(t *testing.T) {
	raw := []byte(`{"domain": {}, "provenance": {}}`)
	parsed, err := ParseJobResultsJSON(raw)
	require.NoError(t, err)
	assert.NotNil(t, parsed.Results)
	assert.Equal(t, 0, len(parsed.Results))
}

func TestParseJobResultsJSON_LegacyFormatWithMovieDataUnmarshal(t *testing.T) {
	raw := []byte(`{"file1.mp4": {"data_type": "movie", "file_path": "file1.mp4", "movie_id": "ABC-001", "data": "invalid json", "result_id": "r1"}}`)
	parsed, err := ParseJobResultsJSON(raw)
	require.NoError(t, err)
	assert.Len(t, parsed.Results, 1)
	result := parsed.Results["file1.mp4"]
	assert.NotNil(t, result)
	assert.Nil(t, result.Movie)
}

func TestParseJobResultsJSON_LegacyFormatInvalidJSON(t *testing.T) {
	raw := []byte(`{"file1.mp4": {"data_type": "movie"}, "file2.mp4": 123}`)
	_, err := ParseJobResultsJSON(raw)
	assert.Error(t, err)
}

func TestParseJobResultsJSON_EnvelopeFormatInvalidJSON(t *testing.T) {
	raw := []byte(`{"domain": "not a map"}`)
	_, err := ParseJobResultsJSON(raw)
	assert.Error(t, err)
}
