package fsutil

import (
	"os"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type chmodErrnoFs struct {
	afero.Fs
	errno error
}

func (f chmodErrnoFs) Chmod(name string, _ os.FileMode) error {
	return &os.PathError{Op: "chmod", Path: name, Err: f.errno}
}

func TestCreateExclusiveStagingFileModeBestEffort(t *testing.T) {
	for _, tc := range []struct {
		name       string
		errno      error
		bestEffort bool
		wantErr    bool
	}{
		{"best effort EPERM", syscall.EPERM, true, false},
		{"best effort ENOTSUP", syscall.ENOTSUP, true, false},
		{"best effort EOPNOTSUPP", syscall.EOPNOTSUPP, true, false},
		{"best effort EIO", syscall.EIO, true, true},
		{"strict EPERM", syscall.EPERM, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := chmodErrnoFs{Fs: afero.NewMemMapFs(), errno: tc.errno}
			create := CreateExclusiveStagingFile
			if tc.bestEffort {
				create = CreateExclusiveStagingFileModeBestEffort
			}
			path, file, err := create(fs, "/dest/marker", ".tmp", 0, 0o600)
			if tc.wantErr {
				require.ErrorIs(t, err, tc.errno)
				require.Empty(t, path)
				require.Nil(t, file)
				exists, statErr := afero.Exists(fs, "/dest/marker.tmp.0")
				require.NoError(t, statErr)
				require.False(t, exists, "the failed staging file is removed")
				return
			}
			require.NoError(t, err)
			require.Equal(t, "/dest/marker.tmp.0", path)
			require.NoError(t, file.Close())
			exists, statErr := afero.Exists(fs, path)
			require.NoError(t, statErr)
			require.True(t, exists)
		})
	}
}
