package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ValidateAndOpenPath: validation fails (empty allow list) ---

func TestValidateAndOpenPath_Miss_EmptyAllowList(t *testing.T) {
	tempDir := t.TempDir()
	securityCfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{},
		DeniedDirectories:  []string{},
	}

	f, path, err := ValidateAndOpenPath(tempDir, securityCfg)
	require.Error(t, err)
	assert.Nil(t, f)
	assert.Empty(t, path)
	assert.True(t, errors.Is(err, apperrors.ErrAllowedDirsEmpty))
}

// --- ValidateAndOpenPath: path does not exist between validation and stat ---

func TestValidateAndOpenPath_Miss_PathDeletedAfterValidation(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	securityCfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
	}

	// Normal case works
	f, path, err := ValidateAndOpenPath(allowedDir, securityCfg)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	assert.True(t, filepath.IsAbs(path))

	// Delete the directory then try again — validateScanPath will fail
	require.NoError(t, os.Remove(allowedDir))
	f, _, err = ValidateAndOpenPath(allowedDir, securityCfg)
	require.Error(t, err)
	assert.Nil(t, f)
}

// --- ValidateAndOpenPath: file instead of dir after validation ---

func TestValidateAndOpenPath_Miss_FileInsteadOfDirAfterValidation(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "testfile.txt")
	require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

	securityCfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
	}

	// A file should fail the "is a directory" check in ValidateAndOpenPath
	f, _, err := ValidateAndOpenPath(tempFile, securityCfg)
	require.Error(t, err)
	assert.Nil(t, f)
}

// --- PathValidator.validate: stat error that is not IsNotExist ---

func TestPathValidator_Validate_Miss_StatNonNotExistError(t *testing.T) {
	tempDir := t.TempDir()

	// Use a custom fs that returns a non-NotExist error on Stat
	fs := &statErrorFs{delegate: afero.NewOsFs(), err: os.ErrPermission}

	v := NewPathValidator(fs, []string{tempDir}, nil)
	_, err := v.ValidateDir(tempDir)
	require.Error(t, err)
	// The error should be ErrPathInvalid (not ErrPathNotExist)
	assert.True(t, errors.Is(err, apperrors.ErrPathInvalid))
}

// --- PathValidator.validate: file instead of dir ---

func TestPathValidator_Validate_Miss_FileNotDir(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "testfile.txt")
	require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	_, err := v.ValidateDir(tempFile)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathNotDir))
}

// --- PathValidator.validate: dir instead of file ---

func TestPathValidator_Validate_Miss_DirNotFile(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	_, err := v.ValidateFile(subDir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathNotFile))
}

// --- PathValidator.validate: denied path ---

func TestPathValidator_Validate_Miss_DeniedPath(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	deniedDir := filepath.Join(tempDir, "denied")
	require.NoError(t, os.Mkdir(allowedDir, 0755))
	require.NoError(t, os.Mkdir(deniedDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, []string{deniedDir})
	_, err := v.ValidateDir(deniedDir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathInDenylist))
}

// --- PathValidator.validate: path outside allowed directories ---

func TestPathValidator_Validate_Miss_OutsideAllowed(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	otherDir := t.TempDir()

	v := NewPathValidator(afero.NewOsFs(), []string{allowedDir}, nil)
	_, err := v.ValidateDir(otherDir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathOutsideAllowed))
}

// --- PathValidator.validate: path does not exist ---

func TestPathValidator_Validate_Miss_NotExist(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)

	nonExistent := filepath.Join(tempDir, "nonexistent")
	_, err := v.ValidateDir(nonExistent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathNotExist))
}

// --- PathValidator: allowlist entry that doesn't resolve ---

func TestPathValidator_Miss_AllowEntryUnresolvable(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	// Include an unresolvable allow entry alongside a valid one
	v := NewPathValidator(afero.NewOsFs(), []string{"/nonexistent/allow/path", tempDir}, nil)

	result, err := v.ValidateDir(subDir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
}

// --- PathValidator: denylist entry that doesn't resolve ---

func TestPathValidator_Miss_DenyEntryUnresolvable(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, []string{"/nonexistent/deny/path"})

	result, err := v.ValidateDir(subDir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
}

// --- ValidateScanPath: valid path ---

func TestValidateScanPath_Miss_ValidPath(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	cfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
	}

	result, err := ValidateScanPath(allowedDir, cfg)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
}

// --- ValidateScanPath: denied path ---

func TestValidateScanPath_Miss_DeniedPath(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	deniedDir := filepath.Join(tempDir, "denied")
	require.NoError(t, os.Mkdir(allowedDir, 0755))
	require.NoError(t, os.Mkdir(deniedDir, 0755))

	cfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{deniedDir},
	}

	_, err := ValidateScanPath(deniedDir, cfg)
	require.Error(t, err)
}

// --- PathValidator.canonicalizePath: LstatIfPossible error that is not IsNotExist ---

func TestPathValidator_CanonicalizePath_Miss_LstatError(t *testing.T) {
	tempDir := t.TempDir()
	// Use a fs that returns a non-NotExist error on Stat/Lstat
	fs := &statErrorFs{delegate: afero.NewOsFs(), err: os.ErrPermission}

	v := NewPathValidator(fs, []string{tempDir}, nil)
	// canonicalizePath with a non-existent child path under an existing parent
	// will call LstatIfPossible and get an error
	nonExistent := filepath.Join(tempDir, "nonexistent", "child")
	_, err := v.canonicalizePath(nonExistent)
	require.Error(t, err)
}

// --- ValidateAndOpenPath: valid subdirectory ---

func TestValidateAndOpenPath_Miss_ValidSubdirectory(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "nested", "deep")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	securityCfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
	}

	f, path, err := ValidateAndOpenPath(subDir, securityCfg)
	require.NoError(t, err)
	defer f.Close()

	assert.NotNil(t, f)
	assert.True(t, filepath.IsAbs(path))
}
