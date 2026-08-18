package history

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReverterUndoPresentCovW28_MarkerFailureRetainsDestinationAndRetries(t *testing.T) {
	base := afero.NewMemMapFs()
	dest, backup := newW27ArmedReplacement(t, base, "W28-PRESENT")
	require.NoError(t, afero.WriteFile(base, dest, []byte("installed"), 0o644))

	repo := newP3OpRepo()
	op := w27CreateArmedReplacementRow(t, repo, dest, backup)
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	gf.Replacements[0].Installed = true
	row.GeneratedFiles = models.MarshalLedgerJSON(gf)
	require.NoError(t, repo.Update(context.Background(), row))
	op.GeneratedFiles = row.GeneratedFiles

	removeErr := errors.New("backup remove wedged")
	markerErr := errors.New("marker update transient")
	fs := &w27RemoveFailFs{Fs: base, backup: backup, backupErr: removeErr, failBackup: true}
	failingRepo := &failingUpdateRepo{p3OpRepo: repo, updateErr: markerErr}
	ctx := context.Background()

	restored, err := NewReverter(fs, failingRepo).restoreReplacementJournal(ctx, op)
	require.ErrorIs(t, err, removeErr)
	require.True(t, restored[dest])
	require.Equal(t, "old", string(mustRead2(t, base, dest)), "a present destination must survive marker persistence failure")
	require.Equal(t, "old", string(mustRead2(t, base, backup)))

	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the failed cleanup must leave the entry armed")
	require.True(t, gf.Replacements[0].Installed)
	require.False(t, gf.Replacements[0].RestorePending, "the failed marker update cannot set RestorePending")

	fs.failBackup = false
	failingRepo.updateErr = nil
	retryOp, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	restored, err = NewReverter(fs, repo).restoreReplacementJournal(ctx, retryOp)
	require.NoError(t, err, "a fresh explicit revert must retry the armed present-destination state")
	require.True(t, restored[dest])
	require.Equal(t, "old", string(mustRead2(t, base, dest)))
	_, err = base.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)

	row, err = repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err = models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements)
}
