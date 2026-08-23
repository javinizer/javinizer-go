package history

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// TestSweep_ReconcilesIntentBeforeMoveCrash covers both pre-install crash
// states: an intent with no backup only remains journaled for retry, while an
// uninstalled intent with its backup restores the missing destination.
func TestSweep_ReconcilesIntentBeforeMoveCrash(t *testing.T) {
	ctx := context.Background()

	t.Run("intent without backup only cleans stage state", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dest := "/out/INTENT-NOBACK/poster.jpg"
		backup := dest + ".dlbak." + p3HexA
		require.NoError(t, fs.MkdirAll("/out/INTENT-NOBACK", 0o755))
		op := journalRow(t, repo, "job-intent-noback", "INTENT-NOBACK", dest, backup, 1, models.RevertStatusApplied)

		restored := NewReplacementSweeper(fs, repo).restoreAndConsume(ctx, op, backup, dest, sweepSlash(backup), nil)
		require.False(t, restored)
		_, err := fs.Stat(dest)
		require.ErrorIs(t, err, os.ErrNotExist)
		_, err = fs.Stat(backup)
		require.ErrorIs(t, err, os.ErrNotExist)
		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
		require.NoError(t, err)
		require.Len(t, gf.Replacements, 1, "missing backup keeps intent for a later retry")
	})

	t.Run("backup without completed install restores destination", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		repo := newP3OpRepo()
		dir := "/out/INTENT-RESTORE"
		dest := dir + "/poster.jpg"
		backup := dest + ".dlbak." + p3HexB
		require.NoError(t, fs.MkdirAll(dir, 0o755))
		writeSweepFile(t, fs, backup, "pre-crash", time.Hour)
		op := journalRow(t, repo, "job-intent-restore", "INTENT-RESTORE", dest, backup, 1, models.RevertStatusApplied)

		healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, healed)
		data, err := afero.ReadFile(fs, dest)
		require.NoError(t, err)
		require.Equal(t, "pre-crash", string(data))
		_, err = fs.Stat(backup)
		require.ErrorIs(t, err, os.ErrNotExist)
		row, err := repo.FindByID(ctx, op.ID)
		require.NoError(t, err)
		gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
		require.NoError(t, err)
		require.Empty(t, gf.Replacements)
	})
}
