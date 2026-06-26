//go:build !windows

package auth

import (
	"errors"
	"github.com/spf13/afero"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforceCredentialFilePermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows uses ACLs, not POSIX permissions")
	}

	t.Run("accepts_file_with_correct_permissions", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "test.credentials")
		require.NoError(t, os.WriteFile(path, []byte("test"), 0o600))

		err := enforceCredentialFilePermissions(afero.NewOsFs(), path)
		require.NoError(t, err)
	})

	t.Run("repairs_file_with_incorrect_permissions", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "test.credentials")
		require.NoError(t, os.WriteFile(path, []byte("test"), 0o644))

		err := enforceCredentialFilePermissions(afero.NewOsFs(), path)
		require.NoError(t, err)

		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("rejects_directory", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		err := enforceCredentialFilePermissions(afero.NewOsFs(), dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is a directory")
	})

	t.Run("rejects_symlink", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		targetPath := filepath.Join(tmpDir, "target")
		require.NoError(t, os.WriteFile(targetPath, []byte("test"), 0o600))

		symlinkPath := filepath.Join(tmpDir, "symlink")
		require.NoError(t, os.Symlink(targetPath, symlinkPath))

		err := enforceCredentialFilePermissions(afero.NewOsFs(), symlinkPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must not be a symlink")
	})

	t.Run("rejects_non_regular_file_named_pipe", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		pipePath := filepath.Join(tmpDir, "test.pipe")

		err := syscall.Mkfifo(pipePath, 0o600)
		require.NoError(t, err)

		err = enforceCredentialFilePermissions(afero.NewOsFs(), pipePath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a regular file")
	})

	t.Run("returns_error_for_nonexistent_path", func(t *testing.T) {
		t.Parallel()

		err := enforceCredentialFilePermissions(afero.NewOsFs(), "/nonexistent/path/file")
		require.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("handles_unsupported_permission_mutation_eopnotsupp", func(t *testing.T) {
		t.Parallel()

		err := isUnsupportedPermissionMutation(syscall.EOPNOTSUPP)
		assert.True(t, err)
	})

	t.Run("handles_unsupported_permission_mutation_enotsup", func(t *testing.T) {
		t.Parallel()

		err := isUnsupportedPermissionMutation(syscall.ENOTSUP)
		assert.True(t, err)
	})

	t.Run("handles_unsupported_permission_mutation_erofs", func(t *testing.T) {
		t.Parallel()

		err := isUnsupportedPermissionMutation(syscall.EROFS)
		assert.True(t, err)
	})
}

func TestIsUnsupportedPermissionMutation(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("windows does not use this function")
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"EOPNOTSUPP is unsupported", syscall.EOPNOTSUPP, true},
		{"ENOTSUP is unsupported", syscall.ENOTSUP, true},
		{"EROFS is unsupported", syscall.EROFS, true},
		{"EPERM is not unsupported mutation", syscall.EPERM, false},
		{"EACCES is not unsupported mutation", syscall.EACCES, false},
		{"generic error is not unsupported", errors.New("generic error"), false},
		{"nil error is not unsupported", nil, false},
		{"wrapped EOPNOTSUPP is unsupported", errors.Join(errors.New("context"), syscall.EOPNOTSUPP), true},
		{"wrapped EROFS is unsupported", errors.Join(errors.New("context"), syscall.EROFS), true},
		{"wrapped ENOTSUP is unsupported", errors.Join(errors.New("context"), syscall.ENOTSUP), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isUnsupportedPermissionMutation(tt.err))
		})
	}
}
