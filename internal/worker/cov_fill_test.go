package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	wfmocks "github.com/javinizer/javinizer-go/internal/mocks/workflow"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- coverage fills for temp_dir_cleaner.go ---

// CleanupStaleTempDirs non-IsNotExist ReadDir error is returned.
func TestCleanupStaleTempDirsReadDirError(t *testing.T) {
	fs := afero.NewMemMapFs()
	// Create a file where the posters dir should be to force a ReadDir error
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters", []byte("x"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp"}
	_, err := cl.CleanupStaleTempDirs(context.Background())
	assert.Error(t, err)
}

// StartStaleTempCleanup runs reconcile + cleanup once on startup then stops.
func TestStartStaleTempCleanupRunOnce(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, mock.Anything).Return(nil, database.ErrNotFound).Maybe()
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	stop := cl.StartStaleTempCleanup()
	time.Sleep(200 * time.Millisecond)
	close(stop)
}

// ReconcileRekeyWitnesses with nil fs returns 0.
func TestReconcileRekeyWitnessesNilFs(t *testing.T) {
	cl := &TempDirCleaner{fs: nil, tempDir: "/tmp", jobRepo: nil}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// ReconcileRekeyWitnesses with a corrupt rekey witness skips it.
func TestReconcileRekeyWitnessesCorruptRekeyWitness(t *testing.T) {
	fs := afero.NewMemMapFs()
	dir := "/tmp/posters/JC"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD.json"), []byte("{bad"), 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JC").Return(nil, database.ErrNotFound).Maybe()
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	// corrupt witness left in place
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD.json"))
	assert.NoError(t, wErr)
}

// reconcilePromoteWitness: corrupt witness left in place.
func TestReconcilePromoteCorruptWitness(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), []byte("{bad"), 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// reconcilePromoteWitness: job lookup error returns 0.
func TestReconcilePromoteJobLookupError(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, errors.New("db down"))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// reconcilePromoteWitness: committed with no bak and no canon -> both legs
// settled, witness swept, nothing reversed.
func TestReconcilePromoteCommittedNoFiles(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://x", 1), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr)
}

// reconcilePromoteWitness: uncommitted, canon exists, oldSHA empty -> drop
// uncommitted, no bak.
func TestReconcilePromoteUncommittedNoBakDropCanon(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old", 0), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "nothing reversed (dropped, not restored)")
	_, cErr := fs.Stat(filepath.Join(dir, "PI-1.jpg"))
	assert.Error(t, cErr, "uncommitted canon dropped")
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr, "witness swept")
}

// reconcilePromoteWitness: uncommitted, canon hash matches oldSHA -> already
// restored, kept.
func TestReconcilePromoteUncommittedHashMatchKeepsCanon(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("old-bytes"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"crop": shaContentHex([]byte("old-bytes"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old", 0), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	assert.Equal(t, "old-bytes", string(content), "hash-matched canon preserved")
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr, "witness swept")
}

// reconcileCropWitness: corrupt witness left in place.
func TestReconcileCropCorruptWitness(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-stage-x.json"), []byte("{bad"), 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// reconcileCropWitness: job lookup error returns 0.
func TestReconcileCropJobLookupError(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-x", CroppedURL: "https://x"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-stage-x.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, errors.New("db down"))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// reconcileCropWitness: committed, staged absent -> nothing to promote,
// witness swept.
func TestReconcileCropCommittedStagedAbsent(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-x", CroppedURL: cropWitnessFixtureURL, PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-stage-x.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, cropWitnessFixtureURL, 1), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(filepath.Join(dir, ".crop-stage-x.json"))
	assert.Error(t, wErr, "witness swept")
}

// --- coverage fills for job_controller.go ---

// markStarted returns context.Canceled when the lifecycle was cancelled
// before the claim.
func TestMarkStartedCancelledBeforeClaim(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.lifecycle.cancelled = true
	job.lifecycle.Status = models.JobStatusPending
	err := job.Controller().StartScrape(context.Background(), nil, ScrapePhaseConfig{})
	require.NoError(t, err, "cancelled-before-claim returns nil")
	assert.Equal(t, models.JobStatusPending, job.lifecycle.GetJobStatus(), "claim refused — status unchanged")
}

// --- coverage fills for poster_editor.go ---

// r53 P2: committed promotion must sweep .bak legs before witness.
func TestReconcilePromoteCommittedSweepsBakLegs(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1-full.jpg.bak"), []byte("oldfull"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.bak"), []byte("oldcrop"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://x", 1), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, fbErr := fs.Stat(filepath.Join(dir, "PI-1-full.jpg.bak"))
	assert.Error(t, fbErr, "full bak swept")
	_, cbErr := fs.Stat(filepath.Join(dir, "PI-1.jpg.bak"))
	assert.Error(t, cbErr, "crop bak swept")
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr, "witness swept")
}

// rekey source stat error aborts.
func TestUpdateMovieFamilyRekeySourceStatError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "SSNI-R1.jpg"), []byte("crop"), 0o644))
	// Place a directory where SSNI-R1-full.jpg stat would fail (not IsNotExist)
	require.NoError(t, base.MkdirAll(filepath.Join(dir, "SSNI-R1-full.jpg"), 0o755))
	fs := statFailSuffixFS{Fs: base, suffix: "SSNI-R1-full.jpg"}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source stat")
}
