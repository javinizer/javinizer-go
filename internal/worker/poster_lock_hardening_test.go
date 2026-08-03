package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// panickingAtomicUpdater wraps a real result store and panics inside
// AtomicUpdateFileResult while armed — the injected store-callback failure
// the L1 panic-safe poster-lock releases guard against.
type panickingAtomicUpdater struct {
	resultstore.Store
	armed int32
}

func (u *panickingAtomicUpdater) AtomicUpdateFileResult(filePath string, updateFn func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	if atomic.LoadInt32(&u.armed) > 0 {
		panic("injected store callback panic")
	}
	return u.Store.AtomicUpdateFileResult(filePath, updateFn)
}

// expectPanicReleasesLock runs fn (expected to panic) and then asserts the
// poster-source lock for (jobID, movieID) is free. Pre-L1 the explicit
// releasePosterLock() call after the panicking store update never ran, so
// the refcounted entry stayed locked forever and this assertion blocked
// until timeout.
func expectPanicReleasesLock(t *testing.T, jobID, movieID string, fn func()) {
	t.Helper()
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected fn to panic")
			}
		}()
		fn()
	}()
	assertPosterSourceLockFree(t, jobID, movieID)
}

// TestInterpretApplyResult_PanicInFailureWritebackReleasesLock pins L1 for
// the apply-failure write-back (apply_phase.go): a panic inside the
// AtomicUpdateFileResult callback must not leak the refcounted poster lock.
func TestInterpretApplyResult_PanicInFailureWritebackReleasesLock(t *testing.T) {
	const movieID = "PNK-001"
	filePath := "/input/" + movieID + ".mp4"
	tracker := resultstore.New(1, []string{filePath})
	movie := &models.Movie{ID: movieID, Title: "Scraped"}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusRunning,
		Movie:         movie,
	})
	updater := &panickingAtomicUpdater{Store: tracker, armed: 1}
	jobID := models.NewJobID()
	inputs := applyPhaseInputs{JobID: jobID, Broadcaster: &stubBroadcaster{}, Updater: updater}
	afc := &ApplyFileContext{FilePath: filePath, Match: models.FileMatchInfo{Path: filePath, MovieID: movieID}}

	expectPanicReleasesLock(t, jobID.String(), movieID, func() {
		interpretApplyResult(filePath, movie, time.Now(), time.Minute, inputs, ApplyPhaseConfig{},
			context.Background(), afc, workflow.ApplyCmd{}, nil, errors.New("simulated apply failure"))
	})
	assert.Equal(t, int32(1), atomic.LoadInt32(&updater.armed))
}

// TestInterpretApplyResult_PanicInSuccessWritebackReleasesLock pins L1 for
// the apply-success write-back (apply_phase.go's second lock site).
func TestInterpretApplyResult_PanicInSuccessWritebackReleasesLock(t *testing.T) {
	const movieID = "PNK-002"
	filePath := "/input/" + movieID + ".mp4"
	tracker := resultstore.New(1, []string{filePath})
	movie := &models.Movie{ID: movieID, Title: "Scraped"}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusRunning,
		Movie:         movie,
	})
	updater := &panickingAtomicUpdater{Store: tracker, armed: 1}
	jobID := models.NewJobID()
	inputs := applyPhaseInputs{JobID: jobID, Broadcaster: &stubBroadcaster{}, Updater: updater}
	afc := &ApplyFileContext{FilePath: filePath, Match: models.FileMatchInfo{Path: filePath, MovieID: movieID}}

	expectPanicReleasesLock(t, jobID.String(), movieID, func() {
		interpretApplyResult(filePath, movie, time.Now(), time.Minute, inputs, ApplyPhaseConfig{},
			context.Background(), afc, workflow.ApplyCmd{}, &workflow.ApplyResult{Movie: movie.Clone()}, nil)
	})
}

// TestPersistScrapeOutcome_PanicInWritebackReleasesLock pins L1 for the
// scrape persist pool's DB write-back (scrape_phase.go).
func TestPersistScrapeOutcome_PanicInWritebackReleasesLock(t *testing.T) {
	const movieID = "PNK-003"
	filePath := "/input/" + movieID + ".mp4"
	tracker := resultstore.New(1, []string{filePath})
	scraped := &models.Movie{ID: movieID, Title: "Scraped"}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         scraped,
	})
	updater := &panickingAtomicUpdater{Store: tracker, armed: 1}
	jobID := models.NewJobID()
	inputs := scrapePhaseInputs{
		JobID:       jobID,
		MovieRepo:   stripBoundsPersistRepo{savedTitle: "Persisted"},
		Broadcaster: &stubBroadcaster{},
		Updater:     updater,
	}
	outcome := scrapeFileOutcome{
		FilePath: filePath,
		MovieID:  movieID,
		Success:  true,
		Result:   &scrape.ScrapeResult{Movie: scraped, Status: scrape.StatusCompleted},
	}

	expectPanicReleasesLock(t, jobID.String(), movieID, func() {
		persistScrapeOutcome(context.Background(), outcome, inputs, nil)
	})
}

// TestWithFileRecovery_PanicInWritebackReleasesLock pins L1 for the panic
// recovery write-back (recovery.go): the recover handler's own
// AtomicUpdateFileResult panicking must still free the poster lock — this
// code path only runs DURING recovery, so a leak here is doubly fatal.
func TestWithFileRecovery_PanicInWritebackReleasesLock(t *testing.T) {
	const movieID = "PNK-004"
	filePath := "/input/" + movieID + ".mp4"
	tracker := resultstore.New(1, []string{filePath})
	movie := &models.Movie{ID: movieID, Title: "Scraped"}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusRunning,
		Movie:         movie,
	})
	updater := &panickingAtomicUpdater{Store: tracker, armed: 1}
	jobID := models.NewJobID()
	rc := recoveryContext{
		jobID:    jobID,
		filePath: filePath,
		fmi:      models.FileMatchInfo{Path: filePath, MovieID: movieID},
		movie:    movie,
		updater:  updater,
	}
	outcome := &applyFileOutcome{}

	expectPanicReleasesLock(t, jobID.String(), movieID, func() {
		defer withFileRecovery(rc, outcome)()
		panic("business panic")
	})
}

// TestAcquirePosterSourceLock_CaseFoldedSegments pins L4: the result-store
// family index folds case (resultstore.indexKey) and the temp poster cache
// shares a slot on case-insensitive filesystems, so "ABC-1" and "abc-1" must
// contend on ONE poster-source lock, not bypass each other on verbatim keys.
func TestAcquirePosterSourceLock_CaseFoldedSegments(t *testing.T) {
	jobID := models.NewJobID().String()
	upper := AcquirePosterSourceLock(jobID, "ABC-1")

	acquired := make(chan struct{})
	go func() {
		lower := AcquirePosterSourceLock(jobID, "abc-1")
		lower()
		close(acquired)
	}()
	select {
	case <-acquired:
		upper()
		t.Fatal("case-variant movie IDs bypassed each other's poster-source lock")
	case <-time.After(150 * time.Millisecond):
	}
	upper()
	select {
	case <-acquired:
	case <-time.After(2 * time.Second):
		t.Fatal("folded lock not released after the upper-case holder returned")
	}
	assert.Equal(t, PosterSourceLockMovieID("ABC-1"), PosterSourceLockMovieID("abc-1"))
}

// blockingCommitUpdater blocks the Nth UpdateFileResult call so a test can
// observe whether the scrape-phase commit (UpdateFileResult) runs INSIDE the
// scraped movie's poster-source lock (D10).
type blockingCommitUpdater struct {
	resultstore.Store
	blockOn int32 // call index (1-based) to block on
	calls   int32
	entered chan struct{}
	finish  chan struct{}
}

func (u *blockingCommitUpdater) UpdateFileResult(filePath string, result *resultstore.MovieResult) {
	n := atomic.AddInt32(&u.calls, 1)
	u.Store.UpdateFileResult(filePath, result)
	if n == atomic.LoadInt32(&u.blockOn) {
		close(u.entered)
		<-u.finish
	}
}

// newScrapeLockFixture wires scrapeFile with a stub workflow resolving
// movieID and a per-test Updater wrapper around a real tracker.
func newScrapeLockFixture(t *testing.T, movieID, filePath string, gen *blockingPosterGen, updater resultstore.ResultUpdater) scrapePhaseInputs {
	t.Helper()
	return scrapePhaseInputs{
		JobID:       models.NewJobID(),
		WF:          &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: movieID, Title: "Scraped", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}}}},
		PosterGen:   gen,
		Updater:     updater,
		Broadcaster: &stubBroadcaster{},
	}
}

// TestScrapePhase_GenerationHoldsPosterLock pins L2: the scrape phase's
// GeneratePoster replaces the job's cached {movieID}-full.jpg, so the whole
// generation must run under the shared per-(jobID, movieID) poster-source
// lock. While generation is mid-flight, a crop/edit/rekey writer on the same
// key must BLOCK.
func TestScrapePhase_GenerationHoldsPosterLock(t *testing.T) {
	const movieID = "GEN-001"
	filePath := "/input/" + movieID + ".mp4"
	tracker := resultstore.New(1, []string{filePath})
	gen := &blockingPosterGen{entered: make(chan struct{}), finish: make(chan struct{})}
	inputs := newScrapeLockFixture(t, movieID, filePath, gen, tracker)

	done := make(chan scrapeFileOutcome, 1)
	go func() {
		done <- scrapeFile(context.Background(), filePath,
			models.FileMatchInfo{Path: filePath, MovieID: movieID},
			scrape.ScrapeCmd{MovieID: movieID}, true, inputs, ScrapePhaseConfig{})
	}()

	select {
	case <-gen.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("scrape phase never reached poster generation")
	}

	// A writer on the scraped movie's key (crop/PATCH/override/rescrape all
	// take this lock) must NOT acquire it while generation replaces the cache.
	acquired := make(chan func(), 1)
	go func() { acquired <- AcquirePosterSourceLock(inputs.JobID.String(), movieID) }()
	select {
	case release := <-acquired:
		release()
		t.Fatal("poster-source lock acquired while the scrape was mid-generation (L2 regression)")
	case <-time.After(150 * time.Millisecond):
	}

	close(gen.finish)
	var outcome scrapeFileOutcome
	select {
	case outcome = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scrapeFile did not finish")
	}
	require.True(t, outcome.Success, "outcome: %+v", outcome)
	select {
	case release := <-acquired:
		release()
	case <-time.After(2 * time.Second):
		t.Fatal("poster-source lock not free after the scrape finished")
	}
}

// TestScrapePhase_CommitUnderScrapedIDLock pins D10: the family JOIN
// (UpdateFileResult commit) runs under the same scraped-ID poster-source
// lock the generation held, so an in-flight scrape cannot join a destination
// ID family between a rekey's collision check and its migration — the rekey
// holds that same key's lock across check+move.
func TestScrapePhase_CommitUnderScrapedIDLock(t *testing.T) {
	const movieID = "GEN-002"
	filePath := "/input/" + movieID + ".mp4"
	tracker := resultstore.New(1, []string{filePath})
	// scrapeFile writes: (1) Running status, (2) the final commit — block on 2.
	updater := &blockingCommitUpdater{
		Store: tracker, blockOn: 2, entered: make(chan struct{}), finish: make(chan struct{}),
	}
	gen := &blockingPosterGen{}
	inputs := newScrapeLockFixture(t, movieID, filePath, gen, updater)

	done := make(chan scrapeFileOutcome, 1)
	go func() {
		done <- scrapeFile(context.Background(), filePath,
			models.FileMatchInfo{Path: filePath, MovieID: movieID},
			scrape.ScrapeCmd{MovieID: movieID}, true, inputs, ScrapePhaseConfig{})
	}()

	select {
	case <-updater.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("scrape phase never reached the commit write")
	}

	// While the commit is in flight, the scraped ID's poster lock (the one a
	// PATCH-rename/rekey pairs on via CheckRenameDestinationCollision) must
	// still be held — the family join and the generation are one section.
	acquired := make(chan func(), 1)
	go func() { acquired <- AcquirePosterSourceLock(inputs.JobID.String(), movieID) }()
	select {
	case release := <-acquired:
		release()
		t.Fatal("poster-source lock acquired while the scrape commit was in flight (D10 regression)")
	case <-time.After(150 * time.Millisecond):
	}

	close(updater.finish)
	select {
	case outcome := <-done:
		require.True(t, outcome.Success, "outcome: %+v", outcome)
	case <-time.After(5 * time.Second):
		t.Fatal("scrapeFile did not finish")
	}
	select {
	case release := <-acquired:
		release()
	case <-time.After(2 * time.Second):
		t.Fatal("poster-source lock not free after the scrape finished")
	}
}

// TestRescrapePhase_CancelBeforeGenerationPreservesCache pins C5: a rescrape
// cancelled AFTER the scrape returned but BEFORE poster generation never
// touched the destination cache — its failure cleanup must not delete a
// pre-existing cached poster.
func TestRescrapePhase_CancelBeforeGenerationPreservesCache(t *testing.T) {
	const movieID = "CNX-001"
	filePath := "/source/" + movieID + ".mp4"
	jobID := models.NewJobID()
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: "https://new.example/poster.jpg"}},
	}}
	gen := &stubOverridePosterGen{}
	inputs, _ := rescrapeLockFixture(t, jobID, movieID, filePath, wf, gen)

	// Pre-seed the destination cache the cancel must not delete.
	fs := afero.NewMemMapFs()
	tempDir := "/tmp/ctxcancel"
	cacheDir := tempDir + "/posters/" + jobID.String()
	require.NoError(t, fs.MkdirAll(cacheDir, 0o755))
	oldAssets := []byte("pre-existing-cache")
	require.NoError(t, afero.WriteFile(fs, cacheDir+"/"+movieID+"-full.jpg", oldAssets, 0o644))
	require.NoError(t, afero.WriteFile(fs, cacheDir+"/"+movieID+".jpg", oldAssets, 0o644))
	inputs.Fs = fs
	inputs.TempDir = tempDir

	// Cancellation fires on the SECOND Err() call: ScrapeSingle's in-flight
	// timeout check is the first, the rescrape closure's pre-generation
	// re-check the second (see errOnNthCallCtx in rescrape_poster_lock_test.go).
	ctx := &errOnNthCallCtx{Context: context.Background()}
	res, err := NewRescrapePhase().Rescrape(ctx, inputs, RescrapeCmd{MovieID: movieID, FilePath: filePath})

	require.Error(t, err, "cancellation must surface")
	assert.True(t, errors.Is(err, context.Canceled), "err: %v", err)
	assert.Nil(t, res)
	assert.Equal(t, 0, gen.calls, "poster generation must not have run")

	got, readErr := afero.ReadFile(fs, cacheDir+"/"+movieID+"-full.jpg")
	require.NoError(t, readErr, "pre-existing full image must survive a pre-generation cancel (C5)")
	assert.Equal(t, oldAssets, got)
	got, readErr = afero.ReadFile(fs, cacheDir+"/"+movieID+".jpg")
	require.NoError(t, readErr, "pre-existing preview must survive a pre-generation cancel (C5)")
	assert.Equal(t, oldAssets, got)
}

// TestRescrapePhase_MirrorsPosterStateOntoSiblings pins I7: a same-ID
// (non-rekey) rescrape of ONE multipart sibling regenerates the shared
// {movieID}-full.jpg for the whole family — the OTHER siblings' poster state
// must be mirrored from the rescraped movie (the same fan-out
// mergeOverrideOntoPart performs for field overrides), or they keep state
// measured against the old image while sharing the new cached one.
func TestRescrapePhase_MirrorsPosterStateOntoSiblings(t *testing.T) {
	const movieID = "SIB-001"
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + movieID + "-cd2.mp4"
	jobID := models.NewJobID()
	newURL := "https://new.example/poster.jpg"

	tracker := resultstore.New(2, []string{fileCD1, fileCD2})
	for i, fp := range []string{fileCD1, fileCD2} {
		_ = i
		tracker.UpdateFileResult(fp, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fp, MovieID: movieID},
			Movie: &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{
				PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
			}},
			Status: models.JobStatusCompleted,
		})
	}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: newURL}},
	}}
	gen := &stubOverridePosterGen{stampCroppedURL: "/api/v1/temp/posters/" + jobID.String() + "/" + movieID + ".jpg?v=42"}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}

	res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: movieID, FilePath: fileCD1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, models.RescrapeStatusSuccess, res.Status, "res: %+v", res)

	sibling, getErr := tracker.GetMovieResult(fileCD2)
	require.NoError(t, getErr)
	require.NotNil(t, sibling.Movie)
	assert.Equal(t, newURL, sibling.Movie.Poster.PosterURL,
		"the non-rescraped sibling must see the refreshed poster source (I7)")
	assert.Equal(t, gen.stampCroppedURL, sibling.Movie.Poster.CroppedPosterURL,
		"the sibling's preview URL must match the regenerated cache")
	assert.False(t, sibling.Movie.Poster.ShouldCropPoster,
		"the sibling mirrors the rescraped movie's crop intent (scraper default)")
	assertPosterSourceLockFree(t, jobID.String(), movieID)
}

// panickingResultLookup wraps a real result store and panics inside
// GetFileResultByResultID while armed — but only on calls AFTER the first:
// ApplyFieldOverride reads the result once BEFORE taking the poster-source
// lock, so the injected failure must land on the second call (inside the
// post-acquisition convergence loop) to hit the pre-L1 leak window.
type panickingResultLookup struct {
	resultstore.Store
	armed int32
	calls int32
}

func (s *panickingResultLookup) GetFileResultByResultID(resultID string) (*resultstore.MovieResult, string, bool) {
	if atomic.LoadInt32(&s.armed) > 0 && atomic.AddInt32(&s.calls, 1) > 1 {
		panic("injected post-lock lookup panic")
	}
	return s.Store.GetFileResultByResultID(resultID)
}

// TestApplyFieldOverride_PanicAfterAcquisitionReleasesPosterLock pins Codex
// P1-2 for the override editor: the poster-source lock is refcounted, so a
// panic recovered by a caller BETWEEN AcquirePosterSourceLock and the end of
// ApplyFieldOverride must still release the CURRENT entry — the
// immediately-registered closure-form defer follows the release→re-acquire
// handoffs by variable, not by value.
func TestApplyFieldOverride_PanicAfterAcquisitionReleasesPosterLock(t *testing.T) {
	je, tracker, resultID, _, _ := multipartOverrideFixture(t, "PNK-001", "https://old.example/p.jpg", "", false)
	lookup := &panickingResultLookup{Store: tracker, armed: 1}
	je.store = lookup

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected the injected panic to propagate to the recover")
			}
		}()
		_, _, _ = je.ApplyFieldOverride(context.Background(), resultID, "title", "dmm")
	}()

	// Pre-fix (explicit, non-deferred releases) the lock entry for
	// (job-mp, PNK-001) is still held here: any acquirer would block forever.
	assertPosterSourceLockFree(t, "job-mp", "PNK-001")
}
