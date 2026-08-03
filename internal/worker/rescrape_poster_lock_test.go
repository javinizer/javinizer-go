package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingPosterGen is a PosterGenerator test double whose GeneratePoster can
// signal entry and block until released, so tests can open a deterministic
// window INSIDE the rescrape phase's poster-asset critical section.
type blockingPosterGen struct {
	mu       sync.Mutex
	calls    int
	entered  chan struct{} // closed on the first GeneratePoster call when set
	finish   chan struct{} // when set, GeneratePoster blocks until closed
	err      error
	afterGen func() // runs after the block, before returning (e.g. cancel ctx, bump revision)
}

func (g *blockingPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	if g.entered != nil {
		close(g.entered)
	}
	if g.finish != nil {
		<-g.finish
	}
	if g.afterGen != nil {
		g.afterGen()
	}
	return g.err
}

// errOnNthCallCtx wraps a context so the FIRST Err() call reports the
// wrapped context's error and every later Err() reports Canceled. Used to
// land a cancellation deterministically in the razor-thin window between
// ScrapeSingle's in-flight timeout check (Err() call #1) and the rescrape
// closure's pre-poster-generation ctx.Err() re-check (call #2).
type errOnNthCallCtx struct {
	context.Context
	calls int32
}

func (c *errOnNthCallCtx) Err() error {
	if atomic.AddInt32(&c.calls, 1) > 1 {
		return context.Canceled
	}
	return c.Context.Err()
}

// stubRescrapeFinder satisfies resultstore.FileFinder (GetRevision only in
// practice -- the FilePath-set path never calls FindFileForMovieID) so the
// rescrape lock block can re-capture the revision underneath the lock.
type stubRescrapeFinder struct{ revision uint64 }

func (stubRescrapeFinder) FindFileForMovieID(string) (*resultstore.FileLookupResult, error) {
	return nil, errors.New("not found")
}
func (s stubRescrapeFinder) GetRevision(string) uint64 { return s.revision }

func (g *blockingPosterGen) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// rescrapeLockFixture wires a real result store holding one completed result
// (movieID, revision 1, old poster URL) behind the rescrape phase inputs.
func rescrapeLockFixture(t *testing.T, jobID models.JobID, movieID, filePath string, wf *stubRescrapeWorkflow, gen poster.PosterGenerator) (rescrapePhaseInputs, resultstore.Store) {
	t.Helper()
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Movie:         &models.Movie{ID: movieID, Poster: models.PosterState{PosterURL: "https://old.example/poster.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}
	return inputs, tracker
}

// TestRescrapePhase_RescrapeRecapturesRevisionUnderPosterLock pins the
// Finding-B fix from the crop-first direction: a manual crop that wins the
// shared per-(jobID, movieID) lock BEFORE the rescrape persists its bounds
// (bumping the result revision), and the rescrape — blocked on the lock —
// re-captures the revision underneath it, so its commit CAS sees the
// post-crop revision and SUCCEEDS instead of losing the race. Pre-fix the
// revision was captured at file lookup (before the lock existed), so this
// exact interleave returned a Conflict while the shared -full.jpg had already
// been replaced by the scrape poster generation.
func TestRescrapePhase_RescrapeRecapturesRevisionUnderPosterLock(t *testing.T) {
	const (
		jobID   = models.JobID("job-rescrape-b1")
		movieID = "RSL-001"
		newURL  = "https://new.example/poster.jpg"
	)
	filePath := "/source/" + movieID + ".mp4"
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: newURL}},
	}}
	inputs, tracker := rescrapeLockFixture(t, jobID, movieID, filePath, wf, &stubOverridePosterGen{})

	// The crop side holds the shared lock first, exactly like
	// updateBatchMoviePosterCrop, blocking the rescrape before its
	// scrape/poster/commit critical section.
	release := AcquirePosterSourceLock(jobID.String(), movieID)

	type rescrapeOutcome struct {
		res *RescrapeResult
		err error
	}
	done := make(chan rescrapeOutcome, 1)
	go func() {
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
			RescrapeCmd{MovieID: movieID, FilePath: filePath})
		done <- rescrapeOutcome{res, err}
	}()

	// The rescrape must NOT slip its scrape/commit through while the crop
	// holds the lock.
	select {
	case out := <-done:
		release()
		t.Fatalf("rescrape completed (status=%v, err=%v) while the crop held the shared poster lock", out.res, out.err)
	case <-time.After(150 * time.Millisecond):
	}

	// Manual crop state update under the lock: attach bounds + bump the
	// revision from 1 to 2 (mirrors UpdatePosterCrop's state write).
	require.NoError(t, tracker.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		movie := current.Movie.Clone()
		movie.Poster.CropBounds = &models.CropBounds{X: 1, Y: 2, Width: 300, Height: 400, ImageWidth: 1000, ImageHeight: 1500}
		movie.Poster.CroppedPosterURL = "/tmp/cropped.jpg"
		current.Movie = movie
		return current, nil
	}))
	release()

	var out rescrapeOutcome
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape did not finish after the crop released the shared lock")
	}
	require.NoError(t, out.err)
	require.NotNil(t, out.res)
	assert.Equal(t, models.RescrapeStatusSuccess, out.res.Status,
		"with the revision re-captured under the lock, the commit CAS must observe the post-crop revision")

	final, getErr := tracker.GetMovieResult(filePath)
	require.NoError(t, getErr)
	require.NotNil(t, final.Movie)
	assert.Equal(t, newURL, final.Movie.Poster.PosterURL,
		"the rescrape (running after the crop) supersedes it wholesale")
	assert.Equal(t, uint64(3), final.Revision, "initial(1) → crop(2) → rescrape commit(3); no lost update")
	assertPosterSourceLockFree(t, jobID.String(), movieID)
}

// TestRescrapePhase_RescrapePosterGenerationBlocksConcurrentCrop pins the
// Finding-B fix from the rescrape-first direction: while the rescrape's
// poster generation is replacing the shared -full.jpg INSIDE the critical
// section, a concurrent manual crop (shared lock acquire, as
// updateBatchMoviePosterCrop does) must BLOCK — it can only measure the image
// after the rescrape committed the new source URL, so bounds are never
// measured against the new image while job state references the old source.
func TestRescrapePhase_RescrapePosterGenerationBlocksConcurrentCrop(t *testing.T) {
	const (
		jobID   = models.JobID("job-rescrape-b2")
		movieID = "RSL-002"
		newURL  = "https://new.example/poster.jpg"
	)
	filePath := "/source/" + movieID + ".mp4"
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: newURL}},
	}}
	gen := &blockingPosterGen{entered: make(chan struct{}), finish: make(chan struct{})}
	inputs, tracker := rescrapeLockFixture(t, jobID, movieID, filePath, wf, gen)

	done := make(chan error, 1)
	go func() {
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
			RescrapeCmd{MovieID: movieID, FilePath: filePath})
		if err == nil && res != nil && res.Status != models.RescrapeStatusSuccess {
			err = errors.New("unexpected rescrape status: " + string(res.Status))
		}
		done <- err
	}()

	// Wait until the rescrape is INSIDE GeneratePoster — i.e. holding the
	// shared lock across the poster-asset replacement.
	select {
	case <-gen.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape never reached poster generation")
	}

	// The crop path must block acquiring the shared lock for this poster
	// while the rescrape is mid-generation.
	acquired := make(chan struct{})
	go func() {
		release := AcquirePosterSourceLock(jobID.String(), movieID)
		release()
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatal("crop path acquired the shared poster lock while the rescrape was replacing the -full.jpg")
	case <-time.After(150 * time.Millisecond):
	}

	// Let the generation finish; the commit must then succeed (nobody bumped
	// the revision inside the critical section — the crop was blocked).
	close(gen.finish)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape did not finish after poster generation was released")
	}
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("crop path still blocked after the rescrape released the shared lock")
	}

	final, getErr := tracker.GetMovieResult(filePath)
	require.NoError(t, getErr)
	require.NotNil(t, final.Movie)
	assert.Equal(t, newURL, final.Movie.Poster.PosterURL)
	assert.Equal(t, uint64(2), final.Revision)

	// A crop that runs AFTER the commit measures the newly scraped image and
	// attaches its bounds to the same source — the consistent ordering the
	// lock guarantees.
	release := AcquirePosterSourceLock(jobID.String(), movieID)
	require.NoError(t, tracker.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		movie := current.Movie.Clone()
		movie.Poster.CropBounds = &models.CropBounds{X: 5, Y: 5, Width: 200, Height: 300, ImageWidth: 1000, ImageHeight: 1500}
		current.Movie = movie
		return current, nil
	}))
	release()
	final, getErr = tracker.GetMovieResult(filePath)
	require.NoError(t, getErr)
	assert.Equal(t, newURL, final.Movie.Poster.PosterURL, "crop must not touch the source URL")
	assert.NotNil(t, final.Movie.Poster.CropBounds)
	assert.Equal(t, uint64(3), final.Revision)
	assertPosterSourceLockFree(t, jobID.String(), movieID)
}

// TestRescrapePhase_RescrapePosterLockReleasedOnAllPaths is the release-table
// for the rescrape critical section: success, scrape error, failed status,
// poster-generation failure, post-generation cancellation, commit CAS
// conflict, and the nil-ResultMap guard must ALL leave the shared per-
// (jobID, movieID) poster lock free; a leak would deadlock every future
// poster edit for that movie.
func TestRescrapePhase_RescrapePosterLockReleasedOnAllPaths(t *testing.T) {
	const movieID = "RSL-003"
	newMovie := func() *models.Movie {
		return &models.Movie{ID: movieID, Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}}
	}

	newFixture := func(t *testing.T) (rescrapePhaseInputs, resultstore.Store, string) {
		t.Helper()
		filePath := "/source/" + movieID + ".mp4"
		tracker := resultstore.New(1, []string{filePath})
		tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
			Movie:         &models.Movie{ID: movieID},
			Status:        models.JobStatusCompleted,
		})
		inputs := rescrapePhaseInputs{
			JobID:     "job-rescrape-rel",
			ResultMap: tracker,
			Finder:    tracker,
			Lifecycle: &stubLifecycle{},
		}
		return inputs, tracker, filePath
	}

	t.Run("success releases the lock", func(t *testing.T) {
		inputs, _, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: newMovie()}}
		inputs.PosterGen = &stubOverridePosterGen{}
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})
		require.NoError(t, err)
		require.Equal(t, models.RescrapeStatusSuccess, res.Status)
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("scrape error releases the lock", func(t *testing.T) {
		inputs, _, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeErr: errors.New("network down")}
		_, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})
		require.Error(t, err)
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("failed scrape status releases the lock", func(t *testing.T) {
		inputs, _, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Status: scrape.StatusFailed, Message: "no results"}}
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})
		require.NoError(t, err)
		require.Equal(t, models.RescrapeStatusFailed, res.Status)
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("poster generation failure still releases the lock", func(t *testing.T) {
		inputs, _, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: newMovie()}}
		inputs.PosterGen = &stubOverridePosterGen{err: errors.New("download failed")}
		// Generation errors are recorded on the result, not propagated...
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})
		require.NoError(t, err)
		require.Equal(t, models.RescrapeStatusSuccess, res.Status)
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("cancellation after poster generation releases the lock", func(t *testing.T) {
		inputs, _, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: newMovie()}}
		ctx, cancel := context.WithCancel(context.Background())
		inputs.PosterGen = &blockingPosterGen{afterGen: cancel}
		_, err := NewRescrapePhase().Rescrape(ctx, inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})
		require.Error(t, err, "the post-generation ctx.Err() re-check must abort the commit")
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("commit CAS conflict is cleanly surfaced and releases the lock", func(t *testing.T) {
		inputs, tracker, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: newMovie()}}
		// Simulate a state write that skips the shared lock (e.g. a legacy
		// caller) landing INSIDE the rescrape's critical section: the
		// revision captured under the lock is stale by commit time, so the
		// CAS conflict surfaces as a clean Conflict status — the intended
		// backstop for lock-key mismatches and lock-agnostic writers.
		inputs.PosterGen = &blockingPosterGen{afterGen: func() {
			require.NoError(t, tracker.AtomicUpdateFileResult(filePath, func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
				return current, nil
			}))
		}}
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})
		require.NoError(t, err, "a revision conflict is a status, not a Go error")
		require.NotNil(t, res)
		assert.Equal(t, models.RescrapeStatusConflict, res.Status)
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("scrape returning a nil result releases the lock", func(t *testing.T) {
		inputs, _, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeResult: nil}
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, models.RescrapeStatusFailed, res.Status)
		assert.Equal(t, "scrape produced no result", res.Error)
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("merge-enabled with an existing result releases the lock", func(t *testing.T) {
		inputs, _, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: newMovie()}}
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{
			MovieID:      movieID,
			FilePath:     filePath,
			MergeEnabled: true,
			Merge: workflow.MergeOptions{
				ScalarStrategy: nfo.PreferNFO,
				ArrayStrategy:  true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, models.RescrapeStatusSuccess, res.Status)
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("non-conflict commit error releases the lock", func(t *testing.T) {
		// A ResultMap whose CommitResult fails with a NON-conflict error (a real
		// system error, distinct from the CAS conflict status) must propagate as
		// a Go error from Rescrape.
		rm := newStubResultMap()
		rm.results["f1.mp4"] = &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "f1.mp4", MovieID: movieID},
			Movie:         &models.Movie{ID: movieID},
			Status:        models.JobStatusCompleted,
		}
		rm.commitErr = errors.New("disk full")
		inputs := rescrapePhaseInputs{
			JobID:     "job-rescrape-rel",
			WF:        &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: newMovie()}},
			ResultMap: rm,
			Finder:    stubRescrapeFinder{},
			Lifecycle: &stubLifecycle{},
		}
		_, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieID, FilePath: "f1.mp4"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "disk full")
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("cancellation before poster generation releases the lock", func(t *testing.T) {
		inputs, _, filePath := newFixture(t)
		inputs.WF = &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: newMovie()}}
		// Canceled strictly between the scrape's in-flight ctx check and the
		// closure's pre-poster-generation re-check.
		_, err := NewRescrapePhase().Rescrape(&errOnNthCallCtx{Context: context.Background()}, inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assertPosterSourceLockFree(t, "job-rescrape-rel", movieID)
	})

	t.Run("nil ResultMap does not panic and releases the lock", func(t *testing.T) {
		// Defensive guard coverage: with no ResultMap, the lock key falls
		// back to the (possibly empty) pre-rescrape movie ID and the
		// re-capture reads are skipped; the scrape failure still propagates.
		tracker := resultstore.New(1, []string{"f1.mp4"})
		inputs := rescrapePhaseInputs{
			JobID:     "job-rescrape-nilmap",
			WF:        &stubRescrapeWorkflow{scrapeErr: errors.New("network down")},
			Finder:    tracker,
			ResultMap: nil,
			Lifecycle: &stubLifecycle{},
		}
		_, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieID, FilePath: "f1.mp4"})
		require.Error(t, err)
		assertPosterSourceLockFree(t, "job-rescrape-nilmap", "")
	})
}

// TestRescrapePhase_RescrapeReconvergesOnRekeyWhileWaitingForPosterLock pins
// the post-lock key convergence: a writer holding the pre-rescrape key (A)
// can re-key the result A→X while the rescrape waits on A's lock. Re-reading
// OldMovieID without re-resolving the LOCK KEY left the rescrape holding A's
// (now unrelated) lock through the poster snapshot/generation and the commit
// — unserialized against X's crop/edit writers. The loop must release A and
// converge onto X before proceeding.
//
// The mid-wait rekey target (X) deliberately DIFFERS from the scrape's
// resolved ID (B): when they coincide, the destination-lock block acquires B
// anyway and masks the stale origin key. With X != B the pre-fix rescrape
// held only {A, B}, leaving X's writers unserialized.
func TestRescrapePhase_RescrapeReconvergesOnRekeyWhileWaitingForPosterLock(t *testing.T) {
	const (
		jobID  = models.JobID("job-rescrape-converge")
		movieA = "RKY-A"
		movieX = "RKY-X"
		movieB = "RKY-B"
		newURL = "https://new.example/poster.jpg"
	)
	filePath := "/source/" + movieA + ".mp4"
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Rekeyed", Poster: models.PosterState{PosterURL: newURL}},
	}}
	gen := &blockingPosterGen{entered: make(chan struct{}), finish: make(chan struct{})}
	inputs, tracker := rescrapeLockFixture(t, jobID, movieA, filePath, wf, gen)

	// A rekeying writer holds A's lock first; the rescrape blocks behind it.
	releaseA := AcquirePosterSourceLock(jobID.String(), movieA)

	type rescrapeOutcome struct {
		res *RescrapeResult
		err error
	}
	done := make(chan rescrapeOutcome, 1)
	go func() {
		res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
			RescrapeCmd{MovieID: movieA, FilePath: filePath})
		done <- rescrapeOutcome{res, err}
	}()

	// The rescrape must NOT slip through while A's lock is held.
	select {
	case out := <-done:
		releaseA()
		t.Fatalf("rescrape completed (status=%v, err=%v) while the origin lock was held", out.res, out.err)
	case <-time.After(150 * time.Millisecond):
	}

	// The writer re-keys the result A→X mid-wait, then releases A.
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieX},
		Movie:         &models.Movie{ID: movieX, Poster: models.PosterState{PosterURL: "https://old.example/poster.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	releaseA()

	// The rescrape re-reads the result under A, sees the re-key, and must
	// converge onto X BEFORE poster generation (then pair with the scrape's
	// resolved destination B — X sorts after B, so the origin is released
	// and the (B, X) pair is taken in lexical order inside the closure).
	select {
	case <-gen.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape did not reach poster generation after the origin lock was released")
	}

	// While it generates, X's poster-source lock (the CURRENT key of the
	// file's result family) must be held by the rescrape: a crop on the
	// re-keyed movie blocking here is the fix's observable. Pre-fix the
	// rescrape kept holding only {A, B} and this acquire succeeded
	// immediately.
	acquired := make(chan func(), 1)
	go func() { acquired <- AcquirePosterSourceLock(jobID.String(), movieX) }()
	select {
	case r := <-acquired:
		r()
		t.Fatal("movie X's poster-source lock was free while the rescrape ran — it kept holding the stale pre-rekey key")
	case <-time.After(150 * time.Millisecond):
	}

	close(gen.finish)
	var out rescrapeOutcome
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("rescrape did not finish after poster generation was released")
	}
	// Drain the pending acquirer now that the rescrape released X.
	(<-acquired)()

	require.NoError(t, out.err)
	require.NotNil(t, out.res)
	assert.Equal(t, models.RescrapeStatusSuccess, out.res.Status)

	final, getErr := tracker.GetMovieResult(filePath)
	require.NoError(t, getErr)
	require.NotNil(t, final.Movie)
	assert.Equal(t, movieB, final.Movie.ID)
	assert.Equal(t, newURL, final.Movie.Poster.PosterURL)
	assertPosterSourceLockFree(t, jobID.String(), movieA)
	assertPosterSourceLockFree(t, jobID.String(), movieX)
	assertPosterSourceLockFree(t, jobID.String(), movieB)
}
