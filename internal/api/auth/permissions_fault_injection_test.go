//go:build !windows

package auth

import (
	"os"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type permissionFaultFS struct {
	afero.Fs
	fault   string
	changed bool
}

func (f *permissionFaultFS) Chmod(name string, mode os.FileMode) error {
	f.changed = true
	switch f.fault {
	case "unsupported":
		return syscall.EOPNOTSUPP
	case "denied":
		return os.ErrPermission
	case "unchanged":
		return nil
	}
	return f.Fs.Chmod(name, mode)
}
func (f *permissionFaultFS) Stat(name string) (os.FileInfo, error) {
	if f.changed && f.fault == "stat" {
		return nil, errStorageFault
	}
	info, err := f.Fs.Stat(name)
	if err == nil && f.changed && f.fault == "symlink" {
		return permissionFileInfo{FileInfo: info}, nil
	}
	return info, err
}

type permissionFileInfo struct{ os.FileInfo }

func (permissionFileInfo) Mode() os.FileMode { return os.ModeSymlink | 0600 }
func TestCredentialPermissionMutationFailures(t *testing.T) {
	for _, tc := range []struct{ fault, want string }{
		{"unsupported", "filesystem does not support chmod"},
		{"denied", "permission denied"},
		{"unchanged", "expected 600"},
		{"stat", "injected storage failure"},
		{"symlink", "must not be a symlink"},
	} {
		t.Run(tc.fault, func(t *testing.T) {
			base := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(base, "/credentials", []byte("private"), 0644))
			fs := &permissionFaultFS{Fs: base, fault: tc.fault}
			require.ErrorContains(t, enforceCredentialFilePermissions(fs, "/credentials"), tc.want)
			require.True(t, fs.changed, "the failure must occur during or after the permission mutation")
			data, err := afero.ReadFile(base, "/credentials")
			require.NoError(t, err)
			require.Equal(t, "private", string(data))
		})
	}
}
