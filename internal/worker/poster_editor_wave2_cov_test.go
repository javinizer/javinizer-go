package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- wrapper-level (pe.*) coverage: CAS guards, carry, casework ---

func TestUpdateMovieFamilyWrapperRejectsStaleSingleRevision(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-cas", "CAS-1", "")
	pe := newEditorForStore(store)
	stale := uint64(42)
	err := pe.UpdateMovieFamily(context.Background(), "CAS-1", "res-cas", &models.Movie{ID: "CAS-1"}, FamilySaveOptions{ExpectedResultRevision: &stale})
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestUpdateMovieFamilyWrapperMultipartCASGuards(t *testing.T) {
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "CAS-2", "")
	seedFamilyResult(store, "/f/b.mp4", "res-b", "CAS-2", "")
	pe := newEditorForStore(store)

	t.Run("multipart map omitting the target is refused outright (codex cloud P2)", func(t *testing.T) {
		bCur, _, ok := store.GetFileResultByResultID("res-b")
		require.True(t, ok)
		// every LISTED part validates — the target still needs its own revision.
		err := pe.UpdateMovieFamily(context.Background(), "CAS-2", "res-a", &models.Movie{ID: "CAS-2"}, FamilySaveOptions{ExpectedResultRevisions: map[string]uint64{"res-b": bCur.Revision}})
		require.ErrorContains(t, err, "omits the target result")
	})
	t.Run("vanished result", func(t *testing.T) {
		aCur, _, ok := store.GetFileResultByResultID("res-a")
		require.True(t, ok)
		err := pe.UpdateMovieFamily(context.Background(), "CAS-2", "res-a", &models.Movie{ID: "CAS-2"}, FamilySaveOptions{ExpectedResultRevisions: map[string]uint64{"res-a": aCur.Revision, "res-ghost": 3}})
		require.ErrorContains(t, err, "vanished for CAS check")
	})
	t.Run("stale part revision", func(t *testing.T) {
		aCur, _, ok := store.GetFileResultByResultID("res-a")
		require.True(t, ok)
		err := pe.UpdateMovieFamily(context.Background(), "CAS-2", "res-a", &models.Movie{ID: "CAS-2"}, FamilySaveOptions{ExpectedResultRevisions: map[string]uint64{"res-a": aCur.Revision, "res-b": 99}})
		require.ErrorContains(t, err, "revision stale")
	})
}

func TestUpdateMovieFamilyWrapperCarriesStoredGeometry(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	stored := &models.Movie{ID: "CAS-3", ContentID: "cas3"}
	stored.Poster.PosterCropBounds = &models.CropBounds{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5}
	stored.Poster.PosterCropSourceFull = true
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-c3", Status: models.JobStatusCompleted, Movie: stored,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "CAS-3"},
	})
	pe := newEditorForStore(store)
	payload := &models.Movie{ID: "CAS-3"}
	require.NoError(t, pe.UpdateMovieFamily(context.Background(), "CAS-3", "res-c3", payload, FamilySaveOptions{CarryCropGeometry: true}))
	require.NotNil(t, payload.Poster.PosterCropBounds, "omitted-bounds carry must restore the stored geometry under the key")
	assert.True(t, payload.Poster.PosterCropSourceFull)
}

// --- identity-change rejection arms ---

func TestUpdateMovieFamilyForeignFamilyRekeyRejected(t *testing.T) {
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/x.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "FA-1", "")
	seedFamilyResult(store, "/f/x.mp4", "res-2", "FA-2", "")
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "FA-1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "FA-2"})
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestUpdateMovieFamilyConflictingContentIDRejected(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "CE-1", "111")
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "CE-1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "CE-1", ContentID: "222"})
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, err, &conflict)
}

// --- single-file save arms ---

func TestUpdateMovieSingleRejectsNilMovieAndMissingResult(t *testing.T) {
	store := resultstore.New(1, []string{"/f/untracked.mp4"})
	pe := newEditorForStore(store)
	err := pe.UpdateMovieSingle(context.Background(), "/nowhere/none.mp4", nil)
	require.ErrorContains(t, err, "movie is required")
}

func TestUpdateMovieSingleMissingResultIsTypedEmptyError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/tracked.mp4"})
	pe := newEditorForStore(store)
	err := pe.UpdateMovieSingle(context.Background(), "/f/tracked.mp4", &models.Movie{ID: "S-1"})
	require.ErrorIs(t, err, ErrMovieFamilyEmpty)
}

func TestUpdateMovieSingleRejectsConflictingContentID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-s1", "S-2", "111")
	pe := newEditorForStore(store)
	err := pe.UpdateMovieSingle(context.Background(), "/f/a.mp4", &models.Movie{ID: "S-2", ContentID: "222"})
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestUpdateMovieSingleSnapshotsCoverOriginalOnChange(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-s3", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "S-3", Poster: models.PosterState{CoverURL: "https://i.example/c1.jpg"}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "S-3"},
	})
	pe := newEditorForStore(store)
	payload := &models.Movie{ID: "S-3", Poster: models.PosterState{CoverURL: "https://i.example/c2.jpg"}}
	require.NoError(t, pe.UpdateMovieSingle(context.Background(), "/f/a.mp4", payload))
	assert.Equal(t, "https://i.example/c1.jpg", payload.Poster.OriginalCoverURL, "changed cover must snapshot the pre-edit URL as original")
}

// --- include-exclusion wrapper + remaining ExcludeFamily arms ---

func TestExcludeMovieFamilyWrapperEmptyFamily(t *testing.T) {
	store := resultstore.New(0, nil)
	pe := newEditorForStore(store)
	err := pe.ExcludeMovieFamily(context.Background(), "GHOST-1")
	require.ErrorIs(t, err, ErrMovieFamilyEmpty)
}

func TestExcludeFamilyCommitterWithoutLifecycleCancelsNothing(t *testing.T) {
	committer := NewEditCommitter(&okTransactor{}, newKeyedMutexRegistry(), "JOB-55", newKeyedMutexRegistry())
	store, pe := excludedEditor(t, &posterEditEnv{
		committer: committer,
		envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
			return nil, nil
		},
	})
	m := &LockedMovieOps{pe: pe, movieID: "EXC-1"}
	require.NoError(t, m.ExcludeFamily(context.Background()))
	assert.True(t, store.IsAllExcluded())
}

// --- committer-leg publish error (post-tx) ---

func TestCommitCandidateCommitterLegPublishErrorSurfaces(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "CP-1", "")
	committer := NewEditCommitter(&okTransactor{}, newKeyedMutexRegistry(), "JOB-CP", newKeyedMutexRegistry())
	pe := NewPosterEditor(store, &failAtomicUpdater{err: errors.New("publish wedged")}, nil)
	pe.attachEnv(&posterEditEnv{committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return nil, nil
	}})
	err := pe.UpdatePosterCrop("CP-1", "https://img.example/crop.jpg", &models.CropBounds{X: 0, Y: 0, Width: 0.5, Height: 0.5}, false)
	require.ErrorContains(t, err, "publish wedged")
}

// --- legacy renames leg with repos wired ---

func TestLegacyRenamesPersistThroughActressRepo(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "RN-1", "")
	movieRepo := mocks.NewMockMovieRepositoryInterface(t)
	actressRepo := mocks.NewMockActressRepositoryInterface(t)
	actressRepo.EXPECT().FindByID(context.Background(), uint(7)).Return(&models.Actress{ID: 7, FirstName: "old", LastName: "name"}, nil)
	actressRepo.EXPECT().RenameNameFields(context.Background(), uint(7), "new", "name", "").Return(nil)
	movieRepo.EXPECT().Upsert(context.Background(), &models.Movie{ID: "RN-1", Actresses: []models.Actress{{ID: 7, FirstName: "new", LastName: "name"}}}).Return(nil, nil)
	pe := NewPosterEditor(store, store, movieRepo)
	pe.attachEnv(&posterEditEnv{actressRepo: actressRepo})
	m := &LockedMovieOps{pe: pe, movieID: "RN-1"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "RN-1", Actresses: []models.Actress{{ID: 7, FirstName: "new", LastName: "name"}}}))
}

// --- relocation tails ---

func TestUpdateMovieFamilyRelocationSkipsMissingFullSuffix(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "RL-1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-RL")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RL-1.jpg"), []byte("crop"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-RL"})
	m := &LockedMovieOps{pe: pe, movieID: "RL-1"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "RL-9"}))
	_, err := fs.Stat(filepath.Join(dir, "RL-9.jpg"))
	assert.NoError(t, err, "present .jpg relocates even when -full.jpg is absent")
}

func TestUpdateMovieFamilyCommitRollbackWarnsWhenReverseFails(t *testing.T) {
	store, base, _ := familyRelocationSetup(t)
	fs := &seqRenameFailFS{Fs: base, failOn: map[int]bool{4: true}}
	store2Unused := store
	_ = store2Unused
	pe := newEditorForStore(store)
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-9", newKeyedMutexRegistry())
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9", committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "tx wedged")
	assert.Equal(t, 5, fs.call, "forward pair, then two reverse rollback attempts (first failed, second ok)")
}
