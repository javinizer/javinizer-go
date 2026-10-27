package jobpersist

import (
	"encoding/json"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestEvidenceHistoricalEnvelopeSurvivesRestartAndMultipart(t *testing.T) {
	admitted := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	report := &scrapeevidence.Report{Outcome: scrapeevidence.Available, SnapshotDigest: "historical-A", AdmittedAt: admitted, Recovered: true, Observations: []scrapeevidence.Observation{{Source: "r18dev:derived_actress", SourceID: "7", Outcome: scrapeevidence.Available, JapaneseName: "Original evidence"}, {Source: "r18dev:derived_actress", SourceID: "007", Outcome: scrapeevidence.Unmatched}}}
	original := Snapshot{ID: "historical", Files: []string{"part1", "part2"}, Results: map[string]*resultstore.MovieResult{}, Provenance: map[string]*resultstore.ProvenanceData{}}
	for _, path := range original.Files {
		original.Results[path] = &resultstore.MovieResult{ResultID: path, Revision: 8, Movie: &models.Movie{ID: "ID", Title: "Canonical"}}
		original.Provenance[path] = &resultstore.ProvenanceData{OfflineEvidence: report.Clone()}
	}
	job, err := Encode(original)
	require.NoError(t, err)
	report.SnapshotDigest = "latest-B"
	restored, errs := Decode(job)
	require.Empty(t, errs)
	for _, path := range original.Files {
		require.Equal(t, "historical-A", restored.Provenance[path].OfflineEvidence.SnapshotDigest)
		require.Equal(t, []string{"7", "007"}, []string{restored.Provenance[path].OfflineEvidence.Observations[0].SourceID, restored.Provenance[path].OfflineEvidence.Observations[1].SourceID})
		require.Equal(t, uint64(8), restored.Results[path].Revision)
		require.Equal(t, "Canonical", restored.Results[path].Movie.Title)
	}
	encoded, err := json.Marshal(restored.Results)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "offline_evidence")
	require.NotContains(t, string(encoded), "Original evidence")
	again, err := Encode(restored)
	require.NoError(t, err)
	restoredAgain, errs := Decode(again)
	require.Empty(t, errs)
	require.Equal(t, restored.Provenance, restoredAgain.Provenance)
}
func TestEvidenceDefaultOffEnvelopeShape(t *testing.T) {
	data, err := json.Marshal(&resultstore.ProvenanceData{FieldSources: map[string]string{"title": "r18dev"}})
	require.NoError(t, err)
	require.NotContains(t, string(data), "offline_evidence")
}
