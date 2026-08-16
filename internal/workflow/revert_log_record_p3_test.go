package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
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
