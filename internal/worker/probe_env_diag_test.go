package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

func TestDiagCandidateEnvelopeExcludedRunning(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4", "/f/b.mp4"})
	job.results.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-a", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "D-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "D-1"},
	})
	job.results.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		ResultID: "res-b", Status: models.JobStatusRunning, Movie: &models.Movie{ID: "D-2"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "D-2"},
	})
	row, err := s_candidateEnvelope(job, nil, nil, map[string]bool{"/f/b.mp4": true, "/f/a.mp4": false})
	require.NoError(t, err)
	require.NotNil(t, row)
	snap, errs := jobpersist.Decode(row)
	require.Empty(t, errs)
	require.NotNil(t, snap.Results["/f/b.mp4"])
	assert.Equal(t, models.JobStatusCancelled, snap.Results["/f/b.mp4"].Status)
	assert.Equal(t, models.JobStatusCompleted, snap.Results["/f/a.mp4"].Status)
}

// Rebuilt-from-payload snapshots may contain nil result rows (legacy data);
// the envelope projection must skip them without crashing.
func TestCandidateEnvelopeSkipsNilResults(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.results = resultstore.NewFromSnapshot(1, []string{"/f/a.mp4", "/f/ghost.mp4"},
		map[string]*resultstore.MovieResult{
			"/f/a.mp4":     {ResultID: "res-a", Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "N-1"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "N-1"}},
			"/f/ghost.mp4": nil,
		},
		nil, nil, nil, 1, 0, 100)
	row, err := s_candidateEnvelope(job, nil, nil, map[string]bool{})
	require.NoError(t, err)
	require.NotNil(t, row)
}
