package contracts

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Regression tests for bugs found during rebase ---

// TestBatchFileResult_UnmarshalJSON_LegacyDataField verifies that the legacy
// "data" JSON field (used by the old worker.FileResult before the rebase)
// is correctly mapped to the "movie" field in BatchFileResult.
//
// Regression: /jobs page thumbnails not showing because persisted DB results
// used "data" but the new contract expected "movie", resulting in nil Movie.
func TestBatchFileResult_UnmarshalJSON_LegacyDataField(t *testing.T) {
	t.Parallel()

	// Simulate a persisted result from the old format using "data"
	legacyJSON := `{
		"result_id": "test-uuid-123",
		"file_path": "/videos/ABCD-123.mp4",
		"movie_id": "ABCD-123",
		"status": "completed",
		"data": {
			"id": "ABCD-123",
			"content_id": "ABCD-123",
			"title": "Test Movie",
			"poster_url": "https://example.com/poster.jpg",
			"cropped_poster_url": "https://example.com/cropped.jpg",
			"cover_url": "https://example.com/cover.jpg"
		},
		"started_at": "2024-01-15T10:00:00Z"
	}`

	var result BatchFileResult
	err := json.Unmarshal([]byte(legacyJSON), &result)
	require.NoError(t, err)

	assert.Equal(t, "test-uuid-123", result.ResultID)
	assert.Equal(t, "/videos/ABCD-123.mp4", result.FilePath)
	assert.Equal(t, "ABCD-123", result.MovieID)
	require.NotNil(t, result.Movie, "Movie should be populated from legacy 'data' field")
	assert.Equal(t, "ABCD-123", result.Movie.ID)
	assert.Equal(t, "ABCD-123", result.Movie.Code, "content_id should be projected to Code")
	assert.Equal(t, "https://example.com/poster.jpg", result.Movie.PosterURL)
	assert.Equal(t, "https://example.com/cropped.jpg", result.Movie.CroppedPosterURL)
}

// TestBatchFileResult_UnmarshalJSON_NewMovieField verifies that the new "movie"
// JSON field is correctly unmarshaled.
func TestBatchFileResult_UnmarshalJSON_NewMovieField(t *testing.T) {
	t.Parallel()

	newJSON := `{
		"result_id": "test-uuid-456",
		"file_path": "/videos/EFGH-456.mp4",
		"movie_id": "EFGH-456",
		"status": "completed",
		"movie": {
			"id": "EFGH-456",
			"content_id": "EFGH-456",
			"title": "New Movie",
			"poster_url": "https://example.com/new-poster.jpg"
		},
		"started_at": "2024-01-15T10:00:00Z"
	}`

	var result BatchFileResult
	err := json.Unmarshal([]byte(newJSON), &result)
	require.NoError(t, err)

	assert.Equal(t, "test-uuid-456", result.ResultID)
	require.NotNil(t, result.Movie, "Movie should be populated from 'movie' field")
	assert.Equal(t, "EFGH-456", result.Movie.ID)
	assert.Equal(t, "EFGH-456", result.Movie.Code, "content_id should be projected to Code")
	assert.Equal(t, "https://example.com/new-poster.jpg", result.Movie.PosterURL)
}

// TestBatchFileResult_UnmarshalJSON_MovieTakesPrecedenceOverData verifies that
// when both "data" and "movie" are present, "movie" takes precedence.
func TestBatchFileResult_UnmarshalJSON_MovieTakesPrecedenceOverData(t *testing.T) {
	t.Parallel()

	bothFieldsJSON := `{
		"result_id": "test-uuid-789",
		"file_path": "/videos/test.mp4",
		"movie_id": "TEST-001",
		"status": "completed",
		"data": {
			"id": "OLD-ID",
			"content_id": "OLD-ID",
			"title": "Old Title"
		},
		"movie": {
			"id": "NEW-ID",
			"content_id": "NEW-ID",
			"title": "New Title"
		},
		"started_at": "2024-01-15T10:00:00Z"
	}`

	var result BatchFileResult
	err := json.Unmarshal([]byte(bothFieldsJSON), &result)
	require.NoError(t, err)

	require.NotNil(t, result.Movie)
	assert.Equal(t, "NEW-ID", result.Movie.ID, "'movie' should take precedence over 'data'")
	assert.Equal(t, "New Title", result.Movie.Title)
}

// TestBatchFileResult_UnmarshalJSON_NoMovieOrData verifies that when neither
// "data" nor "movie" is present, Movie is nil.
func TestBatchFileResult_UnmarshalJSON_NoMovieOrData(t *testing.T) {
	t.Parallel()

	noMovieJSON := `{
		"result_id": "test-uuid-nomovie",
		"file_path": "/videos/test.mp4",
		"movie_id": "TEST-001",
		"status": "failed",
		"error": "scrape failed",
		"started_at": "2024-01-15T10:00:00Z"
	}`

	var result BatchFileResult
	err := json.Unmarshal([]byte(noMovieJSON), &result)
	require.NoError(t, err)

	assert.Nil(t, result.Movie)
	assert.Equal(t, "scrape failed", result.Error)
}

// TestBatchJobResponse_FilesField verifies that the Files field is present
// in the serialized JSON so the /jobs page can show file names instead of
// falling back to Object.keys(results).
//
// Regression: /jobs page showing "domain +1more" because Files was missing
// from the API response.
func TestBatchJobResponse_FilesField(t *testing.T) {
	t.Parallel()

	resp := BatchJobResponse{
		ID:         "test-job-id",
		Status:     models.JobStatusCompleted,
		TotalFiles: 2,
		Completed:  2,
		Failed:     0,
		Progress:   100,
		Files:      []string{"/videos/ABCD-123.mp4", "/videos/EFGH-456.mp4"},
		Results: map[string]*BatchFileResult{
			"/videos/ABCD-123.mp4": {
				ResultID: "r1",
				FilePath: "/videos/ABCD-123.mp4",
				MovieID:  "ABCD-123",
				Status:   models.JobStatusCompleted,
			},
		},
		StartedAt: "2024-01-15T10:00:00Z",
		Update:    false,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	// Verify "files" is present in the JSON output
	files, ok := parsed["files"].([]interface{})
	require.True(t, ok, "files field should be present in JSON output")
	require.Len(t, files, 2, "files should contain 2 entries")
	assert.Equal(t, "/videos/ABCD-123.mp4", files[0])
	assert.Equal(t, "/videos/EFGH-456.mp4", files[1])
}

// TestBatchFileResult_EmptyResultID verifies that a BatchFileResult with an
// empty result_id (from legacy DB entries) can still be unmarshaled.
// The list endpoint assigns a UUID for empty result_ids.
func TestBatchFileResult_EmptyResultID(t *testing.T) {
	t.Parallel()

	legacyNoResultID := `{
		"file_path": "/videos/ABCD-123.mp4",
		"movie_id": "ABCD-123",
		"status": "completed",
		"data": {
			"id": "ABCD-123",
			"content_id": "ABCD-123",
			"title": "Test"
		},
		"started_at": "2024-01-15T10:00:00Z"
	}`

	var result BatchFileResult
	err := json.Unmarshal([]byte(legacyNoResultID), &result)
	require.NoError(t, err)

	assert.Equal(t, "", result.ResultID, "legacy entries have empty result_id")
	require.NotNil(t, result.Movie, "Movie should still be populated from 'data'")
	assert.Equal(t, "ABCD-123", result.Movie.ID)
}

// TestBatchFileResult_LegacyDataField_PosterURLRoundTrip verifies that poster_url
// and cropped_poster_url survive the full DB→unmarshal→re-marshal round trip.
// This is critical for /jobs page thumbnails which read r.movie?.poster_url.
//
// Regression: /jobs page thumbnails not showing because the legacy "data" field
// was not mapped to "movie", causing poster URLs to be lost in the API response.
func TestBatchFileResult_LegacyDataField_PosterURLRoundTrip(t *testing.T) {
	t.Parallel()

	// Simulate a persisted result from the old format with poster URLs
	legacyJSON := `{
		"result_id": "poster-test-uuid",
		"file_path": "/videos/ABCD-123.mp4",
		"movie_id": "ABCD-123",
		"status": "completed",
		"data": {
			"id": "ABCD-123",
			"content_id": "ABCD-123",
			"title": "Test Movie With Poster",
			"poster_url": "https://r18.dev/images/poster.jpg",
			"cropped_poster_url": "/api/v1/temp/posters/job-123/ABCD-123.jpg?v=1234567890",
			"cover_url": "https://r18.dev/images/cover.jpg"
		},
		"started_at": "2024-01-15T10:00:00Z"
	}`

	var result BatchFileResult
	err := json.Unmarshal([]byte(legacyJSON), &result)
	require.NoError(t, err)

	// Verify Movie is populated with poster data
	require.NotNil(t, result.Movie, "Movie should be populated from legacy 'data' field")
	assert.Equal(t, "https://r18.dev/images/poster.jpg", result.Movie.PosterURL,
		"poster_url should be preserved through data→movie mapping")
	assert.Equal(t, "/api/v1/temp/posters/job-123/ABCD-123.jpg?v=1234567890", result.Movie.CroppedPosterURL,
		"cropped_poster_url should be preserved through data→movie mapping")

	// Re-serialize (simulating what the API does) and verify poster fields are in the output
	reencoded, err := json.Marshal(result)
	require.NoError(t, err)

	var reSerialized map[string]interface{}
	require.NoError(t, json.Unmarshal(reencoded, &reSerialized))

	movie := reSerialized["movie"].(map[string]interface{})
	assert.Equal(t, "https://r18.dev/images/poster.jpg", movie["poster_url"],
		"poster_url should be in re-serialized JSON for frontend thumbnail rendering")
	assert.Equal(t, "/api/v1/temp/posters/job-123/ABCD-123.jpg?v=1234567890", movie["cropped_poster_url"],
		"cropped_poster_url should be in re-serialized JSON")
	// content_id is now code in the API contract
	assert.Equal(t, "ABCD-123", movie["code"],
		"content_id should be projected to code in re-serialized JSON")
}
