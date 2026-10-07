package fsutil

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
)

// The mode-aware twins publish the caller's explicit staging mode verbatim, so
// the deferred publication can hand down the ADMITTED source's permission bits
// instead of widening a private or read-only source to the umask-masked
// staging default (codex P2, PRRT_kwDORn9KaM6pw_EY).
func TestVerifiedCompositesPublishExplicitStagingMode(t *testing.T) {
	fs := afero.NewOsFs()
	admitAll := func(string, os.FileInfo) error { return nil }
	content := []byte("admitting source bytes")

	newSource := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "source.mp4")
		require.NoError(t, os.WriteFile(path, content, 0o600))
		require.NoError(t, os.Chmod(path, 0o640))
		return path
	}
	permOf := func(t *testing.T, path string) os.FileMode {
		t.Helper()
		info, err := os.Lstat(path)
		require.NoError(t, err)
		return info.Mode().Perm()
	}

	t.Run("copy twin applies the explicit mode", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "out.mp4")
		require.NoError(t, CopyFileNoReplaceVerifiedMode(fs, newSource(t), dst, admitAll, 0o640))
		assert.Equal(t, os.FileMode(0o640), permOf(t, dst), "the mode is written verbatim, umask unmasked")
		got, err := os.ReadFile(dst)
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("copy digest twin applies the explicit mode", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "out.mp4")
		sum := sha256.Sum256(content)
		digest, err := CopyFileNoReplaceVerifiedDigestMode(fs, newSource(t), dst, admitAll, 0o640)
		require.NoError(t, err)
		assert.Equal(t, hex.EncodeToString(sum[:]), digest)
		assert.Equal(t, os.FileMode(0o640), permOf(t, dst))
	})

	t.Run("move twins keep the consumed object's bits", func(t *testing.T) {
		src := newSource(t)
		dst := filepath.Join(t.TempDir(), "moved.mp4")
		require.NoError(t, MoveFileNoReplaceVerified(fs, src, dst, admitAll))
		assert.Equal(t, os.FileMode(0o640), permOf(t, dst), "the default wrapper's rename carries the inode's bits")

		src2 := newSource(t)
		dst2 := filepath.Join(t.TempDir(), "moved-mode.mp4")
		require.NoError(t, MoveFileNoReplaceVerifiedMode(fs, src2, dst2, admitAll, 0o640))
		assert.Equal(t, os.FileMode(0o640), permOf(t, dst2))
	})

	t.Run("default wrappers keep the umask-masked staging mode", func(t *testing.T) {
		src := newSource(t)
		dst := filepath.Join(t.TempDir(), "default.mp4")
		require.NoError(t, CopyFileNoReplaceVerified(fs, src, dst, admitAll))
		assert.Equal(t, StagingFileMode(), permOf(t, dst))

		dst2 := filepath.Join(t.TempDir(), "default-digest.mp4")
		digest, err := CopyFileNoReplaceVerifiedDigest(fs, src, dst2, admitAll)
		require.NoError(t, err)
		assert.NotEmpty(t, digest)
		assert.Equal(t, StagingFileMode(), permOf(t, dst2))
	})

	t.Run("nil proof keeps the by-name leg and the default staging mode", func(t *testing.T) {
		src := newSource(t)
		dst := filepath.Join(t.TempDir(), "nil-proof.mp4")
		require.NoError(t, CopyFileNoReplaceVerifiedMode(fs, src, dst, nil, 0o640))
		assert.Equal(t, StagingFileMode(), permOf(t, dst), "an unadmitted lane keeps the composite default")

		dst2 := filepath.Join(t.TempDir(), "nil-proof-digest.mp4")
		digest, err := CopyFileNoReplaceVerifiedDigestMode(fs, src, dst2, nil, 0o640)
		require.NoError(t, err)
		assert.NotEmpty(t, digest)
		assert.Equal(t, StagingFileMode(), permOf(t, dst2))
	})

	t.Run("StagingFileMode mirrors the umask-masked default", func(t *testing.T) {
		assert.Equal(t, config.FilePerm&^os.FileMode(config.UmaskValue()), StagingFileMode())
	})
}
