package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotStubPosterGen is a PosterGenerator that also satisfies the
// snapshot/restore capability the rescrape phase typeswitches on: it records
// snapshot/restore calls and fails them on demand.
type snapshotStubPosterGen struct {
	snapshotErr error
	// generateErr, when non-nil, makes GeneratePoster fail after the
	// pre-generation snapshot was taken — the fail-closed leg tested must
	// still see the armed rollback.
	generateErr error
	// snapshotErrFor fails the snapshot for a specific poster ID only — used
	// to exercise the rekey origin-snapshot failure without breaking the
	// destination snapshot.
	snapshotErrFor map[string]error
	restoreErr     error
	snapshots      int
	restores       int
	generated      int
	// snapshotIDs records the poster ID of every snapshot call in order; snaps
	// holds the returned snapshot values and restored the ones passed back to
	// RestorePosterAssets, so tests can correlate which assets were restored.
	snapshotIDs []string
	snaps       []*poster.AssetsSnapshot
	restored    []*poster.AssetsSnapshot
}

func (g *snapshotStubPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	g.generated++
	return g.generateErr
}
func (g *snapshotStubPosterGen) SnapshotPosterAssets(_, movieID string) (*poster.AssetsSnapshot, error) {
	g.snapshots++
	g.snapshotIDs = append(g.snapshotIDs, movieID)
	if err, ok := g.snapshotErrFor[movieID]; ok {
		return nil, err
	}
	if g.snapshotErr != nil {
		return nil, g.snapshotErr
	}
	snap := &poster.AssetsSnapshot{}
	g.snaps = append(g.snaps, snap)
	return snap, nil
}
func (g *snapshotStubPosterGen) RestorePosterAssets(snap *poster.AssetsSnapshot) error {
	g.restores++
	g.restored = append(g.restored, snap)
	return g.restoreErr
}

func rescrapePhaseTestInputs(t *testing.T, wf *stubRescrapeWorkflow, gen *snapshotStubPosterGen) (rescrapePhaseInputs, *resultstore.ResultTracker, string) {
	t.Helper()
	const filePath = "/source/rbk-001.mp4"
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "RBK-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "RBK-001", Title: "Old", Poster: models.PosterState{PosterURL: "https://old.invalid/poster.jpg"}},
	})
	return rescrapePhaseInputs{
		JobID:     models.NewJobID(),
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}, tracker.(*resultstore.ResultTracker), filePath
}

// TestRescrapePhase_Rescrape_PersistRunsUnderHeldLocks pins P1-1's core move:
// the job-envelope persist is part of the rescrape's poster-source-lock
// critical section. A probe contending on the SAME (job, movie) key while
// the persist runs must block until the phase returns — and observe the
// fully post-persist state afterwards.
func TestRescrapePhase_Rescrape_PersistRunsUnderHeldLocks(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie:        &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
		FieldSources: map[string]string{"title": "stub"},
	}}
	gen := &snapshotStubPosterGen{}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)

	persistRan := make(chan struct{})
	probeAcquired := make(chan struct{})
	probeDone := make(chan struct{})
	inputs.PersistEnvelope = func() error {
		// A concurrent writer contending on the same key while the persist
		// executes must NOT acquire until the phase's locks are released.
		go func() {
			defer close(probeDone)
			release := AcquirePosterSourceLock(inputs.JobID.String(), "RBK-001")
			defer release()
			close(probeAcquired)
		}()
		close(persistRan)
		select {
		case <-probeAcquired:
			t.Error("probe acquired the poster-source lock while the phase still held it")
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	assert.NoError(t, outcome.PersistErr)
	<-persistRan
	<-probeAcquired // the probe could only acquire after the phase released
	<-probeDone

	assert.Equal(t, 1, gen.snapshots, "the cache is snapshotted before GeneratePoster replaces it")
	assert.Equal(t, 1, gen.generated)
	assert.Equal(t, 0, gen.restores, "a successful persist never rolls back")
	committed, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, "New", committed.Movie.Title, "the committed state stands")
	// Provenance propagated to the store BEFORE the persist (it rides the
	// envelope) — moved into the phase from jobController.Rescrape.
	prov := tracker.GetProvenance(filePath)
	require.NotNil(t, prov)
	assert.Equal(t, "stub", prov.FieldSources["title"])
}

// TestRescrapePhase_Rescrape_NoPersistFnKeepsPreHoistFlow pins the degrade:
// without an envelope-persist seam (standalone jobs, tests), the phase
// persists nothing and reports no failure — exactly the pre-hoist non-API
// flow.
func TestRescrapePhase_Rescrape_NoPersistFnKeepsPreHoistFlow(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	assert.NoError(t, outcome.PersistErr)
	assert.Equal(t, 1, gen.snapshots)
	assert.Equal(t, 0, gen.restores, "with no persist there is nothing to roll back")
	committed, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, "New", committed.Movie.Title)
}

// TestRescrapePhase_Rescrape_PersistFailureRollsBackEverythingUnderLock pins
// the rollback half: a failed in-section persist restores the in-memory
// MovieResult AND the pre-rescrape provenance (P2-4) AND the replaced poster
// cache — all before the phase's locks release. A probe holding the same key
// afterwards observes the fully pre-rescrape state, never an interleaved
// half-revert.
func TestRescrapePhase_Rescrape_PersistFailureRollsBackEverythingUnderLock(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie:        &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
		FieldSources: map[string]string{"title": "stub"},
	}}
	gen := &snapshotStubPosterGen{}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)
	preProv := &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "old-source"}}
	tracker.SetProvenance(filePath, preProv)

	// A probe contending on the same (job, movie) key from inside the
	// persist: it can only acquire AFTER the phase released its locks, so
	// whatever it observes must be the fully-rolled-back (pre-rescrape)
	// state — never an interleaved half-revert.
	probeObservedTitle := make(chan string, 1)
	probeDone := make(chan struct{})
	inputs.PersistEnvelope = func() error {
		go func() {
			defer close(probeDone)
			release := AcquirePosterSourceLock(inputs.JobID.String(), "RBK-001")
			defer release()
			if live, getErr := tracker.GetMovieResult(filePath); getErr == nil && live.Movie != nil {
				probeObservedTitle <- live.Movie.Title
			} else {
				probeObservedTitle <- "<missing>"
			}
		}()
		return errors.New("job repository unavailable")
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	<-probeDone
	assert.Equal(t, "Old", <-probeObservedTitle,
		"the contending probe sees only the fully pre-rescrape state — the whole rollback completed under the held locks before release")
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	require.Error(t, outcome.PersistErr, "the in-section persist failure must surface on the outcome")
	assert.Contains(t, outcome.PersistErr.Error(), "job repository unavailable")
	assert.NotContains(t, outcome.PersistErr.Error(), "degraded", "every rollback leg is armed here")

	restored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, restored.Movie)
	assert.Equal(t, "Old", restored.Movie.Title, "F1: memory converges back to the pre-rescrape state")
	assert.Equal(t, "https://old.invalid/poster.jpg", restored.Movie.Poster.PosterURL)

	gotProv := tracker.GetProvenance(filePath)
	require.NotNil(t, gotProv)
	assert.Equal(t, "old-source", gotProv.FieldSources["title"],
		"P2-4: provenance is restored to the pre-rescrape attribution")

	assert.Equal(t, 1, gen.restores, "the destination cache leg restores the pre-generation assets")
}

// TestRescrapePhase_Rescrape_PersistFailureRekeyRestoresOriginToo pins F2
// under the in-section persist: a rekeying rescrape (A→B) whose envelope
// persist fails restores the destination's pre-generation assets AND origin
// A's pre-cleanup assets (withRescrapeStatus's orphan cleanup deleted them
// before the persist), and re-keys memory back to A.
func TestRescrapePhase_Rescrape_PersistFailureRekeyRestoresOriginToo(t *testing.T) {
	const (
		movieA   = "RBK-ORIG"
		movieB   = "RBK-ZDEST" // sorts after the origin: the no-swap lock path
		filePath = "/source/rbk-rekey.mp4"
	)
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Old A", Poster: models.PosterState{PosterURL: "https://old.invalid/a.jpg"}},
	})
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: "https://new.invalid/b.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs := rescrapePhaseInputs{
		JobID:     models.NewJobID(),
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}
	var persistCalls atomic.Int32
	inputs.PersistEnvelope = func() error {
		persistCalls.Add(1)
		return errors.New("job repository unavailable")
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	require.Error(t, outcome.PersistErr)
	assert.Equal(t, int32(1), persistCalls.Load(), "the persist ran once, inside the phase")

	assert.Equal(t, movieA, tracker.GetCurrentMovieID(filePath),
		"F1 on a rekey: memory converges back to the pre-rescrape identity")
	restored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, "Old A", restored.Movie.Title)

	assert.Equal(t, []string{movieB, movieA}, gen.snapshotIDs,
		"the destination snapshot precedes generation; the origin snapshot rides along")
	assert.Equal(t, 2, gen.restores,
		"F2: origin A's deleted assets must be recoverable too")
	assert.ElementsMatch(t, gen.snaps, gen.restored,
		"every snapshot taken (destination + origin) is restored by the cache leg")
}

// TestRescrapePhase_Rescrape_PersistFailureNotesDegradedRollback pins P3-7:
// when the destination asset snapshot fails (so the cache leg cannot be
// armed), the surfaced persist error must SAY the rollback is degraded
// instead of silently implying a full revert.
// TestRescrapePhase_Rescrape_DestinationSnapshotFailureFailsClosed pins the
// fail-closed contract (Codex P1): a destination-asset snapshot error rejects
// the rescrape BEFORE GeneratePoster replaces the cache. Continuing used to
// degrade to no-rollback, after which ANY later in-section failure (commit
// error, envelope-persist failure) would DELETE the destination's existing
// poster assets via the blanket failure cleanup (or leave the store rolled
// back against replaced bytes) — the snapshot is the only way back.
func TestRescrapePhase_Rescrape_DestinationSnapshotFailureFailsClosed(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{snapshotErr: errors.New("disk gone")}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)
	var persistCalls atomic.Int32
	inputs.PersistEnvelope = func() error {
		persistCalls.Add(1)
		return nil
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusFailed, outcome.Status, "a snapshot failure fails the rescrape closed")
	assert.Contains(t, outcome.Error, "failed to snapshot destination poster assets")
	assert.Equal(t, 1, gen.snapshots)
	assert.Equal(t, 0, gen.generated, "generation must never replace the cache without an armed rollback")
	assert.Equal(t, 0, gen.restores, "nothing mutated — no rollback runs")
	assert.Equal(t, int32(0), persistCalls.Load(), "a rescrape that failed before the commit never persists")
	current, cerr := tracker.GetMovieResult(filePath)
	require.NoError(t, cerr)
	assert.Equal(t, "Old", current.Movie.Title, "the pre-rescrape state stands untouched")
	assert.Equal(t, "RBK-001", tracker.GetCurrentMovieID(filePath))
}

// TestRescrapePhase_Rescrape_PosterGenerationFailureFailsClosedAndRestores
// pins the generation-failure leg of the fail-closed contract: GeneratePoster
// replaces the destination cache NON-atomically (DownloadFromURL removes the
// existing {movieID}-full.jpg BEFORE finalizing the replacement), so degrading
// a failure to success-with-metadata would commit the NEW poster URL while the
// OLD image is already gone, with no rollback ever replaying the snapshot.
// With a pre-generation snapshot armed the rescrape must FAIL, the failure
// cleanup must replay the armed destination rollback, and nothing may commit
// or persist.
func TestRescrapePhase_Rescrape_PosterGenerationFailureFailsClosedAndRestores(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{generateErr: errors.New("finalize exploded")}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)
	var persistCalls atomic.Int32
	inputs.PersistEnvelope = func() error {
		persistCalls.Add(1)
		return nil
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusFailed, outcome.Status, "a generation failure with an armed snapshot fails the rescrape closed")
	assert.Contains(t, outcome.Error, "poster generation failed")
	assert.Equal(t, 1, gen.snapshots)
	assert.Equal(t, 1, gen.generated)
	assert.Equal(t, 1, gen.restores, "the armed destination rollback replays the pre-generation snapshot")
	assert.Equal(t, int32(0), persistCalls.Load(), "a rescrape that failed before the commit never persists")
	current, cerr := tracker.GetMovieResult(filePath)
	require.NoError(t, cerr)
	assert.Equal(t, "Old", current.Movie.Title, "the pre-rescrape state stands untouched")
	assert.Nil(t, current.PosterError, "a fail-closed rescrape records no degraded success metadata")
	assert.Equal(t, "RBK-001", tracker.GetCurrentMovieID(filePath))
}

// TestRescrapePhase_Rescrape_PosterGenerationNoSourceDegradesUnderArmedSnapshot
// pins the no-mutation leg: ErrNoPosterSource is a PRE-DOWNLOAD rejection —
// nothing was touched — so even with a snapshot armed the rescrape keeps the
// degrade (success with PosterError metadata) instead of failing over an
// unrestorable nothing.
func TestRescrapePhase_Rescrape_PosterGenerationNoSourceDegradesUnderArmedSnapshot(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New"}, // no poster/cover URL on the merged movie
	}}
	gen := &snapshotStubPosterGen{generateErr: poster.ErrNoPosterSource}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status, "the pre-download no-source rejection never mutates the cache — degrade, don't fail")
	assert.Equal(t, 1, gen.snapshots, "the snapshot is still armed pre-generation")
	assert.Equal(t, 0, gen.restores, "nothing mutated — no restore")
	current, cerr := tracker.GetMovieResult(filePath)
	require.NoError(t, cerr)
	assert.Equal(t, "New", current.Movie.Title)
	require.NotNil(t, current.PosterError, "the degrade records the no-source failure")
	assert.Contains(t, *current.PosterError, "no poster or cover URL available")
}

// plainFailingPosterGen is a PosterGenerator WITHOUT the snapshot/restore
// capability that always fails generation: no rollback exists for it, and the
// blanket-delete fallback would strip assets the uncommitted store still
// references — so the phase must keep the pre-existing degrade.
type plainFailingPosterGen struct{ err error }

func (g *plainFailingPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	return g.err
}

// TestRescrapePhase_Rescrape_PosterGenerationFailureWithoutSnapshotDegrades
// pins the degrade leg: a generator without the snapshot capability cannot
// restore whatever a failed generation mutated, so the rescrape keeps the
// pre-existing success-with-metadata behavior (PosterError records the
// failure) instead of failing an unrestorable rescrape.
func TestRescrapePhase_Rescrape_PosterGenerationFailureWithoutSnapshotDegrades(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)
	inputs.PosterGen = &plainFailingPosterGen{err: errors.New("CDN 404")}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status, "no rollback exists for a snapshot-less generator — degrade to metadata")
	current, cerr := tracker.GetMovieResult(filePath)
	require.NoError(t, cerr)
	assert.Equal(t, "New", current.Movie.Title, "the degrade still commits the rescraped movie")
	require.NotNil(t, current.PosterError, "the degrade records the poster failure on the committed result")
	assert.Contains(t, *current.PosterError, "CDN 404")
}

// TestRescrapePhase_Rescrape_RekeyOriginSnapshotFailureFailsClosed pins the
// rekey half of the fail-closed contract (Codex P1): the success path's
// orphan cleanup DELETES origin A's assets after the commit and BEFORE the
// envelope persist, so continuing past a failed origin snapshot would let a
// later persist failure roll the in-memory state back onto A while A's
// assets are already deleted. The rescrape must fail closed before any
// destructive work; the destination snapshot that already succeeded is a
// read-only capture, and the Failed outcome carries a nil movieResult so the
// failure cleanup is a no-op.
func TestRescrapePhase_Rescrape_RekeyOriginSnapshotFailureFailsClosed(t *testing.T) {
	const (
		movieA   = "RBK-ORIG"
		movieB   = "RBK-ZDEST"
		filePath = "/source/rbk-rekey-snapfail.mp4"
	)
	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Old A", Poster: models.PosterState{PosterURL: "https://old.invalid/a.jpg"}},
	})
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: "https://new.invalid/b.jpg"}},
	}}
	gen := &snapshotStubPosterGen{snapshotErrFor: map[string]error{movieA: errors.New("origin disk gone")}}
	inputs := rescrapePhaseInputs{
		JobID:     models.NewJobID(),
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}
	var persistCalls atomic.Int32
	inputs.PersistEnvelope = func() error {
		persistCalls.Add(1)
		return nil
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusFailed, outcome.Status, "a failed origin snapshot fails the rescrape closed")
	assert.Contains(t, outcome.Error, "failed to snapshot origin poster assets")
	assert.Equal(t, []string{movieB, movieA}, gen.snapshotIDs, "the destination snapshot runs first, then the failing origin snapshot")
	assert.Equal(t, 0, gen.generated, "generation never ran — no destructive work followed the failed snapshot")
	assert.Equal(t, 0, gen.restores, "nothing mutated — no rollback runs")
	assert.Equal(t, int32(0), persistCalls.Load())
	assert.Equal(t, movieA, tracker.GetCurrentMovieID(filePath), "the origin key stands untouched")
	current, cerr := tracker.GetMovieResult(filePath)
	require.NoError(t, cerr)
	assert.Equal(t, "Old A", current.Movie.Title)
}

// TestRescrapePhase_Rescrape_PersistFailureReadOnlyStoreNotesDegradation pins
// the state-leg degrade: a ResultMap without write-back capability (stub
// accessor) still rolls the cache back, and the surfaced error notes the
// in-memory state could not be restored.
func TestRescrapePhase_Rescrape_PersistFailureReadOnlyStoreNotesDegradation(t *testing.T) {
	const filePath = "/source/rbk-ro.mp4"
	stub := newStubResultMap()
	stub.results[filePath] = &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: "RBK-RO"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "RBK-RO", Title: "Old"},
	}
	stub.matchInfo[filePath] = models.FileMatchInfo{Path: filePath, MovieID: "RBK-RO"}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-RO", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs := rescrapePhaseInputs{
		JobID:     models.NewJobID(),
		WF:        wf,
		PosterGen: gen,
		ResultMap: stub,
		Finder:    resultstore.New(0, nil), // cmd.FilePath is set — only GetRevision is used
		Lifecycle: &stubLifecycle{},
	}
	inputs.PersistEnvelope = func() error { return errors.New("job repository unavailable") }

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-RO", FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	require.Error(t, outcome.PersistErr)
	assert.Contains(t, outcome.PersistErr.Error(), "in-memory state and provenance rollback unavailable",
		"the un-armable state leg must be noted (P3-7)")
	assert.Equal(t, 1, gen.restores, "the asset rollback is independent of the store write-back capability")
}

// TestRescrapePhase_Rescrape_FailedScrapeNeverPersists pins the failure half:
// non-success outcomes already unwind the generated assets via
// withRescrapeStatus's cleanup and must NOT touch the envelope persist.
func TestRescrapePhase_Rescrape_FailedScrapeNeverPersists(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Status:  scrape.StatusFailed,
		Message: "no results",
	}}
	gen := &snapshotStubPosterGen{}
	inputs, _, filePath := rescrapePhaseTestInputs(t, wf, gen)
	var persistCalls atomic.Int32
	inputs.PersistEnvelope = func() error {
		persistCalls.Add(1)
		return nil
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusFailed, outcome.Status)
	assert.NoError(t, outcome.PersistErr)
	assert.Equal(t, 0, gen.snapshots, "a failed scrape never reaches poster generation")
	assert.Equal(t, int32(0), persistCalls.Load(), "a failed rescrape never persists the envelope")
}

// TestRescrapePhase_Rescrape_PersistFailureCacheRollbackErrorSurfaced pins
// the error channel of the in-section cache leg: a failed restore rides
// along on the surfaced persist error while the memory restore still runs.
func TestRescrapePhase_Rescrape_PersistFailureCacheRollbackErrorSurfaced(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{restoreErr: errors.New("restore exploded")}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)
	inputs.PersistEnvelope = func() error { return errors.New("job repository unavailable") }

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.Error(t, outcome.PersistErr)
	assert.Contains(t, outcome.PersistErr.Error(), "job repository unavailable")
	assert.Contains(t, outcome.PersistErr.Error(), "poster rollback failed: restore exploded")

	restored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, "Old", restored.Movie.Title, "the memory leg still runs when the cache leg fails")
}

// TestChainRollbacks pins the fusion helper: nil steps drop out (nil when all
// drop), surviving steps run in order, and a failing step does not
// short-circuit the rest while its error still surfaces via the join.
func TestChainRollbacks(t *testing.T) {
	assert.Nil(t, chainRollbacks(nil, nil), "no steps means no rollback")

	errFirst := errors.New("first restore exploded")
	var ran []string
	rb := chainRollbacks(
		func() error { ran = append(ran, "first"); return errFirst },
		nil,
		func() error { ran = append(ran, "second"); return nil },
	)
	require.NotNil(t, rb)
	err := rb()
	require.ErrorIs(t, err, errFirst)
	assert.Equal(t, []string{"first", "second"}, ran,
		"every step attempts its restore even after an earlier failure")
}

// atomicRollbackFailStore delegates every call to a real tracker except
// AtomicUpdateFileResult, which fails — driving the persist-failure state
// leg's restore error deterministically.
type atomicRollbackFailStore struct {
	resultstore.Store
	err error
}

func (s *atomicRollbackFailStore) AtomicUpdateFileResult(string, func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	return s.err
}

// TestRescrapePhase_Rescrape_PersistFailureStateRollbackErrorSurfaced flows
// an in-memory restore failure to the surfaced persist error while
// provenance restore and the cache leg still run.
func TestRescrapePhase_Rescrape_PersistFailureStateRollbackErrorSurfaced(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie:        &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
		FieldSources: map[string]string{"title": "stub"},
	}}
	gen := &snapshotStubPosterGen{}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)
	tracker.SetProvenance(filePath, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "old-source"}})
	inputs.ResultMap = &atomicRollbackFailStore{Store: tracker, err: errors.New("state store jammed")}
	inputs.PersistEnvelope = func() error { return errors.New("job repository unavailable") }

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.Error(t, outcome.PersistErr)
	assert.Contains(t, outcome.PersistErr.Error(), "job repository unavailable")
	assert.Contains(t, outcome.PersistErr.Error(), "state rollback failed: state store jammed",
		"a failed in-memory restore surfaces alongside the persist error")

	// The provenance leg still ran (delegated store) — restored to the
	// pre-rescrape attribution even though the MovieResult restore failed.
	prov := tracker.GetProvenance(filePath)
	require.NotNil(t, prov)
	assert.Equal(t, "old-source", prov.FieldSources["title"])
	assert.Equal(t, 1, gen.restores, "the cache leg still runs")
}

// staleRevisionFinder wraps a FileFinder reporting a permanently-stale
// revision so CompleteRescrape's CAS check conflicts deterministically —
// the lock-agnostic state-write race the Conflict status exists for.
type staleRevisionFinder struct{ resultstore.FileFinder }

func (staleRevisionFinder) GetRevision(string) uint64 { return 0 }

// fsBackedRescrapePosterGen is a poster generator whose GeneratePoster writes
// DETERMINISTIC placeholder assets directly (no network) while
// snapshot/restore go through the REAL PosterManager, so the rescrape
// failure-cleanup contract is pinned byte-for-byte: did the bystander's
// pre-generation bytes actually come back?
type fsBackedRescrapePosterGen struct {
	manager    *poster.PosterManager
	fs         afero.Fs
	tempDir    string
	onGenerate func(movieID string) // runs BEFORE the assets are replaced
	generated  []string
	snapshots  []string
	restores   int
}

func (g *fsBackedRescrapePosterGen) GeneratePoster(_ context.Context, jobID string, movie *models.Movie) error {
	g.generated = append(g.generated, movie.ID)
	if g.onGenerate != nil {
		g.onGenerate(movie.ID)
	}
	dir := filepath.Join(g.tempDir, "posters", jobID)
	if err := g.fs.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := afero.WriteFile(g.fs, filepath.Join(dir, movie.ID+"-full.jpg"), []byte("fresh:"+movie.ID+":full"), 0o644); err != nil {
		return err
	}
	return afero.WriteFile(g.fs, filepath.Join(dir, movie.ID+".jpg"), []byte("fresh:"+movie.ID+":preview"), 0o644)
}

func (g *fsBackedRescrapePosterGen) SnapshotPosterAssets(jobID, movieID string) (*poster.AssetsSnapshot, error) {
	g.snapshots = append(g.snapshots, movieID)
	return g.manager.SnapshotAssets(jobID, movieID)
}

func (g *fsBackedRescrapePosterGen) RestorePosterAssets(snap *poster.AssetsSnapshot) error {
	g.restores++
	return g.manager.RestoreAssets(snap)
}

// TestRescrapePhase_Rescrape_RekeyCollisionLeavesBystanderCacheUntouched pins
// the Codex P0 at the ASSET level: a rekeying rescrape (A→B) where ANOTHER
// result (the bystander, fileB) already owns movie ID B is REJECTED by
// CheckRenameDestinationCollision under the held (A,B) poster-lock pair —
// BEFORE any asset snapshot, GeneratePoster, or commit. B's cached
// {B}-full.jpg/preview keep their pre-rescrape bytes byte-for-byte, its
// persisted preview URL still names real files, and A's state never rekeys.
// (This scenario previously exercised the failure-cleanup snapshot replay /
// blanket-delete replacement after a failed commit; the collision rejection
// makes that interleave unreachable by rescrape, so the pin moves from
// rollback-correctness to never-touch. The snapshot-replay legs remain
// covered by the unoccupied-destination rollback tests above.)
func TestRescrapePhase_Rescrape_RekeyCollisionLeavesBystanderCacheUntouched(t *testing.T) {
	const (
		jobID   = "job-bys-clean"
		movieA  = "BYS-ORIG"
		movieB  = "BYS-ZDEST"
		tempDir = "/temp"
	)
	fileA := "/source/bys-a.mp4"
	fileB := "/source/bys-b.mp4"
	bystanderPreviewURL := "/api/v1/temp/posters/" + jobID + "/" + movieB + ".jpg?v=77"

	setup := func(t *testing.T) (*fsBackedRescrapePosterGen, *resultstore.ResultTracker, rescrapePhaseInputs) {
		t.Helper()
		tracker := resultstore.New(2, []string{fileA, fileB})
		tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieA, Title: "Old A", Poster: models.PosterState{PosterURL: "https://old.invalid/a.jpg"}},
		})
		tracker.UpdateFileResult(fileB, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fileB, MovieID: movieB},
			Status:        models.JobStatusCompleted,
			Movie: &models.Movie{ID: movieB, Title: "Bystander B", Poster: models.PosterState{
				PosterURL:        "https://old.invalid/b.jpg",
				CroppedPosterURL: bystanderPreviewURL,
			}},
		})
		fs := afero.NewMemMapFs()
		dir := filepath.Join(tempDir, "posters", jobID)
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, movieA+"-full.jpg"), []byte("orig:a:full"), 0o644))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, movieA+".jpg"), []byte("orig:a:preview"), 0o644))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, movieB+"-full.jpg"), []byte("bystander:b:full"), 0o644))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, movieB+".jpg"), []byte("bystander:b:preview"), 0o644))
		gen := &fsBackedRescrapePosterGen{
			manager: poster.NewPosterManager(fs, tempDir, nil),
			fs:      fs,
			tempDir: tempDir,
		}
		wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: "https://new.invalid/b.jpg"}},
		}}
		inputs := rescrapePhaseInputs{
			JobID:     models.JobID(jobID),
			WF:        wf,
			PosterGen: gen,
			ResultMap: tracker,
			Finder:    tracker,
			Lifecycle: &stubLifecycle{},
			Fs:        fs,
			TempDir:   tempDir,
		}
		return gen, tracker.(*resultstore.ResultTracker), inputs
	}

	gen, tracker, inputs := setup(t)
	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: fileA})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusFailed, outcome.Status,
		"rekeying onto an ID another result family owns is rejected, not applied")
	assert.Contains(t, outcome.Error, "already uses that movie ID")
	assert.NoError(t, outcome.PersistErr)

	// Rejected BEFORE any asset work: no snapshot, no generation, no restore.
	assert.Empty(t, gen.generated, "poster generation never ran for the rejected rekey")
	assert.Empty(t, gen.snapshots)
	assert.Equal(t, 0, gen.restores)

	dir := filepath.Join(tempDir, "posters", jobID)
	for name, want := range map[string]string{
		movieB + "-full.jpg": "bystander:b:full",
		movieB + ".jpg":      "bystander:b:preview",
		movieA + "-full.jpg": "orig:a:full",
		movieA + ".jpg":      "orig:a:preview",
	} {
		data, rerr := afero.ReadFile(gen.fs, filepath.Join(dir, name))
		require.NoError(t, rerr)
		assert.Equal(t, want, string(data), "%s must keep its pre-rescrape bytes byte-for-byte", name)
	}

	// The rejected rescrape committed nothing: both stored results stand.
	finalB, err := tracker.GetMovieResult(fileB)
	require.NoError(t, err)
	require.NotNil(t, finalB.Movie)
	assert.Equal(t, movieB, finalB.Movie.ID)
	assert.Equal(t, bystanderPreviewURL, finalB.Movie.Poster.CroppedPosterURL,
		"the bystander's persisted preview URL is still backed by real bytes")
	finalA, err := tracker.GetMovieResult(fileA)
	require.NoError(t, err)
	assert.Equal(t, movieA, finalA.Movie.ID)
	assert.Equal(t, "https://old.invalid/a.jpg", finalA.Movie.Poster.PosterURL)

	assertPosterSourceLockFree(t, jobID, movieA)
	assertPosterSourceLockFree(t, jobID, movieB)
}

// TestRescrapePhase_Rescrape_FailureCleanupRestoreErrorSurfaced pins r10
// P1-4: a failed destination-snapshot replay during the failure cleanup
// previously only LOGGED — the caller got the rescrape failure with no
// indication the cache may still hold replaced/corrupted assets. The
// rollback error now rides on the surfaced failure, matching the
// persist-failure rollback's join pattern.
func TestRescrapePhase_Rescrape_FailureCleanupRestoreErrorSurfaced(t *testing.T) {
	const (
		movieA = "WRN-ORIG"
		movieB = "WRN-ZDEST"
	)
	fileA := "/source/wrn-a.mp4"
	tracker := resultstore.New(1, []string{fileA})
	tracker.UpdateFileResult(fileA, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileA, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieA, Title: "Old A", Poster: models.PosterState{PosterURL: "https://old.invalid/a.jpg"}},
	})
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: "https://new.invalid/b.jpg"}},
	}}
	gen := &snapshotStubPosterGen{restoreErr: errors.New("restore exploded")}
	inputs := rescrapePhaseInputs{
		JobID:     models.NewJobID(),
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    &staleRevisionFinder{FileFinder: tracker},
		Lifecycle: &stubLifecycle{},
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: fileA})
	require.NoError(t, err, "a failure-cleanup restore error rides on the outcome, not the Go error return")
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusConflict, outcome.Status)
	assert.Contains(t, outcome.Error, "poster cache rollback failed",
		"the rollback error is surfaced so the caller knows the cache may still hold replaced/corrupted assets")
	assert.Contains(t, outcome.Error, "restore exploded")
	assert.Equal(t, []string{movieB, movieA}, gen.snapshotIDs, "both snapshots were armed")
	assert.Equal(t, 1, gen.restores, "the destination replay was the leg attempted during failure cleanup")
}

// TestWithRescrapeStatus_FailurePosterRewind pins the rewiring seams of
// withRescrapeStatus directly: the wired failureCleanup replaces the blanket
// delete, the delete stays as the no-hook fallback, and both failure sites
// err and Gone/Conflict/Failed still audit through HistoryRepo.
func TestWithRescrapeStatus_FailurePosterRewind(t *testing.T) {
	newLifecycle := func(inputs rescrapePhaseInputs) rescrapeLifecycle {
		return rescrapeLifecycle{
			inputs: inputs,
			lookup: &resultstore.FileLookupResult{FilePath: "/src/wrs.mp4", OldMovieID: "WRS-OLD"},
		}
	}
	boom := errors.New("boom")

	t.Run("nil outcome with no error passes through untouched", func(t *testing.T) {
		lc := newLifecycle(rescrapePhaseInputs{JobID: "job-wrs-nil"})
		outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return nil, nil, nil
		})
		require.NoError(t, err)
		assert.Nil(t, outcome, "no cleanup, no audit — a nil outcome is a pass-through")
	})

	t.Run("no failureCleanup falls back to deleting the destination assets", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		dir := filepath.Join("/temp", "posters", "job-wrs-fb")
		full := filepath.Join(dir, "WRS-1-full.jpg")
		preview := filepath.Join(dir, "WRS-1.jpg")
		require.NoError(t, afero.WriteFile(fs, full, []byte("f"), 0o644))
		require.NoError(t, afero.WriteFile(fs, preview, []byte("p"), 0o644))
		lc := newLifecycle(rescrapePhaseInputs{JobID: "job-wrs-fb", Fs: fs, TempDir: "/temp"})
		movieResult := &resultstore.MovieResult{Movie: &models.Movie{ID: "WRS-1"}}

		outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return nil, movieResult, boom
		})
		require.ErrorIs(t, err, boom)
		assert.Nil(t, outcome)
		for _, p := range []string{full, preview} {
			_, statErr := fs.Stat(p)
			assert.True(t, os.IsNotExist(statErr), "%s must be deleted by the fallback cleanup", p)
		}
	})

	t.Run("wired failureCleanup replaces the delete on a failed outcome", func(t *testing.T) {
		var cleaned *models.Movie
		lc := newLifecycle(rescrapePhaseInputs{JobID: "job-wrs-hook"})
		lc.failureCleanup = func(m *models.Movie) error { cleaned = m; return nil }
		movieResult := &resultstore.MovieResult{Movie: &models.Movie{ID: "WRS-2"}}

		outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: "nope"}, movieResult, nil
		})
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusFailed, outcome.Status)
		require.NotNil(t, cleaned, "the wired cleanup received the failed rescrape's movie")
		assert.Equal(t, "WRS-2", cleaned.ID)
	})

	t.Run("error path joins a failure-cleanup rollback error onto the surfaced error", func(t *testing.T) {
		lc := newLifecycle(rescrapePhaseInputs{JobID: "job-wrs-join"})
		rbBoom := errors.New("dest replay exploded")
		lc.failureCleanup = func(m *models.Movie) error { return rbBoom }
		movieResult := &resultstore.MovieResult{Movie: &models.Movie{ID: "WRS-5"}}

		outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return nil, movieResult, boom
		})
		require.ErrorIs(t, err, boom, "the original failure leads")
		assert.Contains(t, err.Error(), "poster cache rollback failed", "the rollback error is joined, not swallowed")
		assert.Contains(t, err.Error(), "dest replay exploded")
		assert.Nil(t, outcome)
	})

	t.Run("failed outcome joins a failure-cleanup rollback error onto outcome.Error", func(t *testing.T) {
		lc := newLifecycle(rescrapePhaseInputs{JobID: "job-wrs-join2"})
		rbBoom := errors.New("dest replay exploded")
		lc.failureCleanup = func(m *models.Movie) error { return rbBoom }
		movieResult := &resultstore.MovieResult{Movie: &models.Movie{ID: "WRS-6"}}

		outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: "explicit boom"}, movieResult, nil
		})
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusFailed, outcome.Status)
		assert.Contains(t, outcome.Error, "explicit boom")
		assert.Contains(t, outcome.Error, "poster cache rollback failed")
		assert.Contains(t, outcome.Error, "dest replay exploded")
	})

	t.Run("error path audits the failed movie's ID", func(t *testing.T) {
		repo := &failingHistoryRepo{err: boom}
		lc := newLifecycle(rescrapePhaseInputs{JobID: "job-wrs-aud1", HistoryRepo: repo})
		movieResult := &resultstore.MovieResult{Movie: &models.Movie{ID: "WRS-3"}}

		_, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return nil, movieResult, boom
		})
		require.ErrorIs(t, err, boom)
		assert.Equal(t, 1, repo.callCount, "the audit ran (log-and-continue on a failing repo)")
	})

	t.Run("failed outcome audits with the movie's ID and the explicit message", func(t *testing.T) {
		repo := &failingHistoryRepo{err: boom}
		lc := newLifecycle(rescrapePhaseInputs{JobID: "job-wrs-aud2", HistoryRepo: repo})
		movieResult := &resultstore.MovieResult{Movie: &models.Movie{ID: "WRS-4"}}

		outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: "explicit boom"}, movieResult, nil
		})
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusFailed, outcome.Status)
		assert.Equal(t, 1, repo.callCount)
	})

	t.Run("gone outcome audits the lookup's old movie ID with a synthetic message", func(t *testing.T) {
		repo := &failingHistoryRepo{err: boom}
		lc := newLifecycle(rescrapePhaseInputs{JobID: "job-wrs-aud3", HistoryRepo: repo})

		outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return &RescrapeResult{Status: models.RescrapeStatusGone}, nil, nil
		})
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusGone, outcome.Status)
		assert.Equal(t, 1, repo.callCount,
			"auditRescapeFailure ran with lookup.OldMovieID and the synthetic 'rescrape gone' message")
	})
}

// getResultFailStore hides GetMovieResult so the phase cannot capture a
// pre-rescrape in-memory snapshot (the no-snapshot degrade leg).
type getResultFailStore struct{ resultstore.Store }

func (getResultFailStore) GetMovieResult(string) (*resultstore.MovieResult, error) {
	return nil, errors.New("read gone")
}

// TestRescrapePhase_Rescrape_PersistFailureNoSnapshotDegrade pins the
// state-leg degrade when NO snapshot could be captured: the cache leg still
// restores and the surfaced error names the un-armable state leg (P3-7).
func TestRescrapePhase_Rescrape_PersistFailureNoSnapshotDegrade(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)
	inputs.ResultMap = &getResultFailStore{Store: tracker}
	inputs.PersistEnvelope = func() error { return errors.New("job repository unavailable") }

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.Error(t, outcome.PersistErr)
	assert.Contains(t, outcome.PersistErr.Error(), "no pre-rescrape snapshot captured",
		"the un-armable state leg must be named (P3-7)")
	assert.Contains(t, outcome.PersistErr.Error(), "degraded rollback")
	assert.Equal(t, 1, gen.restores, "the cache leg still restores")

	current, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, "New", current.Movie.Title, "memory stays rescraped — the degrade is reported, not hidden")
}
