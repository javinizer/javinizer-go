package workflow

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recursiveRemovalSwapFS struct {
	afero.Fs
	root    string
	aside   string
	swapped bool
}

func (f *recursiveRemovalSwapFS) Open(path string) (afero.File, error) {
	file, err := f.Fs.Open(path)
	if err == nil && path == f.root && !f.swapped {
		if err := f.swap(path); err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return file, err
}

func (f *recursiveRemovalSwapFS) RemoveAll(path string) error {
	if err := f.swap(path); err != nil {
		return err
	}
	return f.Fs.RemoveAll(path)
}

func (f *recursiveRemovalSwapFS) swap(path string) error {
	if !f.swapped && path == f.root {
		info, err := f.Fs.Stat(path)
		if err != nil {
			return err
		}
		f.aside = path + ".retained"
		if err := f.Fs.Rename(path, f.aside); err != nil {
			return err
		}
		if err := f.Fs.MkdirAll(path, 0o700); err != nil {
			return err
		}
		if err := afero.WriteFile(f.Fs, filepath.Join(path, "foreign.bin"), []byte("foreign"), 0o600); err != nil {
			return err
		}
		stamp := info.ModTime().Add(time.Second)
		if err := f.Fs.Chtimes(path, stamp, stamp); err != nil {
			return err
		}
		f.swapped = true
	}
	return nil
}

func TestRecursiveStagingRemovalPreservesReplacementTree(t *testing.T) {
	base := afero.NewOsFs()
	root := filepath.Join(t.TempDir(), artifactStageDirPrefix+"owned"+artifactStageQuarantineMark+"test")
	require.NoError(t, base.MkdirAll(root, 0o700))
	require.NoError(t, afero.WriteFile(base, filepath.Join(root, "owned.bin"), []byte("owned"), 0o600))
	identity := artifactStageDirectoryIdentity(base, root)
	require.True(t, identity.known)
	fs := &recursiveRemovalSwapFS{Fs: base, root: root}
	err := removeArtifactTreeWithRetry(fs, root, identity)
	assert.Error(t, err)
	if runtime.GOOS == "windows" && !fs.swapped {
		require.ErrorIs(t, err, syscall.Errno(32))
		body, readErr := os.ReadFile(filepath.Join(root, "owned.bin"))
		require.NoError(t, readErr)
		assert.Equal(t, "owned", string(body))
		return
	}
	require.True(t, fs.swapped)
	body, err := os.ReadFile(filepath.Join(root, "foreign.bin"))
	require.NoError(t, err)
	assert.Equal(t, "foreign", string(body))
	_, err = os.Stat(fs.aside)
	require.NoError(t, err)
}
