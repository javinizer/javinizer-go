package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// This synthetic filesystem records namespace selection without touching any
// system drive, UNC share, or virtual path named by the test cases.
type primaryUndoParentNamespaceFS struct {
	afero.Fs
	created, renamedSource, renamedDestination string
}

func (f *primaryUndoParentNamespaceFS) Stat(string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}
func (f *primaryUndoParentNamespaceFS) MkdirAll(path string, _ os.FileMode) error {
	f.created = path
	return nil
}
func (f *primaryUndoParentNamespaceFS) Rename(source, destination string) error {
	f.renamedSource, f.renamedDestination = source, destination
	return nil
}

func primaryUndoCheckParentNamespace(t *testing.T, destination, wantParent string) {
	t.Helper()
	fs := &primaryUndoParentNamespaceFS{Fs: afero.NewMemMapFs()}
	op := &models.BatchFileOperation{OperationType: models.OperationTypeMove, NewPath: "synthetic-library-source", OriginalPath: destination}
	result, err := revertPrimaryFileFS(context.Background(), fs, newP3OpRepo(), op, func(string, string) {})
	require.NoError(t, err)
	require.Nil(t, result)
	require.Equal(t, wantParent, fs.created)
	require.Equal(t, op.NewPath, fs.renamedSource)
	require.Equal(t, destination, fs.renamedDestination)
	require.Empty(t, op.GeneratedFiles, "same-volume namespace selection must not enter EXDEV recovery")
}

func TestPrimaryUndoParentNamespacePreservesRelativeAndVirtualPaths(t *testing.T) {
	for _, destination := range []string{filepath.Join("relative", "incoming", "movie.mkv"), filepath.Join(string(filepath.Separator), "virtual", "incoming", "movie.mkv")} {
		t.Run(destination, func(t *testing.T) {
			want, err := fsutil.CanonicalizePath(filepath.Dir(destination))
			require.NoError(t, err)
			primaryUndoCheckParentNamespace(t, destination, want)
		})
	}
	parent := filepath.Join(t.TempDir(), "incoming")
	primaryUndoCheckParentNamespace(t, filepath.Join(parent, "movie.mkv"), parent)
}
