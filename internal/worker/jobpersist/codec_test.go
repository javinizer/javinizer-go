package jobpersist

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecode_RoundTrip(t *testing.T) {
	completedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	organizedAt := time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC)

	original := Snapshot{
		ID:                    "round-trip-job",
		Status:                models.JobStatusCompleted,
		TotalFiles:            3,
		Completed:             2,
		Failed:                1,
		Progress:              66.6,
		Files:                 []string{"file1.mp4", "file2.mp4", "file3.mp4"},
		Results:               map[string]*resultstore.MovieResult{"file1.mp4": {Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "ABC-001"}}},
		Provenance:            map[string]*resultstore.ProvenanceData{"file1.mp4": {FieldSources: map[string]string{"title": "r18dev"}}},
		Excluded:              map[string]bool{"file3.mp4": true},
		FileMatchInfo:         map[string]models.FileMatchInfo{"file1.mp4": {Path: "file1.mp4", MovieID: "ABC-001"}},
		Destination:           "/output",
		TempDir:               "/tmp/job",
		OperationModeOverride: operationmode.OperationModeOrganize,
		StartedAt:             time.Date(2026, 3, 1, 11, 0, 0, 0, time.UTC),
		CompletedAt:           &completedAt,
		OrganizedAt:           &organizedAt,
		Update:                true,
	}

	dbJob, err := Encode(original)
	require.NoError(t, err)
	require.NotNil(t, dbJob)

	decoded, errs := Decode(dbJob)
	assert.Empty(t, errs, "round-trip should produce no decode errors")

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Status, decoded.Status)
	assert.Equal(t, original.TotalFiles, decoded.TotalFiles)
	assert.Equal(t, original.Completed, decoded.Completed)
	assert.Equal(t, original.Failed, decoded.Failed)
	assert.InDelta(t, original.Progress, decoded.Progress, 0.001)
	assert.Equal(t, original.Files, decoded.Files)
	assert.Equal(t, original.Destination, decoded.Destination)
	assert.Equal(t, original.TempDir, decoded.TempDir)
	assert.Equal(t, original.OperationModeOverride, decoded.OperationModeOverride)
	assert.Equal(t, original.StartedAt, decoded.StartedAt)
	require.NotNil(t, decoded.CompletedAt)
	assert.Equal(t, *original.CompletedAt, *decoded.CompletedAt)
	require.NotNil(t, decoded.OrganizedAt)
	assert.Equal(t, *original.OrganizedAt, *decoded.OrganizedAt)
	assert.Nil(t, decoded.RevertedAt)
	assert.Equal(t, original.Update, decoded.Update)
	assert.True(t, decoded.Excluded["file3.mp4"])
	assert.Equal(t, "ABC-001", decoded.Results["file1.mp4"].Movie.ID)
	assert.Equal(t, "r18dev", decoded.Provenance["file1.mp4"].FieldSources["title"])
	assert.Equal(t, "ABC-001", decoded.FileMatchInfo["file1.mp4"].MovieID)
}

func TestEncode_ResultsColumnIsEnvelope(t *testing.T) {
	snapshot := Snapshot{
		ID:      "envelope-check",
		Results: map[string]*resultstore.MovieResult{"file1.mp4": {Status: models.JobStatusCompleted}},
	}
	dbJob, err := Encode(snapshot)
	require.NoError(t, err)

	var envelope JobResultsEnvelope
	require.NoError(t, json.Unmarshal([]byte(dbJob.Results), &envelope))
	assert.Contains(t, envelope.Domain, "file1.mp4")
}

func TestDecode_AlwaysReturnsSnapshotWithScalarFields(t *testing.T) {
	dbJob := &models.Job{
		ID:            "scalar-job",
		Status:        models.JobStatusPending,
		TotalFiles:    5,
		Completed:     0,
		Failed:        0,
		Progress:      0,
		Files:         "not valid json",
		Results:       "also not valid json",
		Excluded:      "bad json too",
		FileMatchInfo: "more bad json",
		Destination:   "/dest",
		TempDir:       "/tmp",
	}

	snapshot, errs := Decode(dbJob)
	assert.Len(t, errs, 4, "one error per failed JSON column")
	assert.Equal(t, "scalar-job", snapshot.ID)
	assert.Equal(t, models.JobStatusPending, snapshot.Status)
	assert.Equal(t, 5, snapshot.TotalFiles)
	assert.Equal(t, "/dest", snapshot.Destination)
	assert.Equal(t, "/tmp", snapshot.TempDir)
	assert.NotNil(t, snapshot.Files)
	assert.NotNil(t, snapshot.Results)
	assert.NotNil(t, snapshot.Provenance)
	assert.NotNil(t, snapshot.Excluded)
	assert.NotNil(t, snapshot.FileMatchInfo)
}

func TestDecode_LegacyFormat(t *testing.T) {
	legacy := `{"file1.mp4": {"file_path": "file1.mp4", "movie_id": "LEG-001", "status": "completed", "data_type": "movie", "data": {"id": "LEG-001", "title": "Legacy"}, "started_at": "2026-01-01T00:00:00Z"}}`
	dbJob := &models.Job{ID: "legacy", Results: legacy}

	snapshot, errs := Decode(dbJob)
	assert.Empty(t, errs)
	require.Contains(t, snapshot.Results, "file1.mp4")
	mr := snapshot.Results["file1.mp4"]
	require.NotNil(t, mr.Movie)
	assert.Equal(t, "LEG-001", mr.Movie.ID)
}

func TestDecode_OldMovieResultFormat(t *testing.T) {
	old := `{"file1.mp4": {"file_match_info": {"path": "file1.mp4", "movie_id": "OLD-001"}, "status": "completed", "movie": {"id": "OLD-001"}}}`
	dbJob := &models.Job{ID: "old", Results: old}

	snapshot, errs := Decode(dbJob)
	assert.Empty(t, errs)
	require.Contains(t, snapshot.Results, "file1.mp4")
	assert.Equal(t, "OLD-001", snapshot.Results["file1.mp4"].Movie.ID)
}
