package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- fakes shared by the coverage tests below ---

type failAtomicUpdater struct{ err error }

func (f *failAtomicUpdater) UpdateFileResult(string, *resultstore.MovieResult) {}
func (f *failAtomicUpdater) SetProvenance(string, *resultstore.ProvenanceData) {}
func (f *failAtomicUpdater) UpdateMovie(string, *models.Movie) error           { return nil }
func (f *failAtomicUpdater) MarkExcluded(string)                               {}
func (f *failAtomicUpdater) AtomicUpdateFileResult(string, func(*resultstore.MovieResult) (*resultstore.MovieResult, error)) error {
	return f.err
}

func (f *failAtomicUpdater) AtomicUpdateFileResultWithProvenance(string, func(*resultstore.MovieResult, *resultstore.ProvenanceData) (*resultstore.MovieResult, *resultstore.ProvenanceData, error)) error {
	return f.err
}

type failTransactor struct{ err error }

func (t failTransactor) WithEditTx(context.Context, func(database.EditUnit) error) error {
	return t.err
}

type okTransactor struct{ calls int }

func (t *okTransactor) WithEditTx(_ context.Context, fn func(database.EditUnit) error) error {
	t.calls++
	return fn(database.EditUnit{})
}

type noFamilyLookup struct{ resultstore.ResultReadFacade }

func (noFamilyLookup) FindMovieResultForMovieID(string) (*resultstore.MovieResult, error) {
	return nil, nil
}

type seqRenameFailFS struct {
	afero.Fs
	call   int
	failOn map[int]bool
}

func (f *seqRenameFailFS) Rename(oldname, newname string) error {
	f.call++
	if f.failOn[f.call] {
		return errors.New("simulated rename failure")
	}
	return f.Fs.Rename(oldname, newname)
}

type removeFailFS struct{ afero.Fs }

func (removeFailFS) Remove(string) error { return errors.New("simulated remove failure") }

func seedFamilyResult(store resultstore.Store, path, resultID, movieID, contentID string) {
	store.UpdateFileResult(path, &resultstore.MovieResult{
		ResultID:      resultID,
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: movieID, ContentID: contentID},
		FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movieID},
	})
}

func newEditorForStore(store resultstore.Store) *PosterEditor {
	return NewPosterEditor(store, store, nil)
}

// Poster-only commits must not rewrite the matcher alias (codex P3-A).
func TestUpdatePosterCropPreservesMatcherAlias(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-al", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "CANON-9"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "ALIAS-9"},
	})
	store.SetFileMatchInfo("/f/a.mp4", models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "ALIAS-9"})
	pe := NewPosterEditor(store, store, nil)
	require.NoError(t, pe.UpdatePosterCrop("ALIAS-9", "https://img.example/crop.jpg", &models.CropBounds{X: 0, Y: 0, Width: 0.5, Height: 0.5}, false))
	fmi, ok := store.GetFileMatchInfo("/f/a.mp4")
	require.True(t, ok)
	assert.Equal(t, "ALIAS-9", fmi.MovieID, "poster-only commits must not stamp canonical IDs onto aliases")
}

// --- registry / key plumbing ---

func TestPosterEditorLockRegistryLazyInit(t *testing.T) {
	pe := &PosterEditor{}
	reg := pe.lockRegistry()
	require.NotNil(t, reg)
	assert.Same(t, reg, pe.lockRegistry(), "second call returns the memoized registry")
}

func TestIdentityKeysForAddsCanonicalAndContentID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID:      "res-1",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "CANON-9", ContentID: "555"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "ALIAS-9"},
	})
	pe := newEditorForStore(store)

	keys := pe.identityKeysFor("ALIAS-9")
	assert.ElementsMatch(t, []string{"ALIAS-9", "CANON-9", "cid:555"}, keys)

	t.Run("canonical alias equal-fold is not duplicated", func(t *testing.T) {
		keys = pe.identityKeysFor("canon-9")
		assert.ElementsMatch(t, []string{"canon-9", "cid:555"}, keys)
	})

	t.Run("unresolvable family yields the matcher key only", func(t *testing.T) {
		assert.Equal(t, []string{"nope-1"}, pe.identityKeysFor("nope-1"))
	})
}

func TestLockedMovieOpsMovieIDAccessor(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	pe := newEditorForStore(store)
	var got string
	require.NoError(t, pe.WithMovieEditLock("K-1", func(m *LockedMovieOps) error {
		got = m.MovieID()
		return nil
	}))
	assert.Equal(t, "K-1", got)
}

// Movie ID clearing is an identity-change request — rejected up-front
// (codex P1-F): a blank ID would restamp every part's matcher key and drop
// the family out of the result index entirely.
func TestUpdateMovieFamilyRejectsEmptyID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "NE-1", "")
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "NE-1"}
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "", Title: "cleared"}), &conflict)
}

func TestUpdateMovieSingleRejectsEmptyID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "NE-2", "")
	pe := newEditorForStore(store)
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, pe.UpdateMovieSingle(context.Background(), "/f/a.mp4", &models.Movie{ID: "", Title: "cleared"}), &conflict)
}

// --- commit pipeline error arms ---

func TestPublishCandidatesPropagatesAtomicUpdateError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	boom := errors.New("store wedged")
	pe := &PosterEditor{lookup: store, updater: &failAtomicUpdater{err: boom}}
	m := &LockedMovieOps{pe: pe, movieID: "X-1"}
	err := m.publishCandidates(map[string]*resultstore.MovieResult{
		"/f/a.mp4": {Movie: &models.Movie{ID: "X-1"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "X-1"}},
	})
	require.ErrorIs(t, err, boom)
}

func TestPublishCandidatesRewritesRekeyedFileMatchInfo(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "OLD-1", "")
	store.SetFileMatchInfo("/f/a.mp4", models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OLD-1"})
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "OLD-1"}
	cur, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	cand := cur.Clone()
	cand.FileMatchInfo.MovieID = "NEW-9"
	cand.Movie.ID = "NEW-9"
	require.NoError(t, m.publishCandidates(map[string]*resultstore.MovieResult{"/f/a.mp4": cand}))
	fmi, ok := store.GetFileMatchInfo("/f/a.mp4")
	require.True(t, ok)
	assert.Equal(t, "NEW-9", fmi.MovieID, "rekeyed candidates must rewrite the live match-info map")
}

func TestCommitCandidateLegacyPublishErrorSkipsPersist(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-1", "")
	persistCalled := false
	pe := NewPosterEditor(store, &failAtomicUpdater{err: errors.New("boom")}, nil)
	pe.attachEnv(&posterEditEnv{persistFn: func() error {
		persistCalled = true
		return nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-1"}
	err := m.commitCandidate(context.Background(), map[string]*resultstore.MovieResult{
		"/f/a.mp4": {Movie: &models.Movie{ID: "SSNI-1"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "SSNI-1"}},
	}, nil, nil)
	require.Error(t, err)
	assert.False(t, persistCalled, "publish failure must stop before the persist leg")
}

func TestCommitCandidateLegacyPersistFailureIsWarnedNotReturned(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-2", "")
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{persistFn: func() error { return errors.New("disk full") }})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-2"}
	require.NoError(t, m.commitCandidate(context.Background(), map[string]*resultstore.MovieResult{}, nil, nil),
		"post-commit envelope persist failures degrade to a warning (state is committed)")
}

// --- UpdateMovieFamily guards ---

func TestUpdateMovieFamilyRejectsNilMovie(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "X-2"}
	require.ErrorContains(t, m.UpdateMovieFamily(context.Background(), nil), "movie is required")
}

func TestUpdateMovieFamilyEmptyCandidatesIsTypedEmptyError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		Status:        models.JobStatusCompleted,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "M-EMPTY"},
	})
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "M-EMPTY"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "M-EMPTY"})
	require.ErrorIs(t, err, ErrMovieFamilyEmpty)
}

func TestUpdateMovieFamilySkipsIDZeroActressRenames(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-7", "ssni-content-7")
	pe := newEditorForStore(store)
	movie := &models.Movie{
		ID:    "SSNI-7",
		Title: "edited title",
		Actresses: []models.Actress{
			{ID: 0, FirstName: "new", LastName: "girl"},
			{ID: 42, FirstName: "keep", LastName: "name"},
		},
	}
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-7"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), movie))
	final, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "edited title", final.Movie.Title)
	assert.Equal(t, "ssni-content-7", final.Movie.ContentID, "stored content-id must survive an omitted code")
}

func TestUpdateMovieFamilyRejectsUnsafeRekeyID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-8", "")
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-8"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "../evil"})
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestUpdateMovieFamilyClearsStaleCroppedOnSourceChange(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-1",
		Status:   models.JobStatusCompleted,
		Movie: &models.Movie{ID: "SSNI-9", Poster: models.PosterState{
			PosterURL: "https://img.example/a.jpg",
		}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "SSNI-9"},
	})
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-9"}
	movie := &models.Movie{ID: "SSNI-9", Poster: models.PosterState{
		PosterURL:        "https://img.example/b.jpg",
		CroppedPosterURL: "https://img.example/old-crop.jpg",
	}}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), movie))
	assert.Empty(t, movie.Poster.CroppedPosterURL, "crop URL referencing evicted bytes must be cleared with the geometry")
}

func TestUpdateMovieFamilyStalePosterFallbackToMatcherKey(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID:      "res-1",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "KEY-9"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "KEY-9"},
	})
	pe := NewPosterEditor(noFamilyLookup{store}, store, nil)
	m := &LockedMovieOps{pe: pe, movieID: "KEY-9"}
	movie := &models.Movie{ID: "KEY-9", Poster: models.PosterState{PosterURL: "https://img.example/new.jpg"}}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), movie))
}

// --- poster-pair relocation (rekey) ---

func familyRelocationSetup(t *testing.T) (resultstore.Store, afero.Fs, string) {
	t.Helper()
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "SSNI-R1.jpg"), []byte("crop"), 0o644))
	return store, fs, dir
}

func TestUpdateMovieFamilyRelocationSuccessAndNewIDPairEvicted(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	movie := &models.Movie{ID: "SSNI-N9", Poster: models.PosterState{PosterURL: "https://img.example/changed.jpg"}}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), movie))
	_, errOld := fs.Stat(filepath.Join(dir, "SSNI-N9-full.jpg"))
	assert.Error(t, errOld, "relocatable pair is evicted once its source changed (it was stale at the new name)")
}

func TestUpdateMovieFamilyRelocationRejectsExistingTarget(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "SSNI-N9-full.jpg"), []byte("preexisting"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "already exists")
	for _, name := range []string{"SSNI-R1-full.jpg", "SSNI-R1.jpg"} {
		_, statErr := fs.Stat(filepath.Join(dir, name))
		assert.NoError(t, statErr, "originals must remain after a rejected relocation")
	}
}

func TestUpdateMovieFamilyCommitFailureRollsBackRelocation(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	pe := newEditorForStore(store)
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-9", newKeyedMutexRegistry())
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9", committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9", Poster: models.PosterState{PosterURL: "https://img.example/new.jpg"}})
	require.ErrorContains(t, err, "tx wedged")
	for _, name := range []string{"SSNI-R1-full.jpg", "SSNI-R1.jpg"} {
		_, statErr := fs.Stat(filepath.Join(dir, name))
		assert.NoError(t, statErr, "pair must be rolled back to the old identity after a failed commit")
	}
}

func TestUpdateMovieFamilyRelocationRollbackWarnsOnReverseFailure(t *testing.T) {
	store, base, _ := familyRelocationSetup(t)
	fs := &seqRenameFailFS{Fs: base, failOn: map[int]bool{3: true, 4: true}}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "simulated rename failure")
	assert.Equal(t, 4, fs.call, "forward pair (2 calls) plus reverse rollback of the already-moved file")
}

// codex r33 P1: a stored canonical ID carrying traversal components must
// never reach a filesystem join — the save proceeds, the pair simply stays
// at its existing name.
func TestUpdateMovieFamilySkipsRelocationForUnsafeStoredID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID:      "res-1",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "../legacy-evil"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "../legacy-evil"},
	})
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JOB-9", 0o755))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "../legacy-evil"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-SAFE", Title: "t"}))
	got, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "SSNI-SAFE", got.Movie.ID, "the save itself still commits")
	for _, escaped := range []string{"/tmp/posters/legacy-evil-full.jpg", "/tmp/posters/legacy-evil.jpg", "/tmp/legacy-evil-full.jpg", "/tmp/legacy-evil.jpg"} {
		_, statErr := fs.Stat(escaped)
		assert.Error(t, statErr, "unsafe stored ID must never resolve outside the poster dir: %s", escaped)
	}
}

// codex r39 P2: a rekey relocation moves the pair bytes; the stored crop
// URLs must follow or the review UI renders a broken poster after the save.
func TestUpdateMovieFamilyRekeyRewritesCropURLs(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	const oldURL = "/api/v1/temp/posters/JOB-9/SSNI-R1.jpg?v=111"
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-1", Status: models.JobStatusCompleted,
		Movie: &models.Movie{ID: "SSNI-R1", Poster: models.PosterState{
			PosterURL:                "https://img.example/same.jpg",
			CroppedPosterURL:         oldURL,
			OriginalCroppedPosterURL: oldURL,
			PosterCropBounds:         &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
		}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "SSNI-R1"},
	})
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	movie := &models.Movie{ID: "SSNI-N9", Poster: models.PosterState{
		PosterURL:                "https://img.example/same.jpg", // unchanged source: no eviction
		CroppedPosterURL:         oldURL,
		OriginalCroppedPosterURL: oldURL,
		PosterCropBounds:         &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
	}}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), movie))
	assert.Contains(t, movie.Poster.CroppedPosterURL, "/api/v1/temp/posters/JOB-9/SSNI-N9.jpg", "crop URL follows the relocated bytes")
	assert.Contains(t, movie.Poster.OriginalCroppedPosterURL, "SSNI-N9.jpg", "original/reset crop URL follows too")
	assert.NotContains(t, movie.Poster.CroppedPosterURL, "SSNI-R1")
	_, statErr := fs.Stat(filepath.Join(dir, "SSNI-N9.jpg"))
	assert.NoError(t, statErr, "pair moved to the new identity")
}

func TestRewriteTempPosterURL(t *testing.T) {
	assert.Equal(t,
		"/api/v1/temp/posters/J-1/IPX-9.jpg?v=42",
		rewriteTempPosterURL("/api/v1/temp/posters/J-1/OLD-1.jpg?v=42", "J-1", "OLD-1", "IPX-9"))
	// no query string
	assert.Equal(t,
		"/api/v1/temp/posters/J-1/NEW.jpg",
		rewriteTempPosterURL("/api/v1/temp/posters/J-1/OLD.jpg", "J-1", "OLD", "NEW"))
	// non-poster URLs and mismatched segments pass through untouched
	assert.Equal(t, "https://cdn.example/x.jpg",
		rewriteTempPosterURL("https://cdn.example/x.jpg", "J-1", "OLD", "NEW"))
	assert.Equal(t, "/api/v1/temp/posters/J-2/OLD.jpg",
		rewriteTempPosterURL("/api/v1/temp/posters/J-2/OLD.jpg", "J-1", "OLD", "NEW"), "other job's URL untouched")
	assert.Equal(t, "", rewriteTempPosterURL("", "J-1", "OLD", "NEW"))
	// IDs round-trip through PathEscape, matching manager.go's encoding.
	assert.Equal(t,
		"/api/v1/temp/posters/J%201/A%2FB.jpg",
		rewriteTempPosterURL("/api/v1/temp/posters/J%201/C%20D.jpg", "J 1", "C D", "A/B"))
}

// --- codex r43: case-only rekeys + stat-error abort ---

// capVariantSetup relocates under a case-ONLY spelling change.
func capVariantStore(t *testing.T) (resultstore.Store, afero.Fs, string) {
	t.Helper()
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "CAP-1", "cap1-content")
	fs := afero.NewMemMapFs() // case-SENSITIVE by construction
	dir := filepath.Join("/tmp", "posters", "JOB-C1")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "CAP-1-full.jpg"), []byte("full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "CAP-1.jpg"), []byte("crop"), 0o644))
	return store, fs, dir
}

// Case-sensitive fs: a pure-capitalization rekey moves the pair (r43 P2a).
func TestUpdateMovieFamilyCaseOnlyRekeyRelocatesOnCaseSensitiveFS(t *testing.T) {
	store, fs, dir := capVariantStore(t)
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-C1"})
	m := &LockedMovieOps{pe: pe, movieID: "CAP-1"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "cap-1"}))
	_, statErr := fs.Stat(filepath.Join(dir, "cap-1.jpg"))
	assert.NoError(t, statErr, "pair moved to the new-case identity")
	_, oldErr := fs.Stat(filepath.Join(dir, "CAP-1.jpg"))
	assert.Error(t, oldErr, "old-case name released")
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-CAP-1.json"))
	assert.Error(t, wErr, "witness swept after commit")
}

// Case-insensitive fs: both spellings resolve to the same entry — no move.
func TestUpdateMovieFamilyCaseOnlyRekeySkipsOnCaseInsensitiveFS(t *testing.T) {
	store, fs, dir := capVariantStore(t)
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-C1", ciProbe: func(string) bool { return true }})
	m := &LockedMovieOps{pe: pe, movieID: "CAP-1"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "cap-1"}))
	_, statErr := fs.Stat(filepath.Join(dir, "CAP-1.jpg"))
	assert.NoError(t, statErr, "pair untouched — same on-disk entry")
}

// r43 P2b: a transient source-stat error aborts the relocation coherently —
// no commit, no moved bytes, no silent leg drop.
type statFailSuffixFS struct {
	afero.Fs
	suffix string
}

func (f statFailSuffixFS) Stat(name string) (os.FileInfo, error) {
	if strings.HasSuffix(name, f.suffix) {
		return nil, os.ErrPermission
	}
	return f.Fs.Stat(name)
}

func TestUpdateMovieFamilyRekeyAbortsOnSourceStatError(t *testing.T) {
	store, base, dir := familyRelocationSetup(t)
	fs := statFailSuffixFS{Fs: base, suffix: "SSNI-R1-full.jpg"}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "source stat")
	_, statErr := base.Stat(filepath.Join(dir, "SSNI-R1-full.jpg"))
	assert.NoError(t, statErr, "nothing moved")
	got, gErr := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, gErr)
	assert.Equal(t, "SSNI-R1", got.Movie.ID, "state stays on the old identity")
}

// --- stale-pair eviction ---

func TestEvictStalePosterPairUnsafeIDNoop(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/JOB-9", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/sentinelfull.jpg", []byte("keep"), 0o644))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-G"}
	m.evictStalePosterPair("../../sentine", "") // would clean to a path outside JOB-9 if joined naively
	m.evictStalePosterPair("", "")              // bare-name guard
	_, err := fs.Stat("/tmp/posters/sentinelfull.jpg")
	assert.NoError(t, err, "no bytes outside the intended dir are ever removed for unsafe IDs")
}

// codex r48 P2: a padded rekey ID is normalized ONCE — the committed row,
// FileMatchInfo, relocated bytes, and rewritten URLs all carry the trimmed
// spelling.
func TestUpdateMovieFamilyRekeyNormalizesWhitespaceID(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	movie := &models.Movie{ID: "  SSNI-N9  "}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), movie))
	assert.Equal(t, "SSNI-N9", movie.ID, "payload normalized in place")
	_, statErr := fs.Stat(filepath.Join(dir, "SSNI-N9.jpg"))
	assert.NoError(t, statErr, "bytes at the trimmed identity")
	got, err := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "SSNI-N9", got.Movie.ID)
	assert.Equal(t, "SSNI-N9", got.FileMatchInfo.MovieID)
}

func TestIsSafePosterFileID(t *testing.T) {
	for id, want := range map[string]bool{
		"SSNI-1":    true,
		"A.B_C":     true,
		"":          false,
		".":         false,
		"..":        false,
		"../x":      false,
		"a/b":       false,
		"a\\b":      false,
		"..x":       true, // prefix-dots alone are fine; only separator/base tricks are unsafe
		"x/../../y": false,
	} {
		assert.Equalf(t, want, isSafePosterFileID(id), "id=%q", id)
	}
}

// codex cloud P2: when the eviction witness cannot persist BEFORE the commit,
// the save aborts pre-commit — never a durable commit with zero recovery record.

func TestUpdateMovieFamilySourceChangeAbortOnWedgedEvictWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-w", "SRCW-9", "")
	mr, gerr := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, gerr)
	require.NotNil(t, mr.Movie)
	mr.Movie.Poster.PosterURL = "https://cook/old.jpg"
	mr.Movie.Poster.CoverURL = "https://cook/old-cover.jpg"
	store.UpdateFileResult("/f/a.mp4", mr)

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters/J-EW2", 0o755))
	wedged := createWedgeFS{Fs: fs, contains: ".evict-"}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: wedged, tempDir: "/tmp", jobID: "JOB-EW2"})
	err := pe.UpdateMovieFamily(context.Background(), "SRCW-9", "res-w", &models.Movie{ID: "SRCW-9", Poster: models.PosterState{PosterURL: "https://cook/new.jpg"}}, FamilySaveOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stale poster eviction witness")
	// the save aborted BEFORE commitCandidate → family state untouched.
	cur, cErr := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, cErr)
	assert.Equal(t, "https://cook/old.jpg", cur.Movie.Poster.PosterURL, "pre-commit state unchanged by an aborted write")
}

func TestEvictStalePosterPairWarnsOnRemoveFailure(t *testing.T) {
	store, base, dir := familyRelocationSetup(t)
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: removeFailFS{base}, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	m.evictStalePosterPair("SSNI-R1", "")
	_, statErr := base.Stat(filepath.Join(dir, "SSNI-R1.jpg"))
	assert.NoError(t, statErr, "failed removals leave bytes in place")
}

func TestEvictStalePosterPairNoopWithoutEnv(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	m.evictStalePosterPair("SSNI-R1", "")
}

// --- field overrides ---

func overrideTestResult(store resultstore.Store) {
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID:      "res-ovr",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "OVR-1", Maker: "Old Maker", Actresses: []models.Actress{{ID: 0, FirstName: "anon"}}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "OVR-1"},
	})
	store.SetProvenance("/f/a.mp4", &resultstore.ProvenanceData{FieldSources: map[string]string{}})
}

func TestApplyFieldOverrideRejectsIdentityKeys(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	overrideTestResult(store)
	pe := newEditorForStore(store)
	for _, key := range []string{"content_id", "id"} {
		m := &LockedMovieOps{pe: pe, movieID: "OVR-1"}
		_, _, err := m.ApplyFieldOverride(context.Background(), "res-ovr", key, "dmm")
		var conflict *EditAdmissionConflictError
		require.ErrorAs(t, err, &conflict, "identity keys must go through rescrape (%s)", key)
	}
}

func TestApplyFieldOverrideRejectsResultFromForeignFamily(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	overrideTestResult(store)
	pe := newEditorForStore(store)
	// Lock under a family that does not own the result.
	m := &LockedMovieOps{pe: pe, movieID: "OTHER-9"}
	_, _, err := m.ApplyFieldOverride(context.Background(), "res-ovr", "maker", "dmm")
	require.ErrorIs(t, err, ErrFamilyRekeyed)
}

func TestApplyFieldOverrideWrapsCommitFailure(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	movie, prov := overrideFixture()
	movie.Actresses = append(movie.Actresses, models.Actress{ID: 7, FirstName: "grown", LastName: "up"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID:      "res-ovr",
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: movie.ID},
	})
	store.SetProvenance("/f/a.mp4", prov)
	pe := newEditorForStore(store)
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-1", newKeyedMutexRegistry())
	pe.attachEnv(&posterEditEnv{committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: movie.ID}
	_, _, err := m.ApplyFieldOverride(context.Background(), "res-ovr", "maker", "dmm")
	require.ErrorContains(t, err, "persist field override")
}

// --- exclusion pipelines ---

func excludedEditor(t *testing.T, env *posterEditEnv) (resultstore.Store, *PosterEditor) {
	t.Helper()
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "EXC-1", "")
	seedFamilyResult(store, "/f/b.mp4", "res-2", "EXC-1", "")
	pe := newEditorForStore(store)
	if env != nil {
		pe.attachEnv(env)
	}
	return store, pe
}

func TestExcludeFamilyLegacyWithoutLifecycle(t *testing.T) {
	store, pe := excludedEditor(t, nil)
	m := &LockedMovieOps{pe: pe, movieID: "EXC-1"}
	require.NoError(t, m.ExcludeFamily(context.Background()))
	assert.True(t, store.IsAllExcluded(), "both family files must end up excluded")
}

func TestExcludeFamilyLegacyPersistErrorSurfaces(t *testing.T) {
	_, pe := excludedEditor(t, &posterEditEnv{persistFn: func() error { return errors.New("disk full") }})
	m := &LockedMovieOps{pe: pe, movieID: "EXC-1"}
	require.ErrorContains(t, m.ExcludeFamily(context.Background()), "post-exclusion persist")
}

func TestExcludeFamilyCommitterErrorPropagates(t *testing.T) {
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-7", newKeyedMutexRegistry())
	_, pe := excludedEditor(t, &posterEditEnv{committer: committer, envelope: nil})
	// Both committer AND envelope are required for the tx leg; rewire properly.
	pe.attachEnv(&posterEditEnv{committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: "EXC-1"}
	require.ErrorContains(t, m.ExcludeFamily(context.Background()), "tx wedged")
}

func TestExcludeFamilyCommitterLifecyclePersistFailure(t *testing.T) {
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	tx := &okTransactor{}
	committer := NewEditCommitter(tx, newKeyedMutexRegistry(), "JOB-8", newKeyedMutexRegistry())
	_, pe := excludedEditor(t, &posterEditEnv{
		committer: committer,
		envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
			return nil, nil // nil row: envelope upsert leg is gracefully skipped
		},
		lifecycle: lc,
		persistFn: func() error { return errors.New("disk full") },
	})
	m := &LockedMovieOps{pe: pe, movieID: "EXC-1"}
	err := m.ExcludeFamily(context.Background())
	require.ErrorContains(t, err, "post-exclusion lifecycle persist")
	assert.Equal(t, models.JobStatusCancelled, lc.GetJobStatus(), "all-excluded exclusion auto-cancels before the repersist fails")
}

func TestExcludedSnapshotUnionsPriorExclusions(t *testing.T) {
	store, pe := excludedEditor(t, &posterEditEnv{})
	store.MarkExcluded("/f/unrelated.mp4")
	m := &LockedMovieOps{pe: pe, movieID: "EXC-1"}
	got := m.excludedSnapshot([]string{"/f/a.mp4"})
	assert.True(t, got["/f/unrelated.mp4"], "snapshot must preserve unrelated exclusions")
	assert.True(t, got["/f/a.mp4"])
	assert.False(t, got["/f/b.mp4"])
}
