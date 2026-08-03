package worker

import (
	"context"
	"errors"
	"image/color"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aliasCacheFixture builds the Codex P2-C scenario on a CASE-SENSITIVE
// in-memory filesystem: a multipart-ish family where the rescraped file and
// its sibling are stored under case-VARIANT raw movie IDs (CASEVS-001 vs
// casevs-001). Both variants have distinct pre-existing cache files under
// their raw keys (folded family, but distinct files on this fs), and the
// server serves the refreshed image the rescrape will generate from.
type aliasCacheFixture struct {
	fs        afero.Fs
	pm        *poster.PosterManager
	tracker   resultstore.Store
	inputs    rescrapePhaseInputs
	jobID     models.JobID
	cacheDir  string
	newPoster string
}

func newAliasCacheFixture(t *testing.T) *aliasCacheFixture {
	t.Helper()
	const movieID = "CASEVS-001"
	const variantID = "casevs-001"
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + variantID + "-cd2.mp4"
	jobID := models.NewJobID()

	newPoster := "/new.jpg"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(encodeTestJPEG(t, 220, 330, color.RGBA{B: 0xaa, A: 0xff}))
	}))
	t.Cleanup(srv.Close)

	fs := afero.NewMemMapFs()
	pm := poster.NewPosterManager(fs, "/tmp", srv.Client()).WithSSRFCheck(func(_ string) error { return nil })
	gen := poster.NewScrapePosterGenerator(pm, "", "")

	cacheDir := filepath.Join("/tmp", "posters", jobID.String())
	// Pre-existing STALE cache under BOTH raw keys.
	for _, id := range []string{movieID, variantID} {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(cacheDir, id+"-full.jpg"), []byte("stale-"+id), 0o644))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(cacheDir, id+".jpg"), []byte("stale-preview-"+id), 0o644))
	}

	tracker := resultstore.New(2, []string{fileCD1, fileCD2})
	tracker.UpdateFileResult(fileCD1, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileCD1, MovieID: movieID},
		Movie: &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{
			PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
		}},
		Status: models.JobStatusCompleted,
	})
	tracker.UpdateFileResult(fileCD2, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fileCD2, MovieID: variantID},
		Movie: &models.Movie{ID: variantID, Title: "Old Sibling", Poster: models.PosterState{
			PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
		}},
		Status: models.JobStatusCompleted,
	})

	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: srv.URL + newPoster}},
	}}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
	}
	return &aliasCacheFixture{fs: fs, pm: pm, tracker: tracker, inputs: inputs, jobID: jobID, cacheDir: cacheDir, newPoster: newPoster}
}

// TestRescrapePhase_CaseVariantSiblingCacheAliasRefreshed pins Codex P2-C:
// the folded fan-out mirrors refreshed poster STATE onto the case-variant
// sibling, but on a case-sensitive filesystem the sibling's RAW-keyed cache
// ({casevs-001}-full.jpg / {casevs-001}.jpg) is a DISTINCT file that
// GeneratePoster (keyed on the rescraped movie's raw ID) never touched — the
// crop modal resolving the sibling's raw movie_id would measure the STALE
// alias. The mirror must copy the refreshed assets onto the variant's raw
// key too (the folded poster-source lock is already held for both variants).
func TestRescrapePhase_CaseVariantSiblingCacheAliasRefreshed(t *testing.T) {
	fx := newAliasCacheFixture(t)
	const movieID = "CASEVS-001"
	const variantID = "casevs-001"
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + variantID + "-cd2.mp4"

	oldRev, err := fx.pm.FullSourceRevision(fx.jobID.String(), variantID)
	require.NoError(t, err)

	res, err := NewRescrapePhase().Rescrape(context.Background(), fx.inputs,
		RescrapeCmd{MovieID: movieID, FilePath: fileCD1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, models.RescrapeStatusSuccess, res.Status, "res: %+v", res)

	// The refreshed assets under the rescraped movie's raw key...
	freshFull, err := afero.ReadFile(fx.fs, filepath.Join(fx.cacheDir, movieID+"-full.jpg"))
	require.NoError(t, err)
	assert.NotEqual(t, "stale-"+movieID, string(freshFull))
	freshPreview, err := afero.ReadFile(fx.fs, filepath.Join(fx.cacheDir, movieID+".jpg"))
	require.NoError(t, err)

	// ...land on the case-variant sibling's raw key too (pre-fix these
	// were the stale bytes; the crop modal resolves this raw key).
	aliasFull, err := afero.ReadFile(fx.fs, filepath.Join(fx.cacheDir, variantID+"-full.jpg"))
	require.NoError(t, err)
	assert.Equal(t, freshFull, aliasFull,
		"the case-variant sibling's raw-keyed -full.jpg must track the refreshed cache")
	aliasPreview, err := afero.ReadFile(fx.fs, filepath.Join(fx.cacheDir, variantID+".jpg"))
	require.NoError(t, err)
	assert.Equal(t, freshPreview, aliasPreview,
		"the case-variant sibling's raw-keyed preview must track the refreshed cache")

	newRev, err := fx.pm.FullSourceRevision(fx.jobID.String(), variantID)
	require.NoError(t, err)
	assert.NotEqual(t, oldRev, newRev, "a crop lookup under the variant's raw ID must see the refresh")

	// The state mirror itself still clones only the poster group.
	sibling, err := fx.tracker.GetMovieResult(fileCD2)
	require.NoError(t, err)
	require.NotNil(t, sibling.Movie)
	assert.Equal(t, variantID, sibling.Movie.ID, "the sibling keeps its own case-variant identity")
	assert.Contains(t, sibling.Movie.Poster.PosterURL, fx.newPoster)

	assertPosterSourceLockFree(t, fx.jobID.String(), movieID)
	assertPosterSourceLockFree(t, fx.jobID.String(), variantID)
}

// TestRescrapePhase_PersistFailureRestoresCaseVariantAliasCache: the
// alias-cache copy rides into the persist-failure rollback — the restored
// sibling STATE must never reference an image the sibling's raw key no
// longer holds.
func TestRescrapePhase_PersistFailureRestoresCaseVariantAliasCache(t *testing.T) {
	fx := newAliasCacheFixture(t)
	const movieID = "CASEVS-001"
	const variantID = "casevs-001"
	fileCD1 := "/source/" + movieID + "-cd1.mp4"
	fileCD2 := "/source/" + variantID + "-cd2.mp4"
	fx.inputs.PersistEnvelope = func() error { return errors.New("persist down") }

	res, err := NewRescrapePhase().Rescrape(context.Background(), fx.inputs,
		RescrapeCmd{MovieID: movieID, FilePath: fileCD1})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.PersistErr, "the persist failure surfaces on the outcome: %+v", res)

	aliasFull, err := afero.ReadFile(fx.fs, filepath.Join(fx.cacheDir, variantID+"-full.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "stale-"+variantID, string(aliasFull),
		"the alias cache restore rolls the variant's raw key back alongside the cache and the state")
	aliasPreview, err := afero.ReadFile(fx.fs, filepath.Join(fx.cacheDir, variantID+".jpg"))
	require.NoError(t, err)
	assert.Equal(t, "stale-preview-"+variantID, string(aliasPreview))

	sibling, err := fx.tracker.GetMovieResult(fileCD2)
	require.NoError(t, err)
	assert.Equal(t, "https://old.example/poster.jpg", sibling.Movie.Poster.PosterURL,
		"the sibling state rollback restores the pre-mirror poster source")

	assertPosterSourceLockFree(t, fx.jobID.String(), movieID)
	assertPosterSourceLockFree(t, fx.jobID.String(), variantID)
}

// stubAliasCopierGen is a PosterGenerator stub with the snapshot, copy, and
// restore capabilities, so the alias-failure and rollback-failure legs can
// be driven without a filesystem. snapFailID / copyFailID target one raw
// movie ID; restoreErr fails the rollback restore.
type stubAliasCopierGen struct {
	stubOverridePosterGen
	snapFailID string
	copyFailID string
	snapErr    error
	copyErr    error
	restoreErr error
	copyFrom   string
	copyTo     string
	restores   int
}

func (s *stubAliasCopierGen) SnapshotPosterAssets(_, movieID string) (*poster.AssetsSnapshot, error) {
	if movieID == s.snapFailID {
		return nil, s.snapErr
	}
	return &poster.AssetsSnapshot{}, nil
}

func (s *stubAliasCopierGen) RestorePosterAssets(_ *poster.AssetsSnapshot) error {
	s.restores++
	return s.restoreErr
}

func (s *stubAliasCopierGen) CopyPosterAssets(_, fromMovieID, toMovieID string) error {
	s.copyFrom = fromMovieID
	s.copyTo = toMovieID
	if toMovieID == s.copyFailID {
		return s.copyErr
	}
	return nil
}

// aliasCaseStore wraps a Store to inject failure into ONE
// AtomicUpdateFileResult call — used to prove the revert-failure leg logs
// and continues instead of panicking or corrupting the family.
type aliasCaseStore struct {
	resultstore.Store
	sibPath    string
	failOnCall int
	callCount  int
}

func (s *aliasCaseStore) AtomicUpdateFileResult(filePath string, fn func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	if filePath == s.sibPath {
		s.callCount++
		if s.callCount == s.failOnCall {
			return errors.New("injected sibling update failure")
		}
	}
	return s.Store.AtomicUpdateFileResult(filePath, fn)
}

// TestRescrapePhase_CaseVariantAliasCopyFailureRevertsMirror: when the
// variant's raw-keyed cache CANNOT converge (copy fails), the state mirror
// is REVERTED so the sibling keeps old state alongside its old cache —
// never referencing a refreshed image its raw key does not hold. A
// snapshot failure degrades to the same revert.
func TestRescrapePhase_CaseVariantAliasCopyFailureRevertsMirror(t *testing.T) {
	run := func(t *testing.T, gen *stubAliasCopierGen) resultstore.Store {
		const movieID = "ALFAIL-001"
		const variantID = "alfail-001"
		fileCD1 := "/source/" + movieID + "-cd1.mp4"
		fileCD2 := "/source/" + variantID + "-cd2.mp4"
		jobID := models.NewJobID()

		tracker := resultstore.New(2, []string{fileCD1, fileCD2})
		tracker.UpdateFileResult(fileCD1, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fileCD1, MovieID: movieID},
			Movie:         &models.Movie{ID: movieID, Title: "Old", Poster: models.PosterState{PosterURL: "https://old.example/p.jpg"}},
			Status:        models.JobStatusCompleted,
		})
		tracker.UpdateFileResult(fileCD2, &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: fileCD2, MovieID: variantID},
			Movie:         &models.Movie{ID: variantID, Title: "Sibling", Poster: models.PosterState{PosterURL: "https://old.example/p.jpg"}},
			Status:        models.JobStatusCompleted,
		})
		wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
			Movie: &models.Movie{ID: movieID, Title: "Refreshed", Poster: models.PosterState{PosterURL: "https://new.example/p.jpg"}},
		}}
		gen.stampCroppedURL = "/api/v1/temp/posters/" + jobID.String() + "/" + movieID + ".jpg?v=1"
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
		return tracker
	}

	t.Run("copy failure", func(t *testing.T) {
		gen := &stubAliasCopierGen{copyFailID: "alfail-001", copyErr: errors.New("copy blew up")}
		tracker := run(t, gen)
		sibling, err := tracker.GetMovieResult("/source/alfail-001-cd2.mp4")
		require.NoError(t, err)
		assert.Equal(t, "https://old.example/p.jpg", sibling.Movie.Poster.PosterURL,
			"a failed alias copy reverts the state mirror — old state stays with the old cache")
	})

	t.Run("snapshot failure", func(t *testing.T) {
		gen := &stubAliasCopierGen{snapFailID: "alfail-001", snapErr: errors.New("snapshot blew up")}
		tracker := run(t, gen)
		sibling, err := tracker.GetMovieResult("/source/alfail-001-cd2.mp4")
		require.NoError(t, err)
		assert.Equal(t, "https://old.example/p.jpg", sibling.Movie.Poster.PosterURL,
			"a failed pre-copy snapshot reverts the state mirror too")
		assert.Empty(t, gen.copyFrom, "the copy never runs when the snapshot failed")
	})
}

// aliasEdgeFixture wires a case-variant family around a stub generator with
// the alias capabilities, letting subtests inject per-sibling update
// failures and persist failures without a filesystem.
type aliasEdgeFixture struct {
	jobID   models.JobID
	fileCD1 string
	fileCD2 string
}

func newAliasEdgeFixture(t *testing.T) *aliasEdgeFixture {
	t.Helper()
	return &aliasEdgeFixture{
		jobID:   models.NewJobID(),
		fileCD1: "/source/ALEDGE-001-cd1.mp4",
		fileCD2: "/source/aledge-001-cd2.mp4",
	}
}

func (fx *aliasEdgeFixture) store() resultstore.Store {
	tracker := resultstore.New(2, []string{fx.fileCD1, fx.fileCD2})
	tracker.UpdateFileResult(fx.fileCD1, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fx.fileCD1, MovieID: "ALEDGE-001"},
		Movie:         &models.Movie{ID: "ALEDGE-001", Title: "Old", Poster: models.PosterState{PosterURL: "https://old.example/p.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	tracker.UpdateFileResult(fx.fileCD2, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: fx.fileCD2, MovieID: "aledge-001"},
		Movie:         &models.Movie{ID: "aledge-001", Title: "Sibling", Poster: models.PosterState{PosterURL: "https://old.example/p.jpg"}},
		Status:        models.JobStatusCompleted,
	})
	return tracker
}

func (fx *aliasEdgeFixture) rescrape(t *testing.T, gen *stubAliasCopierGen, store resultstore.Store, persistErr error) *RescrapeResult {
	t.Helper()
	gen.stampCroppedURL = "/api/v1/temp/posters/" + fx.jobID.String() + "/ALEDGE-001.jpg?v=2"
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "ALEDGE-001", Title: "Refreshed", Poster: models.PosterState{PosterURL: "https://new.example/p.jpg"}},
	}}
	inputs := rescrapePhaseInputs{
		JobID:     fx.jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: store,
		Finder:    store,
		Lifecycle: &stubLifecycle{},
	}
	if persistErr != nil {
		inputs.PersistEnvelope = func() error { return persistErr }
	}
	res, err := NewRescrapePhase().Rescrape(context.Background(), inputs,
		RescrapeCmd{MovieID: "ALEDGE-001", FilePath: fx.fileCD1})
	require.NoError(t, err)
	require.NotNil(t, res)
	return res
}

// TestRescrapePhase_AliasMirrorRevertFailureTolerated: when the alias-cache
// copy fails AND the revert itself errors, the mirror stays (best effort —
// logged) and the rescrape still succeeds for the rescraped file.
func TestRescrapePhase_AliasMirrorRevertFailureTolerated(t *testing.T) {
	fx := newAliasEdgeFixture(t)
	gen := &stubAliasCopierGen{copyFailID: "aledge-001", copyErr: errors.New("copy blew up")}
	store := &aliasCaseStore{Store: fx.store(), sibPath: fx.fileCD2, failOnCall: 2} // 1st = mirror, 2nd = revert

	res := fx.rescrape(t, gen, store, nil)
	require.Equal(t, models.RescrapeStatusSuccess, res.Status, "res: %+v", res)
	sibling, err := store.GetMovieResult(fx.fileCD2)
	require.NoError(t, err)
	assert.Equal(t, "https://new.example/p.jpg", sibling.Movie.Poster.PosterURL,
		"a failed revert leaves the mirrored state in place (best effort, logged)")
}

// TestRescrapePhase_AliasCacheRollbackFailureSurfaces: when the envelope
// persist fails and the sibling's alias-cache restore ALSO fails, the
// failure rides along on the surfaced persist error.
func TestRescrapePhase_AliasCacheRollbackFailureSurfaces(t *testing.T) {
	fx := newAliasEdgeFixture(t)
	gen := &stubAliasCopierGen{restoreErr: errors.New("restore blew up")}
	store := fx.store()

	res := fx.rescrape(t, gen, store, errors.New("persist down"))
	require.NotNil(t, res.PersistErr, "the persist failure surfaces: %+v", res)
	assert.Contains(t, res.PersistErr.Error(), "sibling cache rollback failed", "the alias-cache restore failure rides along: %v", res.PersistErr)
}
