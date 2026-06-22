package batch

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovieResultToResponse_NilInput(t *testing.T) {
	result := movieResultToResponse(nil, nil)
	assert.Nil(t, result)
}

func TestMovieResultToResponse_WithProvenance(t *testing.T) {
	now := time.Now()
	mr := &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/test/movie.mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-001"},
		StartedAt:     now,
		EndedAt:       &now,
	}
	prov := &worker.ProvenanceData{
		FieldSources:   map[string]string{"title": "r18dev"},
		ActressSources: map[string]string{"actress1": "dmm"},
	}

	resp := movieResultToResponse(mr, prov)
	assert.NotNil(t, resp)
	assert.Equal(t, "TEST-001", resp.Movie.ID)
	assert.Equal(t, models.JobStatusCompleted, resp.Status)
	assert.Equal(t, "r18dev", resp.FieldSources["title"])
	assert.Equal(t, "dmm", resp.ActressSources["actress1"])
}

func TestMovieResultToResponse_WithoutProvenance(t *testing.T) {
	mr := &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/test/movie.mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "TEST-002"},
	}

	resp := movieResultToResponse(mr, nil)
	assert.NotNil(t, resp)
	assert.Equal(t, "TEST-002", resp.Movie.ID)
	assert.Nil(t, resp.FieldSources)
	assert.Nil(t, resp.ActressSources)
}

func TestMovieResultToSlimResponse_NilInput(t *testing.T) {
	result := movieResultToSlimResponse(nil, nil)
	assert.Nil(t, result)
}

func TestMovieResultToSlimResponse_WithProvenance(t *testing.T) {
	now := time.Now()
	mr := &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/test/movie.mp4"},
		Status:        models.JobStatusCompleted,
		StartedAt:     now,
		EndedAt:       &now,
	}
	prov := &worker.ProvenanceData{
		FieldSources:   map[string]string{"title": "r18dev"},
		ActressSources: map[string]string{"actress1": "dmm"},
	}

	resp := movieResultToSlimResponse(mr, prov)
	assert.NotNil(t, resp)
	assert.Equal(t, models.JobStatusCompleted, resp.Status)
	assert.Equal(t, "r18dev", resp.FieldSources["title"])
	assert.Equal(t, "dmm", resp.ActressSources["actress1"])
}

// makeBatchJobStatus creates a BatchJobStatus via JSON round-trip since
// batchJobBase is unexported.
func makeBatchJobStatus(t *testing.T, jsonStr string) *worker.BatchJobStatus {
	t.Helper()
	var status worker.BatchJobStatus
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &status))
	return &status
}

func TestBuildBatchJobResponse_WithProvenance(t *testing.T) {
	job := makeBatchJobStatus(t, `{
		"id": "prov-job",
		"status": "completed",
		"total_files": 1,
		"completed": 1,
		"results": {
			"/test/movie.mp4": {
				"file_match_info": {"path": "/test/movie.mp4"},
				"status": "completed",
				"movie": {"id": "PROV-001"},
				"started_at": "2026-01-01T00:00:00Z"
			}
		},
		"provenance": {
			"/test/movie.mp4": {
				"field_sources": {"title": "r18dev"},
				"actress_sources": {"actress1": "dmm"}
			}
		}
	}`)

	resp := buildBatchJobResponse(job)
	assert.NotNil(t, resp)
	assert.Equal(t, "prov-job", resp.ID)
	assert.Equal(t, 1, len(resp.Results))
	result, ok := resp.Results["/test/movie.mp4"]
	assert.True(t, ok)
	assert.NotNil(t, result)
	assert.Equal(t, "PROV-001", result.Movie.ID)
	assert.Equal(t, "r18dev", result.FieldSources["title"])
}

func TestBuildBatchJobSlimResponse_WithProvenance(t *testing.T) {
	job := makeBatchJobStatus(t, `{
		"id": "slim-prov-job",
		"status": "completed",
		"total_files": 1,
		"completed": 1,
		"results": {
			"/test/movie.mp4": {
				"file_match_info": {"path": "/test/movie.mp4"},
				"status": "completed",
				"started_at": "2026-01-01T00:00:00Z"
			}
		},
		"provenance": {
			"/test/movie.mp4": {
				"field_sources": {"title": "r18dev"},
				"actress_sources": {"actress1": "dmm"}
			}
		}
	}`)

	resp := buildBatchJobSlimResponse(job)
	assert.NotNil(t, resp)
	assert.Equal(t, "slim-prov-job", resp.ID)
	assert.Equal(t, 1, len(resp.Results))
	result, ok := resp.Results["/test/movie.mp4"]
	assert.True(t, ok)
	assert.NotNil(t, result)
	assert.Equal(t, "r18dev", result.FieldSources["title"])
}

func TestBuildBatchJobResponse_NilProvenance(t *testing.T) {
	job := makeBatchJobStatus(t, `{
		"id": "nil-prov-job",
		"status": "completed",
		"total_files": 1,
		"completed": 1,
		"results": {
			"/test/movie.mp4": {
				"file_match_info": {"path": "/test/movie.mp4"},
				"status": "completed",
				"movie": {"id": "NIL-001"},
				"started_at": "2026-01-01T00:00:00Z"
			}
		}
	}`)

	resp := buildBatchJobResponse(job)
	assert.NotNil(t, resp)
	result := resp.Results["/test/movie.mp4"]
	assert.NotNil(t, result)
	assert.Nil(t, result.FieldSources)
	assert.Nil(t, result.ActressSources)
}

func TestMovieResultToResponse_WithError(t *testing.T) {
	mr := &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/test/fail.mp4"},
		Status:        models.JobStatusFailed,
		Error:         "scrape failed: timeout",
		StartedAt:     time.Now(),
	}

	resp := movieResultToResponse(mr, nil)
	assert.NotNil(t, resp)
	assert.Equal(t, models.JobStatusFailed, resp.Status)
	assert.Equal(t, "scrape failed: timeout", resp.Error)
	assert.Nil(t, resp.Movie)
}

func TestMovieResultToSlimResponse_WithoutProvenance(t *testing.T) {
	mr := &worker.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/test/movie.mp4"},
		Status:        models.JobStatusCompleted,
		StartedAt:     time.Now(),
	}

	resp := movieResultToSlimResponse(mr, nil)
	assert.NotNil(t, resp)
	assert.Equal(t, models.JobStatusCompleted, resp.Status)
	assert.Nil(t, resp.FieldSources)
	assert.Nil(t, resp.ActressSources)
}
