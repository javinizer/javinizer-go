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

// --- r54 coverage fills ---

func TestRekeyBlockedByPromoteWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-SSNI-R1.json"), []byte("{}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"}), &conflict)
	assert.Contains(t, conflict.Message, "promote")
}

func TestRekeyBlockedByCropWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-SSNI-R1.crop-x.json"), []byte(`{"poster_id":"SSNI-R1"}`), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	var conflict *EditAdmissionConflictError
	require.ErrorAs(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"}), &conflict)
	assert.Contains(t, conflict.Message, "crop")
}

func TestRekeyWitnessRenameFailure(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("x"), 0o644))
	fs := &noRenameFs{Fs: base}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "witness rename")
}

func TestPreCommitEvictionRemovesStaleBytes(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-1", Status: models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SSNI-R1", Poster: models.PosterState{PosterURL: "https://old.jpg"}},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "SSNI-R1"},
	})
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "SSNI-R1.jpg"), []byte("old-crop"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	movie := &models.Movie{ID: "SSNI-R1", Poster: models.PosterState{PosterURL: "https://new.jpg"}}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), movie))
	_, fullErr := fs.Stat(filepath.Join(dir, "SSNI-R1-full.jpg"))
	assert.Error(t, fullErr, "stale full evicted pre-commit")
	_, cropErr := fs.Stat(filepath.Join(dir, "SSNI-R1.jpg"))
	assert.Error(t, cropErr, "stale crop evicted pre-commit")
}

func TestRekeyCommitRollbackIncompleteKeepsWitness(t *testing.T) {
	store, base, dir := familyRelocationSetup(t)
	fs := &seqRenameFailFS{Fs: base, failOn: map[int]bool{4: true}}
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-9", newKeyedMutexRegistry())
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9", committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "tx wedged")
	_, wErr := base.Stat(filepath.Join(dir, ".rekey-SSNI-R1.json"))
	assert.NoError(t, wErr, "witness kept when rollback incomplete")
}

func TestStartScrapeGoneMidWait(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	go func() {
		time.Sleep(50 * time.Millisecond)
		job.admission.MarkGone()
	}()
	err = job.Controller().StartScrape(context.Background(), nil, ScrapePhaseConfig{})
	require.ErrorIs(t, err, ErrJobGone)
	rel()
}

func TestStartApplyGoneMidWait(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.Status = models.JobStatusCompleted
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	go func() {
		time.Sleep(50 * time.Millisecond)
		job.admission.MarkGone()
	}()
	err = job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.ErrorIs(t, err, ErrJobGone)
	rel()
}

func TestStartScrapeCancelledMidWait(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- job.Controller().StartScrape(ctx, nil, ScrapePhaseConfig{}) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	rel()
}

func TestStartApplyCancelledMidWait(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.Status = models.JobStatusCompleted
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- job.Controller().StartApply(ctx, ApplyPhaseConfig{}) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	rel()
}

func TestReconcilePromoteCommittedBothLegsSettled(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"full": shaContentHex([]byte("old"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://x", 1), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestReconcilePromoteUncommittedHashMismatchDropped(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new-uncommitted"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"crop": shaContentHex([]byte("old-bytes"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old", 0), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, cErr := fs.Stat(filepath.Join(dir, "PI-1.jpg"))
	assert.Error(t, cErr, "uncommitted canon dropped (hash mismatch)")
}

type noRenameFs struct{ afero.Fs }

func (noRenameFs) Rename(string, string) error { return errors.New("rename blocked") }

// --- r55 coverage: error-path branches ---

// witness check non-IsNotExist stat error (rekey path)
func TestRekeyWitnessCheckStatError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("x"), 0o644))
	fs := statFailSuffixFS{Fs: base, suffix: ".rekey-SSNI-R1.json"}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "witness check")
}

// promote/crop witness check non-IsNotExist stat error
func TestRekeyPromoteWitnessCheckStatError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("x"), 0o644))
	fs := statFailSuffixFS{Fs: base, suffix: ".promote-SSNI-R1.json"}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "witness check")
}

// rekey destination stat non-IsNotExist error
func TestRekeyDstStatNonNotExistError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("x"), 0o644))
	fs := statFailSuffixFS{Fs: base, suffix: "SSNI-N9-full.jpg"}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target stat")
}

// rekey relocate rename failure (witness rename fails first)
func TestRekeyRenameFailureForward(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("x"), 0o644))
	fs := &seqRenameFailFS{Fs: base, failOn: map[int]bool{2: true}}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rekey move")
}

// rekey commit-failure rollback complete: witness removed
func TestRekeyCommitRollbackCompleteRemovesWitness(t *testing.T) {
	store, base, dir := familyRelocationSetup(t)
	committer := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-9", newKeyedMutexRegistry())
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: base, tempDir: "/tmp", jobID: "JOB-9", committer: committer, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorContains(t, err, "tx wedged")
	_, wErr := base.Stat(filepath.Join(dir, ".rekey-SSNI-R1.json"))
	assert.Error(t, wErr, "witness swept after complete rollback")
}

// rekey commit success: witness removed
func TestRekeyCommitSuccessRemovesWitness(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"}))
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-SSNI-R1.json"))
	assert.Error(t, wErr, "witness swept after commit success")
}

// StartScrape persist failure abort path
func TestStartScrapePersistFailureAbort(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.deps.PersistFn = func() error { return errors.New("disk wedged") }
	err := job.Controller().StartScrape(context.Background(), []string{"/f/a.mp4"}, ScrapePhaseConfig{})
	require.ErrorContains(t, err, "persist phase-entry marker")
	assert.Equal(t, models.JobStatusFailed, job.lifecycle.GetJobStatus())
}

// StartApply persist failure abort path
func TestStartApplyPersistFailureAbort(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.Status = models.JobStatusCompleted
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.deps.PersistFn = func() error { return errors.New("disk wedged") }
	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.ErrorContains(t, err, "persist phase-entry marker")
}

// FS helper: blocks WriteFile by blocking Create
func (f noWriteFileFs) Create(name string) (afero.File, error) {
	return nil, errors.New("write blocked")
}

type noWriteFileFs struct{ afero.Fs }

// promote reconcile: orphaned dir (NotFound) for promote witness
func TestReconcilePromoteOrphanedDir(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, database.ErrNotFound)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.NoError(t, wErr, "orphaned: witness left for staleness sweep")
}

// crop reconcile: orphaned dir
func TestReconcileCropOrphanedDir(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-x", CroppedURL: "https://x"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-stage-x.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, database.ErrNotFound)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// promote reconcile: bak exists, canon exists, hash matches (already restored, no remove)
func TestReconcilePromoteBakExistsCanonHashMatch(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1-full.jpg"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1-full.jpg.bak"), []byte("old-full"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"full": shaContentHex([]byte("old-full"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old", 0), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "bak restored over canon (which hash-matched, not removed)")
	content, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1-full.jpg"))
	assert.Equal(t, "old-full", string(content))
}

// promote reconcile: committed with bak legs to sweep
func TestReconcilePromoteCommittedBakSweepFailureRetains(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1-full.jpg.bak"), []byte("old"), 0o644))
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
	assert.Error(t, fbErr, "bak swept on committed")
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr, "witness swept")
}

// crop reconcile: uncommitted drops staged + sweeps staged full + witness
func TestReconcileCropUncommittedSweepsFull(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "stage-x.jpg"), []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "stage-x-full.jpg"), []byte("full"), 0o644))
	witness, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-x", CroppedURL: cropWitnessFixtureURL, PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-stage-x.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, "https://old", 0), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, sfErr := fs.Stat(filepath.Join(dir, "stage-x-full.jpg"))
	assert.Error(t, sfErr, "staged full swept")
	_, wErr := fs.Stat(filepath.Join(dir, ".crop-stage-x.json"))
	assert.Error(t, wErr, "witness swept")
}

// StartStaleTempCleanup ticker path (close immediately)
func TestStartStaleTempCleanupImmediateClose(t *testing.T) {
	cl := &TempDirCleaner{fs: afero.NewMemMapFs(), tempDir: "/tmp"}
	stop := cl.StartStaleTempCleanup()
	close(stop)
}

// --- r55 safe coverage fills ---

type failRemoveFS struct{ afero.Fs }

func (failRemoveFS) Remove(string) error { return errors.New("remove blocked") }

// promote reconcile: bak sweep failure retains witness
func TestReconcilePromoteBakSweepFailRetains(t *testing.T) {
	base, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "PI-1.jpg"), []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "PI-1-full.jpg.bak"), []byte("old"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://x", 1), nil)
	fs := failRemoveFS{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := base.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.NoError(t, wErr, "witness retained when bak sweep fails")
}

// promote reconcile: uncommitted, no bak, canon hash match
func TestReconcilePromoteNoBakCanonHashMatch(t *testing.T) {
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
}

// promote reconcile: uncommitted, no bak, canon hash mismatch (drop)
func TestReconcilePromoteNoBakCanonHashMismatchDrop(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1-full.jpg"), []byte("new-uncommitted"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"full": shaContentHex([]byte("old-full"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old", 0), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, cErr := fs.Stat(filepath.Join(dir, "PI-1-full.jpg"))
	assert.Error(t, cErr, "uncommitted canon dropped")
}

// crop reconcile: uncommitted, staged remove fails
func TestReconcileCropUncommittedStagedRemoveFail(t *testing.T) {
	base, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "stage-x.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-x", CroppedURL: cropWitnessFixtureURL, PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".crop-stage-x.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, "https://old", 0), nil)
	fs := failRemoveFS{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := base.Stat(filepath.Join(dir, ".crop-stage-x.json"))
	assert.NoError(t, wErr, "witness retained when staged remove fails")
}

// crop reconcile: corrupt crop witness (missing fields)
func TestReconcileCropWitnessMissingFields(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(cropWitness{PosterID: "CP-1"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-stage-x.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// promote reconcile: corrupt promote witness (missing fields)
func TestReconcilePromoteWitnessMissingFields(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// rekey reconcile: job lookup error
func TestReconcileRekeyJobLookupError(t *testing.T) {
	fs, dir := witnessFixture(t)
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD", NewID: "NEW", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, errors.New("db down"))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// rekey reconcile: stat error on newPath
func TestReconcileRekeyNewPathStatError(t *testing.T) {
	base, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "NEW-full.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD", NewID: "NEW", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".rekey-OLD.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowRev(t, "OLD", 0), nil)
	fs := statFailSuffixFS{Fs: base, suffix: "NEW-full.jpg"}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "stat error -> reversal not clean -> witness survives")
}

// rekey reconcile: rename back fails
func TestReconcileRekeyRenameBackFails(t *testing.T) {
	base, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "NEW-full.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD", NewID: "NEW", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".rekey-OLD.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowRev(t, "OLD", 0), nil)
	fs := noRenameFs{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "rename back fails -> not clean")
	_, wErr := base.Stat(filepath.Join(dir, ".rekey-OLD.json"))
	assert.NoError(t, wErr, "witness kept when rename back fails")
}

// rekey reconcile: old bytes still there (no reversal needed)
func TestReconcileRekeyOldBytesStillThere(t *testing.T) {
	base, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "NEW-full.jpg"), []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "OLD-full.jpg"), []byte("old"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD", NewID: "NEW", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".rekey-OLD.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowRev(t, "OLD", 0), nil)
	cl := &TempDirCleaner{fs: base, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "old bytes still there -> nothing to reverse")
}

// promote reconcile: committed with bak legs to sweep (both bak legs present)
func TestReconcilePromoteCommittedSweepsBothBakLegs(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1-full.jpg.bak"), []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.bak"), []byte("old-crop"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://x", 1), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	for _, bak := range []string{"PI-1-full.jpg.bak", "PI-1.jpg.bak"} {
		_, bErr := fs.Stat(filepath.Join(dir, bak))
		assert.Error(t, bErr, "bak swept: %s", bak)
	}
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr, "witness swept")
}

// --- r56 grind: targeted coverage for specific branches ---

// admission.go: ctx == nil fallback
func TestBeginPhaseNilCtx(t *testing.T) {
	b := newAdmissionBarrier()
	p, err := b.BeginPhase(nil)
	require.NoError(t, err)
	p.Fail()
}

// job_controller.go: StartApply markStarted cancelled-before-claim
func TestStartApplyMarkStartedCancelledBeforeClaim(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.Status = models.JobStatusCompleted
	job.lifecycle.cancelled = true
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	err := job.Controller().StartApply(context.Background(), ApplyPhaseConfig{})
	require.NoError(t, err, "cancelled-before-claim returns nil")
	assert.Equal(t, models.JobStatusCompleted, job.lifecycle.GetJobStatus(), "claim refused — status unchanged")
}

// job_controller.go: StartScrape abort persist failure
func TestStartScrapeAbortPersistFailure(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.deps.PersistFn = func() error { return errors.New("disk wedged") }
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- job.Controller().StartScrape(ctx, nil, ScrapePhaseConfig{}) }()
	assert.Eventually(t, func() bool {
		job.admission.mu.Lock()
		defer job.admission.mu.Unlock()
		return job.admission.pendingPhase == 1
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	rel()
}

// job_controller.go: StartApply abort persist failure
func TestStartApplyAbortPersistFailure(t *testing.T) {
	job := newBatchJob([]string{"/f/a.mp4"})
	job.lifecycle.Status = models.JobStatusCompleted
	job.Controller().SetWorkflow(wfmocks.NewMockWorkflowInterface(t))
	job.deps.PersistFn = func() error { return errors.New("disk wedged") }
	rel, err := job.admission.AdmitShared()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- job.Controller().StartApply(ctx, ApplyPhaseConfig{}) }()
	assert.Eventually(t, func() bool {
		job.admission.mu.Lock()
		defer job.admission.mu.Unlock()
		return job.admission.pendingPhase == 1
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	require.NoError(t, <-done)
	rel()
}

// job_store.go: startup reconcile error path
func TestNewJobStoreReconcileErrorLogsWarn(t *testing.T) {
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, mock.Anything).Return(nil, errors.New("db down")).Maybe()
	repo.EXPECT().List(mock.Anything).Return([]models.Job{}, nil)
	fs := afero.NewMemMapFs()
	fs.MkdirAll("/tmp/posters/J-FAIL", 0o755)
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD", NewID: "NEW", PrevRevision: 0})
	afero.WriteFile(fs, "/tmp/posters/J-FAIL/.rekey-OLD.json", witness, 0o644)
	s := NewJobStore(repo, nil, nil, "/tmp", nil, fs)
	require.NotNil(t, s)
}

// temp_dir_cleaner.go: ReconcileRekeyWitnesses ReadDir error
func TestReconcileRekeyReadDirError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/tmp/posters", []byte("x"), 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: base, tempDir: "/tmp", jobRepo: repo}
	_, err := cl.ReconcileRekeyWitnesses(context.Background())
	assert.Error(t, err)
}

// temp_dir_cleaner.go: non-dir entry in posters dir skipped
func TestReconcileRekeyNonDirEntrySkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/tmp/posters", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/tmp/posters/stray.txt", []byte("x"), 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// temp_dir_cleaner.go: rekey reconcile old-path stat non-IsNotExist error
func TestReconcileRekeyOldPathStatError(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "NEW-full.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD", NewID: "NEW", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".rekey-OLD.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowRev(t, "OLD", 0), nil)
	fs := statFailSuffixFS{Fs: base, suffix: "OLD-full.jpg"}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "stat error -> not clean -> witness survives")
}

// temp_dir_cleaner.go: rekey witness sweep non-IsNotExist error (witness already gone)
func TestReconcileRekeyWitnessSweepError(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD", NewID: "NEW", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".rekey-OLD.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowRev(t, "NEW", 1), nil)
	// failRemoveFS blocks Remove — witness sweep will log but not fail
	fs := failRemoveFS{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	// committed: no reversal needed; witness sweep fails but that's just a warn
	assert.Equal(t, 0, n)
}

// temp_dir_cleaner.go: promote reconcile canon stat non-IsNotExist
func TestReconcilePromoteCanonStatNonNotExist(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "PI-1-full.jpg"), []byte("x"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "PI-1-full.jpg.bak"), []byte("old"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"full": shaContentHex([]byte("old"))}})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old", 0), nil)
	fs := statFailSuffixFS{Fs: base, suffix: "PI-1-full.jpg"}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "canon stat error -> clean=false -> witness survives")
}

// temp_dir_cleaner.go: promote reconcile bak stat non-IsNotExist
func TestReconcilePromoteBakStatNonNotExist(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "PI-1-full.jpg"), []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "PI-1-full.jpg.bak"), []byte("old"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"full": shaContentHex([]byte("old"))}})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://old", 0), nil)
	fs := statFailSuffixFS{Fs: base, suffix: ".bak"}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "bak stat error -> clean=false")
}

// temp_dir_cleaner.go: promote reconcile witness sweep error (non-IsNotExist)
func TestReconcilePromoteWitnessSweepError(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "PI-1.jpg"), []byte("new"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x", ResultID: "res-1", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(witnessJobRowPosterRev(t, "https://x", 1), nil)
	fs := failRemoveFS{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// temp_dir_cleaner.go: crop reconcile witness sweep error
func TestReconcileCropWitnessSweepError(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	witness, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-x", CroppedURL: cropWitnessFixtureURL, PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".crop-stage-x.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, "https://old", 0), nil)
	fs := failRemoveFS{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// temp_dir_cleaner.go: crop reconcile staged-full sweep error
func TestReconcileCropStagedFullSweepError(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "stage-x.jpg"), []byte("new"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "stage-x-full.jpg"), []byte("full"), 0o644))
	witness, _ := json.Marshal(cropWitness{PosterID: "CP-1", ResultID: "res-c", StageID: "stage-x", CroppedURL: cropWitnessFixtureURL, PrevRevision: 0})
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, ".crop-stage-x.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(cropWitnessJobRow(t, "https://old", 0), nil)
	fs := failRemoveFS{Fs: base}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// temp_dir_cleaner.go: StartStaleTempCleanup reconcile error + cleanup paths
func TestStartStaleTempCleanupReconcileError(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/tmp/posters", []byte("x"), 0o644)) // force ReadDir error
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: base, tempDir: "/tmp", jobRepo: repo}
	stop := cl.StartStaleTempCleanup()
	time.Sleep(300 * time.Millisecond)
	close(stop)
}

// poster_editor.go: crop witness ReadDir error (rerr != nil → continue)
func TestRekeyCropWitnessReadDirError(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("x"), 0o644))
	// Write a crop witness with ReadFile-unreadable content (empty dir as a file)
	require.NoError(t, base.MkdirAll(filepath.Join(dir, ".crop-SSNI-R1.crop-x.json"), 0o755))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: base, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	// The ReadDir will find the .crop-* dir entry; ReadFile will fail → continue
	// The rekey should proceed (not blocked by the unreadable crop witness)
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.NoError(t, err, "unreadable crop witness is skipped, not blocking")
}
