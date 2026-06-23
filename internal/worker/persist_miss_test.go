package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- snapshotForPersist: deleted job returns (nil, false) ---

func TestSnapshotForPersist_DeletedJob_Persist(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.lifecycle.SetDeleted(true)

	dbJob, ok := snapshotForPersist(job)
	assert.False(t, ok)
	assert.Nil(t, dbJob)
}

// --- snapshotForPersist: successful snapshot with all fields ---

func TestSnapshotForPersist_AllFieldsPopulated(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4", "file2.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-001"},
	})
	job.results.UpdateFileResult("file2.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file2.mp4", MovieID: "DEF-002"},
		Status:        models.JobStatusFailed,
		Error:         "timeout",
	})
	job.cfg.destination = "/output"
	job.cfg.update = true
	job.cfg.tempDir = t.TempDir()
	job.cfg.operationMode = "organize"

	dbJob, ok := snapshotForPersist(job)
	require.True(t, ok)
	require.NotNil(t, dbJob)
	assert.Equal(t, job.ID.String(), dbJob.ID)
	assert.Equal(t, "/output", dbJob.Destination)
	assert.True(t, dbJob.Update)
	tempDir := job.cfg.tempDir
	assert.Equal(t, tempDir, dbJob.TempDir)
	assert.Equal(t, "organize", string(dbJob.OperationModeOverride))

	// Verify results JSON is a valid envelope
	var envelope JobResultsEnvelope
	err := json.Unmarshal([]byte(dbJob.Results), &envelope)
	require.NoError(t, err)
	assert.Contains(t, envelope.Domain, "file1.mp4")
	assert.Contains(t, envelope.Domain, "file2.mp4")
}

// --- reconstructBatchJob: legacy format with data_type key ---

func TestReconstructBatchJob_LegacyFormat(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	legacyResults := map[string]any{
		"file1.mp4": map[string]any{
			"file_path": "file1.mp4",
			"movie_id":  "LEG-001",
			"revision":  2,
			"status":    "completed",
			"data_type": "movie",
			"data": map[string]any{
				"id":    "LEG-001",
				"title": "Legacy Movie",
			},
			"started_at": "2026-01-01T00:00:00Z",
		},
	}
	resultsJSON, err := json.Marshal(legacyResults)
	require.NoError(t, err)

	dbJob := &models.Job{
		ID:      "legacy-job",
		Status:  models.JobStatusCompleted,
		Results: string(resultsJSON),
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Contains(t, result.results.Results, "file1.mp4")
	mr := result.results.Results["file1.mp4"]
	assert.Equal(t, "LEG-001", mr.FileMatchInfo.MovieID)
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "LEG-001", mr.Movie.ID)
	assert.Equal(t, uint64(2), mr.Revision)
}

// --- reconstructBatchJob: old format (flat map, no domain/data_type key) ---

func TestReconstructBatchJob_OldFormat(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	// Build an old-format JSON: flat map with embedded FieldSources
	type oldMovieResult struct {
		FileMatchInfo  models.FileMatchInfo `json:"file_match_info"`
		Movie          *models.Movie        `json:"movie,omitempty"`
		Revision       uint64               `json:"revision"`
		Status         models.JobStatus     `json:"status"`
		Error          string               `json:"error,omitempty"`
		FieldSources   map[string]string    `json:"field_sources,omitempty"`
		ActressSources map[string]string    `json:"actress_sources,omitempty"`
		StartedAt      time.Time            `json:"started_at"`
		EndedAt        *time.Time           `json:"ended_at,omitempty"`
	}

	oldResults := map[string]*oldMovieResult{
		"file1.mp4": {
			FileMatchInfo:  models.FileMatchInfo{Path: "file1.mp4", MovieID: "OLD-001"},
			Movie:          &models.Movie{ID: "OLD-001", Title: "Old Movie"},
			Revision:       3,
			Status:         models.JobStatusCompleted,
			FieldSources:   map[string]string{"title": "r18dev"},
			ActressSources: map[string]string{"actress_0": "dmm"},
		},
	}
	resultsJSON, err := json.Marshal(oldResults)
	require.NoError(t, err)

	dbJob := &models.Job{
		ID:      "old-format-job",
		Status:  models.JobStatusCompleted,
		Results: string(resultsJSON),
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Contains(t, result.results.Results, "file1.mp4")
	mr := result.results.Results["file1.mp4"]
	assert.Equal(t, "OLD-001", mr.FileMatchInfo.MovieID)
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "OLD-001", mr.Movie.ID)
	// Provenance should be extracted
	assert.NotNil(t, result.results.Provenance["file1.mp4"])
	assert.Equal(t, "r18dev", result.results.Provenance["file1.mp4"].FieldSources["title"])
}

// --- reconstructBatchJob: old format with nil entry ---

func TestReconstructBatchJob_OldFormat_NilEntry(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	type oldMovieResult struct {
		FileMatchInfo models.FileMatchInfo `json:"file_match_info"`
		Status        models.JobStatus     `json:"status"`
	}

	oldResults := map[string]*oldMovieResult{
		"file1.mp4": nil,
	}
	resultsJSON, err := json.Marshal(oldResults)
	require.NoError(t, err)

	dbJob := &models.Job{
		ID:      "old-nil-job",
		Status:  models.JobStatusPending,
		Results: string(resultsJSON),
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	// nil entries should be skipped
	_, exists := result.results.Results["file1.mp4"]
	assert.False(t, exists, "nil entry should be skipped")
}

// --- reconstructBatchJob: temp poster cleanup ---

func TestReconstructBatchJob_TempPosterCleanup(t *testing.T) {
	memFS := afero.NewMemMapFs()
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: memFS}

	// Create a job with a temp poster that doesn't exist on disk
	dbJob := &models.Job{
		ID:      "poster-cleanup-job",
		Status:  models.JobStatusCompleted,
		TempDir: t.TempDir(),
		Results: `{"domain":{"/path/file1.mp4":{"file_match_info":{"path":"/path/file1.mp4","movie_id":"PC-001"},"status":"completed","movie":{"id":"PC-001","poster_url":"","cropped_poster_url":"cropped.jpg"}}}}`,
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	// CroppedPosterURL should be cleared because the file doesn't exist on disk
	mr := result.results.Results["/path/file1.mp4"]
	require.NotNil(t, mr)
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "", mr.Movie.Poster.CroppedPosterURL, "missing temp poster should be cleared")
}

// --- reconstructBatchJob: invalid Files JSON ---

func TestReconstructBatchJob_InvalidFilesJSON(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	dbJob := &models.Job{
		ID:     "invalid-files-job",
		Status: models.JobStatusPending,
		Files:  "not valid json",
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Empty(t, result.results.Files, "invalid Files JSON should result in empty files slice")
}

// --- reconstructBatchJob: invalid Results JSON ---

func TestReconstructBatchJob_InvalidResultsJSON(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	dbJob := &models.Job{
		ID:      "invalid-results-job",
		Status:  models.JobStatusPending,
		Results: "not valid json at all",
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Empty(t, result.results.Results, "invalid Results JSON should result in empty results map")
}

// --- reconstructBatchJob: Done channel already closed ---

func TestReconstructBatchJob_DoneChannelAlreadyClosed(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	dbJob := &models.Job{
		ID:     "closed-done-job",
		Status: models.JobStatusCompleted,
	}

	// First reconstruct closes the Done channel
	result1 := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result1)

	// Second reconstruct should not panic on double-close
	result2 := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result2)
}

// --- reconstructBatchJob: with valid Excluded JSON ---

func TestReconstructBatchJob_ValidExcluded(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	excludedJSON, _ := json.Marshal(map[string]bool{"file1.mp4": true, "file2.mp4": false})
	dbJob := &models.Job{
		ID:       "excluded-job",
		Status:   models.JobStatusPending,
		Excluded: string(excludedJSON),
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.True(t, result.results.Excluded["file1.mp4"])
	assert.False(t, result.results.Excluded["file2.mp4"])
}

// --- persistToDatabase: nil jobRepo is a no-op ---

func TestPersistToDatabase_NilJobRepo(t *testing.T) {
	jq := NewJobStore(nil, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	// Should not panic
	jq.persistence.PersistJob(job)
}

// --- persistToDatabase: upsert error sets PersistError ---

func TestPersistToDatabase_UpsertError(t *testing.T) {
	mockRepo := &errorJobRepo{err: fmt.Errorf("disk full")}
	jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	jq.persistence.PersistJob(job)

	persistErr := job.persistError
	assert.Contains(t, persistErr, "upsert failed", "PersistError should be set on upsert failure")
}

// --- ParseJobResultsJSON: legacy format with provenance ---

func TestParseJobResultsJSON_LegacyWithProvenance(t *testing.T) {
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

	parsed, err := ParseJobResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "file1.mp4")
	mr := parsed.Results["file1.mp4"]
	assert.Equal(t, "PROV-001", mr.FileMatchInfo.MovieID)

	require.NotNil(t, parsed.Provenance["file1.mp4"])
	assert.Equal(t, "r18dev", parsed.Provenance["file1.mp4"].FieldSources["title"])
	assert.Equal(t, "javdb", parsed.Provenance["file1.mp4"].ActressSources["actress_0"])
}

// --- ParseJobResultsJSON: legacy format no provenance ---

func TestParseJobResultsJSON_LegacyNoProvenance(t *testing.T) {
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

	parsed, err := ParseJobResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "file1.mp4")
	assert.Nil(t, parsed.Provenance["file1.mp4"], "provenance should be nil when FieldSources and ActressSources are nil")
}

type errorJobRepo struct {
	err error
}

func (e *errorJobRepo) Upsert(_ context.Context, _ *models.Job) error { return e.err }
func (e *errorJobRepo) Create(_ context.Context, _ *models.Job) error { return nil }
func (e *errorJobRepo) FindByID(_ context.Context, _ string) (*models.Job, error) {
	return nil, nil
}
func (e *errorJobRepo) List(_ context.Context) ([]models.Job, error) { return nil, nil }
func (e *errorJobRepo) Delete(_ context.Context, _ string) error     { return nil }
func (e *errorJobRepo) DeleteOrganizedOlderThan(_ context.Context, _ time.Time) error {
	return nil
}
func (e *errorJobRepo) Update(_ context.Context, _ *models.Job) error { return nil }
