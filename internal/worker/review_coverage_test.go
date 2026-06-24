package worker

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseJobResultsJSON_FalsePositiveDomainInTitle verifies that a movie
// title containing the literal string "domain" (as a nested field value) does
// NOT trigger envelope format detection. This was the false-positive scenario
// that the old bytes.Contains(raw, []byte("domain")) approach suffered from.
func TestParseJobResultsJSON_FalsePositiveDomainInTitle(t *testing.T) {
	// Old MovieResult format with a movie title containing "domain"
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

	parsed, err := ParseJobResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "/videos/ABC-001.mp4")
	mr := parsed.Results["/videos/ABC-001.mp4"]
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "ABC-001", mr.Movie.ID)
	assert.Equal(t, "Researching domain-specific parsing", mr.Movie.Title)
}

// TestParseJobResultsJSON_FalsePositiveDataTypeInTitle verifies that a movie
// title containing "data_type" as a nested value does not trigger legacy
// format detection.
func TestParseJobResultsJSON_FalsePositiveDataTypeInTitle(t *testing.T) {
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

	parsed, err := ParseJobResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "/videos/ABC-002.mp4")
	mr := parsed.Results["/videos/ABC-002.mp4"]
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "ABC-002", mr.Movie.ID)
}

// TestMovieIDsForResult_CaseInsensitiveDedup verifies that movieIDsForResult
// deduplicates using normalized (lowercased) keys, matching the indexKey()
// used by movieIDIndex. Without normalization, case-only variants (e.g. "ABC"
// and "abc") would both be returned, causing stale entries on removal.
func TestMovieIDsForResult_CaseInsensitiveDedup(t *testing.T) {
	t.Run("same ID different case deduplicates", func(t *testing.T) {
		r := &MovieResult{
			Movie: &models.Movie{ID: "ABC-001"},
			FileMatchInfo: models.FileMatchInfo{
				MovieID: "abc-001",
			},
		}
		ids := movieIDsForResult(r)
		assert.Len(t, ids, 1, "case-only variants should deduplicate to one ID")
		assert.Equal(t, "ABC-001", ids[0])
	})

	t.Run("different IDs both returned", func(t *testing.T) {
		r := &MovieResult{
			Movie: &models.Movie{ID: "ABC-001"},
			FileMatchInfo: models.FileMatchInfo{
				MovieID: "DEF-002",
			},
		}
		ids := movieIDsForResult(r)
		assert.Len(t, ids, 2)
	})

	t.Run("nil movie returns FileMatchInfo MovieID", func(t *testing.T) {
		r := &MovieResult{
			FileMatchInfo: models.FileMatchInfo{
				MovieID: "ABC-001",
			},
		}
		ids := movieIDsForResult(r)
		assert.Len(t, ids, 1)
		assert.Equal(t, "ABC-001", ids[0])
	})

	t.Run("nil result returns nil", func(t *testing.T) {
		ids := movieIDsForResult(nil)
		assert.Nil(t, ids)
	})
}

// TestTrackApplyResults_PanicCountsAsFailed verifies that trackApplyResults
// counts panicked outcomes as failures, so MarkOrganized() is not called
// when panics occurred.
func TestTrackApplyResults_PanicCountsAsFailed(t *testing.T) {
	t.Run("panic without Failed flag counts as failure", func(t *testing.T) {
		outcomes := []applyFileOutcome{
			{FilePath: "a.mp4", Success: true, Failed: false, Panic: false},
			{FilePath: "b.mp4", Success: false, Failed: false, Panic: true, PanicMsg: "boom"},
		}
		var organized, failed int64
		trackApplyResults(outcomes, &organized, &failed)
		assert.Equal(t, int64(1), organized)
		assert.Equal(t, int64(1), failed, "panicked outcome must count toward failed")
	})

	t.Run("all success with no panic", func(t *testing.T) {
		outcomes := []applyFileOutcome{
			{FilePath: "a.mp4", Success: true},
			{FilePath: "b.mp4", Success: true},
		}
		var organized, failed int64
		trackApplyResults(outcomes, &organized, &failed)
		assert.Equal(t, int64(2), organized)
		assert.Equal(t, int64(0), failed)
	})

	t.Run("mixed failed and panic", func(t *testing.T) {
		outcomes := []applyFileOutcome{
			{FilePath: "a.mp4", Success: true},
			{FilePath: "b.mp4", Success: false, Failed: true},
			{FilePath: "c.mp4", Success: false, Panic: true},
		}
		var organized, failed int64
		trackApplyResults(outcomes, &organized, &failed)
		assert.Equal(t, int64(1), organized)
		assert.Equal(t, int64(2), failed)
	})
}

// TestUpdateFileResult_ExcludedDecrementSkip verifies that UpdateFileResult
// does NOT decrement counters when replacing a result for an excluded file.
// Without the !Excluded guard, a file that was Completed (counted), then
// excluded (MarkExcluded decrements), then updated again would double-decrement.
func TestUpdateFileResult_ExcludedDecrementSkip(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	// file1 completes — counter incremented
	job.results.UpdateFileResult("file1.mp4", &MovieResult{Status: models.JobStatusCompleted})
	assert.Equal(t, 1, job.results.Completed)

	// file1 is excluded — MarkExcluded decrements the counter
	job.results.MarkExcluded("file1.mp4")
	assert.Equal(t, 0, job.results.Completed, "MarkExcluded should decrement Completed")
	assert.True(t, job.results.Excluded["file1.mp4"])

	// Now UpdateFileResult is called again for the excluded file.
	// Without the !Excluded guard on decrement, this would decrement
	// Completed from 0 to -1 (counter drift).
	job.results.UpdateFileResult("file1.mp4", &MovieResult{Status: models.JobStatusFailed})
	assert.Equal(t, 0, job.results.Completed, "excluded file must NOT decrement Completed")
	assert.Equal(t, 0, job.results.Failed, "excluded file must NOT increment Failed")
}
