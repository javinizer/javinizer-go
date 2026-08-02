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
	return nil
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
func TestRescrapePhase_Rescrape_PersistFailureNotesDegradedRollback(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{snapshotErr: errors.New("disk gone")}
	inputs, _, filePath := rescrapePhaseTestInputs(t, wf, gen)
	inputs.PersistEnvelope = func() error { return errors.New("job repository unavailable") }

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	require.Error(t, outcome.PersistErr)
	assert.Contains(t, outcome.PersistErr.Error(), "job repository unavailable")
	assert.Contains(t, outcome.PersistErr.Error(), "degraded rollback")
	assert.Contains(t, outcome.PersistErr.Error(), "destination poster cache rollback unavailable",
		"the un-armable cache leg must be called out on the surfaced error")
	assert.Equal(t, 1, gen.generated, "the rescrape itself still ran (snapshot failure never rejects)")
	assert.Equal(t, 0, gen.restores, "the failed snapshot means no cache leg could run")
}

// TestRescrapePhase_Rescrape_PersistFailureRekeyOriginSnapshotDegrade pins
// the rekey half of P3-7: the ORIGIN snapshot failure degrades only its own
// leg — the destination cache still restores and the error names the missing
// origin leg.
func TestRescrapePhase_Rescrape_PersistFailureRekeyOriginSnapshotDegrade(t *testing.T) {
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
	inputs.PersistEnvelope = func() error { return errors.New("job repository unavailable") }

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	require.Error(t, outcome.PersistErr)
	assert.Equal(t, []string{movieB, movieA}, gen.snapshotIDs, "both snapshots are attempted")
	assert.Equal(t, 1, gen.restores, "only the destination snapshot could be restored")
	assert.Contains(t, outcome.PersistErr.Error(), "origin poster cache rollback unavailable",
		"the un-armable ORIGIN leg is named on the surfaced error")
	assert.Equal(t, movieA, tracker.GetCurrentMovieID(filePath),
		"memory still re-keys back despite the degraded cache leg")
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

// TestRescrapePhase_Rescrape_FailureCleanupRestoresBystanderDestinationAssets
// pins audit-6's second site: a rekeying rescrape (A→B) where ANOTHER result
// (the bystander, fileB) already owns movie ID B fails AFTER GeneratePoster
// replaced B's shared assets — the commit CAS-conflicts (stale revision) or
// the job's context is cancelled post-generation. withRescrapeStatus's
// failure cleanup must REPLAY the captured pre-generation destination
// snapshot instead of blanket-deleting B's files: under the old delete the
// bystander's persisted preview URL was left pointing at files that no
// longer exist. Origin A's snapshot is armed but never replayed here (the
// origin's assets are deleted only by the SUCCESS path's orphan cleanup), so
// no leg double-runs.
func TestRescrapePhase_Rescrape_FailureCleanupRestoresBystanderDestinationAssets(t *testing.T) {
	const (
		jobID   = "job-bys-clean"
		movieA  = "BYS-ORIG"
		movieB  = "BYS-ZDEST"
		tempDir = "/temp"
	)
	fileA := "/source/bys-a.mp4"
	fileB := "/source/bys-b.mp4"
	bystanderPreviewURL := "/api/v1/temp/posters/" + jobID + "/" + movieB + ".jpg?v=77"

	setup := func(t *testing.T, finder resultstore.FileFinder) (*fsBackedRescrapePosterGen, *resultstore.ResultTracker, rescrapePhaseInputs) {
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
			Finder:    finder,
			Lifecycle: &stubLifecycle{},
			Fs:        fs,
			TempDir:   tempDir,
		}
		return gen, tracker.(*resultstore.ResultTracker), inputs
	}

	assertBystanderAndOriginIntact := func(t *testing.T, gen *fsBackedRescrapePosterGen, tracker *resultstore.ResultTracker) {
		t.Helper()
		dir := filepath.Join(tempDir, "posters", jobID)
		for name, want := range map[string]string{
			movieB + "-full.jpg": "bystander:b:full",
			movieB + ".jpg":      "bystander:b:preview",
		} {
			data, err := afero.ReadFile(gen.fs, filepath.Join(dir, name))
			require.NoError(t, err, "%s must have been restored, not deleted — the bystander's preview URL (%s) still names it", name, bystanderPreviewURL)
			assert.Equal(t, want, string(data), "the bystander's pre-generation bytes came back byte-for-byte")
		}
		for name, want := range map[string]string{
			movieA + "-full.jpg": "orig:a:full",
			movieA + ".jpg":      "orig:a:preview",
		} {
			data, err := afero.ReadFile(gen.fs, filepath.Join(dir, name))
			require.NoError(t, err, "the origin's assets are untouched on failure paths")
			assert.Equal(t, want, string(data))
		}

		assert.Equal(t, []string{movieB}, gen.generated, "generation ran onto the destination key")
		assert.Equal(t, []string{movieB, movieA}, gen.snapshots,
			"the destination snapshot precedes generation; the origin snapshot rides along (armed, unused)")
		assert.Equal(t, 1, gen.restores,
			"only the destination snapshot replays — the origin leg must not double-run a leg that never deleted anything")

		// The failed rescrape committed nothing: both stored results stand.
		finalB, err := tracker.GetMovieResult(fileB)
		require.NoError(t, err)
		require.NotNil(t, finalB.Movie)
		assert.Equal(t, movieB, finalB.Movie.ID)
		assert.Equal(t, bystanderPreviewURL, finalB.Movie.Poster.CroppedPosterURL,
			"the bystander's persisted preview URL is again backed by real bytes")
		finalA, err := tracker.GetMovieResult(fileA)
		require.NoError(t, err)
		assert.Equal(t, movieA, finalA.Movie.ID)

		assertPosterSourceLockFree(t, jobID, movieA)
		assertPosterSourceLockFree(t, jobID, movieB)
	}

	t.Run("commit CAS conflict", func(t *testing.T) {
		gen, tracker, inputs := setup(t, &staleRevisionFinder{FileFinder: resultstore.New(0, nil)})
		// The stale finder reports revision 0 while the real store sits at
		// revision 1 — the commit conflicts deterministically.
		outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: fileA})
		require.NoError(t, err)
		require.NotNil(t, outcome)
		assert.Equal(t, models.RescrapeStatusConflict, outcome.Status)
		assert.NoError(t, outcome.PersistErr)
		assertBystanderAndOriginIntact(t, gen, tracker)
	})

	t.Run("post-generation cancellation", func(t *testing.T) {
		gen, tracker, inputs := setup(t, resultstore.New(0, nil))
		ctx, cancel := context.WithCancel(context.Background())
		gen.onGenerate = func(string) { cancel() }
		defer cancel()

		outcome, err := NewRescrapePhase().Rescrape(ctx, inputs, RescrapeCmd{MovieID: movieA, FilePath: fileA})
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, outcome)
		assertBystanderAndOriginIntact(t, gen, tracker)
	})
}

// TestRescrapePhase_Rescrape_FailureCleanupRestoreErrorWarnsNotFails pins
// the degraded leg of the F-new failure cleanup: when the destination
// snapshot replay ITSELF fails, the rescrape's Conflict outcome still
// stands — the restore failure is logged as a warning (the cleanup owns no
// error channel), never turned into a second failure.
func TestRescrapePhase_Rescrape_FailureCleanupRestoreErrorWarnsNotFails(t *testing.T) {
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
	require.NoError(t, err, "a failed failure-cleanup restore is logged, not surfaced as a rescrape error")
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusConflict, outcome.Status)
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
		lc.failureCleanup = func(m *models.Movie) { cleaned = m }
		movieResult := &resultstore.MovieResult{Movie: &models.Movie{ID: "WRS-2"}}

		outcome, err := withRescrapeStatus(lc, func() (*RescrapeResult, *resultstore.MovieResult, error) {
			return &RescrapeResult{Status: models.RescrapeStatusFailed, Error: "nope"}, movieResult, nil
		})
		require.NoError(t, err)
		assert.Equal(t, models.RescrapeStatusFailed, outcome.Status)
		require.NotNil(t, cleaned, "the wired cleanup received the failed rescrape's movie")
		assert.Equal(t, "WRS-2", cleaned.ID)
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
