package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// A ResultUpdater implementation may have a file result without provenance.
// Persistence must create the envelope atomically and own its evidence copy.
func TestPersistScrapeOutcomeCreatesMissingProvenance(t *testing.T) {
	db := newHistoryTestDB(t)
	updater := newStubUpdater()
	const path = "ABC-001.mp4"
	movie := &models.Movie{ID: "ABC-001", Title: "Persisted title"}
	updater.UpdateFileResult(path, &resultstore.MovieResult{Movie: movie, Status: models.JobStatusCompleted})
	report := workerEvidence("persisted-generation")
	outcome := scrapeFileOutcome{FilePath: path, MovieID: movie.ID, Success: true,
		Result: &scrape.ScrapeResult{Movie: movie, Status: scrape.StatusCompleted, OfflineEvidence: report}}
	require.True(t, persistScrapeOutcome(t.Context(), outcome, scrapePhaseInputs{
		JobID: "missing-provenance", MovieRepo: db.Repositories().MovieRepo, Updater: updater,
	}, nil))
	require.True(t, updater.getResult(path).Persisted)
	require.Equal(t, movie.ID, updater.getResult(path).Movie.ID)
	prov := updater.provenance[path]
	require.NotNil(t, prov)
	require.Equal(t, report, prov.OfflineEvidence)
	require.NotSame(t, report, prov.OfflineEvidence)
	report.Observations[0].Aliases[0] = "caller mutation"
	report.SnapshotDigest = "caller mutation"
	require.Equal(t, "persisted-generation", prov.OfflineEvidence.SnapshotDigest)
	require.Equal(t, "Whole alias", prov.OfflineEvidence.Observations[0].Aliases[0])
}
