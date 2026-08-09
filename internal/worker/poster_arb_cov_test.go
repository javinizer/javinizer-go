package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// codex cloud P2: the sweep survives transient removal wedges (flaky then
// ok) and reports permanent ones after the attempt budget.
type flakyRemoveFS struct {
	afero.Fs
	name       string
	failFirst  int
	call       int
	stuckError error
}

func (f *flakyRemoveFS) Remove(n string) error {
	if filepath.ToSlash(n) == filepath.ToSlash(f.name) {
		f.call++
		if f.failFirst > 0 {
			f.failFirst--
			return errors.New("transient wedge")
		}
		if f.stuckError != nil {
			return f.stuckError
		}
	}
	return f.Fs.Remove(n)
}

func TestRemoveWithRetry(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/tmp/x.json", []byte("{}"), 0o644))

	// transient wedge — succeeds inside the budget:
	flaky := &flakyRemoveFS{Fs: base, name: "/tmp/x.json", failFirst: 2}
	require.NoError(t, removeWithRetry(flaky, "/tmp/x.json"))
	_, err := base.Stat("/tmp/x.json")
	require.Error(t, err, "file gone after transient wedges healed")

	// permanent wedge — surface after attempts:
	require.NoError(t, afero.WriteFile(base, "/tmp/y.json", []byte("{}"), 0o644))
	stuck := &flakyRemoveFS{Fs: base, name: "/tmp/y.json", stuckError: errors.New("permanent wedge")}
	require.ErrorContains(t, removeWithRetry(stuck, "/tmp/y.json"), "permanent wedge")
}

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

// Foreign token + FULL-leg canon match: tokenLegSHA's full-leg select path,
// and the whole drop.
func TestReconcileParkedForeignTokenFullLegCanonMatch(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "XL-9-full.jpg"), []byte("gen-committed"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "XL-9-full.jpg.rsbak.a3.d4"), []byte("orig"), 0o644))
	meta, err := json.Marshal(inFlightMeta{PosterID: "XL-9", PrevRevision: 4})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-XL-9.a3.d4"), meta, 0o644))
	seedCommitToken(t, fs, dir, "XL-9", "a2.c2", "gen-committed", true)
	seedStrandedSentinel(t, fs, dir, "XL-9", "a2.c2", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "XL-9", 9), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 4, healed, "both markers swept + drop + token sweep")
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "XL-9-full.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "gen-committed", string(got))
	_, bErr := fs.Stat(filepath.Join(dir, "XL-9-full.jpg.rsbak.a3.d4"))
	assert.Error(t, bErr, "obsolete backup dropped via full-leg token leg")
}

// removeExactFailFS wedges one exact filename's removal.// codex cloud P1: own token proves only the IN-MEMORY commit — when the
// durable row never advanced (envelope write lost), the canonical bytes
// belong to nothing durable; restore the parked last-committed pair.
func TestReconcileParkedOwnTokenUnpersistedRestores(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-U", "a1.b2", "gen-unpersisted", "committed", 4)
	seedCommitToken(t, fs, dir, "AR-U", "a1.b2", "gen-unpersisted", false)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-U", 4), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 3, healed, "marker sweep + restore + settled token sweep")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "AR-U.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "committed", string(got), "canon rewound — envelope never persisted")
}

// Own token but the durable row is unreadable: undecidable ⇒ keep both.// Own token but the durable row is unreadable: undecidable ⇒ keep both.
// codex cloud P2: a wedged finalize rescan must skip ALL finalization —
// otherwise the sweeps delete provenance while a failed arbitration repair
// still pends in the same dir.
func TestFinalizeRescanUndecidableKeepsRecords(t *testing.T) {
	mem := afero.NewMemMapFs()
	dir := "/tmp/posters/J-FIN"
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "FB-1.jpg"), []byte("live"), 0o644))
	meta, err := json.Marshal(inFlightMeta{PosterID: "FB-1", PrevRevision: 2})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, ".inflight-FB-1.a1.b2"), meta, 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "FB-1.jpg.rsbak.a1.b2"), []byte("backup"), 0o644))
	// main sweep's ReadDir passes; the finalize rescan wedges.
	fs := &openFailAfterNFS{Fs: mem, suffix: dir, allow: 1}
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	_ = cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	_, mErr := mem.Stat(filepath.Join(dir, ".inflight-FB-1.a1.b2"))
	assert.NoError(t, mErr, "marker retained — finalization was undecidable")
	_, bErr := mem.Stat(filepath.Join(dir, "FB-1.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr, "backup retained")
}

// codex cloud P2: the token sweep's pending-base compare must fold — a
// case-variant pending backup is the SAME contested base.
func TestTokenSweepFoldsPendingBase(t *testing.T) {
	fs, dir := witnessFixture(t)
	// The RIVAL's spelling owns this dir — canon lowercase so the pending leg
	// stays un-settle-able this run (unprovenanced keep-both), while the
	// winner's folded base still matches on sweep.
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "wta-1.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "wta-1.jpg.rsbak.a9.d9"), []byte("pending"), 0o644))
	seedCommitToken(t, fs, dir, "WTA-1", "a1.b2", "live", false)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed, "folded pending base retains the token")
	_, tErr := fs.Stat(filepath.Join(dir, ".commit-WTA-1.a1.b2"))
	assert.NoError(t, tErr)
}

// codex cloud P1: a foreign commit token with an un-persisted winner's state
// (its sentinel baseline NOT advanced past the durable row) is zero evidence —
// the bytes may be in-memory-only output; both copies stay on disk.
func TestReconcileParkedForeignTokenUntrustedWithoutDurability(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-U2", "a3.d4", "genA", "pre-op", 4)
	seedCommitToken(t, fs, dir, "AR-U2", "a2.c2", "genA", false)
	// winner's sentinel says prev=4; durable row STILL at 4 → never persisted.
	seedStrandedSentinel(t, fs, dir, "AR-U2", "a2.c2", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-U2", 4), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "winner marker settles cleanly; nothing else moves")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "AR-U2.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "genA", string(got), "canon untouched")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-U2.jpg.rsbak.a3.d4"))
	assert.NoError(t, bErr, "backup kept")
	_, tErr := fs.Stat(filepath.Join(dir, ".commit-AR-U2.a2.c2"))
	assert.NoError(t, tErr, "token retained while its provenance stays pending")
}

// Trusted-lane wedges keep both copies — drop/restore faults never land bytes.
func TestReconcileParkedTrustedLaneWedgesKeepBoth(t *testing.T) {
	t.Run("canon-proven drop wedged", func(t *testing.T) {
		fs, dir := witnessFixture(t)
		seedArbitrationScene(t, fs, dir, "AR-TW1", "a3.d4", "winbytes", "orig", 4)
		seedCommitToken(t, fs, dir, "AR-TW1", "a2.c2", "winbytes", false)
		seedStrandedSentinel(t, fs, dir, "AR-TW1", "a2.c2", 4)
		repo := mocks.NewMockJobRepositoryInterface(t)
		repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-TW1", 9), nil)
		cl := &TempDirCleaner{fs: selectiveFailRemoveFS{Fs: fs, failSuffix: ".rsbak.a3.d4"}, tempDir: "/tmp", jobRepo: repo}
		healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
		_, bErr := fs.Stat(filepath.Join(dir, "AR-TW1.jpg.rsbak.a3.d4"))
		assert.NoError(t, bErr, "backup kept — drop wedged")
		_ = healed
	})

	t.Run("backup-recovery restore wedged", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dir := "/tmp/posters/J-TW2"
		require.NoError(t, base.MkdirAll(dir, 0o755))
		require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "AR-TW2.jpg"), []byte("gen-lost"), 0o644))
		require.NoError(t, afero.WriteFile(base, filepath.Join(dir, "AR-TW2.jpg.rsbak.a1.b2"), []byte("winbytes"), 0o644))
		seedStrandedSentinel(t, base, dir, "AR-TW2", "a1.b2", 4)
		seedCommitToken(t, base, dir, "AR-TW2", "a2.c2", "winbytes", false)
		seedStrandedSentinel(t, base, dir, "AR-TW2", "a2.c2", 4)
		repo := mocks.NewMockJobRepositoryInterface(t)
		repo.EXPECT().FindByID(mock.Anything, "TW2").Return(arbJobRow(t, "AR-TW2", 9), nil)
		fs := &seqRenameFailFS{Fs: base, failOn: map[int]bool{1: true}}
		cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
		healed := cl.reconcileParkedPosterBackups(context.Background(), "TW2", dir)
		got, _ := afero.ReadFile(base, filepath.Join(dir, "AR-TW2.jpg"))
		assert.Equal(t, "gen-lost", string(got), "canon untouched when restore wedges")
		_ = healed
	})
}

// codex cloud P2 (@822): first-token-match is not enough when commits stack.
// With an older loser's token present alongside the winner's, arbitration must
// accept ANY same-base token proving the backup holds the winner's bytes.
func TestReconcileParkedMultipleForeignTokensArbitrateByAnyMatch(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-MX", "a3.d4", "lostgen", "winbytes", 4)
	seedCommitToken(t, fs, dir, "AR-MX", "a1.c2", "oldgen", false)
	seedStrandedSentinel(t, fs, dir, "AR-MX", "a1.c2", 4)
	seedCommitToken(t, fs, dir, "AR-MX", "a2.c2", "winbytes", false)
	seedStrandedSentinel(t, fs, dir, "AR-MX", "a2.c2", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-MX", 9), nil).Times(2)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 6, healed, "parked marker + restore + both winner markers + BOTH settled tokens")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "AR-MX.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "winbytes", string(got), "winner's committed bytes restored via the matching token")
}

// tokenLegSHA skip arm: a foreign token WITHOUT this leg's SHA is skipped as
// arbitration evidence (empty SHA ⇒ no evidence, never "match").
func TestReconcileParkedTokenMissingLegSHAIsSkippedEvidence(t *testing.T) {
	fs, dir := witnessFixture(t)
	// scene: canon carries winner bytes; backup is loser's; foreign token has
	// ONLY the full-leg SHA — crop-leg arbitration evidence lacks entirely.
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-SK.jpg"), []byte("win"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-SK.jpg.rsbak.a3.d4"), []byte("lost"), 0o644))
	meta, err := json.Marshal(inFlightMeta{PosterID: "AR-SK", PrevRevision: 4})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-AR-SK.a3.d4"), meta, 0o644))
	seedCommitToken(t, fs, dir, "AR-SK", "a2.c2", "fulllegonly", true)
	seedStrandedSentinel(t, fs, dir, "AR-SK", "a2.c2", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-SK", 9), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "the token's own marker sweeps; ambiguity retains the bytes")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-SK.jpg.rsbak.a3.d4"))
	assert.NoError(t, bErr, "backup kept — the token cannot prove this leg")
}

func TestReconcileParkedOwnTokenLookupUndecidableKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-V", "a1.b2", "gen", "pre-op", 4)
	seedCommitToken(t, fs, dir, "AR-V", "a1.b2", "gen", false)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, assert.AnError)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed, "undecidable ⇒ token+backup+marker all persist")
}

// Own token + durable row unmoved (envelope never persisted): the restore is
// required — and a wedged restore keeps everything for the next startup.
func TestReconcileParkedOwnTokenUnpersistedRestoreWedgedKeeps(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-W", "a1.b2", "gen", "pre-op", 4)
	seedCommitToken(t, fs, dir, "AR-W", "a1.b2", "gen", false)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-W", 4), nil)
	cl := &TempDirCleaner{fs: &seqRenameFailFS{Fs: fs, failOn: map[int]bool{1: true}}, tempDir: "/tmp", jobRepo: repo}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed)
	_, bErr := fs.Stat(filepath.Join(dir, "AR-W.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-AR-W.a1.b2"))
	assert.NoError(t, mErr)
}

// removeExactFailFS wedges one exact filename's removal.
type removeExactFailFS struct {
	afero.Fs
	name string
}

func (f removeExactFailFS) Remove(n string) error {
	if filepath.ToSlash(n) == filepath.ToSlash(f.name) || strings.HasSuffix(filepath.ToSlash(n), f.name) {
		return errors.New("remove wedged")
	}
	return f.Fs.Remove(n)
}

// seedStrandedSentinel writes the op's stranded in-flight marker so token
// vetting has provenance: content carries prev_revision, name carries nonce.
func seedStrandedSentinel(t *testing.T, fs afero.Fs, dir, id, nonce string, prevRev uint64) {
	t.Helper()
	meta, err := json.Marshal(inFlightMeta{PosterID: id, PrevRevision: prevRev})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-"+id+"."+nonce), meta, 0o644))
}

// seedCommitToken fabricates the op-attributed commit marker startup
// arbitration now uses to distinguish the winning rescrape (codex cloud P1).
func seedCommitToken(t *testing.T, fs afero.Fs, dir, id, nonce, content string, fullLeg bool) {
	t.Helper()
	meta := commitMeta{PosterID: id}
	sha := shaContentHex([]byte(content))
	if fullLeg {
		meta.FullSHA = sha
	} else {
		meta.CropSHA = sha
	}
	payload, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".commit-"+id+"."+nonce), payload, 0o644))
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
	// Own token + durable advance ⇒ drop.
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-2", 9), nil)
	seedCommitToken(t, fs, dir, "AR-2", "a1.b2", "gen-committed", false)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 3, healed, "marker sweep + token-attributed backup drop + settled token sweep")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "AR-2.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "gen-committed", string(got), "committed canonical untouched")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-2.jpg.rsbak.a1.b2"))
	assert.Error(t, bErr, "backup dropped — this op's own token proves the commit")
	_, tErr := fs.Stat(filepath.Join(dir, ".commit-AR-2.a1.b2"))
	assert.Error(t, tErr, "token swept once nothing pends")
}

// codex cloud P1 fail-closed: an undecidable durable row keeps BOTH copies.
func TestReconcileParkedArbitrationLookupErrorKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-3", "a1.b2", "gen", "committed", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(nil, assert.AnError)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 0, healed, "nothing settles — legs keep their marker (P2)")
	_, cErr := fs.Stat(filepath.Join(dir, "AR-3.jpg"))
	assert.NoError(t, cErr, "canonical kept")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-3.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr, "backup kept")
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-AR-3.a1.b2"))
	assert.NoError(t, mErr, "provenance retained for the next startup")
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
	assert.Equal(t, 0, healed, "mismatch settles nothing")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-9.jpg.rsbak.a9.c9"))
	assert.NoError(t, bErr, "mismatched provenance never moves bytes")
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-AR-9.a9.c9"))
	assert.NoError(t, mErr, "marker retained while the leg pends")
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
	seedCommitToken(t, fs, dir, "ARF-9", "aa.bb", "gen-committed", true)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 3, healed, "marker sweep + token-attributed drop + token sweep")
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
	assert.Equal(t, 0, healed, "backup kept on wedged remove — and so is its marker")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-5.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-AR-5.a1.b2"))
	assert.NoError(t, mErr, "provenance retained while legs pend")
}

// Uncommitted-restore with a wedged Rename keeps stranded gen AND the backup.
func TestReconcileParkedUncommittedRestoreFailKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-6", "a1.b2", "gen-uncommitted", "committed", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-6", 4), nil)
	cl := &TempDirCleaner{fs: &seqRenameFailFS{Fs: fs, failOn: map[int]bool{1: true}}, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed, "restore wedged — marker retained with the leg")
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
	assert.Equal(t, 0, healed, "undecidable — everything including the marker kept")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-7.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-AR-7.a1.b2"))
	assert.NoError(t, mErr)
}

// An undecodable job envelope is undecidable evidence → keep both.
func TestReconcileParkedArbitrationGarbageResultsKeepBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-8", "a1.b2", "gen", "committed", 4)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: "{not-json"}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed, "garbage results — nothing settles, marker retained")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-8.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
}

type openSentryFailFS struct{ afero.Fs }

func (f openSentryFailFS) Open(name string) (afero.File, error) {
	if strings.Contains(filepath.ToSlash(name), ".inflight-") {
		return nil, errors.New("sentry read wedged")
	}
	return f.Fs.Open(name)
}

// codex cloud P2: a marker we could not even READ may carry the legs'
// arbitration record — legs AND marker are retained; a later startup with a
// readable marker still arbitrates to a decision.
func TestReconcileParkedMarkerReadFailRetainsProvenance(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JOB-W1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	seedArbitrationScene(t, base, dir, "TR-1", "a1.b2", "gen-uncommitted", "committed", 4)

	// First startup: marker unreadable this run.
	wedged := openSentryFailFS{Fs: base}
	repo0 := mocks.NewMockJobRepositoryInterface(t) // zero expectations: lookup never reached
	cl0 := &TempDirCleaner{fs: wedged, tempDir: "/tmp", jobRepo: repo0}
	healed0 := cl0.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed0, "unreadable marker: nothing settles")
	_, mErr := base.Stat(filepath.Join(dir, ".inflight-TR-1.a1.b2"))
	assert.NoError(t, mErr, "marker retained — it IS the legs' provenance record")
	_, bErr := base.Stat(filepath.Join(dir, "TR-1.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr, "backup retained")

	// Later startup: marker readable again — arbitration lands.
	repo1 := mocks.NewMockJobRepositoryInterface(t)
	repo1.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "TR-1", 4), nil)
	cl1 := &TempDirCleaner{fs: base, tempDir: "/tmp", jobRepo: repo1}
	healed1 := cl1.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 2, healed1, "marker sweep + uncommitted restore")
	got, rerr := afero.ReadFile(base, filepath.Join(dir, "TR-1.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "committed", string(got), "readable marker finally arbitrates the leg")
	_, mErr2 := base.Stat(filepath.Join(dir, ".inflight-TR-1.a1.b2"))
	assert.Error(t, mErr2, "settled legs release their marker")
}

// A-wins/B-crashes: the foreign commit token's SHA matches THIS backup's
// content — restoring it recovers the winner's committed bytes from under
// the loser's stranded generation (codex cloud P1 attribution).
func TestReconcileParkedForeignTokenRestoresCommittedBytes(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-C", "a1.b2", "genB", "genA", 4)
	seedCommitToken(t, fs, dir, "AR-C", "a2.c2", "genA", false) // winner A's token
	seedStrandedSentinel(t, fs, dir, "AR-C", "a2.c2", 4)        // its provenance — durable-advance check passes
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-C", 9), nil) // token decides — no lookup needed for attribution
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 4, healed, "parked marker sweep + restore + winner marker sweep + token sweep")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "AR-C.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "genA", string(got), "winner's committed bytes recovered")
}

// B-wins: the foreign token's SHA matches the CANON — family is whole; the
// stranded loser's backup is obsolete.
func TestReconcileParkedForeignTokenCanonMatchDropsBackup(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-D", "a3.d4", "genB", "orig", 4)
	seedCommitToken(t, fs, dir, "AR-D", "a2.c2", "genB", false)
	seedStrandedSentinel(t, fs, dir, "AR-D", "a2.c2", 4) // FIXME
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-D", 9), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 4, healed, "parked marker sweep + drop + winner marker sweep + token sweep")
	got, err := afero.ReadFile(fs, filepath.Join(dir, "AR-D.jpg"))
	require.NoError(t, err)
	assert.Equal(t, "genB", string(got))
	_, bErr := fs.Stat(filepath.Join(dir, "AR-D.jpg.rsbak.a3.d4"))
	assert.Error(t, bErr, "loser's backup dropped — canon carries the winner's bytes")
}

// A commit exists but matches neither the canon nor the backup — unwind order
// decides this leg; keep both rather than guess.
func TestReconcileParkedAmbiguousContentKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-E", "a3.d4", "other-gen", "other-bak", 4)
	seedCommitToken(t, fs, dir, "AR-E", "a2.c2", "committed-bytes", false)
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)

	assert.Equal(t, 0, healed, "ambiguous — nothing settles this run")
	for _, n := range []string{"AR-E.jpg.rsbak.a3.d4", ".commit-AR-E.a2.c2", ".inflight-AR-E.a3.d4"} {
		_, sErr := fs.Stat(filepath.Join(dir, n))
		assert.NoError(t, sErr, "%s retained", n)
	}
}

// A commit token with NO pending legs is settled evidence — sweep it.
func TestReconcileCommitTokenSweepsWhenNothingPends(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-Z.jpg"), []byte("live"), 0o644))
	seedCommitToken(t, fs, dir, "AR-Z", "a9.c9", "live", false)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}

	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "orphan token swept")
	_, tErr := fs.Stat(filepath.Join(dir, ".commit-AR-Z.a9.c9"))
	assert.Error(t, tErr)
}

// codex cloud P2 literal case: a marker for an ID literally ending in
// ".rsbak" must sweep as a sentinel — never be rehomed as a parked leg.
func TestReconcileMarkerForIDRsbakSuffixSweepsAsSentinel(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".inflight-X.rsbak.a1.b2"), []byte("{}"), 0o644))
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 1, healed, "sentinel swept")
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-X.rsbak.a1.b2"))
	assert.Error(t, mErr, "sentinel removed")
	_, xErr := fs.Stat(filepath.Join(dir, ".inflight-X"))
	assert.Error(t, xErr, "no phantom canon invented from the sentinel name")
}

// writeCommitToken: full + crop SHAs captured; write and rename failures are
// surfaced (rename failure also sweeps the tmp file).
func TestWriteCommitTokenArms(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/posters/JW"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	shas := map[string]string{
		"W-1-full.jpg": shaContentHex([]byte("cf")),
		"W-1.jpg":      shaContentHex([]byte("cc")),
	}
	tok := filepath.Join(dir, ".commit-W-1.a1.b2")
	require.NoError(t, writeCommitToken(base, tok, "W-1", shas))
	data, err := afero.ReadFile(base, tok)
	require.NoError(t, err)
	var meta commitMeta
	require.NoError(t, json.Unmarshal(data, &meta))
	assert.Equal(t, "W-1", meta.PosterID)
	assert.Equal(t, shas["W-1-full.jpg"], meta.FullSHA)
	assert.Equal(t, shas["W-1.jpg"], meta.CropSHA)

	// Only the full leg fingerprinted → no crop SHA recorded
	tok2 := filepath.Join(dir, ".commit-W-2.a1.b2")
	require.NoError(t, writeCommitToken(base, tok2, "W-2", map[string]string{"W-2-full.jpg": shaContentHex([]byte("f2"))}))
	data2, _ := afero.ReadFile(base, tok2)
	var meta2 commitMeta
	_ = json.Unmarshal(data2, &meta2)
	assert.Equal(t, shaContentHex([]byte("f2")), meta2.FullSHA)
	assert.Empty(t, meta2.CropSHA)

	writeWedged := createWedgeFS{Fs: base, contains: ".tmp"}
	err3 := writeCommitToken(writeWedged, filepath.Join(dir, ".commit-W-3.a1.b2"), "W-1", shas)
	require.ErrorContains(t, err3, "commit token write")

	renameWedged := &seqRenameFailFS{Fs: base, failOn: map[int]bool{1: true}}
	err4 := writeCommitToken(renameWedged, filepath.Join(dir, ".commit-W-4.a1.b2"), "W-1", shas)
	require.ErrorContains(t, err4, "commit token rename")
}

// Own-token drop with a wedged Remove keeps the backup AND its marker.
func TestReconcileParkedOwnTokenRemoveWedgedKeeps(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-J", "a1.b2", "gen-committed", "pre-op", 4)
	seedCommitToken(t, fs, dir, "AR-J", "a1.b2", "gen-committed", false)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(arbJobRow(t, "AR-J", 9), nil)
	wedged := selectiveFailRemoveFS{Fs: fs, failSuffix: ".rsbak.a1.b2"}
	cl := &TempDirCleaner{fs: wedged, tempDir: "/tmp", jobRepo: repo}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed, "drop wedged — everything persists for retry")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-J.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr)
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-AR-J.a1.b2"))
	assert.NoError(t, mErr, "marker retained while the backup attests nothing settled")
}

// Canon-match drop with a wedged Remove keeps the loser's backup.
func TestReconcileParkedForeignTokenDropWedgedKeepsBackup(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-K", "a3.d4", "genB", "orig", 4)
	seedCommitToken(t, fs, dir, "AR-K", "a2.c2", "genB", false)
	repo := mocks.NewMockJobRepositoryInterface(t)
	wedged := selectiveFailRemoveFS{Fs: fs, failSuffix: ".rsbak.a3.d4"}
	cl := &TempDirCleaner{fs: wedged, tempDir: "/tmp", jobRepo: repo}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed)
	_, bErr := fs.Stat(filepath.Join(dir, "AR-K.jpg.rsbak.a3.d4"))
	assert.NoError(t, bErr)
}

// Winner-recovery restore with a wedged Rename keeps both the backup and the
// stranded generation for the next startup.
func TestReconcileParkedForeignTokenRestoreWedgedKeepsBoth(t *testing.T) {
	fs, dir := witnessFixture(t)
	seedArbitrationScene(t, fs, dir, "AR-L", "a3.d4", "genB", "genA", 4)
	seedCommitToken(t, fs, dir, "AR-L", "a2.c2", "genA", false)
	repo := mocks.NewMockJobRepositoryInterface(t)
	cl := &TempDirCleaner{fs: &seqRenameFailFS{Fs: fs, failOn: map[int]bool{1: true}}, tempDir: "/tmp", jobRepo: repo}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed)
	_, bErr := fs.Stat(filepath.Join(dir, "AR-L.jpg.rsbak.a3.d4"))
	assert.NoError(t, bErr)
	got, _ := afero.ReadFile(fs, filepath.Join(dir, "AR-L.jpg"))
	assert.Equal(t, "genB", string(got), "stranded generation untouched when the restore wedges")
}

// A commit token whose SAME-base parked legs still pend must NOT be swept —
// it is their attribution record.
func TestCommitTokenKeptWhileFullLegPends(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-M.jpg"), []byte("live"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-M-full.jpg"), []byte("live-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-M-full.jpg.rsbak.a9.d9"), []byte("pending-full"), 0o644))
	seedCommitToken(t, fs, dir, "AR-M", "a1.b2", "live", false)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed, "unprovenanced pending leg halts everything including the token sweep")
	_, tErr := fs.Stat(filepath.Join(dir, ".commit-AR-M.a1.b2"))
	assert.NoError(t, tErr, "token retained while the base's legs pend")
}

// Token sweep with a wedged Remove keeps the token for the next startup.
func TestCommitTokenSweepWedgedKeepsToken(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "AR-N.jpg"), []byte("live"), 0o644))
	seedCommitToken(t, fs, dir, "AR-N", "a1.b2", "live", false)
	cl := &TempDirCleaner{fs: removeExactFailFS{Fs: fs, name: ".commit-AR-N.a1.b2"}, tempDir: "/tmp", jobRepo: nil}
	healed := cl.reconcileParkedPosterBackups(context.Background(), "JOB-W1", dir)
	assert.Equal(t, 0, healed)
	_, tErr := fs.Stat(filepath.Join(dir, ".commit-AR-N.a1.b2"))
	assert.NoError(t, tErr, "wedged sweep keeps the token")
}

// Row scanning skips nil rows and non-matching IDs// Row scanning skips nil rows and non-matching IDs and takes the match-info
// fallback when Movie is absent — the committed decision still stands.
type openExactFailFS struct {
	afero.Fs
	path string
}

func (f openExactFailFS) Open(name string) (afero.File, error) {
	if filepath.ToSlash(name) == filepath.ToSlash(f.path) {
		return nil, errors.New("dir read wedged")
	}
	return f.Fs.Open(name)
}

// The periodic stale sweep's ticker arm: sweep errors surface as warnings.
func TestStaleCleanupTickerErrorWarn(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/stallroot/posters", 0o755))
	fs := openExactFailFS{Fs: base, path: "/stallroot/posters"}
	cl := &TempDirCleaner{fs: fs, tempDir: "/stallroot", jobRepo: nil}
	old := staleCleanupInterval
	staleCleanupInterval = 10 * time.Millisecond
	t.Cleanup(func() { staleCleanupInterval = old })
	stop := cl.StartStaleTempCleanup()
	time.Sleep(60 * time.Millisecond)
	close(stop)
}

// ...and a tick that actually removes a stale dir logs the count.
func TestStaleCleanupTickerRemovesStale(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/tickroot/posters", 0o755))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "STALE-9").Return(nil, database.ErrNotFound)
	cl := &TempDirCleaner{fs: base, tempDir: "/tickroot", jobRepo: repo}
	old := staleCleanupInterval
	staleCleanupInterval = 10 * time.Millisecond
	t.Cleanup(func() { staleCleanupInterval = old })
	stop := cl.StartStaleTempCleanup()
	// The stale dir appears only AFTER the startup sweep — the removal must
	// land on a periodic TICK (exercising that arm), not at startup.
	time.Sleep(40 * time.Millisecond)
	require.NoError(t, base.MkdirAll("/tickroot/posters/STALE-9", 0o755))
	time.Sleep(150 * time.Millisecond)
	close(stop)
	_, err := base.Stat("/tickroot/posters/STALE-9")
	assert.Error(t, err, "stale dir removed across ticks")
}

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
	assert.Equal(t, 0, healed, "unattributed advance ⇒ kept both (and the marker)")
	_, bErr := fs.Stat(filepath.Join(dir, "AR-9.jpg.rsbak.a1.b2"))
	assert.NoError(t, bErr, "backup kept — revision advance without a token is not attribution")
	_, mErr := fs.Stat(filepath.Join(dir, ".inflight-AR-9.a1.b2"))
	assert.NoError(t, mErr, "marker retained while the leg pends")
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
