package batch

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
)

// --- Rebase regression tests ---
//
// These tests verify bugs found after rebasing the architecture-refactor
// branch onto main. Each test corresponds to a regression the user reported.

// TestRebase_PreviewUsesResultID verifies that the preview endpoint routes
// by result_id (not movie_id), so that the preview still works after a
// rescrape changes the movie_id.
//
// Regression: review page not showing output preview because the rebase
// accidentally used movie_id for routing, which changes on rescrape/edit.
func TestRebase_PreviewUsesResultID(t *testing.T) {
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile: true,
			},
			MediaFormat: config.OutputMediaFormatConfig{
				PosterFormat:     "<ID>-poster.jpg",
				FanartFormat:     "<ID>-fanart.jpg",
				ScreenshotFolder: "extrafanart",
			},
			Download: config.OutputDownloadConfig{
				DownloadCover:  true,
				DownloadPoster: true,
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/path", "/output"},
			},
		},
	}

	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})

	// Create a result with a known ResultID
	result := &worker.MovieResult{
		ResultID:      "stable-result-uuid-001",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", Title: "Test Movie"},
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/path/to/IPX-535.mp4", result)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{
		Destination: "/output",
	})

	// Use the stable resultID, not the movieID
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/stable-result-uuid-001/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code, "preview should succeed when using result_id routing")

	var resp contracts.OrganizePreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.FolderName)
	assert.NotEmpty(t, resp.FileName)
}

// TestRebase_PreviewResultIDNotFound verifies that using a nonexistent
// result_id returns 404, not a 500 or silent failure.
func TestRebase_PreviewResultIDNotFound(t *testing.T) {
	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			Operation: config.OutputOperationConfig{RenameFile: true},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/path", "/output"},
			},
		},
	}

	deps := createTestDeps(t, cfg, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/IPX-535.mp4"})
	result := &worker.MovieResult{
		ResultID:      "real-result-id",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/IPX-535.mp4", MovieID: "IPX-535"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "IPX-535", Title: "Test"},
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/path/to/IPX-535.mp4", result)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	body, _ := json.Marshal(contracts.OrganizePreviewRequest{Destination: "/output"})

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/nonexistent-result-id/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code, "nonexistent result_id should return 404")
}

// TestRebase_JobResponseIncludesFiles verifies that the BatchJobResponse
// includes a Files field so the /jobs page can show file names.
//
// Regression: /jobs page showing "domain +1more" because the BatchJobResponse
// contract was missing a Files field, causing the frontend to fall back to
// Object.keys(results) which showed unexpected keys.
func TestRebase_JobResponseIncludesFiles(t *testing.T) {
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/videos/ABCD-123.mp4", "/videos/EFGH-456.mp4"})
	result := &worker.MovieResult{
		ResultID:      "r1",
		FileMatchInfo: models.FileMatchInfo{Path: "/videos/ABCD-123.mp4", MovieID: "ABCD-123"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABCD-123", Title: "Test"},
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/videos/ABCD-123.mp4", result)

	// Get the full job status which is what buildBatchJobResponse uses
	status := job.GetStatus()

	resp := buildBatchJobResponse(status)

	require.NotNil(t, resp.Files, "Files should be populated in BatchJobResponse")
	assert.Len(t, resp.Files, 2, "Files should contain the original file list")
	assert.Contains(t, resp.Files, "/videos/ABCD-123.mp4")
	assert.Contains(t, resp.Files, "/videos/EFGH-456.mp4")
}

// TestRebase_ResultIDPopulatedInResponse verifies that the BatchFileResult
// in the API response has a populated result_id, even for results that
// were created without one (legacy DB entries).
//
// Regression: review page not showing output preview because result_id was
// empty in the list endpoint response, causing the preview API to 404.
func TestRebase_ResultIDPopulatedInResponse(t *testing.T) {
	initTestWebSocket(t)

	cfg := &config.Config{}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobStore.CreateJobBatch([]string{"/videos/ABCD-123.mp4"})

	// Simulate a result with an empty ResultID (legacy entry)
	result := &worker.MovieResult{
		ResultID:      "", // Empty — simulates legacy persisted result
		FileMatchInfo: models.FileMatchInfo{Path: "/videos/ABCD-123.mp4", MovieID: "ABCD-123"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABCD-123", Title: "Test"},
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/videos/ABCD-123.mp4", result)

	status := job.GetStatus()
	resp := buildBatchJobResponse(status)

	// The live BatchJob assigns ResultIDs in rebuildMovieIDIndexLocked
	for _, r := range resp.Results {
		assert.NotEmpty(t, r.ResultID, "result_id should be populated even for legacy entries")
	}
}

// TestRebase_ListEndpointHandlesEnvelopeFormat verifies that the list endpoint
// correctly parses the new envelope DB format ({"domain": ...}) and produces
// BatchFileResult with populated file_path, movie_id, movie data, and poster URLs.
//
// Regression: /jobs page thumbnails not showing AND poster crop modal broken because
// the list endpoint used raw ParseResults into BatchFileResult, which didn't handle
// the envelope format or the nested file_match_info in MovieResult. This meant
// file_path, movie_id, and Movie were all empty in the API response.
func TestRebase_ListEndpointHandlesEnvelopeFormat(t *testing.T) {
	// Construct a models.Job with envelope-format Results
	movieResultJSON := `{
		"result_id": "env-test-uuid",
		"file_match_info": {"path": "/videos/ABCD-123.mp4", "movie_id": "ABCD-123"},
		"revision": 1,
		"status": "completed",
		"movie": {
			"id": "ABCD-123",
			"content_id": "ABCD-123",
			"title": "Envelope Test",
			"poster_url": "https://example.com/poster.jpg",
			"cropped_poster_url": "/api/v1/temp/posters/test-job/ABCD-123.jpg?v=123"
		},
		"started_at": "2024-01-15T10:00:00Z"
	}`

	envelopeJSON := `{"domain": {"/videos/ABCD-123.mp4": ` + movieResultJSON + `}}`

	job := &models.Job{
		ID:      "test-job-id",
		Status:  models.JobStatusCompleted,
		Results: envelopeJSON,
	}

	results := parseAndConvertJobResults(job, nil)

	require.Len(t, results, 1)

	r, ok := results["/videos/ABCD-123.mp4"]
	require.True(t, ok)
	assert.Equal(t, "env-test-uuid", r.ResultID)
	assert.Equal(t, "/videos/ABCD-123.mp4", r.FilePath, "file_path should be populated from file_match_info.path")
	assert.Equal(t, "ABCD-123", r.MovieID, "movie_id should be populated from file_match_info.movie_id")
	require.NotNil(t, r.Movie, "Movie should be populated")
	assert.Equal(t, "ABCD-123", r.Movie.ID)
	assert.Equal(t, "https://example.com/poster.jpg", r.Movie.PosterURL,
		"poster_url should be accessible for /jobs thumbnail rendering")
	assert.Equal(t, "/api/v1/temp/posters/test-job/ABCD-123.jpg?v=123", r.Movie.CroppedPosterURL)
}

// TestRebase_ListEndpointHandlesLegacyFormat verifies that the list endpoint
// correctly parses the legacy FileResult DB format with "data" field.
func TestRebase_ListEndpointHandlesLegacyFormat(t *testing.T) {
	legacyResults := `{
		"/videos/OLD-001.mp4": {
			"result_id": "legacy-uuid-001",
			"file_path": "/videos/OLD-001.mp4",
			"movie_id": "OLD-001",
			"status": "completed",
			"data_type": "movie",
			"data": {
				"id": "OLD-001",
				"content_id": "OLD-001",
				"title": "Legacy Movie",
				"poster_url": "https://example.com/legacy-poster.jpg",
				"cropped_poster_url": "/api/v1/temp/posters/old-job/OLD-001.jpg?v=999"
			},
			"started_at": "2024-01-15T10:00:00Z"
		}
	}`

	job := &models.Job{
		ID:      "legacy-job-id",
		Status:  models.JobStatusCompleted,
		Results: legacyResults,
	}

	results := parseAndConvertJobResults(job, nil)

	require.Len(t, results, 1)

	r, ok := results["/videos/OLD-001.mp4"]
	require.True(t, ok)
	assert.Equal(t, "legacy-uuid-001", r.ResultID)
	assert.Equal(t, "/videos/OLD-001.mp4", r.FilePath)
	assert.Equal(t, "OLD-001", r.MovieID)
	require.NotNil(t, r.Movie, "Movie should be populated from legacy 'data' field")
	assert.Equal(t, "OLD-001", r.Movie.ID)
	assert.Equal(t, "https://example.com/legacy-poster.jpg", r.Movie.PosterURL,
		"poster_url from legacy data should be preserved for /jobs thumbnail")
	assert.Equal(t, "/api/v1/temp/posters/old-job/OLD-001.jpg?v=999", r.Movie.CroppedPosterURL)
}

// TestRebase_ListEndpointHandlesOldMovieResultFormat verifies the old MovieResult
// format with nested file_match_info but no envelope wrapper.
func TestRebase_ListEndpointHandlesOldMovieResultFormat(t *testing.T) {
	oldResults := `{
		"/videos/MID-001.mp4": {
			"result_id": "old-uuid-001",
			"file_match_info": {"path": "/videos/MID-001.mp4", "movie_id": "MID-001"},
			"revision": 1,
			"status": "completed",
			"movie": {
				"id": "MID-001",
				"content_id": "MID-001",
				"title": "Mid-format Movie",
				"poster_url": "https://example.com/mid-poster.jpg"
			},
			"started_at": "2024-01-15T10:00:00Z"
		}
	}`

	job := &models.Job{
		ID:      "mid-job-id",
		Status:  models.JobStatusCompleted,
		Results: oldResults,
	}

	results := parseAndConvertJobResults(job, nil)

	require.Len(t, results, 1)

	r, ok := results["/videos/MID-001.mp4"]
	require.True(t, ok)
	assert.Equal(t, "old-uuid-001", r.ResultID)
	assert.Equal(t, "/videos/MID-001.mp4", r.FilePath)
	assert.Equal(t, "MID-001", r.MovieID)
	require.NotNil(t, r.Movie)
	assert.Equal(t, "MID-001", r.Movie.ID)
	assert.Equal(t, "https://example.com/mid-poster.jpg", r.Movie.PosterURL)
}

// TestRebase_ListEndpointClearsStaleCroppedPoster verifies the list endpoint
// path drops cropped_poster_url when the temp poster file no longer exists on
// disk (e.g. after upgrading from v0.3.15-alpha whose temp dir was not
// preserved), while preserving the remote poster_url fallback. This keeps the
// list view consistent with the detail view (reconstructBatchJob).
func TestRebase_ListEndpointClearsStaleCroppedPoster(t *testing.T) {
	movieResultJSON := `{
		"result_id": "stale-uuid",
		"file_match_info": {"path": "/videos/ABCD-123.mp4", "movie_id": "ABCD-123"},
		"status": "completed",
		"movie": {
			"id": "ABCD-123",
			"content_id": "ABCD-123",
			"title": "Stale Poster",
			"poster_url": "https://example.com/poster.jpg",
			"cropped_poster_url": "/api/v1/temp/posters/stale-job/ABCD-123.jpg?v=123"
		},
		"started_at": "2024-01-15T10:00:00Z"
	}`
	envelopeJSON := `{"domain": {"/videos/ABCD-123.mp4": ` + movieResultJSON + `}}`

	job := &models.Job{
		ID:      "stale-job",
		TempDir: "/tmp/does-not-exist-javinizer-stale",
		Status:  models.JobStatusCompleted,
		Results: envelopeJSON,
	}

	// MemMapFs has no file at the poster path → cropped URL must be cleared.
	results := parseAndConvertJobResults(job, afero.NewMemMapFs())

	require.Len(t, results, 1)
	r := results["/videos/ABCD-123.mp4"]
	require.NotNil(t, r.Movie)
	assert.Empty(t, r.Movie.CroppedPosterURL,
		"stale cropped_poster_url must be cleared so the frontend falls back to poster_url")
	assert.Equal(t, "https://example.com/poster.jpg", r.Movie.PosterURL,
		"remote poster_url fallback must be preserved")
}
