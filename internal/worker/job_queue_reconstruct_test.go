package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobQueue_ReconstructBatchJob(t *testing.T) {
	t.Parallel()

	t.Run("basic_reconstruction", func(t *testing.T) {
		jq := &JobQueue{
			jobs: make(map[string]*BatchJob),
		}

		now := time.Now()
		dbJob := &models.Job{
			ID:          "test-job-123",
			Status:      string(JobStatusCompleted),
			TotalFiles:  10,
			Completed:   8,
			Failed:      2,
			Progress:    80,
			Destination: "/dest/path",
			TempDir:     "/tmp/test",
			StartedAt:   now,
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, "test-job-123", result.ID)
		assert.Equal(t, JobStatusCompleted, result.Status)
		assert.Equal(t, 10, result.TotalFiles)
		assert.Equal(t, 8, result.Completed)
		assert.Equal(t, 2, result.Failed)
		assert.Equal(t, float64(80), result.Progress)
		assert.Equal(t, "/dest/path", result.Destination)
		assert.Equal(t, "/tmp/test", result.TempDir)
		assert.NotNil(t, result.Results)
		assert.NotNil(t, result.Excluded)
		assert.NotNil(t, result.FileMatchInfo)
		assert.NotNil(t, result.Done)
	})

	t.Run("parse_files_json", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		files := []string{"/path/file1.mp4", "/path/file2.mp4"}
		filesJSON, err := json.Marshal(files)
		require.NoError(t, err)

		dbJob := &models.Job{
			ID:     "test-job",
			Status: string(JobStatusPending),
			Files:  string(filesJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, 2, len(result.Files))
		assert.Equal(t, "/path/file1.mp4", result.Files[0])
	})

	t.Run("parse_results_json", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		results := map[string]*FileResult{
			"/path/file1.mp4": {
				Status:  JobStatusCompleted,
				MovieID: "ABC-123",
				Data:    &models.Movie{ID: "ABC-123"},
			},
		}
		resultsJSON, err := json.Marshal(results)
		require.NoError(t, err)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  string(JobStatusPending),
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, 1, len(result.Results))
		assert.Equal(t, JobStatusCompleted, result.Results["/path/file1.mp4"].Status)
	})

	t.Run("parse_excluded_json", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		excluded := map[string]bool{
			"/path/file1.mp4": true,
		}
		excludedJSON, err := json.Marshal(excluded)
		require.NoError(t, err)

		dbJob := &models.Job{
			ID:       "test-job",
			Status:   string(JobStatusPending),
			Excluded: string(excludedJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.True(t, result.Excluded["/path/file1.mp4"])
	})

	t.Run("parse_file_match_info_json", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		matchInfo := map[string]FileMatchInfo{
			"/path/file1.mp4": {
				MovieID:     "ABC-123",
				IsMultiPart: true,
				PartNumber:  1,
			},
		}
		matchInfoJSON, err := json.Marshal(matchInfo)
		require.NoError(t, err)

		dbJob := &models.Job{
			ID:            "test-job",
			Status:        string(JobStatusPending),
			FileMatchInfo: string(matchInfoJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, "ABC-123", result.FileMatchInfo["/path/file1.mp4"].MovieID)
	})

	t.Run("close_done_channel_for_terminal_states", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		statuses := []JobStatus{JobStatusCompleted, JobStatusFailed, JobStatusCancelled, JobStatusOrganized, JobStatusReverted}
		for _, status := range statuses {
			dbJob := &models.Job{
				ID:     "test-job",
				Status: string(status),
			}

			result := jq.reconstructBatchJob(dbJob)
			assert.NotNil(t, result)

			select {
			case <-result.Done:
				// Channel is closed, as expected
			default:
				t.Errorf("Done channel should be closed for status %s", status)
			}
		}
	})

	t.Run("done_channel_open_for_non_terminal_states", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		statuses := []JobStatus{JobStatusPending, JobStatusRunning}
		for _, status := range statuses {
			dbJob := &models.Job{
				ID:     "test-job",
				Status: string(status),
			}

			result := jq.reconstructBatchJob(dbJob)
			assert.NotNil(t, result)

			select {
			case <-result.Done:
				t.Errorf("Done channel should be open for status %s", status)
			default:
				// Channel is open, as expected
			}
		}
	})

	t.Run("temp_poster_exists", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		tempDir := t.TempDir()
		posterDir := filepath.Join(tempDir, "posters", "test-job")
		require.NoError(t, os.MkdirAll(posterDir, 0755))

		posterPath := filepath.Join(posterDir, "ABC-123.jpg")
		require.NoError(t, os.WriteFile(posterPath, []byte("fake poster"), 0644))

		results := map[string]*FileResult{
			"/path/file1.mp4": {
				Status:  "completed",
				MovieID: "ABC-123",
				Data:    &models.Movie{ID: "ABC-123", CroppedPosterURL: "temp://ABC-123"},
			},
		}
		resultsJSON, _ := json.Marshal(results)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  string(JobStatusCompleted),
			TempDir: tempDir,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		movie := result.Results["/path/file1.mp4"].Data.(*models.Movie)
		assert.Equal(t, "temp://ABC-123", movie.CroppedPosterURL, "Poster URL should not be cleared when file exists")
	})

	t.Run("temp_poster_missing", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		tempDir := t.TempDir()

		results := map[string]*FileResult{
			"/path/file1.mp4": {
				Status:  "completed",
				MovieID: "ABC-123",
				Data:    &models.Movie{ID: "ABC-123", CroppedPosterURL: "temp://ABC-123"},
			},
		}
		resultsJSON, _ := json.Marshal(results)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  string(JobStatusCompleted),
			TempDir: tempDir,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		movie := result.Results["/path/file1.mp4"].Data.(*models.Movie)
		assert.Equal(t, "", movie.CroppedPosterURL, "Poster URL should be cleared when file is missing")
	})

	t.Run("invalid_files_json", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		dbJob := &models.Job{
			ID:     "test-job",
			Status: string(JobStatusPending),
			Files:  "invalid json",
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, 0, len(result.Files), "Files should be empty on parse error")
	})

	t.Run("invalid_results_json", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  string(JobStatusPending),
			Results: "invalid json",
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, 0, len(result.Results), "Results should be empty on parse error")
	})

	t.Run("nil_result_data", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		tempDir := t.TempDir()

		results := map[string]*FileResult{
			"/path/file1.mp4": {
				Status:  "completed",
				MovieID: "ABC-123",
				Data:    nil,
			},
		}
		resultsJSON, _ := json.Marshal(results)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  string(JobStatusCompleted),
			TempDir: tempDir,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		// Should not panic or error
	})

	t.Run("result_data_not_movie", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		tempDir := t.TempDir()

		results := map[string]*FileResult{
			"/path/file1.mp4": {
				Status:  "completed",
				MovieID: "ABC-123",
				Data:    "not a movie",
			},
		}
		resultsJSON, _ := json.Marshal(results)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  string(JobStatusCompleted),
			TempDir: tempDir,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		// Should not panic or error
	})

	t.Run("reverted_status_copies_reverted_at", func(t *testing.T) {
		jq := &JobQueue{jobs: make(map[string]*BatchJob)}

		revertedAt := time.Date(2026, 4, 12, 10, 30, 0, 0, time.UTC)
		organizedAt := time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC)

		dbJob := &models.Job{
			ID:          "test-job-reverted",
			Status:      string(JobStatusReverted),
			OrganizedAt: &organizedAt,
			RevertedAt:  &revertedAt,
		}

		result := jq.reconstructBatchJob(dbJob)
		require.NotNil(t, result)
		assert.Equal(t, JobStatusReverted, result.Status)
		require.NotNil(t, result.RevertedAt)
		assert.Equal(t, revertedAt, *result.RevertedAt)
		require.NotNil(t, result.OrganizedAt)
		assert.Equal(t, organizedAt, *result.OrganizedAt)

		// Verify Done channel is closed for reverted status
		select {
		case <-result.Done:
			// Channel is closed, as expected
		default:
			t.Error("Done channel should be closed for reverted status")
		}
	})
}
