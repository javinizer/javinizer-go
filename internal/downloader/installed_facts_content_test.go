package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestInstalledReplacementFactsWithoutBoundIdentity(t *testing.T) {
	t.Run("regular file hashes installed bytes", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		const path = "/installed/poster.jpg"
		contents := []byte("installed image bytes")
		require.NoError(t, afero.WriteFile(fs, path, contents, 0o600))
		info, err := fs.Stat(path)
		require.NoError(t, err)
		facts, err := captureInstalledReplacementFacts(fs, path)
		require.NoError(t, err)
		sum := sha256.Sum256(contents)
		require.Equal(t, int64(len(contents)), facts.Size)
		require.Equal(t, info.ModTime().Unix(), facts.ModUnix)
		require.Equal(t, hex.EncodeToString(sum[:]), facts.SHA256)
		remaining, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		require.Equal(t, contents, remaining)
	})
	t.Run("dangling symlink hashes link text without following it", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation requires privileges on Windows")
		}
		path := filepath.Join(t.TempDir(), "installed-link")
		const target = "missing-video.mp4"
		require.NoError(t, os.Symlink(target, path))
		info, err := os.Lstat(path)
		require.NoError(t, err)
		facts, err := captureInstalledReplacementFacts(afero.NewOsFs(), path)
		require.NoError(t, err)
		sum := sha256.Sum256([]byte(target))
		require.Equal(t, int64(len(target)), facts.Size)
		require.Equal(t, info.ModTime().Unix(), facts.ModUnix)
		require.Equal(t, hex.EncodeToString(sum[:]), facts.SHA256)
		remaining, err := os.Readlink(path)
		require.NoError(t, err)
		require.Equal(t, target, remaining)
	})
}
