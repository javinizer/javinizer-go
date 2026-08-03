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

// TestRescrapePhase_RekeyCollisionWaitsOnDestinationLockThenRejects pins BOTH
// of the destination-ID Codex findings: a rescrape that re-keys (A→B) while
// ANOTHER result already uses B must (1) hold B's poster-source lock before
// touching anything of B's — proven here by a crop on the existing B result
// holding B's lock, with the rescrape staying out of poster generation until
// it is released — and (2) REJECT the collision outright (Codex P0: shared
// {B}-full.jpg would otherwise be replaced while B's poster URL and crop
// bounds still reference B's old source, and later crops would fan bounds
// across both families). The rejection runs after the lock pair is held, so
// the crop's persisted state survives the rescrape untouched and nothing
// about the rescrape commits.
func TestRescrapePhase_RekeyCollisionWaitsOnDestinationLockThenRejects(t *testing.T) {
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

	// With B's lock free the rescrape completes — REJECTED at the collision
	// check (which runs under the re-acquired lock pair), never reaching
	// poster generation or the commit.
	var out outcome
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape did not finish after the destination lock was released")
	}
	select {
	case <-gen.entered:
		t.Fatal("a collision-rejected rescrape must never reach poster generation")
	default:
	}
	require.NoError(t, out.err)
	require.NotNil(t, out.res)
	assert.Equal(t, models.RescrapeStatusFailed, out.res.Status)
	assert.Contains(t, out.res.Error, "already uses that movie ID")
	assert.Equal(t, 0, gen.callCount(), "no cache write happened for the rejected rekey")

	// A's file never rekeyed — the rejection precedes any state mutation.
	finalA, err := tracker.GetMovieResult(fileA)
	require.NoError(t, err)
	require.NotNil(t, finalA.Movie)
	assert.Equal(t, movieA, finalA.Movie.ID)
	assert.Equal(t, oldAURL, finalA.Movie.Poster.PosterURL)

	// ...and the pre-existing B result — poster source, crop bounds, preview —
	// was never clobbered by the rejected rescrape.
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
// the test fails on timeout. Both directions now REJECT: each destination
// belongs to the other's family, which CheckRenameDestinationCollision
// refuses — deterministically, without either state moving.
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
			assert.Equal(t, models.RescrapeStatusFailed, out.res.Status,
				"each cross-rekey's destination IS the other family — both are collision-rejected")
			assert.Contains(t, out.res.Error, "already uses that movie ID")
		case <-deadline:
			t.Fatal("opposite-direction rekeying rescrapes deadlocked — the destination lock ordering is broken")
		}
	}

	// Neither side moved: both results still hold their original IDs.
	finalA, err := tracker.GetMovieResult(fileA)
	require.NoError(t, err)
	assert.Equal(t, movieA, finalA.Movie.ID)
	finalB, err := tracker.GetMovieResult(fileB)
	require.NoError(t, err)
	assert.Equal(t, movieB, finalB.Movie.ID)
	assertPosterSourceLockFree(t, jobID.String(), movieA)
	assertPosterSourceLockFree(t, jobID.String(), movieB)
}

// gapRekeyingResultMap wraps the tracker so that the fireAt-th
// GetCurrentMovieID call for file first COMMITS a from→to re-key and only
// then answers — deterministically reproducing an edit (a crop/override or
// another rescrape) landing in the destination-before-origin lock handoff
// gap: the rescrape released its origin lock, and by the time it re-reads
// the live key the result already belongs to `to`. Call counting: #1 is the
// pre-lock rescrape lookup, #2 is the initial post-lock convergence loop,
// #3 is the first read made at/after the dest-first handoff (both the old
// single re-capture and the fixed pairing loop's origin re-verify), so the
// re-key lands in exactly the vulnerable window for the old and new code
// alike.
type gapRekeyingResultMap struct {
	resultstore.Store
	file   string
	from   string
	to     string
	fireAt int
	calls  int
}

func (m *gapRekeyingResultMap) GetCurrentMovieID(filePath string) string {
	m.calls++
	if m.calls == m.fireAt && filePath == m.file && m.Store.GetCurrentMovieID(m.file) == m.from {
		_ = m.AtomicUpdateFileResult(m.file, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
			movie := current.Movie.Clone()
			movie.ID = m.to
			current.Movie = movie
			current.FileMatchInfo.MovieID = m.to
			return current, nil
		})
	}
	return m.Store.GetCurrentMovieID(filePath)
}

// TestRescrapePhase_DestFirstHandoffReconvergesOnGapRekey pins the residual
// A→C gap in the destination-BEFORE-origin lock handoff: when the re-read
// under the re-acquired pair shows the result was re-keyed mid-gap (A→C),
// refreshing only lookup.OldMovieID leaves posterLockID stale — the asset
// snapshot/generation and commit would run against C's state while holding
// locks for {destination, A}, unserialized against C's writers. The fix
// re-converges the origin lock onto the LIVE key before pairing. Proven by
// holding C's lock from the test: a converging rescrape must WAIT for it
// before reaching poster generation; the pre-fix code never touches C's
// lock and generates while C's state is unlocked.
func TestRescrapePhase_DestFirstHandoffReconvergesOnGapRekey(t *testing.T) {
	const (
		jobID   = models.JobID("job-rescrape-gap-rekey")
		movieA  = "ZZZ-ORIG" // origin — the destination must sort BEFORE it
		movieB  = "AAA-DEST" // scraped destination
		movieC  = "MMM-LIVE" // mid-gap re-key target (A→C)
		oldAURL = "https://old.example/a-poster.jpg"
		newURL  = "https://new.example/b-poster.jpg"
	)
	fileA := "/source/gap-a.mp4"
	tracker := resultstore.New(1, []string{fileA})
	tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Movie:         &models.Movie{ID: movieA, Poster: models.PosterState{PosterURL: oldAURL}},
		Status:        models.JobStatusCompleted,
	})
	wrapped := &gapRekeyingResultMap{Store: tracker, file: fileA, from: movieA, to: movieC, fireAt: 3}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: newURL}},
	}}
	gen := &blockingPosterGen{entered: make(chan struct{})}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: wrapped,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	// A writer on the LIVE key holds C's poster lock: the converging
	// rescrape must wait for it before touching C-keyed state.
	releaseC := AcquirePosterSourceLock(jobID.String(), movieC)

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

	select {
	case <-gen.entered:
		releaseC()
		t.Fatal("rescrape reached poster generation for a C-keyed result without holding C's lock — the gap re-key was not re-converged")
	case <-time.After(200 * time.Millisecond):
	}
	assert.Equal(t, 0, gen.callCount())

	releaseC()

	var out outcome
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape did not finish after the live key's lock was released")
	}
	require.NoError(t, out.err)
	require.NotNil(t, out.res)
	assert.Equal(t, models.RescrapeStatusSuccess, out.res.Status)

	// The rekeyed-then-rescraped result commits the scraped destination.
	final, err := tracker.GetMovieResult(fileA)
	require.NoError(t, err)
	require.NotNil(t, final.Movie)
	assert.Equal(t, movieB, final.Movie.ID)
	assert.Equal(t, movieB, tracker.GetCurrentMovieID(fileA))

	assertPosterSourceLockFree(t, jobID.String(), movieA)
	assertPosterSourceLockFree(t, jobID.String(), movieB)
	assertPosterSourceLockFree(t, jobID.String(), movieC)
}
