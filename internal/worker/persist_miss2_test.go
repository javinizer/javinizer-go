package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ParseJobResultsJSON: Movie data unmarshal failure ---
// When legacy Data field can't be unmarshaled to Movie, it should be nil

func TestPersistMiss2_ParseLegacy_UnmarshalMovieFails(t *testing.T) {
	legacyResults := map[string]any{
		"file1.mp4": map[string]any{
			"file_path":  "file1.mp4",
			"movie_id":   "FAIL-001",
			"status":     "completed",
			"data_type":  "movie",
			"data":       "not a valid movie object",
			"started_at": "2026-01-01T00:00:00Z",
		},
	}
	resultsJSON, _ := json.Marshal(legacyResults)

	parsed, err := ParseJobResultsJSON(resultsJSON)
	require.NoError(t, err)
	require.Contains(t, parsed.Results, "file1.mp4")
	mr := parsed.Results["file1.mp4"]
	assert.Equal(t, "FAIL-001", mr.FileMatchInfo.MovieID)
	// Movie should be nil because unmarshal failed
	assert.Nil(t, mr.Movie)
}

// --- ParseJobResultsJSON: empty input ---

func TestPersistMiss2_ParseLegacy_EmptyInput(t *testing.T) {
	parsed, err := ParseJobResultsJSON([]byte{})
	require.NoError(t, err)
	require.NotNil(t, parsed.Results)
	assert.Empty(t, parsed.Results)
}

// --- reconstructBatchJob: invalid envelope format (has "domain" key but invalid JSON) ---
// Lines 148-151: envelope unmarshal failure

func TestPersistMiss2_Reconstruct_InvalidEnvelope(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	// JSON that contains "domain" key but is malformed
	dbJob := &models.Job{
		ID:      "bad-envelope-job",
		Status:  models.JobStatusPending,
		Results: `{"domain": bad json}`,
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	// Should have empty results after parse failure
	assert.Empty(t, result.results.Results)
	// deserializeErrors should be incremented
	assert.Equal(t, int64(1), jq.deserializeErrors.Load())
}

// --- reconstructBatchJob: invalid legacy format (has "data_type" key but invalid JSON) ---
// Lines 160-163: legacy results unmarshal failure

func TestPersistMiss2_Reconstruct_InvalidLegacy(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	// JSON that contains "data_type" key but is malformed
	dbJob := &models.Job{
		ID:      "bad-legacy-job",
		Status:  models.JobStatusPending,
		Results: `{"file1.mp4": {"data_type": bad json}}`,
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Empty(t, result.results.Results)
	assert.Equal(t, int64(1), jq.deserializeErrors.Load())
}

// --- reconstructBatchJob: invalid old format (no "domain" or "data_type" but invalid JSON) ---
// Lines 163-169: old format results unmarshal failure

func TestPersistMiss2_Reconstruct_InvalidOldFormat(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	// JSON that doesn't contain "domain" or "data_type" and is malformed
	dbJob := &models.Job{
		ID:      "bad-old-job",
		Status:  models.JobStatusPending,
		Results: `{invalid}`,
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Empty(t, result.results.Results)
	assert.Equal(t, int64(1), jq.deserializeErrors.Load())
}

// --- reconstructBatchJob: with valid FileMatchInfo JSON ---

func TestPersistMiss2_Reconstruct_ValidFileMatchInfo(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	fmiJSON, _ := json.Marshal(map[string]models.FileMatchInfo{
		"file1.mp4": {Path: "file1.mp4", MovieID: "FMI-001"},
	})
	dbJob := &models.Job{
		ID:            "fmi-job",
		Status:        models.JobStatusPending,
		FileMatchInfo: string(fmiJSON),
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Equal(t, "FMI-001", result.results.FileMatchInfo["file1.mp4"].MovieID)
}

// --- reconstructBatchJob: invalid Excluded JSON ---

func TestPersistMiss2_Reconstruct_InvalidExcluded(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	dbJob := &models.Job{
		ID:       "bad-excluded-job",
		Status:   models.JobStatusPending,
		Excluded: "not valid json",
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Empty(t, result.results.Excluded)
	assert.Equal(t, int64(1), jq.deserializeErrors.Load())
}

// --- reconstructBatchJob: invalid FileMatchInfo JSON ---

func TestPersistMiss2_Reconstruct_InvalidFileMatchInfo(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	dbJob := &models.Job{
		ID:            "bad-fmi-job",
		Status:        models.JobStatusPending,
		FileMatchInfo: "not valid json",
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	assert.Empty(t, result.results.FileMatchInfo)
	assert.Equal(t, int64(1), jq.deserializeErrors.Load())
}

// --- persistToDatabase: successful upsert ---

func TestPersistMiss2_PersistToDatabase_Success(t *testing.T) {
	mockRepo := &successJobRepo{}
	jq := NewJobStore(mockRepo, nil, nil, "", nil, nil)
	job := jq.CreateJobBatch([]string{"file1.mp4"})

	jq.persistence.PersistJob(job)

	persistErr := job.persistError
	assert.Empty(t, persistErr, "no persist error expected on success")
}

// --- reconstructBatchJob: with provenance from envelope ---

func TestPersistMiss2_Reconstruct_EnvelopeWithProvenance(t *testing.T) {
	jq := &JobStore{jobs: make(map[models.JobID]*BatchJob), fs: afero.NewMemMapFs()}

	envelope := JobResultsEnvelope{
		Domain: map[string]*MovieResult{
			"file1.mp4": {
				FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ENV-001"},
				Status:        models.JobStatusCompleted,
			},
		},
		Provenance: map[string]*ProvenanceData{
			"file1.mp4": {
				FieldSources:   map[string]string{"title": "r18dev"},
				ActressSources: map[string]string{"actress_0": "dmm"},
			},
		},
	}
	resultsJSON, _ := json.Marshal(envelope)

	dbJob := &models.Job{
		ID:      "envelope-prov-job",
		Status:  models.JobStatusCompleted,
		Results: string(resultsJSON),
	}

	result := jq.reconstructBatchJob(dbJob)
	require.NotNil(t, result)
	mr := result.results.Results["file1.mp4"]
	require.NotNil(t, mr)
	assert.Equal(t, "ENV-001", mr.FileMatchInfo.MovieID)

	prov := result.results.Provenance["file1.mp4"]
	require.NotNil(t, prov)
	assert.Equal(t, "r18dev", prov.FieldSources["title"])
}

type successJobRepo struct{}

func (s *successJobRepo) Upsert(_ context.Context, _ *models.Job) error { return nil }
func (s *successJobRepo) Create(_ context.Context, _ *models.Job) error { return nil }
func (s *successJobRepo) FindByID(_ context.Context, _ string) (*models.Job, error) {
	return nil, nil
}
func (s *successJobRepo) List(_ context.Context) ([]models.Job, error) { return nil, nil }
func (s *successJobRepo) Delete(_ context.Context, _ string) error     { return nil }
func (s *successJobRepo) DeleteOrganizedOlderThan(_ context.Context, _ time.Time) error {
	return nil
}
func (s *successJobRepo) Update(_ context.Context, _ *models.Job) error { return nil }
