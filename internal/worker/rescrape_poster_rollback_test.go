package worker

import (
	"context"
	"errors"
	"testing"

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

// TestRescrapePhase_Rescrape_AttachesPosterCacheRollbackOnSuccess pins F-B's
// worker half: GeneratePoster replaces {movieID}-full.jpg BEFORE the commit,
// so the successful outcome must carry a snapshot-based rollback the caller
// can invoke when its post-commit envelope persist fails. The snapshot is
// taken before generation, and invoking the rollback restores via
// RestorePosterAssets.
func TestRescrapePhase_Rescrape_AttachesPosterCacheRollbackOnSuccess(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs, _, filePath := rescrapePhaseTestInputs(t, wf, gen)

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)

	assert.Equal(t, 1, gen.snapshots, "the cache must be snapshotted before GeneratePoster replaces it")
	assert.Equal(t, 1, gen.generated)
	require.NotNil(t, outcome.PosterCacheRollback,
		"a successful rescrape must carry its pre-generation cache snapshot for the envelope-persist failure path")

	require.NoError(t, outcome.PosterCacheRollback())
	assert.Equal(t, 1, gen.restores, "the rollback restores the pre-generation assets")
}

// TestRescrapePhase_Rescrape_NoRollbackWithoutSnapshooter pins the degrade:
// a generator without the snapshot capability (test-stub style) leaves the
// rollback nil — the persist-failure path then restores nothing (same
// contract as RefreshPosterAssets with a non-snapshotting generator).
func TestRescrapePhase_Rescrape_NoRollbackWithoutSnapshooter(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	inputs, _, filePath := rescrapePhaseTestInputs(t, wf, nil)
	inputs.PosterGen = &recordingPosterGen{} // no snapshot/restore methods

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	assert.Nil(t, outcome.PosterCacheRollback)
}

// TestRescrapePhase_Rescrape_SnapshotFailureDegradesToNoRollback ensures a
// snapshot failure does not reject an otherwise successful rescrape (poster
// generation itself is already best-effort): the outcome carries no rollback.
func TestRescrapePhase_Rescrape_SnapshotFailureDegradesToNoRollback(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{snapshotErr: errors.New("disk gone")}
	inputs, _, filePath := rescrapePhaseTestInputs(t, wf, gen)

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	assert.Equal(t, 1, gen.generated, "the rescrape still generates posters after a snapshot failure")
	assert.Nil(t, outcome.PosterCacheRollback,
		"a failed snapshot means nothing can be restored — no rollback is attached")
}

// TestRescrapePhase_Rescrape_FailedRescrapeCarriesNoRollback pins the failure
// half: non-success outcomes already unwind the generated assets via
// withRescrapeStatus's cleanup, so attaching the snapshot restore there
// would resurrect assets the cleanup deliberately removed.
func TestRescrapePhase_Rescrape_FailedRescrapeCarriesNoRollback(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Status:  scrape.StatusFailed,
		Message: "no results",
	}}
	gen := &snapshotStubPosterGen{}
	inputs, _, filePath := rescrapePhaseTestInputs(t, wf, gen)

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	require.Equal(t, models.RescrapeStatusFailed, outcome.Status)
	assert.Equal(t, 0, gen.snapshots, "a failed scrape never reaches poster generation")
	assert.Nil(t, outcome.PosterCacheRollback)
}

// TestRescrapePhase_Rescrape_AttachesResultStateRollbackOnSuccess pins F1's
// worker half: the pre-rescrape MovieResult is snapshotted (cloned) under the
// poster lock at the commit-capture point, so a post-commit envelope persist
// failure can restore the in-memory result to the exact state the CAS commit
// replaced — memory then converges with the cache rollback and the
// unpersisted envelope.
func TestRescrapePhase_Rescrape_AttachesResultStateRollbackOnSuccess(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	gen := &snapshotStubPosterGen{}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)

	committed, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	require.Equal(t, "New", committed.Movie.Title, "the commit landed before the persist-failure rollback runs")

	require.NotNil(t, outcome.ResultStateRollback,
		"a successful rescrape must carry its pre-rescrape in-memory snapshot for the envelope-persist failure path")
	require.NoError(t, outcome.ResultStateRollback())

	restored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	require.NotNil(t, restored.Movie)
	assert.Equal(t, "Old", restored.Movie.Title, "memory must converge back to the pre-rescrape state")
	assert.Equal(t, "https://old.invalid/poster.jpg", restored.Movie.Poster.PosterURL)
	assert.Equal(t, "RBK-001", restored.FileMatchInfo.MovieID)
	assert.Greater(t, restored.Revision, committed.Revision,
		"the restore is a normal store write: the revision moves forward, so later CAS writers are unaffected")

	// Repeat invocation stays pristine (the closure re-clones the snapshot).
	require.NoError(t, outcome.ResultStateRollback())
	again, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, "Old", again.Movie.Title)
}

// TestRescrapePhase_Rescrape_StateRollbackDegradesForReadOnlyStore pins the
// degrade: a ResultMap without the write-back capability (a stub accessor)
// leaves the state rollback nil — the cache rollback is unaffected.
func TestRescrapePhase_Rescrape_StateRollbackDegradesForReadOnlyStore(t *testing.T) {
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

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-RO", FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	assert.Nil(t, outcome.ResultStateRollback,
		"a store that cannot be written back through cannot restore in-memory state")
	assert.NotNil(t, outcome.PosterCacheRollback, "the asset rollback is independent of the store write-back capability")
}

// TestRescrapePhase_Rescrape_RekeyRollbackRestoresOriginAssets pins F2: a
// rekeying rescrape (A→B) snapshots BOTH the destination's pre-generation
// assets and origin A's pre-cleanup assets — withRescrapeStatus's success-path
// orphan cleanup deletes A's assets after the commit, before the caller's
// envelope persist, so the rollback must be able to recreate them.
func TestRescrapePhase_Rescrape_RekeyRollbackRestoresOriginAssets(t *testing.T) {
	const (
		movieA = "RBK-ORIG"
		movieB = "RBK-ZDEST" // sorts after the origin: the no-swap lock path

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

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)

	require.Equal(t, movieB, tracker.GetCurrentMovieID(filePath), "the rekey committed")
	assert.Contains(t, outcome.OrphanedMovieIDs, movieA)
	assert.Equal(t, []string{movieB, movieA}, gen.snapshotIDs,
		"the destination snapshot is taken before generation, the origin snapshot alongside it")

	// The in-memory rollback re-keys the store entry back to the origin.
	require.NoError(t, outcome.ResultStateRollback())
	assert.Equal(t, movieA, tracker.GetCurrentMovieID(filePath),
		"F1 on a rekey: memory converges back to the pre-rescrape identity")
	restored, err := tracker.GetMovieResult(filePath)
	require.NoError(t, err)
	assert.Equal(t, "Old A", restored.Movie.Title)

	// The cache rollback restores BOTH asset sets: the destination's
	// pre-generation assets and the origin's pre-cleanup assets.
	require.NoError(t, outcome.PosterCacheRollback())
	require.Equal(t, 2, gen.restores, "origin A's deleted assets must be recoverable too (F2)")
	assert.ElementsMatch(t, gen.snaps, gen.restored,
		"every snapshot taken (destination + origin) is restored by the composed rollback")
}

// TestRescrapePhase_Rescrape_RekeyOriginSnapshotFailureDegrades pins F2's
// degrade: when the ORIGIN A's asset snapshot fails on a rekeying rescrape,
// the rescrape still succeeds (snapshot failure never rejects), and the
// rollback restores only the destination's assets — nothing claims origin A
// can be recovered.
func TestRescrapePhase_Rescrape_RekeyOriginSnapshotFailureDegrades(t *testing.T) {
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

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	assert.Equal(t, []string{movieB, movieA}, gen.snapshotIDs,
		"both snapshots are attempted")

	require.NotNil(t, outcome.PosterCacheRollback)
	require.NoError(t, outcome.PosterCacheRollback())
	assert.Equal(t, 1, gen.restores, "only the destination snapshot can be restored")

	// The in-memory restore is independent of the asset snapshots.
	require.NoError(t, outcome.ResultStateRollback())
	assert.Equal(t, movieA, tracker.GetCurrentMovieID(filePath))
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
