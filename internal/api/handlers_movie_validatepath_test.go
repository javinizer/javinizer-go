package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateNFOPath(t *testing.T) {
	// Create temp directory structure for testing
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	forbiddenDir := filepath.Join(tempDir, "forbidden")

	require.NoError(t, os.Mkdir(allowedDir, 0755))
	require.NoError(t, os.Mkdir(forbiddenDir, 0755))

	// Create test files
	validFile := filepath.Join(allowedDir, "valid.nfo")
	forbiddenFile := filepath.Join(forbiddenDir, "forbidden.nfo")

	require.NoError(t, os.WriteFile(validFile, []byte("<movie></movie>"), 0644))
	require.NoError(t, os.WriteFile(forbiddenFile, []byte("<movie></movie>"), 0644))

	tests := []struct {
		name        string
		path        string
		allowedDirs []string
		wantErr     error
		wantPath    string
	}{
		{
			name:        "valid file in allowed directory",
			path:        validFile,
			allowedDirs: []string{allowedDir},
			wantErr:     nil,
			wantPath:    validFile,
		},
		{
			name:        "file outside allowed directory should be denied",
			path:        forbiddenFile,
			allowedDirs: []string{allowedDir},
			wantErr:     ErrNFOAccessDenied,
		},
		{
			name:        "non-existent file should return not found",
			path:        filepath.Join(allowedDir, "nonexistent.nfo"),
			allowedDirs: []string{allowedDir},
			wantErr:     ErrNFONotFound,
		},
		{
			name:        "directory instead of file should be rejected",
			path:        allowedDir,
			allowedDirs: []string{tempDir},
			wantErr:     ErrNFOIsDirectory,
		},
		{
			name:        "empty allowed dirs denies access for security",
			path:        forbiddenFile,
			allowedDirs: []string{},
			wantErr:     ErrNFOAccessDenied,
		},
		{
			name:        "nil allowed dirs denies access for security",
			path:        forbiddenFile,
			allowedDirs: nil,
			wantErr:     ErrNFOAccessDenied,
		},
		{
			name:        "path traversal with ../ should be blocked if outside allowed",
			path:        filepath.Join(allowedDir, "..", "forbidden", "forbidden.nfo"),
			allowedDirs: []string{allowedDir},
			wantErr:     ErrNFOAccessDenied,
		},
		{
			name:        "path traversal within allowed dir should work",
			path:        filepath.Join(allowedDir, "subdir", "..", "valid.nfo"),
			allowedDirs: []string{allowedDir},
			wantErr:     nil,
			wantPath:    validFile,
		},
		{
			name:        "relative path should be resolved",
			path:        "valid.nfo",
			allowedDirs: []string{allowedDir},
			wantErr:     ErrNFONotFound, // Relative path doesn't exist in cwd
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, err := validateNFOPath(tt.path, tt.allowedDirs)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr, "expected error %v, got %v", tt.wantErr, err)
				assert.Empty(t, gotPath, "path should be empty on error")
			} else {
				require.NoError(t, err)
				if tt.wantPath != "" {
					// Clean both paths for comparison (resolve symlinks, etc)
					wantResolved, _ := filepath.EvalSymlinks(tt.wantPath)
					gotResolved, _ := filepath.EvalSymlinks(gotPath)
					assert.Equal(t, wantResolved, gotResolved, "resolved paths should match")
				}
			}
		})
	}
}

func TestValidateNFOPath_SymlinkAttack(t *testing.T) {
	// Create temp directory structure
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	forbiddenDir := filepath.Join(tempDir, "forbidden")

	require.NoError(t, os.Mkdir(allowedDir, 0755))
	require.NoError(t, os.Mkdir(forbiddenDir, 0755))

	// Create file in forbidden directory
	forbiddenFile := filepath.Join(forbiddenDir, "secret.nfo")
	require.NoError(t, os.WriteFile(forbiddenFile, []byte("<secret></secret>"), 0644))

	// Create symlink in allowed directory pointing to forbidden file
	symlinkPath := filepath.Join(allowedDir, "link.nfo")
	err := os.Symlink(forbiddenFile, symlinkPath)
	if err != nil {
		t.Skipf("Skipping symlink test: %v", err)
	}

	// Test that symlink is resolved and access is denied
	_, err = validateNFOPath(symlinkPath, []string{allowedDir})
	assert.ErrorIs(t, err, ErrNFOAccessDenied, "symlink pointing outside allowed dir should be denied")
}

func TestValidateNFOPath_TildeExpansion(t *testing.T) {
	// Get user home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot get user home dir: %v", err)
	}

	// Create a subdirectory in home for testing
	testSubdir := filepath.Join(homeDir, ".javinizer-test-nfo-validation")
	defer func() {
		_ = os.RemoveAll(testSubdir)
	}()

	require.NoError(t, os.Mkdir(testSubdir, 0755))

	// Create a test file in the subdirectory
	testFile := filepath.Join(testSubdir, "test.nfo")
	require.NoError(t, os.WriteFile(testFile, []byte("<movie></movie>"), 0644))

	// Test with tilde in allowed dirs - use subdirectory path with tilde
	tildeSubdir := filepath.Join("~", ".javinizer-test-nfo-validation")
	_, err = validateNFOPath(testFile, []string{tildeSubdir})
	assert.NoError(t, err, "tilde should expand to user home directory")
}

func TestValidateNFOPath_MultipleAllowedDirs(t *testing.T) {
	// Create temp directories
	tempDir := t.TempDir()
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")
	dir3 := filepath.Join(tempDir, "dir3")

	require.NoError(t, os.Mkdir(dir1, 0755))
	require.NoError(t, os.Mkdir(dir2, 0755))
	require.NoError(t, os.Mkdir(dir3, 0755))

	file1 := filepath.Join(dir1, "file1.nfo")
	file2 := filepath.Join(dir2, "file2.nfo")
	file3 := filepath.Join(dir3, "file3.nfo")

	require.NoError(t, os.WriteFile(file1, []byte("<movie></movie>"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("<movie></movie>"), 0644))
	require.NoError(t, os.WriteFile(file3, []byte("<movie></movie>"), 0644))

	// Test that files in any allowed directory are accepted
	allowedDirs := []string{dir1, dir2}

	_, err := validateNFOPath(file1, allowedDirs)
	assert.NoError(t, err, "file1 should be allowed in dir1")

	_, err = validateNFOPath(file2, allowedDirs)
	assert.NoError(t, err, "file2 should be allowed in dir2")

	_, err = validateNFOPath(file3, allowedDirs)
	assert.ErrorIs(t, err, ErrNFOAccessDenied, "file3 should be denied (not in allowed dirs)")
}

func TestValidateNFOPath_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.nfo")
	require.NoError(t, os.WriteFile(testFile, []byte("<movie></movie>"), 0644))

	tests := []struct {
		name        string
		path        string
		allowedDirs []string
		wantErr     error
	}{
		{
			name:        "empty path",
			path:        "",
			allowedDirs: []string{tempDir},
			wantErr:     ErrNFOIsDirectory, // Empty path resolves to current directory
		},
		{
			name:        "dot path (.)",
			path:        ".",
			allowedDirs: []string{tempDir},
			wantErr:     ErrNFOIsDirectory,
		},
		{
			name:        "double dot path (..)",
			path:        "..",
			allowedDirs: []string{tempDir},
			wantErr:     ErrNFOIsDirectory,
		},
		{
			name:        "path with trailing slash",
			path:        tempDir + "/",
			allowedDirs: []string{tempDir},
			wantErr:     ErrNFOIsDirectory,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateNFOPath(tt.path, tt.allowedDirs)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
