package worker

// Patch-coverage top-up for scrape persist-pool hardening legs:
// context.Canceled classification on the DB upsert and the write-back's
// identity-rekey guard when the stale-upsert gate could not see the re-key.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cancelPersistRepo fails UpsertWithTranslations with a wrapped
// context.Canceled — the shutdown-during-persist classification.
type cancelPersistRepo struct{ stripBoundsPersistRepo }

func (cancelPersistRepo) UpsertWithTranslations(context.Context, *models.Movie, []models.GenreTranslationData, []models.ActressTranslationData) (*models.Movie, error) {
	return nil, fmt.Errorf("db closed mid-shutdown: %w", context.Canceled)
}

// TestPersistScrapeOutcome_CancelledUpsertSkipsFailureMarking pins the
// context.Canceled leg: a cancellation-classified upsert failure must NOT
// flip the file to Failed, broadcast a failure, or record a Failed history —
// it returns unhandled so shutdown does not stain the result.
func TestPersistScrapeOutcome_CancelledUpsertSkipsFailureMarking(t *testing.T) {
	const filePath = "/input/CXL-001.mp4"
	tracker := resultstore.New(1, []string{filePath})
	scraped := &models.Movie{ID: "CXL-001", Title: "Scraped", Poster: models.PosterState{
		PosterURL: "https://a.example/poster.jpg",
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "CXL-001"},
		Status:        models.JobStatusCompleted,
		Movie:         scraped,
	})

	failureHookCalled := false
	inputs := scrapePhaseInputs{
		JobID:       models.NewJobID(),
		MovieRepo:   cancelPersistRepo{},
		Broadcaster: &stubBroadcaster{},
		Updater:     tracker,
	}
	outcome := scrapeFileOutcome{
		FilePath: filePath,
		MovieID:  "CXL-001",
		Success:  true,
		Result:   &scrape.ScrapeResult{Movie: scraped, Status: scrape.StatusCompleted},
	}

	handled := persistScrapeOutcome(context.Background(), outcome, inputs,
		func(_, _, _ string) { failureHookCalled = true })

	assert.False(t, handled, "a cancellation is not a per-file failure outcome")
	assert.False(t, failureHookCalled, "cancellation must not fire the per-file failure hook")
	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, stored.Status,
		"a cancelled persist must not flip the result to Failed")
	assert.False(t, stored.Persisted)
}

// blindGateUpdater keeps the store fully functional but blinds the
// stale-identity gate: GetMovieResult always errors, so skipStaleUpsert can
// never arm — the DB upsert with the scrape-time clone proceeds even when
// the live result was re-keyed.
type blindGateUpdater struct {
	resultstore.Store
}

func (blindGateUpdater) GetMovieResult(string) (*resultstore.MovieResult, error) {
	return nil, errors.New("lookup unavailable")
}

// TestPersistScrapeOutcome_WritebackKeepsLiveMovieOnBlindGateRekey pins the
// write-back's own identity guard (Codex P1, second line of defense): when
// the live result was re-keyed A→B but the stale-upsert gate could not see
// it (blinded lookup), the upserted clone must NOT replace the live movie —
// only the pipeline-owned Persisted flag moves.
func TestPersistScrapeOutcome_WritebackKeepsLiveMovieOnBlindGateRekey(t *testing.T) {
	const filePath = "/input/BLND-1.mp4"
	tracker := resultstore.New(1, []string{filePath})
	scrapedA := &models.Movie{ID: "AAA-001", Title: "Scraped A", Poster: models.PosterState{
		PosterURL: "https://a.example/poster.jpg",
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "AAA-001"},
		Status:        models.JobStatusCompleted,
		Movie:         scrapedA,
	})
	liveB := rekeyLiveResult(t, tracker, filePath, "BBB-002")

	inputs := scrapePhaseInputs{
		JobID:       models.NewJobID(),
		MovieRepo:   stripBoundsPersistRepo{savedTitle: "Scraped A (normalized)"},
		Broadcaster: &stubBroadcaster{},
		Updater:     blindGateUpdater{Store: tracker},
	}
	outcome := scrapeFileOutcome{
		FilePath: filePath,
		MovieID:  "AAA-001",
		Success:  true,
		Result:   &scrape.ScrapeResult{Movie: scrapedA, Status: scrape.StatusCompleted},
	}

	handled := persistScrapeOutcome(context.Background(), outcome, inputs, nil)
	assert.True(t, handled)

	stored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.True(t, stored.Persisted, "the pipeline-owned Persisted flag still moves")
	require.NotNil(t, stored.Movie)
	assert.Equal(t, "BBB-002", stored.Movie.ID,
		"the re-keyed live movie survives the write-back even when the gate was blind")
	assert.Equal(t, liveB.Title, stored.Movie.Title)
}
