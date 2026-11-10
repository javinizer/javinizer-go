//go:build linux

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type missingCWDSnapshotFS struct {
	afero.Fs
	opened []string
}

func (f *missingCWDSnapshotFS) Open(path string) (afero.File, error) {
	f.opened = append(f.opened, path)
	return f.Fs.Open(path)
}

func TestReadNFOSnapshotSkipsRelativeCandidateWithoutWorkingDirectory(t *testing.T) {
	// Do not parallelize: t.Chdir retains the descriptor needed to restore cwd
	// after removing only this test's freshly created, empty child directory.
	originalWD, err := os.Getwd()
	require.NoError(t, err)
	originalPWD, hadPWD := os.LookupEnv("PWD")
	root := t.TempDir()
	emptyCWD := filepath.Join(root, "empty-cwd")
	require.NoError(t, os.Mkdir(emptyCWD, 0o700))
	absoluteNFO := filepath.Join(root, "movie.nfo")
	content := []byte("<movie><title>absolute fallback</title></movie>")
	require.NoError(t, os.WriteFile(absoluteNFO, content, 0o600))
	fs := &missingCWDSnapshotFS{Fs: afero.NewOsFs()}

	t.Run("relative candidate is skipped and absolute fallback is read", func(t *testing.T) {
		t.Chdir(emptyCWD)
		require.NoError(t, os.Remove(emptyCWD))
		_, err := os.Getwd()
		require.ErrorIs(t, err, os.ErrNotExist)

		result := readNFOSnapshot(logging.GlobalLogger(), fs, "movie.nfo", absoluteNFO)
		require.Equal(t, absoluteNFO, result.FoundPath)
		require.Equal(t, string(content), result.Content)
		require.Equal(t, []string{absoluteNFO}, fs.opened, "unresolvable candidate must not reach filesystem I/O")
	})

	wd, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, originalWD, wd)
	pwd, present := os.LookupEnv("PWD")
	require.Equal(t, hadPWD, present)
	require.Equal(t, originalPWD, pwd)
	after, err := os.ReadFile(absoluteNFO)
	require.NoError(t, err)
	require.Equal(t, content, after, "snapshot capture must leave the NFO unchanged")
	require.NoDirExists(t, emptyCWD)
}
