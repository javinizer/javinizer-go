package contracts

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/stretchr/testify/require"
)

func TestOfflineEvidenceJSONDefaultOffAndOwnedSidecar(t *testing.T) {
	for _, value := range []any{ScrapeRequest{}, BatchScrapeRequest{}, RescrapeRequest{}, BatchRescrapeRequest{}, BulkRescrapeRequest{}, ScrapeResponse{}, MovieResponse{}, BatchFileResult{}, BatchFileResultSlim{}, BatchRescrapeResponse{}, SourceResultsResponse{}} {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(data), "offline_evidence")
	}
	var request BatchScrapeRequest
	require.NoError(t, json.Unmarshal([]byte(`{"files":["a"],"show_offline_evidence":true,"artifact_directory":"forged","source_id":"007"}`), &request))
	require.True(t, request.ShowOfflineEvidence)
	require.True(t, request.Has("show_offline_evidence"))
	data, err := json.Marshal(request)
	require.NoError(t, err)
	require.NotContains(t, string(data), "artifact_directory")
	require.NotContains(t, string(data), "source_id")
	report := &scrapeevidence.Report{Outcome: scrapeevidence.Available, AdmittedAt: time.Now().UTC(), SnapshotDigest: "historic", Observations: []scrapeevidence.Observation{{Source: "r18dev", SourceID: "007", Outcome: scrapeevidence.Available, JapaneseName: "Supplemental"}}}
	response := ScrapeResponse{Movie: MovieViewFromModel(&models.Movie{ID: "TEST-001", Actresses: []models.Actress{{JapaneseName: "Original"}}}), OfflineEvidence: report}
	data, err = json.Marshal(response)
	require.NoError(t, err)
	var decoded ScrapeResponse
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, report, decoded.OfflineEvidence)
	movie, err := json.Marshal(response.Movie)
	require.NoError(t, err)
	require.NotContains(t, string(movie), "Supplemental")
	require.NotContains(t, string(movie), "offline_evidence")
}
