package jobpersist

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseResultsJSON_ResultDataNotMovie simulates a legacy DB record with a
// non-movie Data field (string instead of Movie). ParseResultsJSON should
// handle this gracefully — the non-movie Data is ignored.
func TestParseResultsJSON_ResultDataNotMovie(t *testing.T) {
	legacyResults := map[string]any{
		"/path/file1.mp4": map[string]any{
			"status":    "completed",
			"movie_id":  "ABC-123",
			"data_type": "movie",
			"data":      "not a movie",
		},
	}
	resultsJSON, _ := json.Marshal(legacyResults)

	parsed, err := ParseResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "/path/file1.mp4")
	assert.Nil(t, parsed.Results["/path/file1.mp4"].Movie)
}

// TestParseResultsJSON_LegacyFormatWithMovieData verifies a legacy-format
// payload with data_type + data produces a non-nil Movie.
func TestParseResultsJSON_LegacyFormatWithMovieData(t *testing.T) {
	legacyResults := map[string]any{
		"/path/file1.mp4": map[string]any{
			"file_path": "/path/file1.mp4",
			"movie_id":  "ABC-123",
			"status":    "completed",
			"data_type": "movie",
			"data": map[string]any{
				"id":    "ABC-123",
				"title": "Test Movie",
			},
			"started_at": "2026-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(legacyResults)

	parsed, err := ParseResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "/path/file1.mp4")

	mr := parsed.Results["/path/file1.mp4"]
	assert.Equal(t, "ABC-123", mr.FileMatchInfo.MovieID)
	assert.Equal(t, "completed", string(mr.Status))
	require.NotNil(t, mr.Movie, "legacy-format JSON with data_type should produce non-nil Movie")
	assert.Equal(t, "ABC-123", mr.Movie.ID)
	assert.Equal(t, "Test Movie", mr.Movie.Title)
}

// TestParseResultsJSON_LegacyFormatWithProvenance verifies provenance
// extraction from a legacy-format payload.
func TestParseResultsJSON_LegacyFormatWithProvenance(t *testing.T) {
	legacyResults := map[string]any{
		"/path/file1.mp4": map[string]any{
			"file_path": "/path/file1.mp4",
			"movie_id":  "ABC-123",
			"status":    "completed",
			"data_type": "movie",
			"data": map[string]any{
				"id":    "ABC-123",
				"title": "Test Movie",
			},
			"field_sources":   map[string]string{"title": "r18dev"},
			"actress_sources": map[string]string{"actress_0": "dmm"},
			"started_at":      "2026-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(legacyResults)

	parsed, err := ParseResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.NotNil(t, parsed.Provenance["/path/file1.mp4"])
	assert.Equal(t, "r18dev", parsed.Provenance["/path/file1.mp4"].FieldSources["title"])
	assert.Equal(t, "dmm", parsed.Provenance["/path/file1.mp4"].ActressSources["actress_0"])
}
