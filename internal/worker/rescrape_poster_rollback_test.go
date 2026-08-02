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
	restoreErr  error
	snapshots   int
	restores    int
	generated   int
}

func (g *snapshotStubPosterGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	g.generated++
	return nil
}
func (g *snapshotStubPosterGen) SnapshotPosterAssets(_, _ string) (*poster.AssetsSnapshot, error) {
	g.snapshots++
	if g.snapshotErr != nil {
		return nil, g.snapshotErr
	}
	return &poster.AssetsSnapshot{}, nil
}
func (g *snapshotStubPosterGen) RestorePosterAssets(_ *poster.AssetsSnapshot) error {
	g.restores++
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
