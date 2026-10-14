package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type allocationFailureFS struct {
	afero.Fs
	parent, dest                                 string
	parentMkdir, parentTemp, destMkdir, destTemp error
}

func (f *allocationFailureFS) MkdirAll(path string, perm os.FileMode) error {
	if filepath.Clean(path) == f.parent && f.parentMkdir != nil {
		return f.parentMkdir
	}
	if filepath.Clean(path) == f.dest && f.destMkdir != nil {
		return f.destMkdir
	}
	return f.Fs.MkdirAll(path, perm)
}

func (f *allocationFailureFS) Mkdir(path string, perm os.FileMode) error {
	if strings.HasPrefix(filepath.Base(path), artifactStageDirPrefix) {
		if filepath.Dir(path) == f.parent && f.parentTemp != nil {
			return f.parentTemp
		}
		if filepath.Dir(path) == f.dest && f.destTemp != nil {
			return f.destTemp
		}
	}
	return f.Fs.Mkdir(path, perm)
}

func TestAllocateArtifactStageFallbackBoundaries(t *testing.T) {
	ioErr := errors.New("unrelated filesystem failure")
	for _, tc := range []struct {
		name                                         string
		parentMkdir, parentTemp, destMkdir, destTemp error
		wantErr                                      error
		fallback                                     bool
	}{
		{name: "writable parent"},
		{name: "read-only filesystem", parentTemp: syscall.EROFS, fallback: true},
		{name: "denied parent mkdir", parentMkdir: os.ErrPermission, fallback: true},
		{name: "parent mkdir unrelated error", parentMkdir: ioErr, wantErr: ioErr},
		{name: "unwritable destination", parentTemp: os.ErrPermission, destMkdir: ioErr, wantErr: ioErr},
		{name: "destination temp failure", parentTemp: os.ErrPermission, destTemp: ioErr, wantErr: ioErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := filepath.Join(string(filepath.Separator), "library")
			dest := filepath.Join(parent, "movie")
			fs := &allocationFailureFS{Fs: afero.NewMemMapFs(), parent: parent, dest: dest, parentMkdir: tc.parentMkdir, parentTemp: tc.parentTemp, destMkdir: tc.destMkdir, destTemp: tc.destTemp}
			root, err := allocateArtifactStage(fs, dest)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Empty(t, root)
				return
			}
			require.NoError(t, err)
			wantParent := parent
			if tc.fallback {
				wantParent = dest
			}
			require.Equal(t, wantParent, filepath.Dir(root))
			require.True(t, strings.HasPrefix(filepath.Base(root), artifactStageDirPrefix))
		})
	}
	fs := &allocationFailureFS{Fs: afero.NewMemMapFs(), parent: string(filepath.Separator), parentTemp: os.ErrPermission}
	root, err := allocateArtifactStage(fs, string(filepath.Separator))
	require.ErrorIs(t, err, os.ErrPermission)
	require.Empty(t, root)
}

func TestAllocateArtifactStageExistingDestination(t *testing.T) {
	parent := filepath.Join(string(filepath.Separator), "library")
	dest := filepath.Join(parent, "movie")
	for _, tc := range []struct {
		name     string
		destTemp error
	}{
		{name: "stage inside existing destination"},
		{name: "do not fall back outside destination", destTemp: os.ErrPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := &allocationFailureFS{Fs: afero.NewMemMapFs(), parent: parent, dest: dest, destTemp: tc.destTemp}
			require.NoError(t, fs.MkdirAll(dest, 0o755))
			root, err := allocateArtifactStage(fs, dest)
			if tc.destTemp != nil {
				require.ErrorIs(t, err, tc.destTemp)
				require.Empty(t, root)
				entries, readErr := afero.ReadDir(fs, parent)
				require.NoError(t, readErr)
				require.Len(t, entries, 1, "must not create a stage on the parent's potentially different filesystem")
				return
			}
			require.NoError(t, err)
			require.Equal(t, dest, filepath.Dir(root))
			info, statErr := fs.Stat(root)
			require.NoError(t, statErr)
			require.True(t, info.IsDir())
		})
	}
}

func TestAllocateArtifactStageNativeSameFilesystem(t *testing.T) {
	fs := afero.NewOsFs()
	parent := t.TempDir()
	dest := filepath.Join(parent, "movie")
	require.NoError(t, fs.Mkdir(dest, 0o755))
	root, err := allocateArtifactStage(fs, dest)
	require.NoError(t, err)
	require.Equal(t, dest, filepath.Dir(root), "equal device IDs do not rule out a bind-mount rename boundary")
}

func TestAllocateArtifactStageSweepsBothLocations(t *testing.T) {
	fs := afero.NewMemMapFs()
	parent := filepath.Join(string(filepath.Separator), "library")
	dest := filepath.Join(parent, "movie")
	require.NoError(t, fs.MkdirAll(dest, 0o755))
	setSweepSeams(t, fsutil.ProcessDead, nil)
	oldSibling := seedStagingRoot(t, fs, parent, nil)
	oldChild := seedStagingRoot(t, fs, dest, nil)
	foreign := filepath.Join(parent, artifactStageDirPrefix+"unverified")
	require.NoError(t, fs.Mkdir(foreign, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(foreign, "keep"), []byte("foreign"), 0o644))

	root, err := allocateArtifactStage(fs, dest)
	require.NoError(t, err)
	require.Equal(t, dest, filepath.Dir(root))
	for _, path := range []string{oldSibling, oldChild} {
		exists, statErr := afero.Exists(fs, path)
		require.NoError(t, statErr)
		require.False(t, exists, "authenticated dead-owner residue must still be swept: %s", path)
	}
	got, err := afero.ReadFile(fs, filepath.Join(foreign, "keep"))
	require.NoError(t, err)
	require.Equal(t, "foreign", string(got))
}

func TestAllocateArtifactStageApplyPlacement(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       operationmode.OperationMode
		move, skip bool
	}{
		{name: "organize copy", mode: operationmode.OperationModeOrganize},
		{name: "in-place copy", mode: operationmode.OperationModeInPlace},
		{name: "in-place move", mode: operationmode.OperationModeInPlace, move: true},
		{name: "in-place skipped organization", mode: operationmode.OperationModeInPlace, skip: true},
		{name: "in-place no folder rename copy", mode: operationmode.OperationModeInPlaceNoRenameFolder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			parent := filepath.Join(string(filepath.Separator), "library")
			dest := filepath.Join(parent, "movie")
			require.NoError(t, fs.MkdirAll(dest, 0o755))
			root, err := (&applyOrchImpl{fs: fs}).allocateArtifactStageForApply(context.Background(), dest, ApplyCmd{OperationMode: tc.mode, Organize: OrganizeOptions{MoveFiles: tc.move, Skip: tc.skip}}, nil)
			require.NoError(t, err)
			require.Equal(t, dest, filepath.Dir(root), "without a planner only the requested destination is known")
		})
	}
}
