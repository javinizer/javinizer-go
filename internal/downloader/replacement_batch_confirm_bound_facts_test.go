package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

type boundFactsOpenFS struct {
	afero.Fs
	beforeOpen func(string)
	openErr    error
}

func (f *boundFactsOpenFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}
func (f *boundFactsOpenFS) ReadlinkIfPossible(path string) (string, error) {
	return f.Fs.(afero.LinkReader).ReadlinkIfPossible(path)
}

func (f *boundFactsOpenFS) Open(path string) (afero.File, error) {
	if f.beforeOpen != nil {
		f.beforeOpen(path)
	}
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.Fs.Open(path)
}

func TestReplacementBatchConfirmFacts(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "open-error", "open-successor"} {
		t.Run(kind, func(t *testing.T) {
			base := afero.NewOsFs()
			dir := t.TempDir()
			src, dst := filepath.Join(dir, "input"), filepath.Join(dir, "output")
			own := []byte("own result")
			require.NoError(t, afero.WriteFile(base, src, own, 0o600))
			require.NoError(t, afero.WriteFile(base, dst, []byte("old backup"), 0o600))
			if kind == "symlink" {
				probe := filepath.Join(dir, "probe")
				if err := os.Symlink(src, probe); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				require.NoError(t, os.Remove(probe))
			}
			wrapped := &boundFactsOpenFS{Fs: base}
			recorder := &boundFactsRecorder{}
			batch, err := NewReplacementBatch(wrapped, "bound-facts-"+kind, recorder)
			require.NoError(t, err)
			defer func() { _ = batch.Rollback(context.Background()) }()
			_, err = batch.BeforePublish(context.Background(), dst, true)
			require.NoError(t, err)
			var installed *fsutil.BoundInstallIdentity
			if kind == "symlink" {
				require.NoError(t, os.Symlink(src, dst))
				info, err := os.Lstat(dst)
				require.NoError(t, err)
				installed = fsutil.NewBoundInstallLinkIdentity(info, src)
				own = []byte(src)
			} else {
				installed, err = fsutil.CopyFileNoReplaceVerifiedInstall(base, src, dst, func(string, os.FileInfo) error { return nil })
				require.NoError(t, err)
			}
			require.NoError(t, batch.ObservePublishResultBound(dst, installed))
			if kind == "open-error" {
				wrapped.openErr = errors.New("injected installed open fault")
			}
			if kind == "open-successor" {
				wrapped.beforeOpen = func(path string) {
					if filepath.Clean(path) != filepath.Clean(dst) {
						return
					}
					wrapped.beforeOpen = nil
					require.NoError(t, base.Rename(dst, dst+".owned"))
					require.NoError(t, afero.WriteFile(base, dst, []byte("foreign output"), 0o600))
				}
			}
			err = batch.ConfirmPublish(context.Background(), dst)
			if kind == "regular" || kind == "symlink" {
				require.NoError(t, err)
				require.Len(t, recorder.installed, 1)
				sum := sha256.Sum256(own)
				require.Equal(t, hex.EncodeToString(sum[:]), recorder.installed[0].SHA256)
				require.Equal(t, int64(len(own)), recorder.installed[0].Size)
			} else {
				require.Error(t, err)
				require.Empty(t, recorder.installed)
			}
			if kind == "open-successor" {
				payload, err := afero.ReadFile(base, dst)
				require.NoError(t, err)
				require.Equal(t, "foreign output", string(payload))
			}
		})
	}
}

type boundFactsRecorder struct {
	replacementBatchRecorder
	installed []models.ReplacementBackupFacts
}

func (r *boundFactsRecorder) ConfirmReplacementInstalled(_ context.Context, _, _, _ string, facts models.ReplacementBackupFacts) error {
	r.installed = append(r.installed, facts)
	return nil
}

func TestReplacementBatchConfirmFactsRejectForeignSuccessor(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "input"), filepath.Join(dir, "output")
	require.NoError(t, afero.WriteFile(fs, src, []byte("owned output"), 0o600))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("old backup"), 0o600))
	recorder := &boundFactsRecorder{}
	batch, err := NewReplacementBatch(fs, "bound-facts", recorder)
	require.NoError(t, err)
	defer func() { _ = batch.Rollback(context.Background()) }()
	replaced, err := batch.BeforePublish(context.Background(), dst, true)
	require.NoError(t, err)
	require.True(t, replaced)
	installed, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, src, dst, func(string, os.FileInfo) error { return nil })
	require.NoError(t, err)
	require.NoError(t, batch.ObservePublishResultBound(dst, installed))
	require.NoError(t, fs.Rename(dst, dst+".owned"))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign data"), 0o600))
	err = batch.ConfirmPublish(context.Background(), dst)
	require.Error(t, err)
	require.Empty(t, recorder.installed)
	require.Error(t, batch.Rollback(context.Background()))
	payload, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	require.Equal(t, "foreign data", string(payload))
}
