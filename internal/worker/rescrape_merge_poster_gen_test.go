package worker

import (
	"context"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingPosterGen captures the movie handed to each GeneratePoster call.
type recordingPosterGen struct {
	mu     sync.Mutex
	movies []models.Movie // clones, so later in-place mutation can't rewrite history
}

func (g *recordingPosterGen) GeneratePoster(_ context.Context, _ string, m *models.Movie) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.movies = append(g.movies, *m.Clone())
	return nil
}

func (g *recordingPosterGen) generated(t *testing.T) []models.Movie {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]models.Movie{}, g.movies...)
}

// TestRescrapePhase_Rescrape_MergeRetainedPoster_GeneratesFromMergedMovie pins
// the Codex P2 finding "generate previews from the reconciled rescrape movie":
// a merge-enabled rescrape whose merge RETAINS the existing effective poster
// source (stored PosterURL=P because the scraper returned only a new
// CoverURL=C) must still generate the shared {movieID}-full.jpg/preview from
// the FINAL merged movie. Generating from the raw scraped movie (the former
// behavior) populated the cache with C while the committed movie referenced
// P, so a manual crop was measured against C, persisted alongside P, and
// Organize applied those coordinates to the wrong image.
func TestRescrapePhase_Rescrape_MergeRetainedPoster_GeneratesFromMergedMovie(t *testing.T) {
	const (
		movieID   = "RET-001"
		oldPoster = "https://old.invalid/poster.jpg" // P: retained effective source
		oldCover  = "https://old.invalid/cover.jpg"
		newCover  = "https://new.invalid/cover.jpg" // C: the scraper's only new image
	)
	filePath := "/source/ret-001.mp4"
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: movieID, Title: "Existing", Poster: models.PosterState{
			PosterURL:        oldPoster,
			CoverURL:         oldCover,
			CropBounds:       &models.CropBounds{X: 10, Y: 20, Width: 300, Height: 450, ImageWidth: 1000, ImageHeight: 1500},
			ShouldCropPoster: false, // completed manual crop measured against P
		}},
	})
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Scraped", Poster: models.PosterState{
			CoverURL:         newCover, // only a fresh cover; no new poster URL
			ShouldCropPoster: true,
		}},
	}}
	gen := &recordingPosterGen{}
	inputs := rescrapePhaseInputs{
		JobID:     models.NewJobID(),
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	result, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{
		MovieID:      movieID,
		FilePath:     filePath,
		MergeEnabled: true,
		Merge:        workflow.MergeOptions{ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, models.RescrapeStatusSuccess, result.Status,
		"the reorder must not disturb the CAS/revision commit path")
	// Exactly one clean CAS commit: no conflict retry happened (UpdateFileResult
	// seeded revision 1, the rescrape committed 1 -> 2).
	assert.Equal(t, uint64(2), tracker.GetRevision(filePath),
		"the reorder must not disturb the CAS/revision commit path")

	// GeneratePoster ran exactly once, on the FINAL reconciled movie: the
	// retained poster P (never the raw scrape's crop-source C) with the
	// preserved manual crop still attached.
	generated := gen.generated(t)
	require.Len(t, generated, 1, "rescrape generates poster assets exactly once")
	genMovie := generated[0]
	assert.Equal(t, oldPoster, genMovie.Poster.PosterURL,
		"cache must be generated from the merged movie's retained poster P, not the raw scrape")
	assert.Equal(t, newCover, genMovie.Poster.CoverURL,
		"the scraper's new cover still refreshes")
	assert.Equal(t, effectivePosterSource(oldPoster, newCover),
		effectivePosterSource(genMovie.Poster.PosterURL, genMovie.Poster.CoverURL),
		"the cached image must match the effective source the committed movie references")
	require.NotNil(t, genMovie.Poster.CropBounds,
		"the retained-source reconciliation keeps the manual crop — generation sees it")
	assert.Equal(t, 10, genMovie.Poster.CropBounds.X)
	assert.False(t, genMovie.Poster.ShouldCropPoster,
		"the crop's deliberate reset intent is preserved on the generated movie")

	// The committed result carries the same reconciled movie the cache was
	// generated from — cache image == persisted effective source.
	stored, gErr := tracker.GetMovieResult(filePath)
	require.NoError(t, gErr)
	require.NotNil(t, stored.Movie)
	assert.Equal(t, oldPoster, stored.Movie.Poster.PosterURL)
	assert.Equal(t, newCover, stored.Movie.Poster.CoverURL)
	require.NotNil(t, stored.Movie.Poster.CropBounds)
	assert.Equal(t, 10, stored.Movie.Poster.CropBounds.X)
	assert.True(t, stored.PosterGenerated)
}

// TestRescrapePhase_Rescrape_NonMergeStillGeneratesFromScrapedMovie guards the
// reorder against regressions on the default (wholesale-replace) path: the
// generated movie is the scraped movie itself (plus the scraped baseline).
func TestRescrapePhase_Rescrape_NonMergeStillGeneratesFromScrapedMovie(t *testing.T) {
	const (
		movieID    = "RAW-001"
		scrapedURL = "https://new.invalid/poster.jpg"
	)
	filePath := "/source/raw-001.mp4"
	tracker := resultstore.New(1, []string{filePath})
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Scraped", Poster: models.PosterState{PosterURL: scrapedURL}},
	}}
	gen := &recordingPosterGen{}
	inputs := rescrapePhaseInputs{
		JobID:     models.NewJobID(),
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	result, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{
		MovieID:  movieID,
		FilePath: filePath,
	})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, result.Status)

	generated := gen.generated(t)
	require.Len(t, generated, 1)
	assert.Equal(t, scrapedURL, generated[0].Poster.PosterURL)
	assert.Equal(t, scrapedURL, generated[0].Poster.OriginalPosterURL,
		"the scraped baseline is established before generation on the non-merge path")
}
