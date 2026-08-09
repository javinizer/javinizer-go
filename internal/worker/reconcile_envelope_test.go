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
		fullBak: "/tmp/posters/JM/NO-1-full.jpg.rsbak.a1.b2",
		cropBak: "/tmp/posters/JM/NO-1.jpg.rsbak.a1.b2",
		hadFull: true,
		hadCrop: true,
	}
	b.restore(nil) // both baks missing → skip quietly (stat-miss continue arc)
}

// audit F-R4-5: sweep warn arcs — Remove wedge keeps the litter in place;
// restore rename wedge keeps the parked copy for the next startup retry.
func TestReconcileParkedPosterBackupsWarns(t *testing.T) {
	base, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "WP-1.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "WP-1.jpg.rsbak.a1.b2"), []byte("stale"), 0o644))
	cl := &TempDirCleaner{fs: removeFailFS{Fs: base}, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir))
	_, err := base.Stat(filepath.Join(dir, "WP-1.jpg.rsbak.a1.b2"))
	assert.NoError(t, err, "wedged remove keeps the litter")

	base2, dir2 := witnessFixture(t)
	require.NoError(t, afero.WriteFile(base2, filepath.Join(dir2, "WP-2-2-2-full.jpg.rsbak.a1.b2"), []byte("committed"), 0o644))
	cl2 := &TempDirCleaner{fs: &seqRenameFailFS{Fs: base2, failOn: map[int]bool{1: true}}, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl2.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir2))
	_, err2 := base2.Stat(filepath.Join(dir2, "WP-2-2-2-full.jpg.rsbak.a1.b2"))
	assert.NoError(t, err2, "wedged restore keeps the parked copy")
}

// codex P2: a transient canonical stat error keeps BOTH copies (never rename
// over the unknown).
func TestReconcileParkedTransientCanonStatKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "TS-1.jpg"), []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "TS-1.jpg.rsbak.a1.b2"), []byte("parked"), 0o644))
	wedged := statFailSuffixFS{Fs: fs, suffix: "TS-1.jpg"}
	cl := &TempDirCleaner{fs: wedged, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir))
	_, err := fs.Stat(filepath.Join(dir, "TS-1.jpg"))
	assert.NoError(t, err, "canonical kept")
	_, err2 := fs.Stat(filepath.Join(dir, "TS-1.jpg.rsbak.a1.b2"))
	assert.NoError(t, err2, "parked kept")
}

// audit F-R5-3: dotted poster ids containing ".dlbak"/".rsbak." are live
// canonical files, never parked backups — the anchored parse must leave them
// alone, and witness FILES must never be reclassified either.
func TestReconcileParkedDottedIDsNotMisparsed(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "foo.dlbak.jpg"), []byte("live-canon"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "x.rsbak.y-full.jpg"), []byte("live-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-see.dlbak.json"), []byte("{bad-json"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed, "no dotted names reclassified")
	for _, n := range []string{"foo.dlbak.jpg", "x.rsbak.y-full.jpg", ".promote-see.dlbak.json"} {
		_, err := fs.Stat(filepath.Join(dir, n))
		assert.NoError(t, err, "%s survives", n)
	}
	_, err := fs.Stat(filepath.Join(dir, "foo"))
	assert.Error(t, err, "no bogus canon created")
}

// audit F-R5-2: a poster with ANY unresolved witness is skipped by the sweep
// entirely — the arbitrators own those bytes.
func TestReconcileParkedSkipsWitnessedPosters(t *testing.T) {
	fs, dir := witnessFixture(t)
	// parked stale litter + live canon + unresolved promote witness
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg"), []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-1.jpg.rsbak.a1.b2"), []byte("stale"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-PI-1.json"), []byte("{}"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed, "witnessed poster swept neither way")
	_, err := fs.Stat(filepath.Join(dir, "PI-1.jpg.rsbak.a1.b2"))
	assert.NoError(t, err, "parked copy kept until arbitration resolves")
	got, _ := afero.ReadFile(fs, filepath.Join(dir, "PI-1.jpg"))
	assert.Equal(t, "current", string(got))
}

// The crop-witness belt reads poster IDs from witness CONTENT (content
// authority, not filename).
func TestReconcileParkedSkipsCropWitnessedPoster(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PI-9.jpg.rsbak.a1.b2"), []byte("stale"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-PI-9.crop-x.json"), []byte("{\"poster_id\":\"PI-9\"}"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir))
	_, err := fs.Stat(filepath.Join(dir, "PI-9.jpg.rsbak.a1.b2"))
	assert.NoError(t, err)
	// corrupt crop witness: unreadable content belt skipped (no fence), parked healed
	require.NoError(t, fs.Remove(filepath.Join(dir, ".crop-PI-9.crop-x.json")))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-PI-9.crop-y.json"), []byte("{corrupt"), 0o644))
	assert.Equal(t, 1, cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir), "corrupt witness does not fence")
}

// audit F-R6-1: a parked leg under the NEW id of a pending rekey witness is
// swept no more than an OLD-side one — the belt reads witness CONTENT.
func TestReconcileParkedBeltCoversRekeyNewID(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-1.jpg.rsbak.a1.b2"), []byte("stale"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-1.json"), []byte("{\"old_id\":\"OLD-1\",\"new_id\":\"NEW-1\"}"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir), "NEW-side parked leg fenced by witness content")
	_, err := fs.Stat(filepath.Join(dir, "NEW-1.jpg.rsbak.a1.b2"))
	assert.NoError(t, err, "not swept, not re-homed while witness pends")

	// Corrupt payload → filename-derived OLD fence still applies (legacy parity)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "OLD-2.jpg.rsbak.a1.b2"), []byte("stale2"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-2.jpg.rsbak.a1.b2"), []byte("stale3"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-2.json"), []byte("{not-json"), 0o644))
	assert.Equal(t, 1, cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir), "corrupt witness: NEW-side heals, OLD-side fenced by name")
	_, err2 := fs.Stat(filepath.Join(dir, "OLD-2.jpg.rsbak.a1.b2"))
	assert.NoError(t, err2, "OLD-side fenced by filename")
	_, err3 := fs.Stat(filepath.Join(dir, "NEW-2.jpg"))
	assert.NoError(t, err3, "NEW-side restored (content unknown ⇒ no fence)")
}

func TestIsBackupNonce(t *testing.T) {
	assert.True(t, isBackupNonce("a1.b2"))
	assert.True(t, isBackupNonce("19c34abc.1f"))
	assert.False(t, isBackupNonce("abc"))
	assert.False(t, isBackupNonce("a1.b2."))
	assert.False(t, isBackupNonce(".rsbak"))
	assert.False(t, isBackupNonce("G1.b2"))
	assert.False(t, isBackupNonce("a1.B2"))
	assert.False(t, isBackupNonce("a1.b2.c3"))
}

// Belt edge branches: bad-escape promote names fence by raw base; unreadable
// crop witnesses skip their content probe without fencing anything.
func TestReconcileParkedWitnessBeltEdgeBranches(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-%zz.json"), []byte("{}"), 0o644))              // invalid escape
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-x.json"), []byte("not-json"), 0o644))             // unreadable content
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".crop-y.json"), []byte("{\"poster_id\":\"\"}"), 0o644)) // empty id
	// valid parked target for raw-named promote fence: base "%zz" after unescape fails → raw key "%zz"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "%zz.jpg.rsbak.a1.b2"), []byte("stale"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "OTHER-1.jpg.rsbak.a1.b2"), []byte("restore"), 0o644))
	// unreadable crop witness: belt content-scan errors out → continue, NO fence
	fsFailingRead := openFailSuffixFS{Fs: fs, suffix: ".crop-y.json"}
	clFail := &TempDirCleaner{fs: fsFailingRead, tempDir: "/tmp", jobRepo: nil}
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RD-1.jpg.rsbak.a1.b2"), []byte("restore"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 2, healed, "only the unwitnessed parked legs heal")
	clFail.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir) // exercises the read-fail continue arc
	_, err := fs.Stat(filepath.Join(dir, "OTHER-1.jpg"))
	assert.NoError(t, err)
	_, err2 := fs.Stat(filepath.Join(dir, "%zz.jpg.rsbak.a1.b2"))
	assert.NoError(t, err2, "escaped-promote witness (%zz base) fences the sweep")
}

// audit F-R21-1: parked legs of a leading-dot ID carry .rsbak.<nonce> tails —
// they must never be misread as in-flight sentinels and swept instead of
// re-homed. The marker branch refuses names that ALSO parse as park legs.
func TestLeadingDotIDParkedLegsRehomedNotMisSwept(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-EVIL-full.jpg.rsbak.a1.b2"), []byte("parked-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-EVIL.jpg.rsbak.a1.b2"), []byte("parked-crop"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-EVIL.a1.b2"), []byte("{}"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 3, healed, "2 parked legs re-homed + 1 marker swept")
	got, err := afero.ReadFile(fs, filepath.Join(dir, ".inflight-EVIL.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "parked-crop", string(got), "parked crop re-homed")
	got2, err2 := afero.ReadFile(fs, filepath.Join(dir, ".inflight-EVIL-full.jpg"))
	require.NoError(t, err2)
	assert.Equal(t, "parked-full", string(got2), "parked full re-homed")
	_, sErr := fs.Stat(filepath.Join(dir, ".inflight-EVIL-full.jpg.rsbak.a1.b2"))
	assert.Error(t, sErr, "parked names consumed")
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-EVIL.a1.b2"))
	assert.Error(t, mErr, "marker swept")
}

// audit F-R4-5: startup reconciliation re-homes parked backup legs stranded
// by a crash/panic between park and restore.
func TestReconcileParkedPosterBackups(t *testing.T) {
	fs, dir := witnessFixture(t)
	// canonical missing → restore parked bytes
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RK-1.jpg.rsbak.a1.b2"), []byte("committed-crop"), 0o644))
	// canonical present but zero op provenance → BOTH kept (codex cloud P1:
	// canonical presence alone never justifies deleting committed backup bytes)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RK-2.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RK-2.jpg.rsbak.a1.b2"), []byte("stale"), 0o644))
	// legacy plain .dlbak (pre-nonce manager parks) handled too
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "RK-3-full.jpg.dlbak"), []byte("committed-full"), 0o644))
	// unrelated files untouched
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "README.txt"), []byte("x"), 0o644))

	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 2, healed, "restores only — the unprovenanced backup is kept, not deleted")
	got, _ := afero.ReadFile(fs, filepath.Join(dir, "RK-1.jpg"))
	assert.Equal(t, "committed-crop", string(got), "stranded crop restored")
	got, _ = afero.ReadFile(fs, filepath.Join(dir, "RK-3-full.jpg"))
	assert.Equal(t, "committed-full", string(got), "legacy .dlbak restored")
	live, _ := afero.ReadFile(fs, filepath.Join(dir, "RK-2.jpg"))
	assert.Equal(t, "live", string(live), "present canonical untouched")
	_, parkErr := fs.Stat(filepath.Join(dir, "RK-2.jpg.rsbak.a1.b2"))
	assert.NoError(t, parkErr, "unprovenanced backup kept pending arbitration")
	readme, _ := afero.ReadFile(fs, filepath.Join(dir, "README.txt"))
	assert.Equal(t, "x", string(readme), "unrelated files untouched")

	// dir read error → no-op zero
	cl2 := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	assert.Equal(t, 0, cl2.reconcileParkedPosterBackups(context.Background(), "JOB-W1", "/nonexistent"))
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
