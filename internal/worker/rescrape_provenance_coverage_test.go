package worker

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

type failingProvenanceCommitter struct {
	resultstore.Store
	err error
}

func (f *failingProvenanceCommitter) CommitResultWithProvenance(string, *resultstore.MovieResult, uint64, *resultstore.ProvenanceData) error {
	return f.err
}

func TestCommitResultWithProvenanceReturnsCommitError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	wantErr := errors.New("provenance commit failed")
	committer := &failingProvenanceCommitter{Store: store, err: wantErr}
	result := &resultstore.MovieResult{ResultID: "res-a", Movie: &models.Movie{ID: "FAM-1"}}

	_, err := commitResultWithProvenanceAndRevisions(committer, "/f/a.mp4", result, 0, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "test"}})
	require.ErrorIs(t, err, wantErr)
}

type successfulProvenanceCommitter struct {
	resultstore.Store
}

func (successfulProvenanceCommitter) CommitResultWithProvenance(string, *resultstore.MovieResult, uint64, *resultstore.ProvenanceData) error {
	return nil
}

func TestCommitResultWithProvenanceReturnsFamilySnapshot(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "FAM-1", "")
	committer := successfulProvenanceCommitter{Store: store}
	result := &resultstore.MovieResult{ResultID: "res-a", Movie: &models.Movie{ID: "FAM-1"}}

	revisions, err := commitResultWithProvenanceAndRevisions(committer, "/f/a.mp4", result, 0, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "test"}})
	require.NoError(t, err)
	require.Equal(t, map[string]uint64{"res-a": 1}, revisions)
}

func TestReplaceRescrapeResultPropagatesTranslationWarningCode(t *testing.T) {
	outcome := &RescrapeResult{Status: models.RescrapeStatusSuccess}
	warning := "Translation (test): provider unavailable or unusable response"
	mr := &resultstore.MovieResult{
		Movie: &models.Movie{ID: "MV-1"},
		OrchestrationState: models.OrchestrationState{
			TranslationWarning:     &warning,
			TranslationWarningCode: "degraded",
		},
	}

	replaceRescrapeResult(outcome, "/f/mv1.mp4", mr, nil)

	require.Equal(t, warning, outcome.TranslationWarning)
	require.Equal(t, "degraded", outcome.TranslationWarningCode)
	require.Equal(t, "/f/mv1.mp4", outcome.FilePath)
	require.Equal(t, mr.Movie, outcome.Movie)
}
