package history

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestRestoreNFOSoftFailureW24_CanonicalizeFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	op := &models.BatchFileOperation{
		ID:           2401,
		OriginalPath: filepath.Join(string(filepath.Separator), "w24-nfo", "title.mp4"),
		NFOPath:      filepath.Join(string(filepath.Separator), "w24-nfo", "title.nfo"),
		NFOSnapshot:  "<nfo>w24</nfo>",
	}
	canonicalizeErr := errors.New("w24 canonicalize failure")
	previousCanonicalizer := canonicalizeNFOPathFunc
	var gotDir string
	canonicalizeNFOPathFunc = func(dir string) (string, error) {
		gotDir = dir
		return "", canonicalizeErr
	}
	t.Cleanup(func() { canonicalizeNFOPathFunc = previousCanonicalizer })

	warning, result := restoreNFOSoftFailure(fs, op, op.NFOPath)

	require.Empty(t, warning, "the cleaned fallback directory should still permit the soft restore")
	require.Nil(t, result)
	require.Equal(t, filepath.Dir(op.OriginalPath), gotDir, "the injected failure must be the canonicalize call for the NFO directory")
	content, err := afero.ReadFile(fs, op.NFOPath)
	require.NoError(t, err)
	require.Equal(t, op.NFOSnapshot, string(content))
}

func TestCleanupEmptyDirDownwardFSW24_RemoveFailureStopsWalk(t *testing.T) {
	base := afero.NewMemMapFs()
	leaf := filepath.Join(string(filepath.Separator), "w24-remove-failure", "parent", "leaf")
	parent := filepath.Dir(leaf)
	require.NoError(t, base.MkdirAll(leaf, 0o755))

	fs := &w24RemoveProbeFs{Fs: base, failPath: parent}
	cleanupEmptyDirDownwardFS(fs, leaf, filepath.Join(string(filepath.Separator), "w24-remove-failure"))

	require.Equal(t, []string{filepath.Clean(leaf), filepath.Clean(parent)}, fs.removed)
	_, err := base.Stat(leaf)
	require.True(t, os.IsNotExist(err), "the child remove should succeed before the parent remove fails")
	_, err = base.Stat(parent)
	require.NoError(t, err, "the directory whose Remove failed must remain")
}

func TestCleanupEmptyDirDownwardFSW24_RootBoundary(t *testing.T) {
	base := afero.NewMemMapFs()
	top := filepath.Join(string(filepath.Separator), "w24-root-boundary")
	leaf := filepath.Join(top, "a", "b")
	require.NoError(t, base.MkdirAll(leaf, 0o755))

	fs := &w24RemoveProbeFs{Fs: base}
	cleanupEmptyDirDownwardFS(fs, leaf, "")

	require.Equal(t, []string{
		filepath.Clean(leaf),
		filepath.Clean(filepath.Dir(leaf)),
		filepath.Clean(top),
	}, fs.removed, "the walk must remove empty levels and stop when the parent is the filesystem root")
	for _, path := range []string{leaf, filepath.Dir(leaf), top} {
		_, err := base.Stat(path)
		require.True(t, os.IsNotExist(err), "%s should be removed", path)
	}
	require.NotContains(t, fs.removed, filepath.Clean(string(filepath.Separator)), "the root itself must not be removed")
}

type w24RemoveProbeFs struct {
	afero.Fs
	failPath string
	removed  []string
}

func (f *w24RemoveProbeFs) Remove(name string) error {
	clean := filepath.Clean(name)
	f.removed = append(f.removed, clean)
	if f.failPath != "" && clean == filepath.Clean(f.failPath) {
		return errors.New("w24 remove failure")
	}
	return f.Fs.Remove(name)
}
