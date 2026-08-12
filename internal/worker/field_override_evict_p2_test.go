package worker

// POSTER-WRITE-HARDENING P2 red suite (D6/R13): an override that changes the
// EFFECTIVE poster source must carry the PATCH path's eviction contract —
// witness BEFORE the commit, evict under the same locked section, clear the
// committed cropped_poster_url so no committed row points at bytes that
// describe the old source.

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// overrideEvictFixture seeds a store row with a manual crop committed against
// the OLD source plus installed pair bytes, and an env so eviction can run.
func overrideEvictFixture(t *testing.T, movieID string, poster PosterStateFixture) *PosterEditor {
	t.Helper()
	store := resultstore.New(1, []string{"/f/ov.mp4"})
	movie := &models.Movie{
		ID: movieID,
		Poster: models.PosterState{
			PosterURL:        poster.posterURL,
			CoverURL:         poster.coverURL,
			CroppedPosterURL: "v1-crops/" + movieID + ".jpg",
			PosterCropBounds: &models.CropBounds{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5},
		},
	}
	store.UpdateFileResult("/f/ov.mp4", &resultstore.MovieResult{
		ResultID:      "res-ov",
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/ov.mp4", MovieID: movieID},
	})
	store.SetProvenance("/f/ov.mp4", &resultstore.ProvenanceData{
		FieldSources: map[string]string{"title": "r18dev"},
		ScraperResults: []*models.ScraperResult{
			{Source: "r18dev", Title: "R18 Title"},
			{Source: "dmm", Title: "DMM Title", PosterURL: "https://new.example/x.jpg", CoverURL: "https://new.example/x-fan.jpg"},
		},
	})
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: poster.fs, tempDir: "/tmp", jobID: "J-OV"})
	return pe
}

type PosterStateFixture struct {
	posterURL string
	coverURL  string
	fs        afero.Fs
}

// seedPair writes the installed pair bytes for movieID inside dir.
func seedPair(t *testing.T, fs afero.Fs, dir, movieID string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, dir+"/"+movieID+"-full.jpg", []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, dir+"/"+movieID+".jpg", []byte("old-crop"), 0o644))
}

func assertPairGone(t *testing.T, fs afero.Fs, dir, movieID string) {
	t.Helper()
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		_, err := fs.Stat(dir + "/" + movieID + suffix)
		assert.Error(t, err, "stale leg %s%s evicted after source change", movieID, suffix)
	}
}

// poster_url override ⇒ effective source changes ⇒ geometry + cropped URL
// cleared, cached pair evicted, witness swept after success.
func TestApplyFieldOverride_PosterURLSourceChangeEvictsAndClears(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-OV"
	seedPair(t, fs, dir, "OV-1")
	pe := overrideEvictFixture(t, "OV-1", PosterStateFixture{posterURL: "https://old.example/p.jpg", fs: fs})

	out, _, err := pe.ApplyFieldOverride(context.Background(), "res-ov", "OV-1", "poster_url", "dmm")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "https://new.example/x.jpg", out.Movie.Poster.PosterURL)
	assert.Empty(t, out.Movie.Poster.CroppedPosterURL, "committed row never points at bytes of the old source")
	assert.Nil(t, out.Movie.Poster.PosterCropBounds, "stale geometry cleared")
	assertPairGone(t, fs, dir, "OV-1")
	entries, derr := afero.ReadDir(fs, dir)
	require.NoError(t, derr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".evict-", "witness swept after successful eviction")
	}
}

// cover_url override while poster_url is EMPTY: the cover IS the effective
// source — same eviction contract.
func TestApplyFieldOverride_CoverURLAsEffectiveSourceEvicts(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-OV"
	seedPair(t, fs, dir, "OV-2")
	pe := overrideEvictFixture(t, "OV-2", PosterStateFixture{coverURL: "https://old.example/c.jpg", fs: fs})

	out, _, err := pe.ApplyFieldOverride(context.Background(), "res-ov", "OV-2", "cover_url", "dmm")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "https://new.example/x-fan.jpg", out.Movie.Poster.CoverURL)
	assert.Empty(t, out.Movie.Poster.CroppedPosterURL)
	assert.Nil(t, out.Movie.Poster.PosterCropBounds)
	assertPairGone(t, fs, dir, "OV-2")
}

// cover_url override UNDER a non-empty poster_url: the effective source did
// NOT change — geometry and bytes stay (a still-valid crop is preserved).
func TestApplyFieldOverride_CoverURLUnderExplicitPosterKeepsCrop(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-OV"
	seedPair(t, fs, dir, "OV-3")
	pe := overrideEvictFixture(t, "OV-3", PosterStateFixture{posterURL: "https://old.example/p.jpg", coverURL: "https://old.example/c.jpg", fs: fs})

	out, _, err := pe.ApplyFieldOverride(context.Background(), "res-ov", "OV-3", "cover_url", "dmm")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "https://new.example/x-fan.jpg", out.Movie.Poster.CoverURL)
	assert.Equal(t, "v1-crops/OV-3.jpg", out.Movie.Poster.CroppedPosterURL, "crop survives fanart-only change")
	assert.NotNil(t, out.Movie.Poster.PosterCropBounds)
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		_, err := fs.Stat(dir + "/OV-3" + suffix)
		assert.NoError(t, err, "bytes remain: effective source unchanged")
	}
}

// Unsafe stored canonical ID: the eviction is refused (no join over an
// unsafe path), the commit itself still lands with cleared crop state, and
// the pair bytes stay put (codex r33 parity with the PATCH path).
func TestApplyFieldOverride_SourceChangeUnsafeIDSkipsEviction(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-OV"
	store := resultstore.New(1, []string{"/f/ov.mp4"})
	store.UpdateFileResult("/f/ov.mp4", &resultstore.MovieResult{
		ResultID: "res-ov",
		Status:   models.JobStatusCompleted,
		Movie: &models.Movie{ID: "../evil", Poster: models.PosterState{
			PosterURL:        "https://old.example/p.jpg",
			CroppedPosterURL: "v1-crops/x.jpg",
		}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/ov.mp4", MovieID: "OV-U"},
	})
	store.SetProvenance("/f/ov.mp4", &resultstore.ProvenanceData{
		ScraperResults: []*models.ScraperResult{{Source: "dmm", PosterURL: "https://new.example/x.jpg"}},
	})
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "J-OV"})

	out, _, err := pe.ApplyFieldOverride(context.Background(), "res-ov", "OV-U", "poster_url", "dmm")
	require.NoError(t, err)
	assert.Empty(t, out.Movie.Poster.CroppedPosterURL, "state-side clearing applies (row stays coherent)")
	_, derr := fs.Stat(dir)
	assert.Error(t, derr, "no eviction machinery touched the filesystem for an unsafe ID")
}

// Empty canonical Movie.ID falls back to the family key for the eviction
// identity (matcher alias naming the installed pair).
func TestApplyFieldOverride_SourceChangeEmptyMovieIDFallsBackToFamilyKey(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/J-OV"
	seedPair(t, fs, dir, "OV-5")
	store := resultstore.New(1, []string{"/f/ov.mp4"})
	store.UpdateFileResult("/f/ov.mp4", &resultstore.MovieResult{
		ResultID:      "res-ov",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "", Poster: models.PosterState{PosterURL: "https://old.example/p.jpg"}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/ov.mp4", MovieID: "OV-5"},
	})
	store.SetProvenance("/f/ov.mp4", &resultstore.ProvenanceData{
		ScraperResults: []*models.ScraperResult{{Source: "dmm", PosterURL: "https://new.example/x.jpg"}},
	})
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "J-OV"})

	_, _, err := pe.ApplyFieldOverride(context.Background(), "res-ov", "OV-5", "poster_url", "dmm")
	require.NoError(t, err)
	assertPairGone(t, fs, dir, "OV-5")
}

// A wedged eviction-witness write aborts the override BEFORE any state or
// byte moves — commit never runs.
func TestApplyFieldOverride_SourceChangeWitnessWriteFailureAborts(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-OV"
	seedPair(t, base, dir, "OV-6")
	store := resultstore.New(1, []string{"/f/ov.mp4"})
	store.UpdateFileResult("/f/ov.mp4", &resultstore.MovieResult{
		ResultID:      "res-ov",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "OV-6", Poster: models.PosterState{PosterURL: "https://old.example/p.jpg", CroppedPosterURL: "v1/x.jpg"}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/ov.mp4", MovieID: "OV-6"},
	})
	prov := &resultstore.ProvenanceData{
		FieldSources:   map[string]string{"title": "r18dev"},
		ScraperResults: []*models.ScraperResult{{Source: "dmm", PosterURL: "https://new.example/x.jpg"}},
	}
	store.SetProvenance("/f/ov.mp4", prov)
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: createWedgeFS{Fs: base, contains: ".evict-"}, tempDir: "/tmp", jobID: "J-OV"})

	_, _, err := pe.ApplyFieldOverride(context.Background(), "res-ov", "OV-6", "poster_url", "dmm")
	require.ErrorContains(t, err, "stale poster eviction witness")
	got, gerr := store.GetMovieResult("/f/ov.mp4")
	require.NoError(t, gerr)
	assert.Equal(t, "https://old.example/p.jpg", got.Movie.Poster.PosterURL, "commit never ran — in-memory state unchanged")
	assert.Equal(t, "r18dev", store.GetProvenance("/f/ov.mp4").FieldSources["title"], "provenance untouched")
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		_, serr := base.Stat(dir + "/OV-6" + suffix)
		assert.NoError(t, serr, "pair intact")
	}
}

// Commit failure sweeps the never-armed witness — a stranded record would
// make the startup reconciler repeatedly evict for a commit that never
// happened.
func TestApplyFieldOverride_SourceChangeCommitFailureSweepsWitness(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/J-OV"
	seedPair(t, base, dir, "OV-7")
	store := resultstore.New(1, []string{"/f/ov.mp4"})
	store.UpdateFileResult("/f/ov.mp4", &resultstore.MovieResult{
		ResultID:      "res-ov",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "OV-7", Poster: models.PosterState{PosterURL: "https://old.example/p.jpg"}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/ov.mp4", MovieID: "OV-7"},
	})
	store.SetProvenance("/f/ov.mp4", &resultstore.ProvenanceData{
		ScraperResults: []*models.ScraperResult{{Source: "dmm", PosterURL: "https://new.example/x.jpg"}},
	})
	pe := newEditorForStore(store)
	committer := NewEditCommitter(failTransactor{err: errWedgeWorker}, newKeyedMutexRegistry(), "J-OV", newKeyedMutexRegistry())
	pe.attachEnv(&posterEditEnv{
		fs: base, tempDir: "/tmp", jobID: "J-OV", committer: committer,
		envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
			return &models.Job{}, nil
		},
	})

	_, _, err := pe.ApplyFieldOverride(context.Background(), "res-ov", "OV-7", "poster_url", "dmm")
	require.ErrorIs(t, err, errWedgeWorker)
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		_, serr := base.Stat(dir + "/OV-7" + suffix)
		assert.NoError(t, serr, "eviction runs only on success — pair intact")
	}
	matches, gerr := afero.Glob(base, dir+"/.evict-*.json")
	require.NoError(t, gerr)
	assert.Empty(t, matches, "witness swept after failed commit")
}

var errWedgeWorker = errWedgeTypeWorker{}

type errWedgeTypeWorker struct{}

func (errWedgeTypeWorker) Error() string { return "tx wedged" }
