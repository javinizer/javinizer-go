package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func TestReplacementBatchDirectBoundOriginTracking(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src, dst, origin := filepath.Join(dir, "input"), filepath.Join(dir, "output"), filepath.Join(dir, "origin")
	require.NoError(t, afero.WriteFile(fs, src, []byte("owned result"), 0o600))
	installed, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	batch, err := NewReplacementBatch(fs, "direct-bound-origin", nil)
	require.NoError(t, err)
	defer func() { _ = batch.Rollback(context.Background()) }()
	require.NoError(t, batch.SetRollbackOriginBound(dst, origin, installed))
	require.NoError(t, batch.Rollback(context.Background()))
	_, err = fs.Stat(dst)
	require.True(t, os.IsNotExist(err))
	payload, err := afero.ReadFile(fs, origin)
	require.NoError(t, err)
	require.Equal(t, "owned result", string(payload))
}
