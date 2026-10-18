package downloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReplacementBatchNilRecoveryIdentityRemainsUnproven(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source.srt"), filepath.Join(dir, "published.srt")
	require.NoError(t, afero.WriteFile(fs, source, []byte("admitted subtitle"), 0600))
	require.NoError(t, fs.Rename(source, destination))
	batch, err := NewReplacementBatch(fs, "missing-install-proof", nil)
	require.NoError(t, err)
	require.ErrorContains(t, batch.SetRollbackOriginBound(destination, source, nil), "requires a bound installed identity")
	require.NoError(t, batch.Rollback(t.Context()))
	var snapshot *models.PublicationRecovery
	require.NotPanics(t, func() { snapshot = batch.RecoverySnapshot() })
	require.True(t, snapshot.Unresolved)
	require.Contains(t, snapshot.Managed, destination)
	require.Empty(t, snapshot.Deletes)
	require.Len(t, snapshot.Moves, 1)
	move := snapshot.Moves[0]
	require.Equal(t, source, move.OriginalPath)
	require.Equal(t, destination, move.NewPath)
	require.NotNil(t, move.Proof)
	require.True(t, move.Proof.RecoveryUnproven)
	require.True(t, move.Proof.RecoveryPublication)
	require.False(t, move.Proof.IdentityPinned)
	require.False(t, move.Proof.IdentityStrong)
	require.Nil(t, move.Proof.IdentityMode)
	_, err = fs.Stat(source)
	require.True(t, os.IsNotExist(err))
	data, err := afero.ReadFile(fs, destination)
	require.NoError(t, err)
	require.Equal(t, "admitted subtitle", string(data))
}
