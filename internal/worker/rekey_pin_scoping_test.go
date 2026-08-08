package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The destination-scan wedge inside relocation hard-errors (fail closed).
func TestRekeyDestinationScanWedgeFailsClosed(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	mem := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-DW")
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, ".rekey-x.json"), []byte("{\"old_id\":\"UNREL\",\"new_id\":\"UNREL2\"}"), 0o644))
	fs := &openFailAfterNFS{Fs: mem, suffix: "/tmp/posters/JOB-DW", allow: 2} // fence's rekey+crop scans succeed; destination scan wedges
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-DW"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination check", "destination scan wedge rejects the rekey")
}

// audit F-R7-1: relocation pins the initiating ResultID in the witness.
func TestRekeyWitnessPinnedResultID(t *testing.T) {
	store3 := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store3, "/f/a.mp4", "res-3", "SSNI-R7", "")
	base3 := afero.NewMemMapFs()
	dir3 := filepath.Join("/tmp", "posters", "JOB-7")
	require.NoError(t, base3.MkdirAll(dir3, 0o755))
	require.NoError(t, afero.WriteFile(base3, filepath.Join(dir3, "SSNI-R7.jpg"), []byte("x"), 0o644))
	committer3 := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-7", newKeyedMutexRegistry())
	pe3 := newEditorForStore(store3)
	fsWedge := &seqRenameFailFS{Fs: base3, failOn: map[int]bool{3: true}} // witness(1), forward(2), reverse-fail(3)
	pe3.attachEnv(&posterEditEnv{fs: fsWedge, tempDir: "/tmp", jobID: "JOB-7", committer: committer3, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m3 := &LockedMovieOps{pe: pe3, movieID: "SSNI-R7"}
	require.Error(t, m3.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N7"}))
	data, rerr := afero.ReadFile(base3, filepath.Join(dir3, ".rekey-SSNI-R7.json"))
	require.NoError(t, rerr, "witness retained on incomplete rollback")
	var w rekeyWitness
	require.NoError(t, json.Unmarshal(data, &w))
	assert.Equal(t, "res-3", w.ResultID, "initiating result pinned")
	assert.Equal(t, "SSNI-R7", w.OldID)
	assert.Equal(t, "SSNI-N7", w.NewID)
}

// audit F-R7-1: arbitration with a pinned ResultID ignores a sibling family
// still on the old spelling.
func TestReconcileRekeyScopedToPinnedResult(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9-full.jpg"), []byte("newfull"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9.jpg"), []byte("newcrop"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "OLD-9.jpg"), []byte("oldcrop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-9", NewID: "NEW-9", PrevRevision: 0, ResultID: "res-a"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(envelopeRow(t, "JOB-W1", map[string]*resultstore.MovieResult{
		"/f/a.mp4": {ResultID: "res-a", Revision: 1, Movie: &models.Movie{ID: "NEW-9"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-9"}},
		"/f/b.mp4": {ResultID: "res-b", Revision: 0, Movie: &models.Movie{ID: "OLD-9"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "OLD-9"}},
	}), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "sibling's OLD presence does not flip the pinned result's commit → no reversal")
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-9.json"))
	assert.Error(t, wErr, "witness swept after committed arbitration")
	_, newErr := fs.Stat(filepath.Join(dir, "NEW-9.jpg"))
	assert.NoError(t, newErr, "committed new bytes stay")
}

// audit F-R7-1: relocation refuses when a SIBLING family shares the canonical
// poster ID.
func TestRekeyRefusedWhenSiblingSharesCanonicalID(t *testing.T) {
	store := resultstore.New(5, []string{"/f/a.mp4", "/f/b.mp4", "/f/c.mp4", "/f/d.mp4", "/f/e.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "SHAR-1", "")
	// Sibling family: SAME canonical movie ID under a DIFFERENT matcher alias.
	store.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		ResultID:      "res-b",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SHAR-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "sibling-alias"},
	})
	store.SetFileMatchInfo("/f/b.mp4", models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "sibling-alias"})
	// Rows the sibling-scan must SKIP: movieless + wrong canonical.
	store.UpdateFileResult("/f/c.mp4", &resultstore.MovieResult{
		ResultID: "res-c", Status: models.JobStatusRunning,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/c.mp4", MovieID: "OTHER-9"},
	})
	store.UpdateFileResult("/f/d.mp4", &resultstore.MovieResult{
		ResultID:      "res-d",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "UNREL-5"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/d.mp4", MovieID: "UNREL-5"},
	})
	// Shares the canonical ID but has NO matcher alias → not a family; the
	// sibling scan skips it without fencing.
	store.UpdateFileResult("/f/e.mp4", &resultstore.MovieResult{
		ResultID:      "res-e",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SHAR-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/e.mp4"},
	})
	fs := afero.NewMemMapFs()
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SHAR-1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "TGT-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared with family", "sibling co-ownership refuses the relocation")
}

// audit F-R7-3: single-save fences the STORED identity too (rekey via single
// save meets a pending witness at the old ID).
func TestUpdateMovieSingleFencedByStoredIDWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), []byte("{\"old_id\":\"SSNI-R1\",\"new_id\":\"ELSE-9\"}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.updateMovieSingleLocked(context.Background(), "/f/a.mp4", &models.Movie{ID: "SSNI-NEW-9", Title: "x"})
	require.Error(t, err, "witness at the STORED id fences the single-save rekey")
	assert.Contains(t, err.Error(), "rekey witness unresolved")
}
