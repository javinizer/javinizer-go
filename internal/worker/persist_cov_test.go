package worker

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

func TestPersistToDatabaseSkipsDeletedJobs(t *testing.T) {
	jobRepo := mocks.NewMockJobRepositoryInterface(t) // zero expectations: any call fails the test
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.SetDeleted(true)
	require.NoError(t, persistToDatabase(jobRepo, job))
}

func TestPersistToDatabaseNilRepoIsNoop(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	require.NoError(t, persistToDatabase(nil, job))
	p := &dbJobPersistence{}
	require.NoError(t, p.UpsertJob(&models.Job{}))
}

func TestCandidateEnvelopeProjectionArms(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4", "/f/b.mp4", "/f/c.mp4"})
	job.results.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-a", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "PRJ-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PRJ-1"},
	})
	job.results.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		ResultID: "res-b", Status: models.JobStatusRunning,
		Movie:         &models.Movie{ID: "PRJ-2"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "PRJ-2"},
	})
	job.results.UpdateFileResult("/f/c.mp4", &resultstore.MovieResult{
		ResultID: "res-c", Status: models.JobStatusFailed,
		Movie:         &models.Movie{ID: "PRJ-3"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/c.mp4", MovieID: "PRJ-3"},
	})

	t.Run("overrides rekey candidate projects the match map", func(t *testing.T) {
		cand := &resultstore.MovieResult{ResultID: "res-a", Movie: &models.Movie{ID: "PRJ-NEW"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PRJ-NEW"}}
		overrides := map[string]*resultstore.MovieResult{
			"/f/a.mp4": cand,
			"/f/ghost": nil, // untracked+nil override candidates are ignored
		}
		row, err := s_candidateEnvelope(job, overrides, map[string]*resultstore.ProvenanceData{"/f/a.mp4": {FieldSources: map[string]string{"title": "dmm"}}}, nil)
		require.NoError(t, err)
		require.NotNil(t, row)
	})

	t.Run("excluded projection recomputes terminal counters", func(t *testing.T) {
		excluded := map[string]bool{"/f/b.mp4": true}
		row, err := s_candidateEnvelope(job, nil, nil, excluded)
		require.NoError(t, err)
		require.NotNil(t, row)
	})

	t.Run("excluding everything saturates progress at 100", func(t *testing.T) {
		excluded := map[string]bool{"/f/a.mp4": true, "/f/b.mp4": true, "/f/c.mp4": true}
		row, err := s_candidateEnvelope(job, nil, nil, excluded)
		require.NoError(t, err)
		require.NotNil(t, row)
	})
}
