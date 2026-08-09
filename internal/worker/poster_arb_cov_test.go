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

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

func arbJobRow(t *testing.T, id string, rev uint64) *models.Job {
	t.Helper()
	res := map[string]*resultstore.MovieResult{
		"/f/x.mp4": {
			ResultID:      "res-arb",
			Revision:      rev,
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: id},
			FileMatchInfo: models.FileMatchInfo{Path: "/f/x.mp4", MovieID: id},
		},
	}
	payload, err := json.Marshal(res)
	require.NoError(t, err)
	return &models.Job{Results: string(payload)}
}

func seedArbitrationScene(t *testing.T, fs afero.Fs, dir, id, nonce, canonContent, backupContent string, prevRev uint64) {
	t.Helper()
	if canonContent != "" {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, id+".jpg"), []byte(canonContent), 0o644))
	}
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, id+".jpg.rsbak."+nonce), []byte(backupContent), 0o644))
	meta, err := json.Marshal(inFlightMeta{PosterID: id, PrevRevision: prevRev})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-"+id+"."+nonce), meta, 0o644))
}

// codex cloud P1: revision unmoved ⇒ commit never landed ⇒ canonical holds
// stranded generation; restore the committed backup over it.
func TestReconcileParkedArbitratesUncommitted(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-1", "a1.b2", "gen-uncommitted", "committed", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-1", 4), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 2, healed, "marker sweep + restore")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "AR-1.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "committed", string(got), "stranded generation rewound")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-1.jpg.rsbak.a1.b2"))
	assert.Error(t, bErr, "backup consumed by the restore")
}

// codex cloud P1: revision advanced past the op's capture ⇒ commit landed ⇒
// canonical bytes are the committed ones; the backup is safe to drop.
func TestReconcileParkedArbitratesCommitted(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-2", "a1.b2", "gen-committed", "pre-op", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-2", 9), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 2, healed, "marker sweep + backup drop")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "AR-2.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "gen-committed", string(got), "committed canonical untouched")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-2.jpg.rsbak.a1.b2"))
	assert.Error(t, bErr, "backup safely discarded")
}

// codex cloud P1 fail-closed: an undecidable durable row keeps BOTH copies.
func TestReconcileParkedArbitrationLookupErrorKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-3", "a1.b2", "gen", "committed", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, assert.AnError)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 1, healed, "only the marker sweep")
	_, cErr := fs.Stat(filepath.Join(dir, "AR-3.jpg"))
	assert.NoError(t, cErr, "canonical kept")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-3.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr, "backup kept")
}

// codex cloud P1: chained crashed ops unwind NEWEST-first — otherwise the
// older backup would re-restore over the newer op's rewind.
func TestReconcileParkedChainUnwindsNewestFirst(t *testing.T) {
	fs, dir := witnessFixture(t)
	// op A parked the original then crashed after generating; op B parked A's
	// bytes then crashed after generating — canon currently holds B's output.
	seedArbitrationScene(t, fs, dir, "CH-1", "1000.1", "genB", "original", 3)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "CH-1.jpg.rsbak.2000.1"), []byte("genA"), 0o644))
	metaB, err := json.Marshal(inFlightMeta{PosterID: "CH-1", PrevRevision: 3})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-CH-1.2000.1"), metaB, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "CH-1", 3), nil).Times(2)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 4, healed, "2 marker sweeps + 2 restores")
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "CH-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "original", string(got), "stack unwind reaches the last committed bytes")
	for _, n := range []string{"CH-1.jpg.rsbak.1000.1", "CH-1.jpg.rsbak.2000.1"} {
		_, bErr := fs.Stat(filepath.Join(dir, n))
		assert.Error(t, bErr, "%s consumed", n)
	}
}

// Legacy .dlbak inline handling retains its arms after the rsbak arbitration
// split: remove happy path, wedged remove, indeterminate canon, wedged restore.
func TestReconcileDlbakLegacyBranches(t *testing.T) {
	// happy: canon present → dlbak is litter, removed
	fs1, dir1 := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs1, filepath.Join(dir1, "LD-1.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs1, filepath.Join(dir1, "LD-1.jpg.dlbak"), []byte("stale"), 0o644))
	cl1 := &TempDirCleaner{fs: fs1, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 1, cl1.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir1))
	_, e1 := fs1.Stat(filepath.Join(dir1, "LD-1.jpg.dlbak"))
	assert.Error(t, e1, "dlbak litter removed when canon present")

	// wedged remove → kept
	fs2, dir2 := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs2, filepath.Join(dir2, "LD-2.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs2, filepath.Join(dir2, "LD-2.jpg.dlbak"), []byte("stale"), 0o644))
	cl2 := &TempDirCleaner{fs: selectiveFailRemoveFS{Fs: fs2, failSuffix: ".dlbak"}, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl2.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir2))
	_, e2 := fs2.Stat(filepath.Join(dir2, "LD-2.jpg.dlbak"))
	assert.NoError(t, e2, "wedged remove keeps the dlbak")

	// indeterminate canon (transient stat error) → kept both
	fs3, dir3 := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs3, filepath.Join(dir3, "LD-3.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs3, filepath.Join(dir3, "LD-3.jpg.dlbak"), []byte("stale"), 0o644))
	cl3 := &TempDirCleaner{fs: statFailSuffixFS{Fs: fs3, suffix: "LD-3.jpg"}, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl3.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir3))
	_, e3 := fs3.Stat(filepath.Join(dir3, "LD-3.jpg.dlbak"))
	assert.NoError(t, e3, "indeterminate canon keeps the dlbak")

	// canon missing + wedged restore rename → kept
	fs4, dir4 := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs4, filepath.Join(dir4, "LD-4-full.jpg.dlbak"), []byte("committed"), 0o644))
	cl4 := &TempDirCleaner{fs: &seqRenameFailFS{Fs: fs4, failOn: map[int]bool{1: true}}, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl4.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir4))
	_, e4 := fs4.Stat(filepath.Join(dir4, "LD-4-full.jpg.dlbak"))
	assert.NoError(t, e4, "wedged restore keeps the dlbak")
}

// Equal-nonce-time ties break on the sequence part — newest op still first.
func TestReconcileParkedChainNonceSeqTieBreak(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "CH-2", "a1.1", "genB", "original", 3)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "CH-2.jpg.rsbak.a1.2"), []byte("genA"), 0o644))
	metaB, err := json.Marshal(inFlightMeta{PosterID: "CH-2", PrevRevision: 3})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-CH-2.a1.2"), metaB, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "CH-2", 3), nil).Times(2)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 4, healed)
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "CH-2.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "original", string(got), "seq tie-break still unwinds the stack in order")
}

// A stranded sentinel whose payload ID doesn't match the backup's owner keeps
// everything — provenance is evidence only for its own noun.
func TestReconcileParkedArbitrationProvenanceMismatchKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-9.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-9.jpg.rsbak.a9.c9"), []byte("backup"), 0o644))
	meta, err := json.Marshal(inFlightMeta{PosterID: "OTHER-1", PrevRevision: 4})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-AR-9.a9.c9"), meta, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t) // zero expectations: mismatch precedes any lookup
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "marker sweep only")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-9.jpg.rsbak.a9.c9"))
	assert.NoError(t, bErr, "mismatched provenance never moves bytes")
}

// Committed arbitration also covers the -full.jpg leg's base trimming.
func TestReconcileParkedArbitrationCommittedFullLeg(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "ARF-9-full.jpg"), []byte("gen-committed"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "ARF-9-full.jpg.rsbak.aa.bb"), []byte("pre-op"), 0o644))
	meta, err := json.Marshal(inFlightMeta{PosterID: "ARF-9", PrevRevision: 4})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-ARF-9.aa.bb"), meta, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "ARF-9", 8), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 2, healed, "marker sweep + committed backup drop")
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "ARF-9-full.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "gen-committed", string(got))
}

// Committed-drop with a wedged Remove keeps the backup for the next startup.
func TestReconcileParkedCommittedRemoveFailKeepsBackup(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-5", "a1.b2", "gen-committed", "pre-op", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-5", 8), nil)
	cl := &TempDirCleaner{fs: selectiveFailRemoveFS{Fs: fs, failSuffix: ".rsbak.a1.b2"}, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "marker swept; backup kept on wedged remove")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-5.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
}

// Uncommitted-restore with a wedged Rename keeps stranded gen AND the backup.
func TestReconcileParkedUncommittedRestoreFailKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-6", "a1.b2", "gen-uncommitted", "committed", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-6", 4), nil)
	cl := &TempDirCleaner{fs: &seqRenameFailFS{Fs: fs, failOn: map[int]bool{1: true}}, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "marker swept; restore wedged")
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "AR-6.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "gen-uncommitted", string(got), "canon untouched when restore wedges")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-6.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
}

// Nil repository: arbitration is undecidable by construction → keep both.
func TestReconcileParkedArbitrationNilRepoKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-7", "a1.b2", "gen", "committed", 4)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "marker sweep only")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-7.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
}

// An undecodable job envelope is undecidable evidence → keep both.
func TestReconcileParkedArbitrationGarbageResultsKeepBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-8", "a1.b2", "gen", "committed", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: "{not-json"}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "marker sweep only")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-8.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
}

// Row scanning skips nil rows and non-matching IDs and takes the match-info
// fallback when Movie is absent — the committed decision still stands.
func TestReconcileParkedArbitrationRowScanArms(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-9", "a1.b2", "gen-committed", "pre-op", 4)
	res := map[string]*resultstore.MovieResult{
		"/f/nilrow.mp4": nil,
		"/f/other.mp4":  {ResultID: "res-o", Revision: 7, Movie: &models.Movie{ID: "OTH-9"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/other.mp4", MovieID: "OTH-9"}},
		"/f/tgt.mp4":    {ResultID: "res-t", Revision: 6, Status: models.JobStatusCompleted, Movie: nil, FileMatchInfo: models.FileMatchInfo{Path: "/f/tgt.mp4", MovieID: "AR-9"}},
	}
	payload, err := json.Marshal(res)
	require.NoError(t, err)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: string(payload)}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 2, healed, "marker sweep + committed drop via match-info fallback")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-9.jpg.rsbak.a1.b2"))
	assert.Error(t, bErr, "backup dropped — revision moved past capture")
} // codex cloud P1: unprovenanced legacy backups are never deleted under a live
// canonical again.
func TestReconcileParkedNoProvenanceKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NP-1.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NP-1.jpg.rsbak.a1.b2"), []byte("backup"), 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t) // zero expectations: never consulted without provenance
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 0, healed)
	got, err := afero.ReadFile(fs, filepath.Join(dir, "NP-1.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "live", string(got))
	_, bErr := fs.Stat(filepath.Join(dir, "NP-1.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr, "backup kept — no deletion without op provenance")
}
