package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobStore_ReconstructBatchJob(t *testing.T) {
	t.Parallel()

	t.Run("basic_reconstruction", func(t *testing.T) {
		jq := &JobStore{
			jobs: make(map[models.JobID]*BatchJob),
		}

		now := time.Now()
		dbJob := &models.Job{
			ID:          "test-job-123",
			Status:      models.JobStatusCompleted,
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
		assert.Equal(t, models.JobID("test-job-123"), result.ID)
		assert.Equal(t, models.JobStatusCompleted, result.lifecycle.Status)
		assert.Equal(t, 10, result.results.TotalFiles)
		assert.Equal(t, 8, result.results.Completed)
		assert.Equal(t, 2, result.results.Failed)
		assert.Equal(t, float64(80), result.results.Progress)
		assert.Equal(t, "/dest/path", result.cfg.destination)
		assert.Equal(t, "/tmp/test", result.cfg.tempDir)
		assert.NotNil(t, result.results.Results)
		assert.NotNil(t, result.results.Excluded)
		assert.NotNil(t, result.results.FileMatchInfo)
		assert.NotNil(t, result.lifecycle.done)
	})

	t.Run("parse_files_json", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		files := []string{"/path/file1.mp4", "/path/file2.mp4"}
		filesJSON, err := json.Marshal(files)
		require.NoError(t, err)

		dbJob := &models.Job{
			ID:     "test-job",
			Status: models.JobStatusPending,
			Files:  string(filesJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, 2, len(result.results.Files))
		assert.Equal(t, "/path/file1.mp4", result.results.Files[0])
	})

	t.Run("parse_results_json", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		results := map[string]*MovieResult{
			"/path/file1.mp4": {
				Status:        models.JobStatusCompleted,
				FileMatchInfo: models.FileMatchInfo{MovieID: "ABC-123"},
				Movie:         &models.Movie{ID: "ABC-123"},
			},
		}
		resultsJSON, err := json.Marshal(results)
		require.NoError(t, err)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  models.JobStatusPending,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, 1, len(result.results.Results))
		assert.Equal(t, models.JobStatusCompleted, result.results.Results["/path/file1.mp4"].Status)
	})

	t.Run("parse_excluded_json", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		excluded := map[string]bool{
			"/path/file1.mp4": true,
		}
		excludedJSON, err := json.Marshal(excluded)
		require.NoError(t, err)

		dbJob := &models.Job{
			ID:       "test-job",
			Status:   models.JobStatusPending,
			Excluded: string(excludedJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.True(t, result.results.Excluded["/path/file1.mp4"])
	})

	t.Run("parse_file_match_info_json", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		matchInfo := map[string]models.FileMatchInfo{
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
			Status:        models.JobStatusPending,
			FileMatchInfo: string(matchInfoJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, "ABC-123", result.results.FileMatchInfo["/path/file1.mp4"].MovieID)
	})

	t.Run("close_done_channel_for_terminal_states", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		statuses := []models.JobStatus{models.JobStatusCompleted, models.JobStatusFailed, models.JobStatusCancelled, models.JobStatusOrganized, models.JobStatusReverted}
		for _, status := range statuses {
			dbJob := &models.Job{
				ID:     "test-job",
				Status: status,
			}

			result := jq.reconstructBatchJob(dbJob)
			assert.NotNil(t, result)

			select {
			case <-result.lifecycle.done:
				// Channel is closed, as expected
			default:
				t.Errorf("Done channel should be closed for status %s", status)
			}
		}
	})

	t.Run("done_channel_closed_for_all_reconstructed_states", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		statuses := []models.JobStatus{models.JobStatusPending, models.JobStatusRunning, models.JobStatusCompleted, models.JobStatusFailed, models.JobStatusCancelled, models.JobStatusOrganized, models.JobStatusReverted}
		for _, status := range statuses {
			dbJob := &models.Job{
				ID:     "test-job",
				Status: status,
			}

			result := jq.reconstructBatchJob(dbJob)
			assert.NotNil(t, result)

			select {
			case <-result.lifecycle.done:
				// Channel is closed, as expected for all reconstructed jobs
			default:
				t.Errorf("Done channel should be closed for reconstructed status %s", status)
			}
		}
	})

	t.Run("temp_poster_exists", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		tempDir := t.TempDir()
		posterDir := filepath.Join(tempDir, "posters", "test-job")
		require.NoError(t, os.MkdirAll(posterDir, 0755))

		posterPath := filepath.Join(posterDir, "ABC-123.jpg")
		require.NoError(t, os.WriteFile(posterPath, []byte("fake poster"), 0644))

		results := map[string]*MovieResult{
			"/path/file1.mp4": {
				Status:        "completed",
				FileMatchInfo: models.FileMatchInfo{MovieID: "ABC-123"},
				Movie:         &models.Movie{ID: "ABC-123", Poster: models.PosterState{CroppedPosterURL: "temp://ABC-123"}},
			},
		}
		resultsJSON, _ := json.Marshal(results)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  models.JobStatusCompleted,
			TempDir: tempDir,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		movie := result.results.Results["/path/file1.mp4"].Movie
		assert.Equal(t, "temp://ABC-123", movie.Poster.CroppedPosterURL, "Poster URL should not be cleared when file exists")
	})

	t.Run("temp_poster_missing", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		tempDir := t.TempDir()

		results := map[string]*MovieResult{
			"/path/file1.mp4": {
				Status:        "completed",
				FileMatchInfo: models.FileMatchInfo{MovieID: "ABC-123"},
				Movie:         &models.Movie{ID: "ABC-123", Poster: models.PosterState{CroppedPosterURL: "temp://ABC-123"}},
			},
		}
		resultsJSON, _ := json.Marshal(results)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  models.JobStatusCompleted,
			TempDir: tempDir,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		movie := result.results.Results["/path/file1.mp4"].Movie
		assert.Equal(t, "", movie.Poster.CroppedPosterURL, "Poster URL should be cleared when file is missing")
	})

	t.Run("invalid_files_json", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		dbJob := &models.Job{
			ID:     "test-job",
			Status: models.JobStatusPending,
			Files:  "invalid json",
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, 0, len(result.results.Files), "Files should be empty on parse error")
	})

	t.Run("invalid_results_json", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  models.JobStatusPending,
			Results: "invalid json",
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		assert.Equal(t, 0, len(result.results.Results), "Results should be empty on parse error")
	})

	t.Run("nil_result_data", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		tempDir := t.TempDir()

		results := map[string]*MovieResult{
			"/path/file1.mp4": {
				Status:        "completed",
				FileMatchInfo: models.FileMatchInfo{MovieID: "ABC-123"},
				Movie:         &models.Movie{ID: "ABC-123", Poster: models.PosterState{CroppedPosterURL: "temp://ABC-123"}},
			},
		}
		resultsJSON, _ := json.Marshal(results)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  models.JobStatusCompleted,
			TempDir: tempDir,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		assert.NotNil(t, result)
		// Should not panic or error
	})

	t.Run("result_data_not_movie", func(t *testing.T) {
		// Simulate a legacy DB record with a non-movie Data field (string instead of Movie).
		// ParseJobResultsJSON should handle this gracefully — the non-movie Data is ignored.
		legacyResults := map[string]any{
			"/path/file1.mp4": map[string]any{
				"status":    "completed",
				"movie_id":  "ABC-123",
				"data_type": "movie",
				"data":      "not a movie",
			},
		}
		resultsJSON, _ := json.Marshal(legacyResults)

		parsed, err := ParseJobResultsJSON(resultsJSON)
		require.NoError(t, err)
		require.Contains(t, parsed.Results, "/path/file1.mp4")
		// Should not panic — the non-movie Data is simply ignored
		assert.Nil(t, parsed.Results["/path/file1.mp4"].Movie)
	})

	t.Run("ParseJobResultsJSON_legacy_format_with_movie_data", func(t *testing.T) {
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

		parsed, err := ParseJobResultsJSON(resultsJSON)
		require.NoError(t, err)
		require.Contains(t, parsed.Results, "/path/file1.mp4")

		mr := parsed.Results["/path/file1.mp4"]
		assert.Equal(t, "ABC-123", mr.FileMatchInfo.MovieID)
		assert.Equal(t, models.JobStatusCompleted, mr.Status)
		require.NotNil(t, mr.Movie, "legacy-format JSON with data_type should produce non-nil Movie")
		assert.Equal(t, "ABC-123", mr.Movie.ID)
		assert.Equal(t, "Test Movie", mr.Movie.Title)
	})

	t.Run("ParseJobResultsJSON_legacy_format_with_provenance", func(t *testing.T) {
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

		parsed, err := ParseJobResultsJSON(resultsJSON)
		require.NoError(t, err)
		require.NotNil(t, parsed.Provenance["/path/file1.mp4"])
		assert.Equal(t, "r18dev", parsed.Provenance["/path/file1.mp4"].FieldSources["title"])
		assert.Equal(t, "dmm", parsed.Provenance["/path/file1.mp4"].ActressSources["actress_0"])
	})

	t.Run("new_format_preferred_over_legacy", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		newResults := map[string]*MovieResult{
			"/path/file1.mp4": {
				FileMatchInfo: models.FileMatchInfo{Path: "/path/file1.mp4", MovieID: "ABC-123"},
				Status:        models.JobStatusCompleted,
				Movie: &models.Movie{
					ID:    "ABC-123",
					Title: "New Format Movie",
				},
				StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		}
		resultsJSON, _ := json.Marshal(newResults)

		dbJob := &models.Job{
			ID:      "test-job",
			Status:  models.JobStatusCompleted,
			Results: string(resultsJSON),
		}

		result := jq.reconstructBatchJob(dbJob)
		require.NotNil(t, result)
		require.Contains(t, result.results.Results, "/path/file1.mp4")

		mr := result.results.Results["/path/file1.mp4"]
		require.NotNil(t, mr.Movie)
		assert.Equal(t, "New Format Movie", mr.Movie.Title)
	})

	t.Run("reverted_status_copies_reverted_at", func(t *testing.T) {
		jq := &JobStore{jobs: make(map[models.JobID]*BatchJob)}

		revertedAt := time.Date(2026, 4, 12, 10, 30, 0, 0, time.UTC)
		organizedAt := time.Date(2026, 4, 12, 9, 0, 0, 0, time.UTC)

		dbJob := &models.Job{
			ID:          "test-job-reverted",
			Status:      models.JobStatusReverted,
			OrganizedAt: &organizedAt,
			RevertedAt:  &revertedAt,
		}

		result := jq.reconstructBatchJob(dbJob)
		require.NotNil(t, result)
		assert.Equal(t, models.JobStatusReverted, result.lifecycle.Status)
		require.NotNil(t, result.lifecycle.RevertedAt)
		assert.Equal(t, revertedAt, *result.lifecycle.RevertedAt)
		require.NotNil(t, result.lifecycle.OrganizedAt)
		assert.Equal(t, organizedAt, *result.lifecycle.OrganizedAt)

		// Verify Done channel is closed for reverted status
		select {
		case <-result.lifecycle.done:
			// Channel is closed, as expected
		default:
			t.Error("Done channel should be closed for reverted status")
		}
	})
}

// --- Tests for reconstruction dep restoration (Findings 2 & 3) ---

// stubReconMatcher is a minimal MatcherInterface for reconstruction tests.
type stubReconMatcher struct{}

func (s *stubReconMatcher) Match(_ []models.FileMatchInfo) []matcher.MatchResult  { return nil }
func (s *stubReconMatcher) MatchFile(_ models.FileMatchInfo) *matcher.MatchResult { return nil }
func (s *stubReconMatcher) MatchString(_ string) string                           { return "TEST-001" }

// stubReconPosterGen is a minimal PosterGenerator for reconstruction tests.
type stubReconPosterGen struct{}

func (s *stubReconPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	return nil
}

// mockMovieRepoForReconstruct is a minimal MovieRepositoryInterface for reconstruction tests.
// Uses the generated mock so the interface stays in sync automatically.
func newMockMovieRepoForReconstruct(t *testing.T) *mocks.MockMovieRepositoryInterface {
	return mocks.NewMockMovieRepositoryInterface(t)
}

// TestReconstructBatchJob_RestoresMovieRepo verifies that wireJobDeps sets
// job.deps.MovieRepo on reconstructed jobs so that jobEditorImpl.UpdateMovie()
// persists edits to the database instead of silently skipping DB persistence.
func TestReconstructBatchJob_RestoresMovieRepo(t *testing.T) {
	t.Parallel()

	mockRepo := &mockJobRepoForPersist{}
	mockMovieRepo := newMockMovieRepoForReconstruct(t)
	jq := NewJobStore(mockRepo, nil, mockMovieRepo, t.TempDir(), nil, nil)

	dbJob := &models.Job{
		ID:         "test-recon-movie-repo",
		Status:     models.JobStatusCompleted,
		TotalFiles: 1,
		Completed:  1,
	}

	reconstructed := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, reconstructed)

	// The critical assertion: MovieRepo must be set on the reconstructed job
	// so that jobEditorImpl (created via getAdapters) can persist movie edits.
	reconstructed.mu.RLock()
	movieRepo := reconstructed.deps.MovieRepo
	reconstructed.mu.RUnlock()
	assert.NotNil(t, movieRepo, "reconstructed job should have MovieRepo set for DB persistence")
}

// TestReconstructBatchJob_RestoresBatchCfgAndPosterGen verifies that
// reconstructBatchJob restores BatchCfg, PosterGen, and Matcher from the
// JobStore's reconstruction deps, so post-restart apply/rescrape uses the
// correct configuration (e.g. NFOEnabled) and can generate posters.
func TestReconstructBatchJob_RestoresBatchCfgAndPosterGen(t *testing.T) {
	t.Parallel()

	m := &stubReconMatcher{}
	pg := &stubReconPosterGen{}
	batchCfg := BatchJobConfig{
		MaxWorkers:    4,
		WorkerTimeout: 45 * time.Second,
		NFOEnabled:    true,
	}

	mockRepo := &mockJobRepoForPersist{}
	jq := NewJobStore(mockRepo, nil, nil, t.TempDir(), nil, nil)
	jq.SetReconstructionDeps(m, pg, batchCfg)

	dbJob := &models.Job{
		ID:         "test-recon-infra-deps",
		Status:     models.JobStatusCompleted,
		TotalFiles: 1,
		Completed:  1,
	}

	reconstructed := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, reconstructed)

	reconstructed.mu.RLock()
	defer reconstructed.mu.RUnlock()

	assert.Equal(t, m, reconstructed.deps.Matcher, "reconstructed job should have Matcher restored")
	assert.Equal(t, pg, reconstructed.deps.PosterGen, "reconstructed job should have PosterGen restored")
	assert.Equal(t, 4, reconstructed.deps.BatchCfg.MaxWorkers, "reconstructed job should have BatchCfg.MaxWorkers restored")
	assert.True(t, reconstructed.deps.BatchCfg.NFOEnabled, "reconstructed job should have BatchCfg.NFOEnabled restored")
}

// TestSetReconstructionDeps_RehydratesExistingJobs verifies that
// SetReconstructionDeps sets infrastructure deps on already-loaded jobs
// (reconstructed at startup before the factory was built), not just on
// future jobs reconstructed afterwards.
func TestSetReconstructionDeps_RehydratesExistingJobs(t *testing.T) {
	t.Parallel()

	mockRepo := &mockJobRepoForPersist{}
	jq := NewJobStore(mockRepo, nil, nil, t.TempDir(), nil, nil)

	// Simulate a job loaded from DB at startup (before SetReconstructionDeps)
	dbJob := &models.Job{
		ID:         "test-rehydrate",
		Status:     models.JobStatusCompleted,
		TotalFiles: 1,
		Completed:  1,
	}
	reconstructed := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, reconstructed)

	// Add to store's map (simulates loadFromDatabase adding reconstructed jobs)
	jq.mu.Lock()
	jq.jobs[reconstructed.ID] = reconstructed
	jq.mu.Unlock()

	// Before SetReconstructionDeps: infra deps are nil/zero
	reconstructed.mu.RLock()
	assert.Nil(t, reconstructed.deps.Matcher)
	assert.Nil(t, reconstructed.deps.PosterGen)
	assert.False(t, reconstructed.deps.BatchCfg.NFOEnabled)
	reconstructed.mu.RUnlock()

	// Now set reconstruction deps — simulates factory being built after startup
	m := &stubReconMatcher{}
	pg := &stubReconPosterGen{}
	batchCfg := BatchJobConfig{NFOEnabled: true, MaxWorkers: 2}
	jq.SetReconstructionDeps(m, pg, batchCfg)

	// The existing job should now have the deps set
	reconstructed.mu.RLock()
	defer reconstructed.mu.RUnlock()
	assert.Equal(t, m, reconstructed.deps.Matcher, "existing job should have Matcher re-hydrated")
	assert.Equal(t, pg, reconstructed.deps.PosterGen, "existing job should have PosterGen re-hydrated")
	assert.True(t, reconstructed.deps.BatchCfg.NFOEnabled, "existing job should have BatchCfg re-hydrated")
}
