package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
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
