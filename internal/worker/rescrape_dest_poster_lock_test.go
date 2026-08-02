package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rekeyingRescrapeWorkflow returns per-query scrape results, so two
// concurrent rescrapes in the SAME job can rekey A→B and B→A at once (the
// deadlock-ordering scenario); the queried movie ID is RescrapeCmd.MovieID,
// which ScrapeSingle forwards as ScrapeCmd.MovieID.
type rekeyingRescrapeWorkflow struct {
	stubRescrapeWorkflow
	byQuery map[string]*scrape.ScrapeResult // keyed by ScrapeCmd.MovieID
}

func (s *rekeyingRescrapeWorkflow) Scrape(ctx context.Context, cmd scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	if r, ok := s.byQuery[cmd.MovieID]; ok {
		return r, nil, nil
	}
	return s.stubRescrapeWorkflow.Scrape(ctx, cmd)
}

// TestRescrapePhase_RekeyedRescrapeLocksDestinationPosterID pins the Codex
// finding "lock the rescrape's destination poster ID": a rescrape that
// re-keys (A→B) while ANOTHER result already uses B must hold B's
// poster-source lock before GeneratePoster rewrites B's shared
// -full.jpg/preview — otherwise a simultaneous crop on the existing B result
// (holding only B's lock) interleaves with the asset replacement. Here a
// crop on the existing B result holds B's lock; the rescrape must stay out
// of poster generation until it is released, and the crop's persisted state
// must survive the rescrape untouched.
func TestRescrapePhase_RekeyedRescrapeLocksDestinationPosterID(t *testing.T) {
	const (
		jobID   = models.JobID("job-rescrape-dest-lock")
		movieA  = "DEST-AAA"
		movieB  = "DEST-BBB"
		oldAURL = "https://old.example/a-poster.jpg"
		oldBURL = "https://old.example/b-poster.jpg"
		newURL  = "https://new.example/b-poster.jpg"
	)
	fileA := "/source/dest-a.mp4"
	fileB := "/source/dest-b.mp4"
	tracker := resultstore.New(2, []string{fileA, fileB})
	tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Movie:         &models.Movie{ID: movieA, Poster: models.PosterState{PosterURL: oldAURL}},
		Status:        models.JobStatusCompleted,
	})
	tracker.UpdateFileResult(fileB, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileB, MovieID: movieB},
		Movie:         &models.Movie{ID: movieB, Poster: models.PosterState{PosterURL: oldBURL}},
		Status:        models.JobStatusCompleted,
	})
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: newURL}},
	}}
	gen := &blockingPosterGen{entered: make(chan struct{})}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	// The crop path on the EXISTING B result holds B's poster lock — the
	// finding's interleave: without the destination lock the rescrape would
	// rewrite B's shared -full.jpg underneath it.
	releaseB := AcquirePosterSourceLock(jobID.String(), movieB)

	type outcome struct {
		res *RescrapeResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
			RescrapeCmd{MovieID: movieA, FilePath: fileA})
		done <- outcome{res, err}
	}()

	// While B's lock is held, the rekeying rescrape (holding only A's lock)
	// must NOT reach poster generation for B's shared assets.
	select {
	case <-gen.entered:
		releaseB()
		t.Fatal("rescrape reached poster generation for the destination while a crop held the destination's lock")
	case <-time.After(150 * time.Millisecond):
	}
	assert.Equal(t, 0, gen.callCount())

	// Under B's lock the crop records its bounds + preview against the OLD B
	// image — exactly what updateBatchMoviePosterCrop persists.
	require.NoError(t, tracker.AtomicUpdateFileResult(fileB, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		m := current.Movie.Clone()
		m.Poster.CropBounds = &models.CropBounds{X: 3, Y: 4, Width: 300, Height: 450, ImageWidth: 1000, ImageHeight: 1500}
		m.Poster.CroppedPosterURL = "/tmp/b-cropped.jpg"
		current.Movie = m
		return current, nil
	}))
	releaseB()

	select {
	case <-gen.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape never reached poster generation after the destination lock was released")
	}
	var out outcome
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape did not finish after the destination lock was released")
	}
	require.NoError(t, out.err)
	require.NotNil(t, out.res)
	assert.Equal(t, models.RescrapeStatusSuccess, out.res.Status)
	assert.Equal(t, 1, gen.callCount())

	// The rescraped result landed on A's file with the corrected ID...
	finalA, err := tracker.GetMovieResult(fileA)
	require.NoError(t, err)
	require.NotNil(t, finalA.Movie)
	assert.Equal(t, movieB, finalA.Movie.ID)
	assert.Equal(t, newURL, finalA.Movie.Poster.PosterURL)

	// ...and the pre-existing B result — poster source, crop bounds, preview —
	// was never clobbered or half-rewritten by the rescrape: serialized, the
	// end state stays asset/source/bounds-consistent.
	finalB, err := tracker.GetMovieResult(fileB)
	require.NoError(t, err)
	require.NotNil(t, finalB.Movie)
	assert.Equal(t, movieB, finalB.Movie.ID)
	assert.Equal(t, oldBURL, finalB.Movie.Poster.PosterURL)
	require.NotNil(t, finalB.Movie.Poster.CropBounds,
		"the crop that serialized against the rescrape must survive")
	assert.Equal(t, "/tmp/b-cropped.jpg", finalB.Movie.Poster.CroppedPosterURL)

	assertPosterSourceLockFree(t, jobID.String(), movieA)
	assertPosterSourceLockFree(t, jobID.String(), movieB)
}

// TestRescrapePhase_OppositeRekeyRescrapesDoNotDeadlock is the lock-ordering
// proof for the destination lock: two rescrapes in the SAME job rekey in
// OPPOSITE directions (A→B and B→A) concurrently. The A→B rescrape takes the
// destination lock directly on top of A (A sorts first); the B→A rescrape
// must release B and acquire in lexical order (A then B) — plus re-capture
// its origin-side revision. Any asymmetric ordering would deadlock here, so
// the test fails on timeout.
func TestRescrapePhase_OppositeRekeyRescrapesDoNotDeadlock(t *testing.T) {
	const (
		jobID  = models.JobID("job-rescrape-cross")
		movieA = "CROSS-AAA"
		movieB = "CROSS-BBB"
	)
	fileA := "/source/cross-a.mp4"
	fileB := "/source/cross-b.mp4"
	tracker := resultstore.New(2, []string{fileA, fileB})
	tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Movie:         &models.Movie{ID: movieA, Poster: models.PosterState{PosterURL: "https://old.example/a.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	tracker.UpdateFileResult(fileB, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileB, MovieID: movieB},
		Movie:         &models.Movie{ID: movieB, Poster: models.PosterState{PosterURL: "https://old.example/b.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	wf := &rekeyingRescrapeWorkflow{byQuery: map[string]*scrape.ScrapeResult{
		movieA: {Movie: &models.Movie{ID: movieB, Poster: models.PosterState{PosterURL: "https://new.example/b.jpg"}}},
		movieB: {Movie: &models.Movie{ID: movieA, Poster: models.PosterState{PosterURL: "https://new.example/a.jpg"}}},
	}}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: &blockingPosterGen{}, // no channels: counts calls, never blocks
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	type outcome struct {
		res *RescrapeResult
		err error
	}
	done := make(chan outcome, 2)
	go func() {
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
			RescrapeCmd{MovieID: movieA, FilePath: fileA})
		done <- outcome{res, err}
	}()
	go func() {
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
			RescrapeCmd{MovieID: movieB, FilePath: fileB})
		done <- outcome{res, err}
	}()

	deadline := time.After(5 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case out := <-done:
			require.NoError(t, out.err)
			require.NotNil(t, out.res)
			assert.Equal(t, models.RescrapeStatusSuccess, out.res.Status,
				"both cross-rekeying rescrapes must commit — the lexical two-lock ordering leaves no lost-update window")
		case <-deadline:
			t.Fatal("opposite-direction rekeying rescrapes deadlocked — the destination lock ordering is broken")
		}
	}
	assertPosterSourceLockFree(t, jobID.String(), movieA)
	assertPosterSourceLockFree(t, jobID.String(), movieB)
}
