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

	"github.com/javinizer/javinizer-go/internal/database"
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
		require.Contains(t, err.Error(), "install is unconfirmed")
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
		require.Contains(t, err.Error(), "ownership is rearm-refused")
		got, readErr := afero.ReadFile(fs, backup)
		require.NoError(t, readErr)
		require.Equal(t, "foreign", string(got))
	})
}

func TestReplacementSweeper_PruneOperationBackups_PendingMarkerFailureBlocksCleanup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-PENDING-FAIL/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/PRUNE-PENDING-FAIL", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	op := installedPruneCandidate(t, repo, "job-pending-fail", "PRUNE-PENDING-FAIL", dest, backup)
	delete(repo.ops, op.ID)

	err := NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.Contains(t, err.Error(), "owner row")
	exists, statErr := afero.Exists(fs, backup)
	require.NoError(t, statErr)
	require.True(t, exists)
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

	prev := acquireReplacementBusyExFn
	acquireReplacementBusyExFn = func(fsys afero.Fs, dest string) (func(), string, error) {
		if dest == destB {
			return nil, "", fsutil.ErrReplacementBusy
		}
		return fsutil.AcquireReplacementBusyEx(fsys, dest)
	}
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })

	err := NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, fsutil.ErrReplacementBusy)
	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, backupB, gf.Replacements[0].Backup, "partial cleanup retracts the consumed shared ledger entry")
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

type revokeOnLedgerRepo struct {
	*p3OpRepo
	dest string
}

func (r *revokeOnLedgerRepo) FindOperationsWithLedger(ctx context.Context) ([]models.BatchFileOperation, error) {
	rows, err := r.p3OpRepo.FindOperationsWithLedger(ctx)
	key := sweepBusyClaims.resolver.Key(r.dest)
	sweepBusyClaims.mu.Lock()
	if claim := sweepBusyClaims.byDest[key]; claim != nil {
		claim.revoke()
	}
	sweepBusyClaims.mu.Unlock()
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
		require.Contains(t, err.Error(), "is absent during prune")
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

type cancelOnPruneRemoveFs struct {
	afero.Fs
	cancel context.CancelFunc
	fired  bool
}

func (f *cancelOnPruneRemoveFs) Remove(name string) error {
	var size int64
	if strings.Contains(name, backupQuarantineSuffix) {
		if info, statErr := f.Fs.Stat(name); statErr == nil {
			size = info.Size()
		}
	}
	err := f.Fs.Remove(name)
	if err == nil && !f.fired && size > 0 {
		f.fired = true
		f.cancel()
	}
	return err
}

type cancelOnPruneQuarantineFs struct {
	afero.Fs
	cancel      context.CancelFunc
	fired       bool
	plantPath   string
	plantBytes  []byte
	postMoveErr error
	quarName    string
	revokeDest  string
	revoke      bool
}

func (f *cancelOnPruneQuarantineFs) Rename(oldname, newname string) error {
	err := f.Fs.Rename(oldname, newname)
	if err == nil && !f.fired && strings.Contains(newname, backupQuarantineSuffix) && !strings.Contains(oldname, backupQuarantineSuffix) {
		f.fired = true
		f.quarName = newname
		if f.revoke {
			key := sweepBusyClaims.resolver.Key(f.revokeDest)
			sweepBusyClaims.mu.Lock()
			if claim := sweepBusyClaims.byDest[key]; claim != nil {
				claim.revoke()
			}
			sweepBusyClaims.mu.Unlock()
		}
		if f.plantPath != "" {
			if werr := afero.WriteFile(f.Fs, f.plantPath, f.plantBytes, 0o644); werr != nil {
				return werr
			}
		}
		f.cancel()
	}
	return err
}

func (f *cancelOnPruneQuarantineFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if f.postMoveErr != nil && f.fired && name == f.quarName {
		return nil, false, f.postMoveErr
	}
	if ls, ok := f.Fs.(afero.Lstater); ok {
		return ls.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
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

func TestReplacementSweeper_PruneOperationBackups_PersistsPartialPostMoveHold(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/PRUNE-QUAR-VERIFY/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	fs := &cancelOnPruneQuarantineFs{Fs: base, cancel: func() {}, plantPath: backup, plantBytes: []byte("foreign"), postMoveErr: errors.New("post-move verification failed")}
	require.NoError(t, fs.MkdirAll("/out/PRUNE-QUAR-VERIFY", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-quar-verify", "PRUNE-QUAR-VERIFY", dest, backup, 1, models.RevertStatusApplied)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))

	err = NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.Contains(t, err.Error(), "post-move verification failed")
	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindPrune, gf.Replacements[0].PendingKind())
	require.Contains(t, gf.Replacements[0].Backup, backupQuarantineSuffix)
}

func TestReplacementSweeper_PruneOperationBackups_RevokedAfterQuarantinePersistsPointer(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/PRUNE-REVOKE-UNLINK/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	fs := &cancelOnPruneQuarantineFs{Fs: base, cancel: func() {}, revoke: true, revokeDest: dest}
	require.NoError(t, fs.MkdirAll("/out/PRUNE-REVOKE-UNLINK", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-revoke-unlink", "PRUNE-REVOKE-UNLINK", dest, backup, 1, models.RevertStatusApplied)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))

	err = NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, fsutil.ErrReplacementBusy)
	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindPrune, gf.Replacements[0].PendingKind())
	require.Contains(t, gf.Replacements[0].Backup, backupQuarantineSuffix)
}

func TestReplacementSweeper_PruneOperationBackups_PersistsQuarantineAfterRestoreRefusal(t *testing.T) {
	base := afero.NewMemMapFs()
	ctx, cancel := context.WithCancel(context.Background())
	dest := "/out/PRUNE-QUAR-PERSIST/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	fs := &cancelOnPruneQuarantineFs{Fs: base, cancel: cancel, plantPath: backup, plantBytes: []byte("foreign")}
	require.NoError(t, fs.MkdirAll("/out/PRUNE-QUAR-PERSIST", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-quar-persist", "PRUNE-QUAR-PERSIST", dest, backup, 1, models.RevertStatusApplied)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))

	err = NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, context.Canceled)
	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.Replacements, 1)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindPrune, gf.Replacements[0].PendingKind())
	require.Contains(t, gf.Replacements[0].Backup, backupQuarantineSuffix)
	foreign, readErr := afero.ReadFile(fs, backup)
	require.NoError(t, readErr)
	require.Equal(t, "foreign", string(foreign))
}

func TestReplacementSweeper_PruneOperationBackups_CancellationRetractsPriorConsumption(t *testing.T) {
	base := afero.NewMemMapFs()
	ctx, cancel := context.WithCancel(context.Background())
	fs := &cancelOnPruneRemoveFs{Fs: base, cancel: cancel}
	jobID := "job-cancel-retract"
	destA := "/out/PRUNE-CANCEL-RETRACT/a.jpg"
	backupA := destA + ".dlbak." + p3HexA
	destB := "/out/PRUNE-CANCEL-RETRACT/b.jpg"
	backupB := destB + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll("/out/PRUNE-CANCEL-RETRACT", 0o755))
	for _, item := range []struct{ path, data string }{{destA, "current-a"}, {backupA, "old-a"}, {destB, "current-b"}, {backupB, "old-b"}} {
		require.NoError(t, afero.WriteFile(fs, item.path, []byte(item.data), 0o644))
	}
	raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: destA, Backup: backupA, Installed: true},
		{Destination: destB, Backup: backupB, Installed: true},
	}})
	repo := newP3OpRepo()
	op := &models.BatchFileOperation{BatchJobID: jobID, GeneratedFiles: raw, RevertStatus: models.RevertStatusApplied}
	require.NoError(t, repo.Create(context.Background(), op))

	err := NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, context.Canceled)
	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, backupB, gf.Replacements[0].Backup)
}

func TestRetractConsumedEntries_LockCancellationSurfaces(t *testing.T) {
	key := "0"
	hold := fsutil.SharedJournalLocks().Acquire(key)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sweeper := NewReplacementSweeper(afero.NewMemMapFs(), newP3OpRepo())
	err := sweeper.retractConsumedEntries(ctx, map[uint]map[string]struct{}{0: {"/x": {}}}, map[uint]struct{}{0: {}})
	require.ErrorIs(t, err, context.Canceled)
	hold()
	probe := fsutil.SharedJournalLocks().Acquire(key)
	probe()
}

func TestAcquireJournalLockWithin_BoundsCanceledWait(t *testing.T) {
	key := "prune-lock-cancel"
	hold := fsutil.SharedJournalLocks().Acquire(key)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := acquireJournalLockWithin(ctx, key)
	require.ErrorIs(t, err, context.Canceled)
	hold()
	probe := fsutil.SharedJournalLocks().Acquire(key)
	probe()
}

type pruneRemovePlantFs struct {
	*removeFailFs
	plantPath  string
	plantBytes []byte
	fired      bool
}

func (f *pruneRemovePlantFs) Rename(oldname, newname string) error {
	err := f.removeFailFs.Fs.Rename(oldname, newname)
	if err == nil && !f.fired && strings.Contains(newname, backupQuarantineSuffix) && !strings.Contains(oldname, backupQuarantineSuffix) {
		f.fired = true
		if werr := afero.WriteFile(f.removeFailFs.Fs, f.plantPath, f.plantBytes, 0o644); werr != nil {
			return werr
		}
	}
	return err
}

func TestReplacementSweeper_PruneOperationBackups_PersistsAfterUnlinkRestoreFailure(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/PRUNE-UNLINK-RESTORE/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	fs := &pruneRemovePlantFs{removeFailFs: &removeFailFs{Fs: base, victim: backup}, plantPath: backup, plantBytes: []byte("foreign")}
	require.NoError(t, fs.MkdirAll("/out/PRUNE-UNLINK-RESTORE", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	repo := newP3OpRepo()
	op := journalRow(t, repo, "job-unlink-restore", "PRUNE-UNLINK-RESTORE", dest, backup, 1, models.RevertStatusApplied)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))

	err = NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.Contains(t, err.Error(), "journaled name")
	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.True(t, gf.Replacements[0].RestorePending)
	require.Equal(t, models.RestorePendingKindPrune, gf.Replacements[0].PendingKind())
	require.Contains(t, gf.Replacements[0].Backup, backupQuarantineSuffix)
}

func TestPersistPruneQuarantine_LedgerBranches(t *testing.T) {
	repo := newP3OpRepo()
	bad := &models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`}
	require.NoError(t, repo.Create(context.Background(), bad))
	sweeper := NewReplacementSweeper(afero.NewMemMapFs(), repo)
	entry := models.ReplacementEntry{Destination: "/out/persist/dest", Backup: "/out/persist/backup"}
	require.Error(t, sweeper.persistPruneQuarantine(bad.ID, entry, "/out/persist/backup.dlq.token"))

	good := journalRow(t, repo, "job-persist-no-match", "PRUNE-PERSIST-NOMATCH", "/out/persist/other", "/out/persist/other.dlbak."+p3HexA, 1, models.RevertStatusApplied)
	require.NoError(t, sweeper.persistPruneQuarantine(good.ID, entry, "/out/persist/backup.dlq.token"))
	row, findErr := repo.FindByID(context.Background(), good.ID)
	require.NoError(t, findErr)
	require.Equal(t, good.GeneratedFiles, row.GeneratedFiles)
}

func TestReplacementSweeper_RetractConsumedEntries_Branches(t *testing.T) {
	repo := newP3OpRepo()
	destA := "/out/RETRACT/a.jpg"
	backupA := destA + ".dlbak." + p3HexA
	destB := "/out/RETRACT/b.jpg"
	backupB := destB + ".dlbak." + p3HexB
	opA := &models.BatchFileOperation{BatchJobID: "retract-a", GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: destA, Backup: backupA, Installed: true},
		{Destination: destB, Backup: backupB, Installed: true},
	}})}
	opB := &models.BatchFileOperation{BatchJobID: "retract-b", GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
		Destination: "/out/RETRACT/c.jpg", Backup: "/out/RETRACT/c.jpg.dlbak." + p3HexC, Installed: true,
	}}})}
	bad := &models.BatchFileOperation{BatchJobID: "retract-bad", GeneratedFiles: `{"replacements":broken`}
	require.NoError(t, repo.Create(context.Background(), opA))
	require.NoError(t, repo.Create(context.Background(), opB))
	require.NoError(t, repo.Create(context.Background(), bad))
	sweeper := NewReplacementSweeper(afero.NewMemMapFs(), repo)
	require.NoError(t, sweeper.retractConsumedEntries(nil, map[uint]map[string]struct{}{opA.ID: {"/unmatched": {}}}, map[uint]struct{}{opA.ID: {}}))

	err := sweeper.retractConsumedEntries(
		context.Background(),
		map[uint]map[string]struct{}{opA.ID: {backupA: {}}},
		map[uint]struct{}{opA.ID: {}, opB.ID: {}, bad.ID: {}, 999: {}},
	)
	require.Error(t, err)
	row, findErr := repo.FindByID(context.Background(), opA.ID)
	require.NoError(t, findErr)
	gf, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.Len(t, gf.Replacements, 1)
	require.Equal(t, backupB, gf.Replacements[0].Backup)
}

type retractErrorRepo struct {
	*p3OpRepo
	err   error
	calls int
}

func (r *retractErrorRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	r.calls++
	if r.calls > 1 {
		return r.err
	}
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}

func TestReplacementSweeper_PrunePendingRetryConsumesWithoutRestore(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-PENDING-RETRY/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	op := installedPruneCandidate(t, repo, "job-prune-pending-retry", "PRUNE-PENDING-RETRY", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))

	healed, err := NewReplacementSweeper(base, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	exists, statErr := afero.Exists(base, backup)
	require.NoError(t, statErr)
	require.False(t, exists)
	require.Equal(t, "organized", string(mustRead2(t, base, dest)))
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

func TestReplacementSweeper_PrunePendingRetryConsumesAbsentBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-PENDING-ABSENT/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	op := installedPruneCandidate(t, repo, "job-prune-pending-absent", "PRUNE-PENDING-ABSENT", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))

	healed, err := NewReplacementSweeper(base, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

func TestReplacementSweeper_SweepRecoversPruneQuarantine(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-QUAR-CRASH/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	quarantine := backup + backupQuarantineSuffix + "0123456789abcdef0123456789abcdef"
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	require.NoError(t, afero.WriteFile(base, quarantine, []byte("old"), 0o644))
	op := installedPruneCandidate(t, repo, "job-prune-quar-crash", "PRUNE-QUAR-CRASH", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))

	healed, err := NewReplacementSweeper(base, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	exists, statErr := afero.Exists(base, quarantine)
	require.NoError(t, statErr)
	require.False(t, exists)
	require.Empty(t, requireLedgerReplacements(t, repo, op.ID))
}

func TestReplacementSweeper_ConsumePrunePendingClaimBranches(t *testing.T) {
	base := afero.NewMemMapFs()
	idx := &replacementLedgerIndex{journaled: map[string]*models.BatchFileOperation{}}
	entry := prunePendingLedgerEntry{dest: "/out/PRUNE-CLAIM/poster.jpg", backup: "/out/PRUNE-CLAIM/poster.jpg.dlbak." + p3HexA}
	prev := acquireReplacementBusyExFn
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })

	acquireReplacementBusyExFn = func(afero.Fs, string) (func(), string, error) {
		return nil, "", fsutil.ErrReplacementBusy
	}
	require.Zero(t, NewReplacementSweeper(base, newP3OpRepo()).consumePrunePending(context.Background(), idx, entry))
	acquireReplacementBusyExFn = func(afero.Fs, string) (func(), string, error) {
		return nil, "", errors.New("busy lookup failed")
	}
	require.Zero(t, NewReplacementSweeper(base, newP3OpRepo()).consumePrunePending(context.Background(), idx, entry))
	released := false
	acquireReplacementBusyExFn = func(afero.Fs, string) (func(), string, error) {
		return func() { released = true }, "", nil
	}
	require.Zero(t, NewReplacementSweeper(base, newP3OpRepo()).consumePrunePending(context.Background(), idx, entry))
	require.True(t, released)
	acquireReplacementBusyExFn = func(afero.Fs, string) (func(), string, error) { return func() {}, "token", nil }
	require.Zero(t, NewReplacementSweeper(base, newP3OpRepo()).consumePrunePending(context.Background(), idx, entry))
}

func TestReplacementSweeper_ConsumePrunePendingBindRevoked(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-CLAIM-REVOKE/poster.jpg"
	entry := prunePendingLedgerEntry{rowID: 1, dest: dest, backup: dest + ".dlbak." + p3HexA, backupSlash: sweepSlash(dest + ".dlbak." + p3HexA)}
	prev := acquireReplacementBusyExFn
	acquireReplacementBusyExFn = func(afero.Fs, string) (func(), string, error) { return func() {}, "token", nil }
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })
	lockRelease := fsutil.SharedDestLocks().Acquire(dest)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan int, 1)
	go func() {
		result <- NewReplacementSweeper(base, repo).consumePrunePending(ctx, &replacementLedgerIndex{journaled: map[string]*models.BatchFileOperation{}}, entry)
	}()
	cancel()
	require.Eventually(t, func() bool { return reclaimAbandonedSweepBusyMarker(dest) }, time.Second, time.Millisecond)
	lockRelease()
	require.Zero(t, <-result)
}

func TestReplacementSweeper_JournalEntryDestinationBranches(t *testing.T) {
	require.Empty(t, journalEntryDestination(&models.BatchFileOperation{GeneratedFiles: `{"replacements":broken`}, "/x"))
	require.Empty(t, journalEntryDestination(&models.BatchFileOperation{GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Backup: "/other"}}})}, "/missing"))
}

func TestReplacementSweeper_PrunePendingRemovalFailureKeepsIntent(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/PRUNE-PENDING-REMOVE/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	repo := newP3OpRepo()
	op := installedPruneCandidate(t, repo, "job-prune-remove-failure", "PRUNE-PENDING-REMOVE", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))
	fs := &w8RemoveFs{Fs: base, victim: backup, err: errors.New("prune unlink wedged"), fail: true}
	require.False(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(context.Background(), op.ID, backup, dest, sweepSlash(backup)))
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	got, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Equal(t, models.RestorePendingKindPrune, got.Replacements[0].PendingKind())
}

func TestReplacementSweeper_PrunePendingConsumptionFailureKeepsIntent(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/PRUNE-PENDING-CONSUME/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	baseRepo := newP3OpRepo()
	op := installedPruneCandidate(t, baseRepo, "job-prune-consume-failure", "PRUNE-PENDING-CONSUME", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, baseRepo.Update(context.Background(), op))
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: errors.New("prune consume wedged"), 3: errors.New("prune marker persist wedged")}}
	require.False(t, NewReplacementSweeper(base, repo).retryPendingRemoval(context.Background(), op.ID, backup, dest, sweepSlash(backup)))
	row, err := baseRepo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	got, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Equal(t, models.RestorePendingKindPrune, got.Replacements[0].PendingKind())
}

func TestReplacementSweeper_PrunePendingAbsentConsumptionFailureKeepsIntent(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/PRUNE-PENDING-ABSENT-FAIL/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	baseRepo := newP3OpRepo()
	op := installedPruneCandidate(t, baseRepo, "job-prune-absent-failure", "PRUNE-PENDING-ABSENT-FAIL", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, baseRepo.Update(context.Background(), op))
	repo := &w18TxFailRepo{p3OpRepo: baseRepo, fail: map[int]error{2: errors.New("prune absent consume wedged")}}
	require.False(t, NewReplacementSweeper(base, repo).retryPendingRemoval(context.Background(), op.ID, backup, dest, sweepSlash(backup)))
}

func TestReplacementSweeper_PrunePendingPersistsPartialQuarantine(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/PRUNE-PENDING-DLQ/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	repo := newP3OpRepo()
	op := installedPruneCandidate(t, repo, "job-prune-dlq", "PRUNE-PENDING-DLQ", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))
	fs := &cancelOnPruneQuarantineFs{Fs: base, cancel: func() {}, plantPath: backup, plantBytes: []byte("foreign"), postMoveErr: errors.New("prune retry post-move wedge")}
	require.False(t, NewReplacementSweeper(fs, repo).retryPendingRemoval(context.Background(), op.ID, backup, dest, sweepSlash(backup)))
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	got, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Contains(t, got.Replacements[0].Backup, backupQuarantineSuffix)
}

// cancelSweepReadDirErrorFs cancels after the ledger index is built, then
// makes the directory scan fail. That leaves the final prune-pending loop as
// the next cancellation observation point.
type cancelSweepReadDirErrorFs struct {
	afero.Fs
	dir    string
	cancel context.CancelFunc
	fired  bool
}

func (f *cancelSweepReadDirErrorFs) Open(name string) (afero.File, error) {
	if !f.fired && filepath.Clean(name) == filepath.Clean(f.dir) {
		f.fired = true
		f.cancel()
		return nil, errors.New("directory scan canceled")
	}
	return f.Fs.Open(name)
}

func TestReplacementSweeper_Sweep_PrunePendingCancellationBeforeRetry(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/out/PRUNE-PENDING-CANCEL"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	repo := newP3OpRepo()
	op := installedPruneCandidate(t, repo, "job-prune-cancel", "PRUNE-PENDING-CANCEL", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))

	ctx, cancel := context.WithCancel(context.Background())
	fs := &cancelSweepReadDirErrorFs{Fs: base, dir: dir, cancel: cancel}
	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, healed)

	row, findErr := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, findErr)
	got, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	require.True(t, got.Replacements[0].RestorePending, "cancellation must not consume the pending prune intent")
	require.Equal(t, models.RestorePendingKindPrune, got.Replacements[0].PendingKind())
}

func TestReplacementSweeper_PrunePendingIndeterminateBackupKeepsIntent(t *testing.T) {
	base := afero.NewMemMapFs()
	dest := "/out/PRUNE-PENDING-IND/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	baseRepo := newP3OpRepo()
	op := installedPruneCandidate(t, baseRepo, "job-prune-indeterminate", "PRUNE-PENDING-IND", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, baseRepo.Update(context.Background(), op))
	fs := &indeterminateStatFs{Fs: base, failPath: backup}
	require.False(t, NewReplacementSweeper(fs, baseRepo).retryPendingRemoval(context.Background(), op.ID, backup, dest, sweepSlash(backup)))
}

func TestReplacementSweeper_PrunePendingAbsentRevocationKeepsIntent(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-PENDING-REVOKE/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	op := installedPruneCandidate(t, repo, "job-prune-revoke", "PRUNE-PENDING-REVOKE", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))
	claim, _, reclaim := w52LiveClaim(t, filepath.ToSlash(dest))
	fs := &w52LstatStageFs{Fs: base, match: filepath.ToSlash(backup), stage: reclaim}
	require.False(t, NewReplacementSweeper(fs, repo).retryPendingRemovalClaimed(context.Background(), op.ID, backup, dest, sweepSlash(backup), claim))
}

func TestReverter_PrunePendingIsNotRestoreComplete(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-REVERT/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(filepath.Dir(dest), 0o755))
	require.NoError(t, afero.WriteFile(base, dest, []byte("organized"), 0o644))
	require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))
	op := installedPruneCandidate(t, repo, "job-prune-revert", "PRUNE-REVERT", dest, backup)
	gf, err := models.ParseGeneratedFiles(op.GeneratedFiles)
	require.NoError(t, err)
	require.True(t, gf.Replacements[0].SetRestorePending(models.RestorePendingKindPrune))
	op.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), op))
	_, err = NewReverter(base, repo).restoreReplacementJournal(context.Background(), op)
	require.Contains(t, err.Error(), "prune-pending")
}

func TestReplacementSweeper_PruneOperationBackups_RetractionFailureSurfaces(t *testing.T) {
	base := newP3OpRepo()
	destA := "/out/RETRACT-FAIL/a.jpg"
	backupA := destA + ".dlbak." + p3HexA
	destB := "/out/RETRACT-FAIL/b.jpg"
	backupB := destB + ".dlbak." + p3HexB
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/RETRACT-FAIL", 0o755))
	writeSweepFile(t, fs, destA, "current-a", time.Hour)
	writeSweepFile(t, fs, backupA, "old-a", time.Hour)
	writeSweepFile(t, fs, destB, "current-b", time.Hour)
	writeSweepFile(t, fs, backupB, "old-b", time.Hour)
	raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: destA, Backup: backupA, Installed: true},
		{Destination: destB, Backup: backupB, Installed: true},
	}})
	op := &models.BatchFileOperation{BatchJobID: "retract-failure", GeneratedFiles: raw, RevertStatus: models.RevertStatusApplied}
	require.NoError(t, base.Create(context.Background(), op))
	repo := &retractErrorRepo{p3OpRepo: base, err: errors.New("journal retraction failed")}
	prev := acquireReplacementBusyExFn
	acquireReplacementBusyExFn = func(fsys afero.Fs, dest string) (func(), string, error) {
		if dest == destB {
			return nil, "", fsutil.ErrReplacementBusy
		}
		return fsutil.AcquireReplacementBusyEx(fsys, dest)
	}
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })

	err := NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.Contains(t, err.Error(), "retract consumed entries")
}

func TestJobRepository_PruneHookFilesystemIntegration(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repos := db.Repositories()
	setter, ok := repos.JobRepo.(interface {
		SetOrganizedJobPruneHook(func(context.Context, []models.BatchFileOperation) error)
	})
	require.True(t, ok)
	fs := afero.NewMemMapFs()
	sweeper := NewReplacementSweeper(fs, repos.BatchFileOpRepo)
	setter.SetOrganizedJobPruneHook(sweeper.PruneOperationBackups)

	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	job := &models.Job{ID: "integration-prune-job", Status: models.JobStatusOrganized, StartedAt: organizedAt, OrganizedAt: &organizedAt}
	require.NoError(t, repos.JobRepo.Create(context.Background(), job))
	dest := "/out/INTEGRATION/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/INTEGRATION", 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("current"), 0o644))
	require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
	raw := models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Destination: dest, Backup: backup, Installed: true}}})
	op := &models.BatchFileOperation{BatchJobID: job.ID, OriginalPath: "/src/integration.mkv", NewPath: dest, OperationType: models.OperationTypeUpdate, GeneratedFiles: raw}
	require.NoError(t, repos.BatchFileOpRepo.Create(context.Background(), op))

	require.NoError(t, repos.JobRepo.DeleteOrganizedOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour)))
	_, err = repos.JobRepo.FindByID(context.Background(), job.ID)
	require.Error(t, err)
	_, err = repos.BatchFileOpRepo.FindByID(context.Background(), op.ID)
	require.Error(t, err)
	exists, statErr := afero.Exists(fs, backup)
	require.NoError(t, statErr)
	require.False(t, exists, "the real repository hook must consume the final unreferenced backup")
}

func TestReplacementSweeper_PruneOperationBackups_RevokedBeforeQuarantine(t *testing.T) {
	fs := afero.NewMemMapFs()
	base := newP3OpRepo()
	dest := "/out/PRUNE-REVOKE/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	op := installedPruneCandidate(t, base, "job-revoke", "PRUNE-REVOKE", dest, backup)
	repo := &revokeOnLedgerRepo{p3OpRepo: base, dest: dest}

	err := NewReplacementSweeper(fs, repo).PruneOperationBackups(context.Background(), []models.BatchFileOperation{*op})
	require.ErrorIs(t, err, fsutil.ErrReplacementBusy)
}

func TestReplacementSweeper_PruneOperationBackups_RevokedBeforeBind(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/PRUNE-REVOKE-BIND/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	op := installedPruneCandidate(t, repo, "job-revoke-bind", "PRUNE-REVOKE-BIND", dest, backup)
	acquired := make(chan struct{})
	prev := acquireReplacementBusyExFn
	acquireReplacementBusyExFn = func(_ afero.Fs, _ string) (func(), string, error) {
		close(acquired)
		return func() {}, "token", nil
	}
	t.Cleanup(func() { acquireReplacementBusyExFn = prev })
	lockRelease := fsutil.SharedDestLocks().Acquire(dest)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- NewReplacementSweeper(fs, repo).PruneOperationBackups(ctx, []models.BatchFileOperation{*op})
	}()
	<-acquired
	cancel()
	require.Eventually(t, func() bool { return reclaimAbandonedSweepBusyMarker(dest) }, time.Second, time.Millisecond)
	lockRelease()
	err := <-errCh
	require.Contains(t, err.Error(), "revoked")
}

func mustRead2(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}
