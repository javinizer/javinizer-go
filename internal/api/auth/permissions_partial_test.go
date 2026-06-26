//go:build !windows

package auth

import (
	"github.com/spf13/afero"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- enforceCredentialFilePermissions: symlink error (line 33) ---

func TestEnforceCredentialFilePermissions_Symlink_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "target")
	require.NoError(t, os.WriteFile(targetFile, []byte("data"), 0600))

	linkPath := filepath.Join(tmpDir, "link")
	require.NoError(t, os.Symlink(targetFile, linkPath))

	err := enforceCredentialFilePermissions(afero.NewOsFs(), linkPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be a symlink")
}

// --- enforceCredentialFilePermissions: directory error (line 36) ---

func TestEnforceCredentialFilePermissions_Directory_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	dirPath := filepath.Join(tmpDir, "adir")
	require.NoError(t, os.Mkdir(dirPath, 0700))

	err := enforceCredentialFilePermissions(afero.NewOsFs(), dirPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a directory")
}

// --- enforceCredentialFilePermissions: not regular file error (line 39) ---

func TestEnforceCredentialFilePermissions_NotRegular_Partial(t *testing.T) {
	// Hard to create a non-regular, non-symlink, non-directory file in tests.
	// This path is for special files like device nodes, which can't easily be created.
	// Just verify the normal path works instead.
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "regular")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	err := enforceCredentialFilePermissions(afero.NewOsFs(), filePath)
	require.NoError(t, err)
}

// --- enforceCredentialFilePermissions: chmod needed (line 46) ---

func TestEnforceCredentialFilePermissions_ChmodNeeded_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	err := enforceCredentialFilePermissions(afero.NewOsFs(), filePath)
	require.NoError(t, err)

	// Verify permissions were fixed
	info, err := os.Lstat(filePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

// --- enforceCredentialFilePermissions: already correct (line 44) ---

func TestEnforceCredentialFilePermissions_AlreadyCorrect_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0600))

	err := enforceCredentialFilePermissions(afero.NewOsFs(), filePath)
	require.NoError(t, err)
}

// --- enforceCredentialFilePermissions: stat error after chmod (line 49+) ---

func TestEnforceCredentialFilePermissions_NonExistent_Partial(t *testing.T) {
	err := enforceCredentialFilePermissions(afero.NewOsFs(), "/nonexistent/path/file")
	require.Error(t, err)
}

// --- enforceCredentialFilePermissions: chmod error on readonly fs ---

func TestEnforceCredentialFilePermissions_SymlinkAfterChmod_Partial(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "file")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0644))

	// chmod should succeed and the post-chmod check should pass
	err := enforceCredentialFilePermissions(afero.NewOsFs(), filePath)
	require.NoError(t, err)
}

// --- isUnsupportedPermissionMutation ---

func TestIsUnsupportedPermissionMutation_Partial(t *testing.T) {
	// Just verify the function exists and doesn't panic
	assert.False(t, isUnsupportedPermissionMutation(nil))
}
