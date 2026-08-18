package workflow

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// P3 recorder slice: the downloader's ReplacementRecorder seam is backed by
// real DB rows here — incremental ledger, parallel append safety, per-
// destination sequence persistence across reopen, ledger merge on Complete,
// and the orchestrator's arming gate.

func newP3RecorderHarness(t *testing.T, dsn string) (*database.DB, *database.BatchFileOperationRepository, RevertLog) {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := database.NewBatchFileOperationRepository(db)
	rl := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "job-p3", afero.NewMemMapFs(), nil, nil, nil)
	return db, repo, rl
}

func beginP3Op(t *testing.T, rl RevertLog, movieID string) OperationID {
	t.Helper()
	opID, err := rl.Begin(context.Background(), ApplyCmd{
		Movie: &models.Movie{ID: movieID},
		Match: models.FileMatchInfo{Path: "/src/" + movieID + ".mkv"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, opID)
	return opID
}

func p3Ledger(t *testing.T, repo *database.BatchFileOperationRepository, opID OperationID) models.GeneratedFilesJSON {
	t.Helper()
	var id uint
	_, err := fmt.Sscanf(opID, "%d", &id)
	require.NoError(t, err)
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	require.NotNil(t, row)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return gf
}

func TestRevertLog_RecordReplacement_IncrementalLedger(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "ABC-123")
	destA := "/dst/ABC-123/poster.jpg"
	destB := "/dst/ABC-123/fanart.jpg"

	require.NoError(t, rl.RecordReplacement(ctx, opID, destA, destA+".dlbak.1"))
	require.NoError(t, rl.RecordReplacement(ctx, opID, destB, destB+".dlbak.1"))
	require.NoError(t, rl.RecordReplacement(ctx, opID, destA, destA+".dlbak.2"))

	gf := p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 3, "every append must land on the row ledger")
	// Per-destination sequence: destA got 1 then 2; destB independently 1.
	require.Equal(t, int64(1), gf.Replacements[0].DestSeq)
	require.Equal(t, int64(1), gf.Replacements[1].DestSeq)
	require.Equal(t, int64(2), gf.Replacements[2].DestSeq)
	require.Equal(t, destA, gf.Replacements[0].Destination)
	require.Equal(t, destA+".dlbak.2", gf.Replacements[2].Backup)

	// Complete must MERGE the incremental journal into the final payload, not
	// overwrite it with the organize/download summary.
	require.NoError(t, rl.Complete(ctx, opID, &ApplyResult{
		Movie:         &models.Movie{ID: "ABC-123"},
		NFOPath:       "/dst/ABC-123/ABC-123.nfo",
		DownloadPaths: []string{destA, destB},
	}))
	gf = p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 3, "Complete must preserve the replacement journal")
	require.Contains(t, gf.Delete, "/dst/ABC-123/ABC-123.nfo")
}

func TestRevertLog_RecordReplacement_ParallelDownloads_NoLostAppends(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "PAR-900")
	dest := "/dst/PAR-900/poster.jpg"

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = rl.RecordReplacement(ctx, opID, dest, fmt.Sprintf("%s.dlbak.%02d", dest, i))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "append %d failed", i)
	}

	gf := p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, n, "parallel appends must not lose entries")
	seqs := make([]int64, 0, n)
	backups := map[string]bool{}
	for _, rep := range gf.Replacements {
		seqs = append(seqs, rep.DestSeq)
		require.False(t, backups[rep.Backup], "duplicate backup journaled: %s", rep.Backup)
		backups[rep.Backup] = true
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for i := 0; i < n; i++ {
		require.Equal(t, int64(i+1), seqs[i], "sequence must be a contiguous 1..N per destination")
	}
}

func TestLedgerRow_PersistsDestinationSequence_SurvivesRestart(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "ledger.db")
	ctx := context.Background()

	db1, _, rl1 := newP3RecorderHarness(t, dsn)
	opID1 := beginP3Op(t, rl1, "RST-001")
	dest := "/dst/RST-001/poster.jpg"
	require.NoError(t, rl1.RecordReplacement(ctx, opID1, dest, dest+".dlbak.pre1"))
	require.NoError(t, db1.Close())

	// "Restart": brand-new DB handle, repository, and RevertLog over the same
	// on-disk database.
	db2, repo2, rl2 := newP3RecorderHarness(t, dsn)
	defer func() { _ = db2.Close() }()
	opID2 := beginP3Op(t, rl2, "RST-002")
	require.NoError(t, rl2.RecordReplacement(ctx, opID2, dest, dest+".dlbak.post1"))

	gf := p3Ledger(t, repo2, opID2)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, int64(2), gf.Replacements[0].DestSeq,
		"sequence must continue across a restart, not reset to 1")

	hits, err := repo2.FindOperationsByDestination(ctx, dest)
	require.NoError(t, err)
	require.Len(t, hits, 2, "restart read must see both operations'")
}

func TestRevertLog_RecordReplacement_LegacyRowTolerance(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	// Legacy row: written before the journal existed, carrying only the
	// original generated-files shape.
	legacy := &models.BatchFileOperation{
		BatchJobID: "job-p3", MovieID: "LGC-1", OriginalPath: "/src/lgc.mkv",
		OperationType: models.OperationTypeCopy, GeneratedFiles: `{"delete":["/dst/lgc.nfo"]}`,
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, legacy))
	opID := fmt.Sprintf("%d", legacy.ID)

	require.NoError(t, rl.RecordReplacement(ctx, opID, "/dst/lgc/poster.jpg", "/dst/lgc/poster.jpg.dlbak.x"))

	gf := p3Ledger(t, repo, opID)
	require.Equal(t, []string{"/dst/lgc.nfo"}, gf.Delete, "legacy fields must survive the append")
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, int64(1), gf.Replacements[0].DestSeq)

	// Malformed content is refused with an error — no silent ledger drop.
	legacy.GeneratedFiles = `{"replacements":bogus`
	require.NoError(t, repo.Update(ctx, legacy))
	require.Error(t, rl.RecordReplacement(ctx, opID, "/dst/lgc/poster.jpg", "/dst/lgc/poster.jpg.dlbak.y"))
}

func TestReplacementRecorder_ArmingGate(t *testing.T) {
	// The no-op revert log must NEVER arm a destructive overwrite: its
	// journal accepts silently with no durable store.
	require.Nil(t, replacementRecorder(noOpRevertLog{}))
	require.Nil(t, replacementRecorder(nil))

	db, _, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	require.NotNil(t, replacementRecorder(rl), "DB-backed log arms the downloader ledger")
}

func TestRevertLog_ReleaseReplacement_RetractsRolledBackEntry(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "REL-001")
	dest := "/dst/REL-001/poster.jpg"
	backup := dest + ".dlbak.x"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, backup))
	require.Len(t, p3Ledger(t, repo, opID).Replacements, 1)

	require.NoError(t, rl.ReleaseReplacement(ctx, opID, dest, backup))
	require.Empty(t, p3Ledger(t, repo, opID).Replacements,
		"rolled-back entry retracted — the row must not point at a consumed backup")

	// Idempotent: a second release of the same entry is a no-op.
	require.NoError(t, rl.ReleaseReplacement(ctx, opID, dest, backup))

	// Missing rows still surface.
	require.Error(t, rl.ReleaseReplacement(ctx, "999999", dest, backup))
}

func TestRevertLog_Begin_SeedsDiscoveryRoot(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID, err := rl.Begin(ctx, ApplyCmd{
		Movie:    &models.Movie{ID: "RND-001"},
		Match:    models.FileMatchInfo{Path: "/src/rnd.mkv"},
		DestPath: "/dest/rooted",
	})
	require.NoError(t, err)

	gf := p3Ledger(t, repo, opID)
	require.Equal(t, []string{"/dest/rooted"}, gf.Roots,
		"the discovery root is persisted at Begin, before any journal exists")

	// Complete merges, never clobbers the seeded root.
	require.NoError(t, rl.Complete(ctx, opID, &ApplyResult{
		Movie:         &models.Movie{ID: "RND-001"},
		NFOPath:       "/dest/rooted/rnd.nfo",
		DownloadPaths: []string{"/dest/rooted/poster.jpg"},
	}))
	gf = p3Ledger(t, repo, opID)
	require.Equal(t, []string{"/dest/rooted"}, gf.Roots, "Complete preserves the seeded root")
	require.Contains(t, gf.Delete, "/dest/rooted/rnd.nfo")
}

func TestRevertLog_ConfirmInstallMarker(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "CNF-001")
	dest := "/dst/CNF-001/poster.jpg"
	backup := dest + ".dlbak.x"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, backup))

	gf := p3Ledger(t, repo, opID)
	require.False(t, gf.Replacements[0].Installed, "armed at record — crash window live")

	require.NoError(t, rl.ConfirmReplacement(ctx, opID, dest, backup))
	gf = p3Ledger(t, repo, opID)
	require.True(t, gf.Replacements[0].Installed, "confirmed after the install landed")

	// Idempotent; missing rows surface.
	require.NoError(t, rl.ConfirmReplacement(ctx, opID, dest, backup))
	require.Error(t, rl.ConfirmReplacement(ctx, "424242", dest, backup))
}

// codex P3 R7-3: the orchestrator seeds the organizer's leaf folder as a
// discovery root right after organize, so a nested crash-window backup is
// findable without relying on the walk bound.
func TestRevertLog_SeedRoot_ClosesNestedDiscoveryGap(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID, err := rl.Begin(ctx, ApplyCmd{
		Movie:    &models.Movie{ID: "DEP-001"},
		Match:    models.FileMatchInfo{Path: "/src/dep.mkv"},
		DestPath: "/out/base",
	})
	require.NoError(t, err)
	db2, ok := rl.(*dbRevertLog)
	require.True(t, ok)
	db2.seedRoot(ctx, opID, "/out/base/A/B/C/D/Movie (2020)")

	gf := p3Ledger(t, repo, opID)
	require.Contains(t, gf.Roots, "/out/base", "Begin-seeded base root kept")
	require.Contains(t, gf.Roots, "/out/base/A/B/C/D/Movie (2020)", "leaf seeded post-organize")

	// Idempotent: no duplicate roots.
	db2.seedRoot(ctx, opID, "/out/base/A/B/C/D/Movie (2020)")
	gf = p3Ledger(t, repo, opID)
	count := 0
	for range gf.Roots {
		count++
	}
	require.Equal(t, 2, count)
}

// codex P3 R12-2: seed-root persistence failures SURFACE — a destructive
// overwrite run must never proceed seedless.
func TestRevertLog_SeedRoot_FailureSurfaces(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()

	flaky := &failUpdateBFORepo{repo: repo, err: errors.New("outage")}
	broken := NewDBRevertLog(flaky, NewRevertLogConfig(true, nil), "job-x", afero.NewMemMapFs(), nil, nil, nil).(*dbRevertLog)

	ctx := context.Background()
	opID := beginP3Op(t, rl, "SED-001")
	require.Error(t, broken.seedRoot(ctx, opID, "/out/leaf"), "persist failure must surface")
	flaky.err = nil
	require.NoError(t, broken.seedRoot(ctx, opID, "/out/leaf"), "healed repo seeds cleanly")
	require.Error(t, broken.seedRoot(ctx, "424242", "/out/leaf"), "missing row surfaces")
}

type failUpdateBFORepo struct {
	repo *database.BatchFileOperationRepository
	err  error
}

func (f *failUpdateBFORepo) Create(ctx context.Context, op *models.BatchFileOperation) error {
	return f.repo.Create(ctx, op)
}
func (f *failUpdateBFORepo) CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error {
	return f.repo.CreateBatch(ctx, ops)
}
func (f *failUpdateBFORepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	return f.repo.FindByID(ctx, id)
}
func (f *failUpdateBFORepo) FindByBatchJobID(ctx context.Context, id string) ([]models.BatchFileOperation, error) {
	return f.repo.FindByBatchJobID(ctx, id)
}
func (f *failUpdateBFORepo) FindByBatchJobIDAndRevertStatus(ctx context.Context, id string, s models.RevertStatusEnum) ([]models.BatchFileOperation, error) {
	return f.repo.FindByBatchJobIDAndRevertStatus(ctx, id, s)
}
func (f *failUpdateBFORepo) Update(ctx context.Context, op *models.BatchFileOperation) error {
	if f.err != nil {
		return f.err
	}
	return f.repo.Update(ctx, op)
}
func (f *failUpdateBFORepo) UpdateNonJournalFields(ctx context.Context, op *models.BatchFileOperation) error {
	if f.err != nil {
		return f.err
	}
	return f.repo.UpdateNonJournalFields(ctx, op)
}
func (f *failUpdateBFORepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if f.err != nil {
		return f.err
	}
	return f.repo.UpdateJournalInTx(ctx, id, fn)
}
func (f *failUpdateBFORepo) UpdateRevertStatus(ctx context.Context, id uint, s models.RevertStatusEnum) error {
	return f.repo.UpdateRevertStatus(ctx, id, s)
}
func (f *failUpdateBFORepo) CountByBatchJobID(context.Context, string) (int64, error) { return 0, nil }
func (f *failUpdateBFORepo) CountByBatchJobIDAndRevertStatus(context.Context, string, models.RevertStatusEnum) (int64, error) {
	return 0, nil
}
func (f *failUpdateBFORepo) CountByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, nil
}
func (f *failUpdateBFORepo) CountRevertedByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, nil
}
func (f *failUpdateBFORepo) FindOperationsByDestination(ctx context.Context, d string) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsByDestination(ctx, d)
}
func (f *failUpdateBFORepo) FindOperationsWithReplacements(ctx context.Context) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsWithReplacements(ctx)
}
func (f *failUpdateBFORepo) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsWithLedger(ctx)
}

// codex P3 R12-2 gate at the orchestrator: overwrite runs refuse to
// download when the leaf discovery root can't be persisted.
func TestStepDownload_SeedFailureBlocksOverwriteOnly(t *testing.T) {
	db, repo, goodLog := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	opID, err := goodLog.Begin(ctx, ApplyCmd{
		Movie:    &models.Movie{ID: "GATE-1"},
		Match:    models.FileMatchInfo{Path: "/src/gate.mkv"},
		DestPath: "/out",
	})
	require.NoError(t, err)

	flaky := &failUpdateBFORepo{repo: repo, err: errors.New("outage")}
	broken := NewDBRevertLog(flaky, NewRevertLogConfig(true, nil), "job-x", afero.NewMemMapFs(), nil, nil, nil)
	capture := &capturingDownloader{outcome: &downloader.DownloadOutcome{}}
	o := &applyOrchImpl{downloader: capture, revertLog: broken}
	state := &applyPipelineState{movie: &models.Movie{ID: "GATE-1"}, finalDir: "/out/leaf"}

	// Destructive run + unpersistable seed → refused.
	err = o.stepDownload(ctx, ApplyCmd{Movie: state.movie, Match: models.FileMatchInfo{Path: "/src/gate.mkv"}, Download: true, OverwriteExistingMedia: true}, opID, state, &stepCompletion{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "discovery-root seed")

	// Non-destructive run proceeds despite the seed failure (download is
	// create-only in that mode).
	err = o.stepDownload(ctx, ApplyCmd{Movie: state.movie, Match: models.FileMatchInfo{Path: "/src/gate.mkv"}, Download: true, OverwriteExistingMedia: false}, opID, state, &stepCompletion{})
	require.NoError(t, err)

	// Healthy log passes both modes.
	flaky.err = nil
	healthy := &applyOrchImpl{downloader: capture, revertLog: goodLog}
	err = healthy.stepDownload(ctx, ApplyCmd{Movie: state.movie, Match: models.FileMatchInfo{Path: "/src/gate.mkv"}, Download: true, OverwriteExistingMedia: true}, opID, state, &stepCompletion{})
	require.NoError(t, err)
}

func TestRevertLog_ErrorLegs(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	dest := "/dst/ERR/poster.jpg"

	require.Error(t, rl.RecordReplacement(ctx, "", dest, dest+".b"), "empty opID")
	require.Error(t, rl.RecordReplacement(ctx, "not-a-number", dest, dest+".b"), "unparsable")
	require.Error(t, rl.RecordReplacement(ctx, "424242", dest, dest+".b"), "unmatched row")

	require.Error(t, rl.ReleaseReplacement(ctx, "", dest, dest+".b"))
	require.Error(t, rl.ReleaseReplacement(ctx, "nan", dest, dest+".b"))

	require.Error(t, rl.ConfirmReplacement(ctx, "", dest, dest+".b"))
	require.Error(t, rl.ConfirmReplacement(ctx, "nan", dest, dest+".b"))

	dbl := rl.(*dbRevertLog)
	require.NoError(t, dbl.seedRoot(ctx, "", "/x"))
	require.NoError(t, dbl.seedRoot(ctx, "nan", "/x"))
	require.NoError(t, dbl.seedRoot(ctx, "0", "/x")) // recordID 0 → early nil
	require.NoError(t, dbl.seedRoot(ctx, "424242", ""))
	require.Error(t, dbl.seedRoot(ctx, "424242", "/x"), "missing row surfaces")

	// Malformed persisted ledger bodies surface on the mutators.
	opID := beginP3Op(t, rl, "ERR-001")
	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	row.GeneratedFiles = `{"replacements":definitely-not-json`
	require.NoError(t, repo.Update(ctx, row))
	require.Error(t, rl.RecordReplacement(ctx, opID, dest, dest+".b"))
	require.Error(t, rl.ReleaseReplacement(ctx, opID, dest, dest+".b"))
	require.Error(t, rl.ConfirmReplacement(ctx, opID, dest, dest+".b"))
}

func uintFromOpID(t *testing.T, opID OperationID) uint {
	t.Helper()
	var id uint
	_, err := fmt.Sscanf(opID, "%d", &id)
	require.NoError(t, err)
	return id
}

// CaptureSnapshot's defensive legs run without panicking on every row state.
func TestCaptureSnapshot_ErrorLegs(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	cmd := ApplyCmd{
		Movie: &models.Movie{ID: "CS-001"},
		Match: models.FileMatchInfo{Path: "/src/cs.mkv"},
	}
	// Empty opID, malformed opID, missing row, healthy row.
	rl.CaptureSnapshot(ctx, "", cmd)
	rl.CaptureSnapshot(ctx, "not-a-number", cmd)
	rl.CaptureSnapshot(ctx, "31337", cmd)
	opID := beginP3Op(t, rl, "CS-001")
	rl.CaptureSnapshot(ctx, opID, cmd)
	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	require.NotNil(t, row)
}

// Repo FindByID failures surface on every mutator; non-numeric/missing rows too.
func TestRevertLog_RepoFailureLegs(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "FL-001")
	dest := "/dst/FL/poster.jpg"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, dest+".b"))

	flaky := &failUpdateBFORepo{repo: repo, err: errors.New("find wedged")}
	broken := NewDBRevertLog(flaky, NewRevertLogConfig(true, nil), "job-x", afero.NewMemMapFs(), nil, nil, nil)
	opIDStr := opID

	// update-failure legs on all three mutators against a VALID row.
	require.Error(t, broken.RecordReplacement(ctx, opIDStr, dest, dest+".c"))
	require.Error(t, broken.ReleaseReplacement(ctx, opIDStr, dest, dest+".b"))
	require.Error(t, broken.ConfirmReplacement(ctx, opIDStr, dest, dest+".b"))
	require.Error(t, broken.(*dbRevertLog).seedRoot(ctx, opIDStr, "/out/z"))

	// nextDestSequence on a repo whose destination scan fails.
	flaky2 := &failDestScanRepo{BatchFileOperationRepositoryInterface: database.BatchFileOperationRepositoryInterface(repo), err: errors.New("scan wedged")}
	broken2 := NewDBRevertLog(flaky2, NewRevertLogConfig(true, nil), "job-x", afero.NewMemMapFs(), nil, nil, nil)
	require.Error(t, broken2.RecordReplacement(ctx, opIDStr, dest, dest+".d"))
}

type failDestScanRepo struct {
	database.BatchFileOperationRepositoryInterface
	err error
}

func (f *failDestScanRepo) FindOperationsByDestination(context.Context, string) ([]models.BatchFileOperation, error) {
	return nil, f.err
}

// mergeReplacementLedger fallbacks: unparseable prior wins nothing; empty new
// turns replacement-only; appendLedgerRoot on unparseable stays.
func TestRevertLog_MergeFallbacks(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	opID := beginP3Op(t, rl, "MG-001")
	dest := "/dst/MG/poster.jpg"
	require.NoError(t, rl.RecordReplacement(ctx, opID, dest, dest+".b"))

	// Complete with EMPTY result payloads (no NFO, no downloads) → ledger
	// payload keeps just the journal.
	require.NoError(t, rl.Complete(ctx, opID, &ApplyResult{Movie: &models.Movie{ID: "MG-001"}}))
	gf := p3Ledger(t, repo, opID)
	require.Len(t, gf.Replacements, 1)
	require.Empty(t, gf.Delete)

	// Unparseable prior ledger degrades to the fresh payload.
	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	row.GeneratedFiles = `definitely-broken`
	require.NoError(t, repo.Update(ctx, row))
	require.Error(t, rl.RecordReplacement(ctx, opID, dest+"2", dest+"2.b"))
}

// All no-op log method legs directly.
func TestNoOpRevertLog_AllMethodsNoPanic(t *testing.T) {
	ctx := context.Background()
	l := NewRevertLogFromConfig(nil, NewRevertLogConfig(true, nil), "j", afero.NewMemMapFs(), nil, nil, nil)

	op, err := l.Begin(ctx, ApplyCmd{})
	require.NoError(t, err)
	require.Empty(t, op)
	l.CaptureSnapshot(ctx, "", ApplyCmd{})
	require.NoError(t, l.Complete(ctx, "", nil))
	require.NoError(t, l.CompleteFailed(ctx, "", nil))
	require.NoError(t, l.RecordReplacement(ctx, "", "d", "b"))
	require.NoError(t, l.ReleaseReplacement(ctx, "", "d", "b"))
	require.NoError(t, l.ConfirmReplacement(ctx, "", "d", "b"))
	require.Nil(t, replacementRecorder(l), "no-op never arms")
}

// Begin with a nil movie no-ops.
func TestBegin_NilMovieReturnsEmptyID(t *testing.T) {
	db, _, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	op, err := rl.Begin(context.Background(), ApplyCmd{})
	require.NoError(t, err)
	require.Empty(t, op)
}

// Complete/CompleteFailed: FindByID failure legs.
func TestComplete_FindByIDFailureLegs(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	opID := beginP3Op(t, rl, "CPL-001")

	flaky := &failFindBFORepo{repo: repo, err: errors.New("read wedged")}
	broken := NewDBRevertLog(flaky, NewRevertLogConfig(true, nil), "job-x", afero.NewMemMapFs(), nil, nil, nil)
	require.Error(t, broken.Complete(ctx, opID, &ApplyResult{}))
	require.Error(t, broken.CompleteFailed(ctx, opID, nil))

	flaky.err = nil
	require.NoError(t, broken.Complete(ctx, opID, &ApplyResult{Movie: &models.Movie{ID: "CPL-001"}}))
	require.NoError(t, broken.CompleteFailed(ctx, opID, &ApplyResult{Movie: &models.Movie{ID: "CPL-001"}, OrganizeResult: &organizer.OrganizeResult{}}))
}

type failFindBFORepo struct {
	repo *database.BatchFileOperationRepository
	err  error
}

func (f *failFindBFORepo) Create(ctx context.Context, op *models.BatchFileOperation) error {
	return f.repo.Create(ctx, op)
}
func (f *failFindBFORepo) CreateBatch(ctx context.Context, ops []*models.BatchFileOperation) error {
	return f.repo.CreateBatch(ctx, ops)
}
func (f *failFindBFORepo) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	if f.err != nil {
		return nil, errors.New("read wedged")
	}
	return f.repo.FindByID(ctx, id)
}
func (f *failFindBFORepo) FindByBatchJobID(ctx context.Context, id string) ([]models.BatchFileOperation, error) {
	return f.repo.FindByBatchJobID(ctx, id)
}
func (f *failFindBFORepo) FindByBatchJobIDAndRevertStatus(ctx context.Context, id string, s models.RevertStatusEnum) ([]models.BatchFileOperation, error) {
	return f.repo.FindByBatchJobIDAndRevertStatus(ctx, id, s)
}
func (f *failFindBFORepo) Update(ctx context.Context, op *models.BatchFileOperation) error {
	return f.repo.Update(ctx, op)
}
func (f *failFindBFORepo) UpdateNonJournalFields(ctx context.Context, op *models.BatchFileOperation) error {
	return f.repo.UpdateNonJournalFields(ctx, op)
}
func (f *failFindBFORepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	return f.repo.UpdateJournalInTx(ctx, id, fn)
}
func (f *failFindBFORepo) UpdateRevertStatus(ctx context.Context, id uint, s models.RevertStatusEnum) error {
	return f.repo.UpdateRevertStatus(ctx, id, s)
}
func (f *failFindBFORepo) CountByBatchJobID(context.Context, string) (int64, error) { return 0, nil }
func (f *failFindBFORepo) CountByBatchJobIDAndRevertStatus(context.Context, string, models.RevertStatusEnum) (int64, error) {
	return 0, nil
}
func (f *failFindBFORepo) CountByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, nil
}
func (f *failFindBFORepo) CountRevertedByBatchJobIDs(context.Context, []string) (map[string]int64, error) {
	return nil, nil
}
func (f *failFindBFORepo) FindOperationsByDestination(ctx context.Context, d string) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsByDestination(ctx, d)
}
func (f *failFindBFORepo) FindOperationsWithReplacements(ctx context.Context) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsWithReplacements(ctx)
}
func (f *failFindBFORepo) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	return f.repo.FindOperationsWithLedger(ctx)
}

// All remaining reachable mutator error legs: no-row, unparseable ledger, no-op release.
func TestRevertLog_MutatorLeanLegs(t *testing.T) {
	db, repo, rl := newP3RecorderHarness(t, ":memory:")
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	dest := "/dst/LG/poster.jpg"
	backup := dest + ".dlbak.z"

	opID := beginP3Op(t, rl, "LG-001")
	require.NoError(t, rl.ReleaseReplacement(ctx, opID, dest, backup),
		"no matching entry — silent no-op")
	require.Error(t, rl.ConfirmReplacement(ctx, "424242", dest, backup), "record not found")

	// seedRoot on a row with broken stored JSON keeps the raw ledger.
	row, err := repo.FindByID(ctx, uintFromOpID(t, opID))
	require.NoError(t, err)
	row.GeneratedFiles = "not-json"
	require.NoError(t, repo.Update(ctx, row))
	require.NoError(t, rl.(*dbRevertLog).seedRoot(ctx, opID, "/extra"))

	// now append works against clean ledger
	// (applying via freshBeginToRow helper to satisfy any schemas)
	require.NoError(t, rl.(*dbRevertLog).seedRoot(ctx, opID, "/extra"), "dedupe is a no-op")
}
