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

// KW3 regression coverage for codex P2 review 4960491781: a targeted sweep
// whose `destinations` list holds several files in ONE folder must scan that
// folder once and arbitrate candidates against EVERY requested destination.
// Before the fix the first destination marked the directory as seen and the
// scan matched only against that destination's key, so a crash-window backup
// for the second destination leaked into the revert's destination-conflict
// checks.

// kw3Replacements returns the live journaled replacement count for an op.
func kw3Replacements(t *testing.T, repo *p3OpRepo, id uint) int {
	t.Helper()
	row, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	return len(gf.Replacements)
}

func TestSweepDestinationsCovKW3_SharedDirRestoresEveryDestinationInOnePass(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/KW3-SHARED"
	coverDest := dir + "/cover.jpg"
	posterDest := dir + "/poster.jpg"
	coverBackup := coverDest + ".dlbak." + p3HexA
	posterBackup := posterDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, coverBackup, "old-cover", time.Hour)
	writeSweepFile(t, fs, posterBackup, "old-poster", time.Hour)
	coverOp := journalRow(t, repo, "job-kw3", "KW3-SHA-C", coverDest, coverBackup, 1, models.RevertStatusApplied)
	posterOp := journalRow(t, repo, "job-kw3", "KW3-SHA-P", posterDest, posterBackup, 2, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{coverDest, posterDest})
	require.NoError(t, err)
	require.Equal(t, 2, healed,
		"both destinations in one folder must arbitrate in a single sweep pass, not just the first")
	require.Equal(t, "old-cover", string(mustRead2(t, fs, coverDest)))
	require.Equal(t, "old-poster", string(mustRead2(t, fs, posterDest)))
	for _, backup := range []string{coverBackup, posterBackup} {
		_, err := fs.Stat(backup)
		require.ErrorIs(t, err, os.ErrNotExist, "consumed backup removed: %s", backup)
	}
	require.Zero(t, kw3Replacements(t, repo, coverOp.ID), "cover crash-window entry consumed")
	require.Zero(t, kw3Replacements(t, repo, posterOp.ID), "poster crash-window entry consumed")
}

func TestSweepDestinationsCovKW3_SharedDirArbitratesPerGroupEntry(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/KW3-PARTIAL"
	coverDest := dir + "/cover.jpg"
	posterDest := dir + "/poster.jpg"
	coverBackup := coverDest + ".dlbak." + p3HexA
	posterBackup := posterDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, coverBackup, "old-cover", time.Hour)
	writeSweepFile(t, fs, posterDest, "current-poster", time.Hour)
	writeSweepFile(t, fs, posterBackup, "old-poster", time.Hour)
	coverOp := journalRow(t, repo, "job-kw3", "KW3-PAR-C", coverDest, coverBackup, 1, models.RevertStatusApplied)
	// Poster's entry is install-confirmed: retention wins over restore, so only
	// cover's armed crash-window entry may heal.
	raw, err := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{
		{Destination: posterDest, Backup: posterBackup, DestSeq: 1, Installed: true},
	}})
	require.NoError(t, err)
	posterOp := &models.BatchFileOperation{
		BatchJobID: "job-kw3", MovieID: "KW3-PAR-P", OriginalPath: "/src/kw3-par-p.mkv",
		OperationType: models.OperationTypeUpdate, GeneratedFiles: string(raw),
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, posterOp))

	// posterDest is listed FIRST: the grouped scan must still reach cover's
	// armed entry (pre-fix the first destination alone filtered the scan).
	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{posterDest, coverDest})
	require.NoError(t, err)
	require.Equal(t, 1, healed, "only the armed crash-window entry is restored")
	require.Equal(t, "old-cover", string(mustRead2(t, fs, coverDest)))
	require.Zero(t, kw3Replacements(t, repo, coverOp.ID), "cover entry consumed")
	require.Equal(t, "current-poster", string(mustRead2(t, fs, posterDest)), "installed destination untouched")
	require.Equal(t, "old-poster", string(mustRead2(t, fs, posterBackup)), "install-confirmed backup retained")
	require.Equal(t, 1, kw3Replacements(t, repo, posterOp.ID), "install-confirmed entry still journaled")
}

func TestSweepDestinationsCovKW3_FoldedSpellingsShareOneScan(t *testing.T) {
	w10SetCaseProbe(t, false) // insensitive/tolerant destination root
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	enumDir := "/Out/KW3-FOLD" // only this on-disk spelling resolves
	altDir := "/out/kw3-fold"
	coverDest := enumDir + "/cover.jpg"
	posterDestFolded := altDir + "/poster.jpg"
	coverBackup := coverDest + ".dlbak." + p3HexA
	posterBackup := enumDir + "/poster.jpg.dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(enumDir, 0o755))
	writeSweepFile(t, fs, coverBackup, "old-cover", time.Hour)
	writeSweepFile(t, fs, posterBackup, "old-poster", time.Hour)
	coverOp := journalRow(t, repo, "job-kw3", "KW3-FLD-C", coverDest, coverBackup, 1, models.RevertStatusApplied)
	posterOp := journalRow(t, repo, "job-kw3", "KW3-FLD-P", posterDestFolded, posterBackup, 2, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{coverDest, posterDestFolded})
	require.NoError(t, err)
	require.Equal(t, 2, healed,
		"case-folded spellings of one insensitive folder group into a single scan")
	require.Equal(t, "old-cover", string(mustRead2(t, fs, coverDest)))
	require.Equal(t, "old-poster", string(mustRead2(t, fs, enumDir+"/poster.jpg")))
	require.Zero(t, kw3Replacements(t, repo, coverOp.ID))
	require.Zero(t, kw3Replacements(t, repo, posterOp.ID))
}

func TestSweepDestinationsCovKW3_SingleDestinationRegression(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/KW3-ONE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	siblingDest := dir + "/fanart.jpg"
	siblingBackup := siblingDest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "old-poster", time.Hour)
	writeSweepFile(t, fs, siblingBackup, "old-fanart", time.Hour)
	op := journalRow(t, repo, "job-kw3", "KW3-ONE-P", dest, backup, 1, models.RevertStatusApplied)
	siblingOp := journalRow(t, repo, "job-kw3", "KW3-ONE-F", siblingDest, siblingBackup, 2, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{dest})
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "old-poster", string(mustRead2(t, fs, dest)))
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, kw3Replacements(t, repo, op.ID))
	require.Equal(t, "old-fanart", string(mustRead2(t, fs, siblingBackup)),
		"an unlisted destination's backup in the same folder stays untouched")
	require.Equal(t, 1, kw3Replacements(t, repo, siblingOp.ID), "unlisted sibling entry remains armed")
	_, err = fs.Stat(siblingDest)
	require.ErrorIs(t, err, os.ErrNotExist, "sibling destination is not restored by the targeted sweep")
}
