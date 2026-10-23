//go:build !windows

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestDiscardUnpublishedArtifactSweepKey(t *testing.T) {
	t.Run("substituted path is preserved", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, artifactStageSecretName)
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		created, err := os.Stat(path)
		require.NoError(t, err)
		require.NoError(t, os.Rename(path, path+".moved"))
		require.NoError(t, os.WriteFile(path, []byte("foreign"), 0o600))
		discardUnpublishedArtifactSweepKey(path, created)
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "foreign", string(body))
	})

	t.Run("substitute planted after the identity check is preserved", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, artifactStageSecretName)
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		created, err := os.Stat(path)
		require.NoError(t, err)
		old := artifactSweepUnlinkVerified
		t.Cleanup(func() { artifactSweepUnlinkVerified = old })
		artifactSweepUnlinkVerified = func(fs afero.Fs, name string, verified os.FileInfo) error {
			require.NoError(t, os.Rename(name, name+".moved"))
			require.NoError(t, os.WriteFile(name, []byte("complete key from a concurrent process"), 0o600))
			return old(fs, name, verified)
		}
		discardUnpublishedArtifactSweepKey(path, created)
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "complete key from a concurrent process", string(body))
	})

	t.Run("missing path is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, artifactStageSecretName)
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		created, err := os.Stat(path)
		require.NoError(t, err)
		require.NoError(t, os.Remove(path))
		discardUnpublishedArtifactSweepKey(path, created)
	})

	t.Run("remove failure is logged and retained", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, artifactStageSecretName)
		require.NoError(t, os.WriteFile(path, nil, 0o600))
		created, err := os.Stat(path)
		require.NoError(t, err)
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		discardUnpublishedArtifactSweepKey(path, created)
		_, err = os.Lstat(path)
		if os.Geteuid() != 0 {
			require.NoError(t, err)
		}
	})

	t.Run("short write discards the partial key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "Javinizer", artifactStageSecretName)
		oldPath, oldCreate := artifactSweepSecretPath, artifactSweepCreateKey
		t.Cleanup(func() { artifactSweepSecretPath, artifactSweepCreateKey = oldPath, oldCreate })
		artifactSweepSecretPath = func() (string, error) { return path, nil }
		artifactSweepCreateKey = func(p string) (artifactSweepKeyFile, error) {
			f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			return &sweepKeyShortWriteFile{f}, err
		}
		_, err := defaultArtifactSweepSecretKey()
		require.ErrorContains(t, err, "short write")
		_, err = os.Lstat(path)
		require.True(t, os.IsNotExist(err))
	})
}

type sweepKeyShortWriteFile struct{ *os.File }

func (f *sweepKeyShortWriteFile) Write(p []byte) (int, error) { return f.File.Write(p[:len(p)/2]) }
