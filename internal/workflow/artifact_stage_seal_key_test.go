package workflow

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeSweepKeyFixture(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), artifactStageSecretName)
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return path
}

func TestReadArtifactSweepSecret_Validation(t *testing.T) {
	valid := []byte(strings.Repeat("k", artifactStageSecretBytes))

	t.Run("valid key", func(t *testing.T) {
		key, err := readArtifactSweepSecret(writeSweepKeyFixture(t, valid))
		require.NoError(t, err)
		require.Equal(t, valid, key)
	})

	t.Run("missing key keeps not-exist classification", func(t *testing.T) {
		_, err := readArtifactSweepSecret(filepath.Join(t.TempDir(), artifactStageSecretName))
		require.True(t, os.IsNotExist(err))
	})

	t.Run("oversized key is corrupt", func(t *testing.T) {
		_, err := readArtifactSweepSecret(writeSweepKeyFixture(t, append(valid, 'x')))
		require.ErrorContains(t, err, "corrupt")
	})

	t.Run("directory is refused", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), artifactStageSecretName)
		require.NoError(t, os.Mkdir(path, 0o700))
		_, err := readArtifactSweepSecret(path)
		require.ErrorContains(t, err, "not a regular file")
	})

	t.Run("stat failure", func(t *testing.T) {
		old := artifactSweepStatKey
		t.Cleanup(func() { artifactSweepStatKey = old })
		artifactSweepStatKey = func(*os.File) (os.FileInfo, error) { return nil, errors.New("stat denied") }
		_, err := readArtifactSweepSecret(writeSweepKeyFixture(t, valid))
		require.ErrorContains(t, err, "stat artifact sweep key")
	})

	t.Run("read failure", func(t *testing.T) {
		old := artifactSweepReadKey
		t.Cleanup(func() { artifactSweepReadKey = old })
		artifactSweepReadKey = func(io.Reader) ([]byte, error) { return nil, errors.New("read denied") }
		_, err := readArtifactSweepSecret(writeSweepKeyFixture(t, valid))
		require.ErrorContains(t, err, "read artifact sweep key")
	})
}
