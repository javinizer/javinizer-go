package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

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
