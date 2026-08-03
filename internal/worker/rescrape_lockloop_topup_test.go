package worker

// Patch-coverage top-up for the rescrape destination-lock pairing loop and the
// multipart sibling mirror/rollback legs:
//  - mid-gap re-key ONTO the scraped destination (origin lock IS the dest lock)
//  - nil ResultMap inside the dest-first handoff (no live key to re-verify)
//  - post-handoff gap re-key (A→C) dropping BOTH locks and re-converging
//  - sibling mirror skipped when the pre-mirror snapshot read fails
//  - sibling mirror skipped when the sibling raced to another key mid-write
//  - persist-failure rollback surfacing a FAILED sibling state rollback

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRescrapePhase_RekeyOntoDestinationByMidGapWriterNoPairNeeded pins the
// pairing loop's identity break: the origin-key re-verify converges the lock
// onto the LIVE key, and when that live key IS the scraped destination (a
// mid-gap writer re-keyed the result onto it) the origin lock already covers
// the destination — no second acquisition and no collision rejection.
func TestRescrapePhase_RekeyOntoDestinationByMidGapWriterNoPairNeeded(t *testing.T) {
	const (
		jobID  = models.JobID("job-rescrape-onto-dest")
		movieA = "SRC-001"
		movieB = "DST-002"
	)
	fileA := "/source/onto-dest.mp4"
	tracker := resultstore.New(1, []string{fileA})
	tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Movie:         &models.Movie{ID: movieA, Poster: models.PosterState{PosterURL: "https://old.example/poster.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	// The re-key lands at the pairing loop's first origin re-verify
	// (GetCurrentMovieID call #3: pre-lock lookup, convergence loop, then the
	// pairing loop's top re-verify) — the writer moved the result ONTO the
	// scraped destination.
	wrapped := &gapRekeyingResultMap{Store: tracker, file: fileA, from: movieA, to: movieB, fireAt: 3}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: wrapped,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: movieA, FilePath: fileA})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, models.RescrapeStatusSuccess, res.Status,
		"already on the destination key: no pair to take, no collision — the rescrape just commits, res: %+v", res)

	final, err := tracker.GetMovieResult(fileA)
	require.NoError(t, err)
	require.NotNil(t, final.Movie)
	assert.Equal(t, movieB, final.Movie.ID)
	assert.Equal(t, "Corrected", final.Movie.Title)

	assertPosterSourceLockFree(t, jobID.String(), movieA)
	assertPosterSourceLockFree(t, jobID.String(), movieB)
}

// TestRescrapePhase_NilResultMapDestFirstHandoffBreaksOut pins the nil-ResultMap
// leg of the dest-first handoff: with no result map there is no live key to
// re-verify, so the loop keeps the handoff pair and breaks. A full Rescrape
// without a ResultMap cannot reach CompleteRescrape (it needs the map's CAS
// commit), so the commit panic is recovered HERE — the assertion target is
// that the pairing break itself neither deadlocks nor leaks locks.
func TestRescrapePhase_NilResultMapDestFirstHandoffBreaksOut(t *testing.T) {
	const (
		jobID  = models.JobID("job-rescrape-nil-map")
		movieA = "ZZZ-ORIG" // sorts AFTER the destination → dest-first handoff
		movieB = "AAA-DEST"
	)
	fileA := "/source/nil-map.mp4"
	tracker := resultstore.New(1, []string{fileA})
	tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Movie:         &models.Movie{ID: movieA, Poster: models.PosterState{PosterURL: "https://old.example/poster.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	inputs := rescrapePhaseInputs{
		JobID: jobID,
		WF:    wf,
		// No FilePath: Finder.FindFileForMovieID supplies the lookup (with a
		// valid OldMovieID), while ResultMap stays nil — the only way the
		// dest-first handoff can see a nil map.
		PosterGen: &snapshotStubPosterGen{},
		ResultMap: nil,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	var panicked any
	func() {
		defer func() { panicked = recover() }()
		_, _ = NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA})
	}()
	require.NotNil(t, panicked,
		"CompleteRescrape cannot commit without a ResultMap — the pairing loop's nil-map break is what this test pins")

	assertPosterSourceLockFree(t, jobID.String(), movieA)
	assertPosterSourceLockFree(t, jobID.String(), movieB)
}

// TestRescrapePhase_PostHandoffGapRekeyReconvergesAgain pins the second
// window of the dest-first handoff: the re-verify at the top of the pairing
// loop is stable, the destination-before-origin handoff runs, and only the
// post-handoff live re-read reveals the result was re-keyed mid-gap (A→C) —
// BOTH locks drop and the origin lock re-converges on C before pairing
// C with the destination.
func TestRescrapePhase_PostHandoffGapRekeyReconvergesAgain(t *testing.T) {
	const (
		jobID  = models.JobID("job-rescrape-post-handoff")
		movieA = "ZZZ-ORIG"
		movieB = "AAA-DEST"
		movieC = "MMM-LIVE"
	)
	fileA := "/source/post-handoff.mp4"
	tracker := resultstore.New(1, []string{fileA})
	tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Movie:         &models.Movie{ID: movieA, Poster: models.PosterState{PosterURL: "https://old.example/poster.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	// GetCurrentMovieID calls: #1 pre-lock lookup, #2 post-lock convergence,
	// #3 pairing-loop origin re-verify (stable), #4 the post-handoff re-read
	// — THAT is the window the gap re-key (A→C) lands in.
	wrapped := &gapRekeyingResultMap{Store: tracker, file: fileA, from: movieA, to: movieC, fireAt: 4}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: wrapped,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: movieA, FilePath: fileA})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, models.RescrapeStatusSuccess, res.Status, "res: %+v", res)

	// Committed through the re-converged (C, B) pair: the result ends at B.
	final, err := tracker.GetMovieResult(fileA)
	require.NoError(t, err)
	require.NotNil(t, final.Movie)
	assert.Equal(t, movieB, final.Movie.ID)
	assert.Equal(t, movieB, tracker.GetCurrentMovieID(fileA))

	assertPosterSourceLockFree(t, jobID.String(), movieA)
	assertPosterSourceLockFree(t, jobID.String(), movieB)
	assertPosterSourceLockFree(t, jobID.String(), movieC)
}

// siblingSnapshotMissStore blinds the sibling mirror's pre-write snapshot read
// for one path only, so the mirror skips it as "nothing coherent to sync or
// restore".
type siblingSnapshotMissStore struct {
	resultstore.Store
	missPath string
}

func (s *siblingSnapshotMissStore) GetMovieResult(filePath string) (*resultstore.MovieResult, error) {
	if filePath == s.missPath {
		return nil, errors.New("injected sibling snapshot miss")
	}
	return s.Store.GetMovieResult(filePath)
}

// TestRescrapePhase_SiblingMirrorSkippedOnSnapshotMiss pins the I7 fan-out
// skip leg: a sibling whose stored result cannot be read is left alone
// entirely — the rescrape still succeeds and the sibling keeps its OLD
// poster state (mirroring into an unsnapshotable sibling would make it
// irreversible).
func TestRescrapePhase_SiblingMirrorSkippedOnSnapshotMiss(t *testing.T) {
	const movieID = "SNIP-001"
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + movieID + "-cd2.mp4"
	jobID := models.NewJobID()

	tracker := resultstore.New(2, []string{fileCD1, fileCD2})
	for _, fp := range []string{fileCD1, fileCD2} {
		tracker.UpdateFileResult(fp, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: movieID},
			Movie: &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{
				PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
			}},
			Status: models.JobStatusCompleted,
		})
	}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: &stubOverridePosterGen{stampCroppedURL: "/api/v1/temp/posters/" + jobID.String() + "/" + movieID + ".jpg?v=42"},
		ResultMap: &siblingSnapshotMissStore{Store: tracker, missPath: fileCD2},
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: movieID, FilePath: fileCD1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, models.RescrapeStatusSuccess, res.Status, "res: %+v", res)

	sibling, err := tracker.GetMovieResult(fileCD2)
	require.NoError(t, err)
	require.NotNil(t, sibling.Movie)
	assert.Equal(t, "https://old.example/poster.jpg", sibling.Movie.Poster.PosterURL,
		"the unreadable sibling must not inherit poster state it cannot roll back")
	assertPosterSourceLockFree(t, jobID.String(), movieID)
}

// siblingRacedRekeyStore, on the sibling-mirror AtomicUpdateFileResult for the
// sibling, first moves the sibling's movie to ANOTHER key — the mirror
// closure then sees the identity mismatch and skips the sibling.
type siblingRacedRekeyStore struct {
	resultstore.Store
	sibPath string
	armed   bool
}

func (s *siblingRacedRekeyStore) AtomicUpdateFileResult(filePath string, fn func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	if s.armed && filePath == s.sibPath {
		s.armed = false
		if err := s.Store.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
			m := current.Movie.Clone()
			m.ID = "RACED-999"
			current.Movie = m
			current.FileMatchInfo.MovieID = "RACED-999"
			return current, nil
		}); err != nil {
			return err
		}
	}
	return s.Store.AtomicUpdateFileResult(filePath, fn)
}

// TestRescrapePhase_SiblingMirrorSkippedOnRacedRekey pins the mirror
// closure's identity guard: a sibling re-keyed between its snapshot and the
// atomic fan-out write is skipped (it no longer belongs to this family), and
// its OLD poster state survives unmirrorred — the rejection shape P2-5
// documented for siblings whose identity raced away mid-write.
func TestRescrapePhase_SiblingMirrorSkippedOnRacedRekey(t *testing.T) {
	const movieID = "RKEY-001"
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + movieID + "-cd2.mp4"
	jobID := models.NewJobID()

	tracker := resultstore.New(2, []string{fileCD1, fileCD2})
	for _, fp := range []string{fileCD1, fileCD2} {
		tracker.UpdateFileResult(fp, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: movieID},
			Movie: &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{
				PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
			}},
			Status: models.JobStatusCompleted,
		})
	}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	store := &siblingRacedRekeyStore{Store: tracker, sibPath: fileCD2, armed: true}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: &stubOverridePosterGen{stampCroppedURL: "/api/v1/temp/posters/" + jobID.String() + "/" + movieID + ".jpg?v=42"},
		ResultMap: store,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: movieID, FilePath: fileCD1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, models.RescrapeStatusSuccess, res.Status, "res: %+v", res)

	sibling, err := tracker.GetMovieResult(fileCD2)
	require.NoError(t, err)
	require.NotNil(t, sibling.Movie)
	assert.Equal(t, "RACED-999", sibling.Movie.ID, "the injected mid-write re-key stands")
	assert.Equal(t, "https://old.example/poster.jpg", sibling.Movie.Poster.PosterURL,
		"a sibling that raced to another key must be left unmirrorred")

	// The rescraped file converged normally.
	rescraped, err := tracker.GetMovieResult(fileCD1)
	require.NoError(t, err)
	assert.Equal(t, "Refreshed", rescraped.Movie.Title)
	assertPosterSourceLockFree(t, jobID.String(), movieID)
	assertPosterSourceLockFree(t, jobID.String(), "RACED-999")
}

// siblingRollbackFailStore mirrors the sibling once (arming the rollback
// entry), then fails the ROLLBACK-time AtomicUpdateFileResult for the
// sibling — the persist-failure compensation must surface it.
type siblingRollbackFailStore struct {
	resultstore.Store
	sibPath   string
	sibCalls  int
	rollbackE error
}

func (s *siblingRollbackFailStore) AtomicUpdateFileResult(filePath string, fn func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	if filePath == s.sibPath {
		s.sibCalls++
		if s.sibCalls > 1 {
			// Run the rollback write itself (the restore closure must execute)
			// and only then report the failure — the error rides the surfaced
			// PersistErr without losing the restore semantics.
			if err := s.Store.AtomicUpdateFileResult(filePath, fn); err != nil {
				return err
			}
			return s.rollbackE
		}
	}
	return s.Store.AtomicUpdateFileResult(filePath, fn)
}

// TestRescrapePhase_PersistFailureSiblingRollbackErrorSurfaced pins the I7
// rollback surfacing leg: the sibling mirror armed a rollback entry, the
// envelope persist failed, and the sibling's OWN state rollback then failed
// — the rollback failure must ride the surfaced PersistErr (never swallowed),
// alongside the main-file restore and the cache restore.
func TestRescrapePhase_PersistFailureSiblingRollbackErrorSurfaced(t *testing.T) {
	const movieID = "SBRB-001"
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + movieID + "-cd2.mp4"
	jobID := models.NewJobID()

	tracker := resultstore.New(2, []string{fileCD1, fileCD2})
	for _, fp := range []string{fileCD1, fileCD2} {
		tracker.UpdateFileResult(fp, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: movieID},
			Movie: &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{
				PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
			}},
			Status: models.JobStatusCompleted,
		})
	}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	store := &siblingRollbackFailStore{Store: tracker, sibPath: fileCD2, rollbackE: errors.New("sibling rollback jammed")}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: store,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
		PersistEnvelope: func() error {
			return errors.New("envelope repository unavailable")
		},
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: movieID, FilePath: fileCD1})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Error(t, outcome.PersistErr)
	assert.Contains(t, outcome.PersistErr.Error(), "envelope repository unavailable")
	assert.Contains(t, outcome.PersistErr.Error(), "sibling state rollback failed for "+fileCD2,
		"a failed sibling rollback must surface on the outcome, not be swallowed")

	// The main file still rolled back to its pre-rescrape state.
	restored, err := tracker.GetMovieResult(fileCD1)
	require.NoError(t, err)
	assert.Equal(t, "Old", restored.Movie.Title)

	// The destination cache leg still restored the pre-generation assets even
	// though the sibling state leg failed (every leg runs).
	assert.Equal(t, 1, gen.restores)

	assertPosterSourceLockFree(t, jobID.String(), movieID)
}
