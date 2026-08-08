package worker

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// envelopeRow encodes results through the REAL persistence codec (codex P1):
// production rows are jobpersist envelopes, not raw result maps.
func envelopeRow(t *testing.T, jobID string, results map[string]*resultstore.MovieResult) *models.Job {
	t.Helper()
	job, err := jobpersist.Encode(jobpersist.Snapshot{ID: jobID, Status: models.JobStatusCompleted, Files: []string{"/f/a.mp4"}, Results: results})
	require.NoError(t, err)
	return job
}

// corruptResultsRow returns an envelope-shaped row whose Results column is
// deliberately truncated (audit F3: decode must fail → arbitration skips).
func corruptResultsRow(t *testing.T, jobID string, results map[string]*resultstore.MovieResult) *models.Job {
	t.Helper()
	job := envelopeRow(t, jobID, results)
	job.Results = "{\"domain\": {\"/f/a.mp4\": {"
	return job
}

// audit F3: undecodable Results ⇒ witnesses are KEPT and nothing is reversed.
func TestReconcileRekeyDecodeFailureKeepsWitness(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9-full.jpg"), []byte("full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9.jpg"), []byte("crop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-9", NewID: "NEW-9", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(corruptResultsRow(t, "JOB-W1", nil), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "no reversal on decode failure")
	_, fullErr := fs.Stat(filepath.Join(dir, "NEW-9-full.jpg"))
	assert.NoError(t, fullErr, "new-ID bytes untouched")
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-9.json"))
	assert.NoError(t, wErr, "witness kept for repair")
}

func TestReconcilePromoteDecodeFailureKeepsWitness(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new-bytes"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x/p.jpg", ResultID: "res-1"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(corruptResultsRow(t, "JOB-W1", nil), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	_, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "new-bytes", string(got), "canon untouched on decode failure")
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.NoError(t, wErr, "witness kept for repair")
}

func TestReconcileCropDecodeFailureKeepsStaged(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.crop-9.jpg"), []byte("staged-crop"), 0o644))
	witness, _ := json.Marshal(cropWitness{PosterID: "PI-1", ResultID: "res-1", StageID: "PI-1.crop-9", CroppedURL: "/x/PI-1.jpg"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-PI-1.crop-9.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(corruptResultsRow(t, "JOB-W1", nil), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	_, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	_, sErr := fs.Stat(filepath.Join(dir, "PI-1.crop-9.jpg"))
	assert.NoError(t, sErr, "staged bytes kept — not dropped on decode failure")
	_, wErr := fs.Stat(filepath.Join(dir, ".crop-PI-1.crop-9.json"))
	assert.NoError(t, wErr, "witness kept for repair")
}

func TestRescapePosterBackupRestoreMissingBak(t *testing.T) {
	mem := afero.NewMemMapFs()
	require.NoError(t, mem.MkdirAll("/tmp/posters/JM", 0o755))
	b := &rescrapePosterBackup{
		fs:      mem,
		full:    "/tmp/posters/JM/NO-1-full.jpg",
		crop:    "/tmp/posters/JM/NO-1.jpg",
		fullBak: "/tmp/posters/JM/NO-1-full.jpg.rsbak.zzz",
		cropBak: "/tmp/posters/JM/NO-1.jpg.rsbak.zzz",
		hadFull: true,
		hadCrop: true,
	}
	b.restore() // both baks missing → skip quietly (stat-miss continue arc)
}

// audit F-R4-5: sweep warn arcs — Remove wedge keeps the litter in place;
// restore rename wedge keeps the parked copy for the next startup retry.
func TestReconcileParkedPosterBackupsWarns(t *testing.T) {
	base, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "WP-1.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "WP-1.jpg.rsbak.abc"), []byte("stale"), 0o644))
	cl := &TempDirCleaner{fs: removeFailFS{Fs: base}, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileParkedPosterBackups(dir))
	_, err := base.Stat(filepath.Join(dir, "WP-1.jpg.rsbak.abc"))
	assert.NoError(t, err, "wedged remove keeps the litter")

	base2, dir2 := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base2, filepath.Join(dir2, "WP-2-2-2-full.jpg.rsbak.abc"), []byte("committed"), 0o644))
	cl2 := &TempDirCleaner{fs: &seqRenameFailFS{Fs: base2, failOn: map[int]bool{1: true}}, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl2.reconcileParkedPosterBackups(dir2))
	_, err2 := base2.Stat(filepath.Join(dir2, "WP-2-2-2-full.jpg.rsbak.abc"))
	assert.NoError(t, err2, "wedged restore keeps the parked copy")
}

// audit F-R4-5: startup reconciliation re-homes parked backup legs stranded
// by a crash/panic between park and restore.
func TestReconcileParkedPosterBackups(t *testing.T) {
	fs, dir := witnessFixture(t)
	// canonical missing → restore parked bytes
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RK-1.jpg.rsbak.abc"), []byte("committed-crop"), 0o644))
	// canonical present → parked litter removed
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RK-2.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RK-2.jpg.rsbak.def"), []byte("stale"), 0o644))
	// legacy plain .dlbak (pre-nonce manager parks) handled too
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RK-3-full.jpg.dlbak"), []byte("committed-full"), 0o644))
	// unrelated files untouched
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "README.txt"), []byte("x"), 0o644))

	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(dir)
	assert.Equal(t, 3, healed)
	got, _ := afero.ReadFile(fs, filepath.Join(dir, "RK-1.jpg"))
	assert.Equal(t, "committed-crop", string(got), "stranded crop restored")
	got, _ = afero.ReadFile(fs, filepath.Join(dir, "RK-3-full.jpg"))
	assert.Equal(t, "committed-full", string(got), "legacy .dlbak restored")
	live, _ := afero.ReadFile(fs, filepath.Join(dir, "RK-2.jpg"))
	assert.Equal(t, "live", string(live), "present canonical untouched")
	_, parkErr := fs.Stat(filepath.Join(dir, "RK-2.jpg.rsbak.def"))
	assert.Error(t, parkErr, "inferior parked litter removed")
	readme, _ := afero.ReadFile(fs, filepath.Join(dir, "README.txt"))
	assert.Equal(t, "x", string(readme), "unrelated files untouched")

	// dir read error → no-op zero
	cl2 := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl2.reconcileParkedPosterBackups("/nonexistent"))
}

// P1 regression: a committed REKEY arbitrated against an envelope row must be
// recognized as committed (witness swept, new-ID bytes kept) — with the raw
// parse every production witness read as uncommitted and got REVERSED.
func TestReconcileRekeyEnvelopeRowCommitted(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9-full.jpg"), []byte("full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9.jpg"), []byte("crop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-9", NewID: "NEW-9", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(envelopeRow(t, "JOB-W1", map[string]*resultstore.MovieResult{
		"/f/a.mp4": {ResultID: "res-1", Revision: 1, Movie: &models.Movie{ID: "NEW-9"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-9"}},
	}), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "committed: nothing reversed")
	_, fullErr := fs.Stat(filepath.Join(dir, "NEW-9-full.jpg"))
	assert.NoError(t, fullErr, "committed new-ID bytes retained")
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-9.json"))
	assert.Error(t, wErr, "witness swept")
}

// P1 regression: committed PROMOTE against an envelope row sweeps the witness
// and keeps the promoted canon (rather than restoring the pre-op .bak).
func TestReconcilePromoteEnvelopeRowCommitted(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("new-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.bak"), []byte("old-bytes"), 0o644))
	witness, _ := json.Marshal(promoteWitness{PosterID: "PI-1", URL: "https://x/p.jpg", ResultID: "res-1", PrevRevision: 0, OldSHA: map[string]string{"crop": shaContentHex([]byte("old-bytes"))}})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(envelopeRow(t, "JOB-W1", map[string]*resultstore.MovieResult{
		"/f/a.mp4": {ResultID: "res-1", Revision: 1, Movie: &models.Movie{ID: "PI-1", Poster: models.PosterState{PosterURL: "https://x/p.jpg"}}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PI-1"}},
	}), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	_ = n
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "new-bytes", string(got), "committed promote keeps its promoted bytes")
	_, wErr := fs.Stat(filepath.Join(dir, ".promote-PI-1.json"))
	assert.Error(t, wErr, "witness swept")
}

// P1 regression: committed CROP against an envelope row completes the staged
// promote (staged bytes land at canonical), instead of being discarded.
func TestReconcileCropEnvelopeRowCommitted(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.crop-9.jpg"), []byte("staged-crop"), 0o644))
	witness, _ := json.Marshal(cropWitness{PosterID: "PI-1", ResultID: "res-1", StageID: "PI-1.crop-9", CroppedURL: "/api/v1/temp/posters/JOB-W1/PI-1.jpg", PrevRevision: 0})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-PI-1.crop-9.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(envelopeRow(t, "JOB-W1", map[string]*resultstore.MovieResult{
		"/f/a.mp4": {ResultID: "res-1", Revision: 1, Movie: &models.Movie{ID: "PI-1", Poster: models.PosterState{CroppedPosterURL: "/api/v1/temp/posters/JOB-W1/PI-1.jpg"}}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "PI-1"}},
	}), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "committed crop promote completed")
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "staged-crop", string(got))
	_, wErr := fs.Stat(filepath.Join(dir, ".crop-PI-1.crop-9.json"))
	assert.Error(t, wErr, "witness swept")
}
