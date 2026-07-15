package jobpersist

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseResultsJSON_FalsePositiveDomainInTitle verifies that a movie
// title containing the literal string "domain" (as a nested field value) does
// NOT trigger envelope format detection. This was the false-positive scenario
// that the old bytes.Contains(raw, []byte("domain")) approach suffered from.
func TestParseResultsJSON_FalsePositiveDomainInTitle(t *testing.T) {
	oldResults := map[string]any{
		"/videos/ABC-001.mp4": map[string]any{
			"result_id": "uuid-001",
			"file_match_info": map[string]any{
				"path":     "/videos/ABC-001.mp4",
				"movie_id": "ABC-001",
			},
			"revision": 1,
			"status":   "completed",
			"movie": map[string]any{
				"id":    "ABC-001",
				"title": "Researching domain-specific parsing",
			},
			"started_at": "2024-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(oldResults)

	parsed, err := ParseResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "/videos/ABC-001.mp4")
	mr := parsed.Results["/videos/ABC-001.mp4"]
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "ABC-001", mr.Movie.ID)
	assert.Equal(t, "Researching domain-specific parsing", mr.Movie.Title)
}

// TestParseResultsJSON_FalsePositiveDataTypeInTitle verifies that a movie
// title containing "data_type" as a nested value does not trigger legacy
// format detection.
func TestParseResultsJSON_FalsePositiveDataTypeInTitle(t *testing.T) {
	oldResults := map[string]any{
		"/videos/ABC-002.mp4": map[string]any{
			"result_id": "uuid-002",
			"file_match_info": map[string]any{
				"path":     "/videos/ABC-002.mp4",
				"movie_id": "ABC-002",
			},
			"revision": 1,
			"status":   "completed",
			"movie": map[string]any{
				"id":    "ABC-002",
				"title": "data_type field analysis",
			},
			"started_at": "2024-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(oldResults)

	parsed, err := ParseResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "/videos/ABC-002.mp4")
	mr := parsed.Results["/videos/ABC-002.mp4"]
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "ABC-002", mr.Movie.ID)
}

// --- ParseResultsJSON: legacy format with provenance ---

func TestParseResultsJSON_LegacyWithProvenance(t *testing.T) {
	legacyResults := map[string]any{
		"file1.mp4": map[string]any{
			"file_path":       "file1.mp4",
			"movie_id":        "PROV-001",
			"status":          "completed",
			"data_type":       "movie",
			"field_sources":   map[string]string{"title": "r18dev", "maker": "dmm"},
			"actress_sources": map[string]string{"actress_0": "javdb"},
			"started_at":      "2026-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(legacyResults)

	parsed, err := ParseResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "file1.mp4")
	mr := parsed.Results["file1.mp4"]
	assert.Equal(t, "PROV-001", mr.FileMatchInfo.MovieID)

	require.NotNil(t, parsed.Provenance["file1.mp4"])
	assert.Equal(t, "r18dev", parsed.Provenance["file1.mp4"].FieldSources["title"])
	assert.Equal(t, "javdb", parsed.Provenance["file1.mp4"].ActressSources["actress_0"])
}

// --- ParseResultsJSON: legacy format no provenance ---

func TestParseResultsJSON_LegacyNoProvenance(t *testing.T) {
	legacyResults := map[string]any{
		"file1.mp4": map[string]any{
			"file_path":  "file1.mp4",
			"movie_id":   "NOPROV-001",
			"status":     "completed",
			"data_type":  "movie",
			"started_at": "2026-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(legacyResults)

	parsed, err := ParseResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "file1.mp4")
	assert.Nil(t, parsed.Provenance["file1.mp4"], "provenance should be nil when FieldSources and ActressSources are nil")
}

func TestParseResultsJSON_Empty(t *testing.T) {
	parsed, err := ParseResultsJSON(nil)
	require.NoError(t, err)
	assert.Empty(t, parsed.Results)
}

func TestParseResultsJSON_LegacyWithDataUncovered(t *testing.T) {
	legacyResults := map[string]any{
		"file1.mp4": map[string]any{
			"file_path":           "file1.mp4",
			"movie_id":            "ABC-001",
			"revision":            1,
			"status":              "completed",
			"translation_warning": "partial",
			"data_type":           "movie",
			"data":                map[string]any{"id": "ABC-001", "title": "Test"},
			"started_at":          "2026-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(legacyResults)

	parsed, err := ParseResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "file1.mp4")
	mr := parsed.Results["file1.mp4"]
	assert.Equal(t, "file1.mp4", mr.FileMatchInfo.Path)
	assert.Equal(t, "ABC-001", mr.FileMatchInfo.MovieID)
	assert.Equal(t, uint64(1), mr.Revision)
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "ABC-001", mr.Movie.ID)
	require.NotNil(t, mr.TranslationWarning)
	assert.Equal(t, "partial", *mr.TranslationWarning)
}
