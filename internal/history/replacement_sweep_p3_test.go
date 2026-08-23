package history

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// P3 replacement sweeper: conservative ownership markers, retention for rows
// in ANY non-reverted status, crash-window restore with journal consumption,
// and conservative orphan handling.

const (
	p3HexA = "0123456789abcdef"
	p3HexB = "fedcba9876543210"
	p3HexC = "aaaabbbbccccdddd"
)

func writeSweepFile(t *testing.T, fs afero.Fs, path, content string, age time.Duration) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o644))
	mtime := time.Now().Add(-age)
	require.NoError(t, fs.Chtimes(path, mtime, mtime))
}

func journalRow(t *testing.T, repo *p3OpRepo, jobID, movieID, dest, backup string, seq int64, status models.RevertStatusEnum) *models.BatchFileOperation {
	t.Helper()
	raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: dest, Backup: backup, DestSeq: seq},
	}})
	require.NoError(t, err)
	op := &models.BatchFileOperation{
		BatchJobID: jobID, MovieID: movieID, OriginalPath: "/src/" + movieID + ".mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: status,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	return op
}

func TestSweep_BackupsOfFailedRecords_AreRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/FLD-001/poster.jpg"
	failedBackup := dest + ".dlbak." + p3HexA
	appliedBackup := "/out/FLD-002/poster.jpg"
	appliedB := appliedBackup + ".dlbak." + p3HexB

	require.NoError(t, fs.MkdirAll("/out/FLD-001", 0o755))
	require.NoError(t, fs.MkdirAll("/out/FLD-002", 0o755))
	writeSweepFile(t, fs, dest, "new", time.Hour)
	writeSweepFile(t, fs, appliedBackup, "new", time.Hour)
	writeSweepFile(t, fs, failedBackup, "old-failed", time.Hour)
	writeSweepFile(t, fs, appliedB, "old-applied", time.Hour)

	journalRow(t, repo, "job-1", "FLD-001", dest, failedBackup, 1, models.RevertStatusFailed)
	journalRow(t, repo, "job-1", "FLD-002", appliedBackup, appliedB, 1, models.RevertStatusApplied)

	sweeper := NewReplacementSweeper(fs, repo)
	healed, err := sweeper.Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed)

	for _, kept := range []string{failedBackup, appliedB} {
		exists, err := afero.Exists(fs, kept)
		require.NoError(t, err)
		require.True(t, exists, "journaled backup must be retained regardless of row status: %s", kept)
	}
}

func TestSweep_RootsAndMarkers(t *testing.T) {
	newSweepHarness := func(t *testing.T) (afero.Fs, *p3OpRepo) {
		fs := afero.NewMemMapFs()
		require.NoError(t, fs.MkdirAll("/out/SWP", 0o755))
		return fs, newP3OpRepo()
	}

	t.Run("orphan with destination present is retained", func(t *testing.T) {
		fs, repo := newSweepHarness(t)
		dest := "/out/SWP/poster.jpg"
		backup := dest + ".dlbak." + p3HexA
		writeSweepFile(t, fs, dest, "final", time.Hour)
		writeSweepFile(t, fs, backup, "stale", time.Hour)
		// A journaled destination in the same directory puts the dir in scope.
		journalRow(t, repo, "job-1", "SWP-001", "/out/SWP/other.jpg", "/out/SWP/other.jpg.dlbak."+p3HexC, 1, models.RevertStatusApplied)

		healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 0, healed)
		exists, _ := afero.Exists(fs, backup)
		require.True(t, exists, "marker shape without journal proof must not delete the orphan")
		require.Equal(t, "final", string(mustRead2(t, fs, dest)), "destination bytes untouched")
	})

	t.Run("orphan with destination missing is restored as last copy", func(t *testing.T) {
		fs, repo := newSweepHarness(t)
		dest := "/out/SWP/poster.jpg"
		writeSweepFile(t, fs, dest+".dlbak."+p3HexA, "last-copy", time.Hour)
		journalRow(t, repo, "job-1", "SWP-001", "/out/SWP/other.jpg", "/out/SWP/other.jpg.dlbak."+p3HexC, 1, models.RevertStatusApplied)

		healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, healed)
		require.Equal(t, "last-copy", string(mustRead2(t, fs, dest)), "orphan backup is the last copy — restore it")
	})

	t.Run("foreign lookalikes are never touched", func(t *testing.T) {
		fs, repo := newSweepHarness(t)
		for _, foreign := range []string{
			"/out/SWP/poster.jpg.dlbak.GHIJKLMNOP",   // non-hex
			"/out/SWP/poster.jpg.dlbak.ABCDEF012345", // uppercase hex, wrong length
			"/out/SWP/poster.jpg.dlbak.short",        // too short
			"/out/SWP/poster.jpg.backup",             // not a marker at all
		} {
			writeSweepFile(t, fs, foreign, "x", time.Hour)
		}
		journalRow(t, repo, "job-1", "SWP-001", "/out/SWP/other.jpg", "/out/SWP/other.jpg.dlbak."+p3HexC, 1, models.RevertStatusApplied)

		healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 0, healed)
	})

	t.Run("live-marker backups are skipped", func(t *testing.T) {
		fs, repo := newSweepHarness(t)
		dest := "/out/SWP/poster.jpg"
		writeSweepFile(t, fs, dest, "final", time.Hour)
		writeSweepFile(t, fs, dest+".dlbak."+p3HexA, "in-flight", -time.Minute) // future mtime
		// A future mtime alone is no longer an in-flight signal; the durable
		// owner marker supplies the skip decision.
		writeW14ABusy(t, fs, dest, os.Getpid())
		journalRow(t, repo, "job-1", "SWP-001", "/out/SWP/other.jpg", "/out/SWP/other.jpg.dlbak."+p3HexC, 1, models.RevertStatusApplied)

		healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 0, healed, "live-owner backup must outlive the sweep")
	})

	t.Run("journaled backup with missing destination restores and consumes", func(t *testing.T) {
		fs, repo := newSweepHarness(t)
		dest := "/out/SWP/poster.jpg"
		backup := dest + ".dlbak." + p3HexA
		writeSweepFile(t, fs, backup, "pre-crash", time.Hour)
		op := journalRow(t, repo, "job-1", "SWP-001", dest, backup, 1, models.RevertStatusApplied)

		healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 1, healed)
		require.Equal(t, "pre-crash", string(mustRead2(t, fs, dest)), "crash window: new bytes never landed, old bytes restored")

		row, err := repo.FindByID(context.Background(), op.ID)
		require.NoError(t, err)
		gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
		require.NoError(t, err)
		require.Empty(t, gf.Replacements, "crash-window restore consumes the journal entry so future reverts never meet a phantom backup")
	})

	t.Run("pruned rows release their backups to the orphan sweep", func(t *testing.T) {
		fs, repo := newSweepHarness(t)
		dest := "/out/SWP/poster.jpg"
		backup := dest + ".dlbak." + p3HexA
		writeSweepFile(t, fs, dest, "final", time.Hour)
		writeSweepFile(t, fs, backup, "stale", time.Hour)
		op := journalRow(t, repo, "job-1", "SWP-001", dest, backup, 1, models.RevertStatusApplied)
		// Conservative ownership: sweep space derives from journaled
		// destination directories, so a sibling journal keeps this dir in scope.
		journalRow(t, repo, "job-1", "SWP-002", "/out/SWP/other.jpg", "/out/SWP/other.jpg.dlbak."+p3HexC, 1, models.RevertStatusApplied)

		// First sweep: retained (journaled). Then prune the row; the backup
		// turns unjournaled and the next sweep retains it (destination present).
		healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 0, healed)
		delete(repo.ops, op.ID)

		healed, err = NewReplacementSweeper(fs, repo).Sweep(context.Background())
		require.NoError(t, err)
		require.Equal(t, 0, healed)
		exists, _ := afero.Exists(fs, backup)
		require.True(t, exists, "prune-hook coverage: unjournaled marker backup is retained")
	})
}

func TestReplacementSweeper_PruneOperationBackups_RemovesOnlyUnreferenced(t *testing.T) {
	ctx := context.Background()

	markInstalled := func(t *testing.T, repo *p3OpRepo, op *models.BatchFileOperation, pendingKind string) {
		t.Helper()
		gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
		require.NoError(t, err)
		gf.Replacements[0].Installed = true
		if pendingKind != "" {
			gf.Replacements[0].SetRestorePending(pendingKind)
		}
		op.GeneratedFiles = models.MarshalLedgerJSON(gf)
		require.NoError(t, repo.Update(context.Background(), op))
	}

	t.Run("removed operation releases its backup", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest := "/out/PRUNE/poster.jpg"
		backup := dest + ".dlbak." + p3HexA
		require.NoError(t, fs.MkdirAll("/out/PRUNE", 0o755))
		writeSweepFile(t, fs, dest, "current", time.Hour)
		writeSweepFile(t, fs, backup, "old", time.Hour)
		op := journalRow(t, repo, "job-pruned", "PRUNE-001", dest, backup, 1, models.RevertStatusApplied)
		markInstalled(t, repo, op, "")
		delete(repo.ops, op.ID)

		err := NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
		require.NoError(t, err)
		exists, statErr := afero.Exists(fs, backup)
		require.NoError(t, statErr)
		require.False(t, exists, "an unreferenced pruned backup must be consumed")
		require.Equal(t, "current", string(mustRead2(t, fs, dest)))
	})

	t.Run("shared backup remains owned", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest := "/out/PRUNE-SHARED/poster.jpg"
		backup := dest + ".dlbak." + p3HexB
		require.NoError(t, fs.MkdirAll("/out/PRUNE-SHARED", 0o755))
		writeSweepFile(t, fs, dest, "current", time.Hour)
		writeSweepFile(t, fs, backup, "old", time.Hour)
		pruned := journalRow(t, repo, "job-pruned", "PRUNE-002", dest, backup, 1, models.RevertStatusApplied)
		markInstalled(t, repo, pruned, "")
		live := journalRow(t, repo, "job-live", "PRUNE-003", dest, backup, 2, models.RevertStatusApplied)
		markInstalled(t, repo, live, "")
		delete(repo.ops, pruned.ID)

		err := NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*pruned})
		require.NoError(t, err)
		exists, statErr := afero.Exists(fs, backup)
		require.NoError(t, statErr)
		require.True(t, exists, "a backup with a remaining ledger reference must stay")
	})

	t.Run("unconfirmed install retains the backup", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest := "/out/PRUNE-UNCONFIRMED/poster.jpg"
		backup := dest + ".dlbak." + p3HexC
		require.NoError(t, fs.MkdirAll("/out/PRUNE-UNCONFIRMED", 0o755))
		writeSweepFile(t, fs, dest, "current", time.Hour)
		writeSweepFile(t, fs, backup, "old", time.Hour)
		op := journalRow(t, repo, "job-pruned", "PRUNE-004", dest, backup, 1, models.RevertStatusApplied)
		delete(repo.ops, op.ID)

		err := NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
		require.NoError(t, err)
		exists, statErr := afero.Exists(fs, backup)
		require.NoError(t, statErr)
		require.True(t, exists, "an unconfirmed install must retain recoverable bytes")
	})

	t.Run("rearm-refused ownership retains the occupant", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest := "/out/PRUNE-REFUSED/poster.jpg"
		backup := dest + ".dlbak." + p3HexA
		require.NoError(t, fs.MkdirAll("/out/PRUNE-REFUSED", 0o755))
		writeSweepFile(t, fs, dest, "current", time.Hour)
		writeSweepFile(t, fs, backup, "foreign", time.Hour)
		op := journalRow(t, repo, "job-pruned", "PRUNE-005", dest, backup, 1, models.RevertStatusApplied)
		markInstalled(t, repo, op, models.RestorePendingKindRearmRefused)
		delete(repo.ops, op.ID)

		err := NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
		require.NoError(t, err)
		got, readErr := afero.ReadFile(fs, backup)
		require.NoError(t, readErr)
		require.Equal(t, "foreign", string(got))
	})
}

func TestReplacementSweeper_PruneOperationBackups_BusyMarkerRetainsBackup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-BUSY/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/PRUNE-BUSY", 0o755))
	writeSweepFile(t, fs, dest, "current", time.Hour)
	writeSweepFile(t, fs, backup, "old", time.Hour)
	op := journalRow(t, repo, "job-pruned", "PRUNE-BUSY", dest, backup, 1, models.RevertStatusApplied)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))
	delete(repo.ops, op.ID)

	prev := acquireReplacementBusyExFn
	acquireReplacementBusyExFn = func(afero.Fs, string) (func(), string, error) {
		return nil, "", fsutil.ErrReplacementBusy
	}
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })

	err = NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, fsutil.ErrReplacementBusy)
	exists, statErr := afero.Exists(fs, backup)
	require.NoError(t, statErr)
	require.True(t, exists, "a busy destination must retain its backup")
}

func TestReplacementSweeper_PruneOperationBackups_ReportsConsumedProgress(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	destA := "/out/PRUNE-PARTIAL/a.jpg"
	backupA := destA + ".dlbak." + p3HexA
	destB := "/out/PRUNE-PARTIAL/b.jpg"
	backupB := destB + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll("/out/PRUNE-PARTIAL", 0o755))
	writeSweepFile(t, fs, destA, "current-a", time.Hour)
	writeSweepFile(t, fs, backupA, "old-a", time.Hour)
	writeSweepFile(t, fs, destB, "current-b", time.Hour)
	writeSweepFile(t, fs, backupB, "old-b", time.Hour)
	raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: destA, Backup: backupA, DestSeq: 1, Installed: true},
		{Destination: destB, Backup: backupB, DestSeq: 2, Installed: true},
	}})
	op := &models.BatchFileOperation{BatchJobID: "job-partial", MovieID: "PRUNE-PARTIAL", GeneratedFiles: raw, RevertStatus: models.RevertStatusApplied}
	require.NoError(t, repo.Create(context.Background(), op))
	delete(repo.ops, op.ID)

	prev := acquireReplacementBusyExFn
	acquireReplacementBusyExFn = func(fsys afero.Fs, dest string) (func(), string, error) {
		if dest == destB {
			return nil, "", fsutil.ErrReplacementBusy
		}
		return fsutil.AcquireReplacementBusyEx(fsys, dest)
	}
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })

	err := NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	var pruneErr *PruneOperationBackupsError
	require.ErrorAs(t, err, &pruneErr)
	require.NotEmpty(t, pruneErr.Error())
	consumed := pruneErr.ConsumedBackups()
	require.Contains(t, consumed[op.ID], backupA)
	require.NotContains(t, consumed[op.ID], backupB)
	existsA, statErr := afero.Exists(fs, backupA)
	require.NoError(t, statErr)
	require.False(t, existsA)
	existsB, statErr := afero.Exists(fs, backupB)
	require.NoError(t, statErr)
	require.True(t, existsB)
}

func TestReplacementSweeper_PruneOperationBackups_EarlyBranches(t *testing.T) {
	repo := newP3OpRepo()
	validRaw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
		Destination: "/out/EARLY/poster.jpg", Backup: "/out/EARLY/poster.jpg.dlbak." + p3HexA, Installed: true,
	}}})
	valid := models.BatchFileOperation{ID: 1, GeneratedFiles: validRaw}

	require.NoError(t, NewReplacementSweeper(afero.NewMemMapFs(), repo).PruneOperationBackups(context.Background(), nil))
	var nilSweeper *ReplacementSweeper
	require.Error(t, nilSweeper.PruneOperationBackups(context.Background(), []models.BatchFileOperation{valid}))
	require.Error(t, (&ReplacementSweeper{}).PruneOperationBackups(context.Background(), []models.BatchFileOperation{valid}))
	require.Error(t, NewReplacementSweeper(afero.NewMemMapFs(), repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{{ID: 2, GeneratedFiles: `{"replacements":`}}))
	incomplete := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Destination: "/out/EARLY/poster.jpg", Installed: true}}})
	require.Error(t, NewReplacementSweeper(afero.NewMemMapFs(), repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{{ID: 3, GeneratedFiles: incomplete}}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, NewReplacementSweeper(afero.NewMemMapFs(), repo).PruneOperationBackups(ctx, []models.BatchFileOperation{valid}))
}

func TestReplacementSweeper_PruneOperationBackups_BusyTokenUnavailable(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-token", "PRUNE-TOKEN", "/out/PRUNE-TOKEN/poster.jpg", "/out/PRUNE-TOKEN/poster.jpg.dlbak."+p3HexA, 1, models.RevertStatusApplied)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))
	delete(repo.ops, op.ID)
	swapBusyAcquireProvenanceUnavailable(t)

	err = NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.Contains(t, err.Error(), "provenance unavailable")
}

func TestReplacementSweeper_PruneOperationBackups_CancellationAfterBusyClaim(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-cancel", "PRUNE-CANCEL", "/out/PRUNE-CANCEL/poster.jpg", "/out/PRUNE-CANCEL/poster.jpg.dlbak."+p3HexA, 1, models.RevertStatusApplied)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))
	delete(repo.ops, op.ID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prev := acquireReplacementBusyExFn
	acquireReplacementBusyExFn = func(_ afero.Fs, _ string) (func(), string, error) {
		cancel()
		return func() {}, "token", nil
	}
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })

	err = NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, context.Canceled)
}

type pruneLedgerErrorRepo struct {
	*p3OpRepo
	err error
}

func (r *pruneLedgerErrorRepo) FindOperationsWithLedger(context.Context) ([]models.BatchFileOperation, error) {
	return nil, r.err
}

type cancelPruneLedgerRepo struct {
	*p3OpRepo
	cancel context.CancelFunc
}

func (r *cancelPruneLedgerRepo) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	rows, err := r.p3OpRepo.FindOperationsWithLedger(ctx)
	r.cancel()
	return rows, err
}

func installedPruneCandidate(t *testing.T, repo *p3OpRepo, jobID, movieID, dest, backup string) *models.BatchFileOperation {
	t.Helper()
	op := journalRow(t, repo, jobID, movieID, dest, backup, 1, models.RevertStatusApplied)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))
	delete(repo.ops, op.ID)
	return op
}

func TestReplacementSweeper_PruneOperationBackups_ErrorBranches(t *testing.T) {
	t.Run("live ledger read failure", func(t *testing.T) {
		base := newP3OpRepo()
		op := installedPruneCandidate(t, base, "job-read-failure", "PRUNE-READ", "/out/PRUNE-READ/poster.jpg", "/out/PRUNE-READ/poster.jpg.dlbak."+p3HexA)
		repo := &pruneLedgerErrorRepo{p3OpRepo: base, err: errors.New("ledger read failed")}
		err := NewReplacementSweeper(afero.NewMemMapFs(), repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
		require.Contains(t, err.Error(), "ledger read failed")
	})

	t.Run("malformed live ledger", func(t *testing.T) {
		repo := newP3OpRepo()
		op := installedPruneCandidate(t, repo, "job-malformed", "PRUNE-MALFORMED", "/out/PRUNE-MALFORMED/poster.jpg", "/out/PRUNE-MALFORMED/poster.jpg.dlbak."+p3HexA)
		require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`}))
		err := NewReplacementSweeper(afero.NewMemMapFs(), repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
		require.Contains(t, err.Error(), "cannot prove backup")
	})

	t.Run("cancellation before quarantine", func(t *testing.T) {
		base := newP3OpRepo()
		op := installedPruneCandidate(t, base, "job-cancel-prune", "PRUNE-CANCEL-LEDGER", "/out/PRUNE-CANCEL-LEDGER/poster.jpg", "/out/PRUNE-CANCEL-LEDGER/poster.jpg.dlbak."+p3HexA)
		ctx, cancel := context.WithCancel(context.Background())
		repo := &cancelPruneLedgerRepo{p3OpRepo: base, cancel: cancel}
		err := NewReplacementSweeper(afero.NewMemMapFs(), repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("absent backup is already consumed", func(t *testing.T) {
		repo := newP3OpRepo()
		op := installedPruneCandidate(t, repo, "job-absent", "PRUNE-ABSENT", "/out/PRUNE-ABSENT/poster.jpg", "/out/PRUNE-ABSENT/poster.jpg.dlbak."+p3HexA)
		err := NewReplacementSweeper(afero.NewMemMapFs(), repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
		require.NoError(t, err)
	})

	t.Run("unlink failure retains backup", func(t *testing.T) {
		inner := afero.NewMemMapFs()
		backup := "/out/PRUNE-UNLINK/poster.jpg.dlbak." + p3HexA
		fs := &removeFailFs{Fs: inner, victim: backup}
		require.NoError(t, fs.MkdirAll("/out/PRUNE-UNLINK", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/out/PRUNE-UNLINK/poster.jpg", []byte("current"), 0o644))
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
		repo := newP3OpRepo()
		op := installedPruneCandidate(t, repo, "job-unlink", "PRUNE-UNLINK", "/out/PRUNE-UNLINK/poster.jpg", backup)
		err := NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
		require.Error(t, err)
		exists, statErr := afero.Exists(fs, backup)
		require.NoError(t, statErr)
		require.True(t, exists)
	})
}

type cancelOnPruneQuarantineFs struct {
	afero.Fs
	cancel context.CancelFunc
	fired  bool
}

func (f *cancelOnPruneQuarantineFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.fired && strings.Contains(newname, backupQuarantineSuffix) {
		f.fired = true
		f.cancel()
	}
	return err
}

func TestReplacementSweeper_PruneOperationBackups_QuarantineFailure(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing"), backup))
	repo := newP3OpRepo()
	op := installedPruneCandidate(t, repo, "job-symlink", "PRUNE-SYMLINK", dest, backup)

	err := NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.Contains(t, err.Error(), "symlink")
	_, statErr := os.Lstat(backup)
	require.NoError(t, statErr, "a symlink occupant must remain untouched")
}

func TestReplacementSweeper_PruneOperationBackups_CancellationAfterQuarantine(t *testing.T) {
	base := afero.NewMemMapFs()
	ctx, cancel := context.WithCancel(context.Background())
	fs := &cancelOnPruneQuarantineFs{Fs: base, cancel: cancel}
	dest := "/out/PRUNE-CANCEL-QUAR/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/PRUNE-CANCEL-QUAR", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	repo := newP3OpRepo()
	op := installedPruneCandidate(t, repo, "job-cancel-quar", "PRUNE-CANCEL-QUAR", dest, backup)

	err := NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, context.Canceled)
	exists, statErr := afero.Exists(fs, backup)
	require.NoError(t, statErr)
	require.True(t, exists, "cancellation after quarantine must restore the backup name")
}

func mustRead2(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}
