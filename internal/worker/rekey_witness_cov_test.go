package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- rekey relocation witnesses (codex r40 P2) ---

// witnessJobRow encodes a durable job row whose single result carries the
// given movie ID — the arbiter the reconciler reads to decide commit-landed
// vs pre-commit-crash.
func witnessJobRow(t *testing.T, movieID string) *models.Job {
	return witnessJobRowRev(t, movieID, 0)
}

func witnessJobRowRev(t *testing.T, movieID string, rev uint64) *models.Job {
	t.Helper()
	res := map[string]*resultstore.MovieResult{
		"/f/a.mp4": {
			ResultID:      "res-1",
			Revision:      rev,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: movieID},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: movieID},
		},
	}
	payload, err := json.Marshal(res)
	require.NoError(t, err)
	return &models.Job{Results: string(payload)}
}

func witnessFixture(t *testing.T) (afero.Fs, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	return fs, dir
}

// Pre-commit crash: durable row still references the old ID — files stranded
// at the NEW name are renamed BACK so the stored crop URL resolves again.
func TestReconcileRekeyWitnessesPreCommitCrashReverses(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9-full.jpg"), []byte("full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9.jpg"), []byte("crop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-9", NewID: "NEW-9"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRow(t, "OLD-9"), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n, "both legs reversed to the old identity")
	for _, name := range []string{"OLD-9-full.jpg", "OLD-9.jpg"} {
		_, statErr := fs.Stat(filepath.Join(dir, name))
		assert.NoError(t, statErr, "reversed file present at %s", name)
	}
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-9.json"))
	assert.Error(t, wErr, "witness swept after reconciliation")
}

// Commit landed (post-commit crash before the witness sweep): the row
// references the new ID — only the witness is removed, files untouched.
func TestReconcileRekeyWitnessesCommittedKeepsNewNames(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9.jpg"), []byte("crop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-9", NewID: "NEW-9"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowRev(t, "NEW-9", 1), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "nothing reversed — the commit DID land")
	_, statErr := fs.Stat(filepath.Join(dir, "NEW-9.jpg"))
	assert.NoError(t, statErr)
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-9.json"))
	assert.Error(t, wErr, "leftover witness swept")
}

// codex r44 P2: a case-ONLY rekey crash on a case-sensitive fs: the durable
// row carries the OLD spelling; EqualFold arbitration would misread that as
// "committed" and sweep the witness without reversing.
func TestReconcileRekeyWitnessesCaseOnlyCrashReverses(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "ABC-123.jpg"), []byte("crop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "abc-123", NewID: "ABC-123"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-abc-123.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRow(t, "abc-123"), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "case-only crash reverses on exact-match arbitration")
	_, statErr := fs.Stat(filepath.Join(dir, "abc-123.jpg"))
	assert.NoError(t, statErr)
}

// An orphaned job directory leaves witness arbitration to the staleness
// sweep — ReconcileRekeyWitnesses must not touch it.
func TestReconcileRekeyWitnessesOrphanedJobUntouched(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9.jpg"), []byte("crop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-9", NewID: "NEW-9"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), witness, 0o644))

	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, database.ErrNotFound)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-9.json"))
	assert.NoError(t, wErr, "witness stays — no DB row to arbitrate")
}

// A corrupt witness is left in place (warn only) rather than mis-arbitrated.
func TestReconcileRekeyWitnessesCorruptWitnessSkipped(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), []byte("{not json"), 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-9.json"))
	assert.NoError(t, wErr)
}

// No fs / no repo → full no-op.
func TestReconcileRekeyWitnessesNilGuards(t *testing.T) {
	assert.Equal(t, 0, func() int { n, _ := (&TempDirCleaner{}).ReconcileRekeyWitnesses(context.Background()); return n }())
	assert.Equal(t, 0, func() int {
		n, _ := (&TempDirCleaner{fs: afero.NewMemMapFs()}).ReconcileRekeyWitnesses(context.Background())
		return n
	}())
}

// codex r50 P2: a rekey MUST refuse to step on an unresolved witness — the
// previous rekey's stranded moves would lose their only recovery record.
func TestUpdateMovieFamilyRekeyRejectsUnresolvedWitness(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	witness, _ := json.Marshal(rekeyWitness{OldID: "SSNI-R1", NewID: "SSNI-OLD-NEW"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-SSNI-R1.json"), witness, 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-NEW2"}), &conflict)
	// audit F5: same-identity saves are ALSO fenced now — a plain PATCH's
	// post-commit eviction could delete old-ID legs while a move is stranded,
	// and the reconciler would then resurrect the stale leg.
	require.ErrorAs(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-R1"}), &conflict, "non-rekey edits fenced while the rekey witness is unresolved")
	data, _ := afero.ReadFile(fs, filepath.Join(dir, ".rekey-SSNI-R1.json"))
	assert.Contains(t, string(data), "SSNI-OLD-NEW", "the original witness content is untouched")
}

// codex r52 P2: a case-folded multipart sibling already at the new spelling
// must NOT satisfy the committed check -- the old-spelling part still exists,
// so the rekey never landed.
func TestReconcileRekeyWitnessesCaseFoldedSiblingNotCommitted(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "ABC-123.jpg"), []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "ABC-123.jpg.bak"), []byte("old"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "abc-123", NewID: "ABC-123", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-abc-123.json"), witness, 0o644))

	res := map[string]*resultstore.MovieResult{
		"/f/a.mp4": {ResultID: "res-a", Revision: 0, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "abc-123"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "abc-123"}},
		"/f/b.mp4": {ResultID: "res-b", Revision: 5, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "ABC-123"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "ABC-123"}},
	}
	payload, merr := json.Marshal(res)
	require.NoError(t, merr)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: string(payload)}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "old spelling still present -- rekey did not commit -- reverse")
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "abc-123.jpg"))
	assert.Equal(t, "new", string(content), "original bytes moved back to the old name")
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-abc-123.json"))
	assert.Error(t, wErr, "witness swept after reversal")
}

// The writer side of the contract: a successful rekey always sweeps its
// witness, so a clean save leaves nothing for the reconciler to arbitrate.
func TestUpdateMovieFamilyRekeySweepsWitnessOnSuccess(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"}))
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-SSNI-R1.json"))
	assert.Error(t, wErr, "witness swept once the commit landed")
}
