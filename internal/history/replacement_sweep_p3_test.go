package history

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

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

func mustRead2(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return data
}
