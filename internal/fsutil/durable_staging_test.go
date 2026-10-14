package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestDurableStagingBindsHandleAndFlushesMetadata(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	stage := filepath.Join(root, "stage")
	require.NoError(t, os.WriteFile(source, []byte("synthetic"), 0600))
	stamp := time.Unix(1700000000, 0)
	require.NoError(t, os.Chtimes(source, stamp, stamp))
	sourceInfo, err := os.Stat(source)
	require.NoError(t, err)
	file, err := fs.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	require.NoError(t, err)
	defer file.Close()
	_, err = file.Write([]byte("synthetic"))
	require.NoError(t, err)
	info, err := file.Stat()
	require.NoError(t, err)
	dev, ino, strong := BoundObjectIdentity(fs, stage, info)
	require.True(t, strong)
	require.NoError(t, SyncStagedMetadata(fs, file, sourceInfo))
	info, err = file.Stat()
	require.NoError(t, err)
	d2, i2, strong := BoundObjectIdentity(fs, stage, info)
	require.True(t, strong)
	require.Equal(t, dev, d2)
	require.Equal(t, ino, i2)
	require.Equal(t, sourceInfo.ModTime(), info.ModTime())
	require.Equal(t, sourceInfo.Mode().Perm(), info.Mode().Perm())
	mem := afero.NewMemMapFs()
	mf, err := mem.Create("stage")
	require.NoError(t, err)
	defer mf.Close()
	require.Error(t, SyncStagedMetadata(mem, mf, sourceInfo))
	old := stagedHandleChtimes
	t.Cleanup(func() { stagedHandleChtimes = old })
	stagedHandleChtimes = func(uintptr, time.Time, time.Time) error { return errors.New("timestamp refusal") }
	require.ErrorContains(t, SyncStagedMetadata(fs, file, sourceInfo), "timestamp refusal")
	stagedHandleChtimes = old
	require.NoError(t, file.Close())
	require.Error(t, SyncStagedMetadata(fs, file, sourceInfo))
}
