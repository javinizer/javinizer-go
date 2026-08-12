package worker

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// POSTER-WRITE-HARDENING P2 (D6): rescrape downloads the poster on a STAGED
// identity outside the family lock; the lock window carries only the fs-only
// promote + state commit.

type rescLockOrderTracker struct {
	mu          sync.Mutex
	lockHeld    bool
	events      []string
	lockAtStage bool
	lockAtPromo bool
}

func (t *rescLockOrderTracker) acquire(ids ...string) func() {
	t.mu.Lock()
	t.lockHeld = true
	t.events = append(t.events, "lock")
	t.mu.Unlock()
	return func() {
		t.mu.Lock()
		t.lockHeld = false
		t.events = append(t.events, "unlock")
		t.mu.Unlock()
	}
}

func (t *rescLockOrderTracker) note(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, name)
	if name == "stage" {
		t.lockAtStage = t.lockHeld
	}
	if name == "promote" {
		t.lockAtPromo = t.lockHeld
	}
}

type stagingTrackerGen struct {
	tracks    *rescLockOrderTracker
	stageInfo *poster.StagedPoster
	stageErr  error
	stageNil  bool
	commitErr error
	discarded *int
}

func (g *stagingTrackerGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	return errors.New("legacy in-lock path must not run for a staging generator")
}

func (g *stagingTrackerGen) StagePoster(_ context.Context, _ string, movie *models.Movie) (*poster.StagedPoster, error) {
	if g.tracks != nil {
		g.tracks.note("stage")
	}
	if g.stageErr != nil {
		return nil, g.stageErr
	}
	if g.stageNil {
		return nil, nil
	}
	if g.stageInfo != nil {
		return g.stageInfo, nil
	}
	return poster.NewStagedPosterHandleForTest("", movie.ID+".stage-x", movie.ID, ""), nil
}

func (g *stagingTrackerGen) CommitStagedPoster(_ *models.Movie, _ *poster.StagedPoster) error {
	if g.tracks != nil {
		g.tracks.note("promote")
	}
	return g.commitErr
}

func (g *stagingTrackerGen) DiscardStaged(_ *poster.StagedPoster) {
	if g.discarded != nil {
		*g.discarded++
	}
}

func TestRescrape_StagedPosterDownloadOutsideLockPromoteInside(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	require.NoError(t, fs.MkdirAll(filepath.Join("/tmp", "posters", jobID.String()), 0o755))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-1", "LOCK-9", "")
	tracks := &rescLockOrderTracker{}
	gen := &stagingTrackerGen{tracks: tracks}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "LOCK-9"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen:  gen,
		EditLockFn: tracks.acquire,
		Fs:         fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "LOCK-9", FilePath: "f1.mp4"})
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.False(t, tracks.lockAtStage, "stage (network) ran OUTSIDE the edit lock")
	assert.True(t, tracks.lockAtPromo, "promote (fs-only) ran INSIDE the edit lock")
	stageIdx := slices.Index(tracks.events, "stage")
	promoIdx := slices.Index(tracks.events, "promote")
	require.GreaterOrEqual(t, stageIdx, 0)
	require.GreaterOrEqual(t, promoIdx, 0)
	assert.Less(t, stageIdx, promoIdx, "stage strictly before promote")
}

// POSTER-WRITE-HARDENING P2: a gated commit failure mid-rescrape (a review
// edit landing between scrape and commit) leaves the world exactly as the
// editor put it — the op's promoted bytes are rewound to the parked
// pre-promotion pair, the live row and provenance stay as the edit left
// them, and nothing of the stale rescrape overlays the family.
type casStagingGen struct {
	t     *testing.T
	bump  func()
	dir   string
	movie string
	fs    afero.Fs
}

func (g *casStagingGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	return errors.New("legacy in-lock path must not run")
}

func (g *casStagingGen) StagePoster(_ context.Context, _ string, movie *models.Movie) (*poster.StagedPoster, error) {
	g.bump() // the mid-op review edit lands here, before the commit
	return poster.NewStagedPosterHandleForTest("", movie.ID+".stage-cas", movie.ID, ""), nil
}

func (g *casStagingGen) CommitStagedPoster(_ *models.Movie, _ *poster.StagedPoster) error {
	// Simulate the promote materializing this op's bytes at canonical names.
	if err := afero.WriteFile(g.fs, filepath.Join(g.dir, g.movie+"-full.jpg"), []byte("new-full"), 0o644); err != nil {
		return err
	}
	return afero.WriteFile(g.fs, filepath.Join(g.dir, g.movie+".jpg"), []byte("new-crop"), 0o644)
}

func (g *casStagingGen) DiscardStaged(_ *poster.StagedPoster) {}

func TestRescrape_AssetPromotionAndProvenanceAtomic_RestoredOnCommitFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "CAS-1-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "CAS-1.jpg"), []byte("old-crop"), 0o644))

	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-cas", "CAS-1", "")
	store.SetProvenance("f1.mp4", &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "r18dev"}})
	before, berr := store.GetMovieResult("f1.mp4")
	require.NoError(t, berr)
	beforeRev := before.Revision

	gen := &casStagingGen{
		t: t, dir: dir, movie: "CAS-1", fs: fs,
		bump: func() {
			// Mid-op concurrent review edit: advances revision AND provenance.
			err := store.AtomicUpdateFileResultWithProvenance("f1.mp4", func(cur *resultstore.MovieResult, _ *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error) {
				cur.Movie.Title = "concurrent review edit"
				return cur, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "user"}}, nil
			})
			if err != nil {
				t.Errorf("concurrent edit injection failed: %v", err)
			}
		},
	}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "CAS-1", Title: "rescrape-stale"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen:  gen,
		EditLockFn: func(ids ...string) func() { return func() {} },
		Fs:         fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "CAS-1", FilePath: "f1.mp4"})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, models.RescrapeStatusConflict, res.Status, "the bumped revision gates the commit")

	// Assets: rewound to the pre-promotion bytes (not the promoted op bytes).
	full, ferr := afero.ReadFile(fs, filepath.Join(dir, "CAS-1-full.jpg"))
	require.NoError(t, ferr)
	assert.Equal(t, "old-full", string(full))
	crop, cerr := afero.ReadFile(fs, filepath.Join(dir, "CAS-1.jpg"))
	require.NoError(t, cerr)
	assert.Equal(t, "old-crop", string(crop))

	// State: the concurrent edit's row is untouched (revision advanced exactly
	// once by the injected edit — the rescrape's stale payload never lands).
	after, aerr := store.GetMovieResult("f1.mp4")
	require.NoError(t, aerr)
	assert.Equal(t, beforeRev+1, after.Revision, "only the review edit advanced the revision")
	require.NotNil(t, after.Movie)
	assert.Equal(t, "concurrent review edit", after.Movie.Title)

	// Provenance: untouched by the failed rescrape — the edit's attribution.
	prov := store.GetProvenance("f1.mp4")
	require.NotNil(t, prov)
	assert.Equal(t, "user", prov.FieldSources["title"])
}

// Staging download failure mid-op: the rescrape still commits, with the
// poster error recorded on the result — identical surface to the legacy
// in-lock generation error.
func TestRescrape_StageFailureSurfacesAsPosterError(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	require.NoError(t, fs.MkdirAll(filepath.Join("/tmp", "posters", jobID.String()), 0o755))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-stg", "STG-1", "")
	boom := errors.New("simulated stage wedge")
	gen := &stagingTrackerGen{stageErr: boom}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "STG-1"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen: gen,
		Fs:        fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "STG-1", FilePath: "f1.mp4"})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, models.RescrapeStatusSuccess, res.Status, "poster stage failure is non-fatal")
	committed, cerr := store.GetMovieResult("f1.mp4")
	require.NoError(t, cerr)
	require.NotNil(t, committed.PosterError, "staging error recorded on the committed result")
	assert.Contains(t, *committed.PosterError, boom.Error())
	assert.True(t, committed.PosterGenerated)
}

// Staging nothing (nil handle, nil error): the op commits without any poster
// state mutation — the staged-nil arm.
func TestRescrape_StagedNilNoPosterState(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	require.NoError(t, fs.MkdirAll(filepath.Join("/tmp", "posters", jobID.String()), 0o755))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-stg2", "STG-2", "")
	gen := &stagingTrackerGen{stageNil: true} // stage resolves to nothing
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "STG-2"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen: gen,
		Fs:        fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "STG-2", FilePath: "f1.mp4"})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusSuccess, res.Status)
	committed, cerr := store.GetMovieResult("f1.mp4")
	require.NoError(t, cerr)
	assert.False(t, committed.PosterGenerated, "no poster attempt recorded for a nil stage")
	assert.Nil(t, committed.PosterError)
}

// A promote/crop witness fencing this family declines the rescrape BEFORE
// the promote and DISCARDS the staged bytes (no unique-name litter).
func TestRescrape_StagedBytesDiscardedWhenWitnessFences(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "DSC-1.jpg"), []byte("old"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-DSC-1.json"), []byte("{}"), 0o644))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-dsc", "DSC-1", "")
	discarded := 0
	gen := &stagingTrackerGen{discarded: &discarded}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "DSC-1"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen: gen,
		Fs:        fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	_, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "DSC-1", FilePath: "f1.mp4"})
	require.Error(t, err, "outstanding witness fences the rescrape")
	var cfe *EditAdmissionConflictError
	assert.ErrorAs(t, err, &cfe)
	assert.Equal(t, 1, discarded, "staged bytes discarded on decline")
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "DSC-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "old", string(got), "canonical bytes untouched by the fenced op")
}

// Legacy (non-staging) generator failing in-lock: same error surface as the
// staged path — PosterError recorded, op commits Success.
type legacyErrGen struct{ err error }

func (g legacyErrGen) GeneratePoster(_ context.Context, _ string, _ *models.Movie) error {
	return g.err
}

func TestRescrape_LegacyGeneratorErrorRecorded(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	require.NoError(t, fs.MkdirAll(filepath.Join("/tmp", "posters", jobID.String()), 0o755))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-leg", "LEG-1", "")
	boom := errors.New("legacy poster wedge")
	inputs := rescrapePhaseInputs{
		WF:        &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "LEG-1"}, Status: scrape.StatusCompleted}},
		ResultMap: store, Finder: store, JobID: jobID,
		PosterGen: legacyErrGen{err: boom},
		Fs:        fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "LEG-1", FilePath: "f1.mp4"})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusSuccess, res.Status)
	committed, cerr := store.GetMovieResult("f1.mp4")
	require.NoError(t, cerr)
	require.NotNil(t, committed.PosterError)
	assert.Contains(t, *committed.PosterError, boom.Error())
}

// The in-lock staged promote failing records PosterError (still Success) —
// the last staged switch arm.
func TestRescrape_StagedCommitFailureRecorded(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	require.NoError(t, fs.MkdirAll(filepath.Join("/tmp", "posters", jobID.String()), 0o755))
	store := resultstore.New(1, []string{"f1.mp4"})
	seedFamilyResult(store, "f1.mp4", "res-cf", "CF-9", "")
	boom := errors.New("promote wedge")
	discarded := 0
	gen := &stagingTrackerGen{commitErr: boom, discarded: &discarded}
	inputs := rescrapePhaseInputs{
		WF:        &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "CF-9"}, Status: scrape.StatusCompleted}},
		ResultMap: store, Finder: store, JobID: jobID,
		PosterGen: gen,
		Fs:        fs, TempDir: "/tmp",
	}
	phase := NewRescrapePhase()
	res, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "CF-9", FilePath: "f1.mp4"})
	require.NoError(t, err)
	assert.Equal(t, models.RescrapeStatusSuccess, res.Status)
	committed, cerr := store.GetMovieResult("f1.mp4")
	require.NoError(t, cerr)
	require.NotNil(t, committed.PosterError)
	assert.Contains(t, *committed.PosterError, boom.Error())
	assert.True(t, committed.PosterGenerated)
	assert.Equal(t, 1, discarded, "failed promote discards the staged residue")
}
