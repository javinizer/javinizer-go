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

type batchBusyReleaseFaultFS struct {
	afero.Fs
	marker   string
	failures int
}

func (f *batchBusyReleaseFaultFS) Open(path string) (afero.File, error) {
	if filepath.Clean(path) == f.marker && f.failures > 0 {
		f.failures--
		return nil, os.ErrPermission
	}
	return f.Fs.Open(path)
}
func (f *batchBusyReleaseFaultFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

func TestReplacementBatchRollbackPreservesForeignBusyMarker(t *testing.T) {
	ctx := context.Background()
	base := afero.NewOsFs()
	root := t.TempDir()
	src, dst := filepath.Join(root, "input"), filepath.Join(root, "fanart.jpg")
	require.NoError(t, afero.WriteFile(base, src, []byte("owned artifact"), 0o600))
	fs := &batchBusyReleaseFaultFS{Fs: base, marker: fsutil.ReplacementBusyPath(dst)}
	batch, err := NewReplacementBatch(fs, "foreign-busy", nil)
	require.NoError(t, err)
	defer func() { _ = batch.Rollback(ctx) }()
	_, err = batch.BeforePublish(ctx, dst, false)
	require.NoError(t, err)
	installed, err := fsutil.CopyFileNoReplaceVerifiedInstall(base, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NoError(t, batch.ObservePublishResultBound(dst, installed))
	fs.failures = 1
	require.NoError(t, batch.ConfirmPublish(ctx, dst))
	require.NoError(t, base.Rename(fs.marker, fs.marker+".held"))
	foreignRelease, foreignToken, err := fsutil.AcquireReplacementBusyEx(base, dst)
	require.NoError(t, err)
	defer foreignRelease()
	require.ErrorIs(t, batch.Rollback(ctx), fsutil.ErrReplacementBusy)
	marker, err := afero.ReadFile(base, fs.marker)
	require.NoError(t, err)
	require.Equal(t, foreignToken, string(marker))
	payload, err := afero.ReadFile(base, dst)
	require.NoError(t, err)
	require.Equal(t, "owned artifact", string(payload))
}

func TestReplacementBatchRollbackPreservesOutputWhileBusyMarkerUnreadable(t *testing.T) {
	ctx := context.Background()
	base := afero.NewOsFs()
	root := t.TempDir()
	src, dst := filepath.Join(root, "input"), filepath.Join(root, "fanart.jpg")
	require.NoError(t, afero.WriteFile(base, src, []byte("owned artifact"), 0o600))
	fs := &batchBusyReleaseFaultFS{Fs: base, marker: fsutil.ReplacementBusyPath(dst)}
	batch, err := NewReplacementBatch(fs, "unreadable-busy", nil)
	require.NoError(t, err)
	defer func() { fs.failures = 0; _ = batch.Rollback(ctx) }()
	_, err = batch.BeforePublish(ctx, dst, false)
	require.NoError(t, err)
	installed, err := fsutil.CopyFileNoReplaceVerifiedInstall(base, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NoError(t, batch.ObservePublishResultBound(dst, installed))
	fs.failures = 3
	require.NoError(t, batch.ConfirmPublish(ctx, dst))
	require.ErrorIs(t, batch.Rollback(ctx), os.ErrPermission)
	payload, err := afero.ReadFile(base, dst)
	require.NoError(t, err)
	require.Equal(t, "owned artifact", string(payload))
	_, err = base.Stat(fs.marker)
	require.NoError(t, err)
	fs.failures = 0
	require.NoError(t, batch.Rollback(ctx))
}

func TestReplacementBatchRollbackRetriesOwnedBusyRelease(t *testing.T) {
	ctx := context.Background()
	base := afero.NewOsFs()
	root := t.TempDir()
	src, dst := filepath.Join(root, "input"), filepath.Join(root, "fanart.jpg")
	require.NoError(t, afero.WriteFile(base, src, []byte("owned artifact"), 0o600))
	fs := &batchBusyReleaseFaultFS{Fs: base, marker: fsutil.ReplacementBusyPath(dst)}
	batch, err := NewReplacementBatch(fs, "busy-retry", nil)
	require.NoError(t, err)
	defer func() { _ = batch.Rollback(ctx) }()
	_, err = batch.BeforePublish(ctx, dst, false)
	require.NoError(t, err)
	installed, err := fsutil.CopyFileNoReplaceVerifiedInstall(base, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NoError(t, batch.ObservePublishResultBound(dst, installed))
	fs.failures = 1
	require.NoError(t, batch.ConfirmPublish(ctx, dst))
	_, err = base.Stat(fs.marker)
	require.NoError(t, err)
	require.NoError(t, batch.Rollback(ctx))
	_, err = base.Stat(dst)
	require.True(t, os.IsNotExist(err))
	_, err = base.Stat(fs.marker)
	require.True(t, os.IsNotExist(err))
}
