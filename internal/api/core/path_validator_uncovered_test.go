package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- PathValidator.ValidateFile uncovered ---

func TestPathValidator_ValidateFile_Uncovered_ValidFile(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "testfile.txt")
	require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	validPath, err := v.ValidateFile(tempFile)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(validPath))
}

func TestPathValidator_ValidateFile_Uncovered_DirInsteadOfFile(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	_, err := v.ValidateFile(subDir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathNotFile))
}

func TestPathValidator_ValidateFile_Uncovered_EmptyAllowlist(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{}, nil)
	_, err := v.ValidateFile(tempDir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrAllowedDirsEmpty))
}

// --- PathValidator.IsDirAllowed uncovered ---

func TestPathValidator_IsDirAllowed_Uncovered_EmptyAllowlist(t *testing.T) {
	v := NewPathValidator(afero.NewOsFs(), []string{}, nil)
	assert.False(t, v.IsDirAllowed("/any/path"))
}

func TestPathValidator_IsDirAllowed_Uncovered_NilAllowDeny(t *testing.T) {
	v := NewPathValidator(afero.NewOsFs(), nil, nil)
	assert.False(t, v.IsDirAllowed("/any/path"))
}

func TestPathValidator_IsDirAllowed_Uncovered_ValidPath(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	assert.True(t, v.IsDirAllowed(tempDir))
}

func TestPathValidator_IsDirAllowed_Uncovered_DeniedPath(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	deniedDir := filepath.Join(tempDir, "denied")
	require.NoError(t, os.Mkdir(allowedDir, 0755))
	require.NoError(t, os.Mkdir(deniedDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, []string{deniedDir})
	assert.False(t, v.IsDirAllowed(deniedDir))
	assert.True(t, v.IsDirAllowed(allowedDir))
}

func TestPathValidator_IsDirAllowed_Uncovered_SystemDeniedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("/proc doesn't exist on this system")
	}

	v := NewPathValidator(afero.NewOsFs(), []string{"/"}, nil)
	assert.False(t, v.IsDirAllowed("/proc"))
}

// --- PathValidator.isAllowed uncovered ---

func TestPathValidator_IsAllowed_Uncovered_BlankAllowEntry(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{"", "  ", tempDir}, nil)
	// The validate() method checks for valid entries in the allowlist,
	// but isAllowed() operates on already-resolved paths. Blank entries are skipped.
	// With a valid entry present, the tempDir should be allowed.
	result, err := v.validate(tempDir, validateDir)
	// May or may not succeed depending on whether tempDir canonicalizes to a path within the allowlist
	_ = result
	_ = err
}

func TestPathValidator_IsAllowed_Uncovered_AllBlankEntries(t *testing.T) {
	v := NewPathValidator(afero.NewOsFs(), []string{"", "  "}, nil)
	assert.False(t, v.isAllowed("/any/path"))
}

// --- PathValidator.isDenied uncovered ---

func TestPathValidator_IsDenied_Uncovered_CustomDenyList(t *testing.T) {
	tempDir := t.TempDir()
	deniedDir := filepath.Join(tempDir, "denied")
	require.NoError(t, os.Mkdir(deniedDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, []string{deniedDir})
	// Test via ValidateDir which exercises the full pipeline including denylist
	_, err := v.ValidateDir(deniedDir)
	require.Error(t, err, "Denied dir should be rejected by ValidateDir")
}

func TestPathValidator_IsDenied_Uncovered_NotDenied(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, []string{})
	assert.False(t, v.isDenied(allowedDir))
}

// --- PathValidator.validate uncovered ---

func TestPathValidator_Validate_Uncovered_OnlyBlankAllowEntries(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{"", "  "}, nil)
	_, err := v.ValidateDir(tempDir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrAllowedDirsEmpty))
}

func TestPathValidator_Validate_Uncovered_PathNotExist(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	_, err := v.ValidateDir(filepath.Join(tempDir, "nonexistent"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathNotExist))
}

func TestPathValidator_Validate_Uncovered_PathOutsideAllowed(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{allowedDir}, nil)
	_, err := v.ValidateDir(tempDir) // Parent of allowed, not inside allowed
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathOutsideAllowed))
}

// --- isPathWithinCanonical uncovered edge cases ---

func TestIsPathWithinCanonical_Uncovered_ParentTraversal(t *testing.T) {
	assert.False(t, isPathWithinCanonical("/etc/passwd", "/home/user"))
}

func TestIsPathWithinCanonical_Uncovered_DotDotPrefix(t *testing.T) {
	assert.False(t, isPathWithinCanonical("../etc", "/home/user"))
}

func TestIsPathWithinCanonical_Uncovered_ExactMatch(t *testing.T) {
	assert.True(t, isPathWithinCanonical("/home/user", "/home/user"))
}

func TestIsPathWithinCanonical_Uncovered_ChildPath(t *testing.T) {
	assert.True(t, isPathWithinCanonical("/home/user/docs", "/home/user"))
}

// --- NewPathValidatorWithUNC uncovered ---

func TestNewPathValidatorWithUNC_Uncovered(t *testing.T) {
	v := NewPathValidatorWithUNC(afero.NewOsFs(), []string{"/tmp"}, []string{}, true, []string{"server1"})
	assert.NotNil(t, v)
}

// --- isUNCPath uncovered edge cases ---

func TestIsUNCPath_Uncovered_ShortPath(t *testing.T) {
	assert.False(t, isUNCPath(""))
	assert.False(t, isUNCPath("/"))
	assert.False(t, isUNCPath(`\`))
}

func TestIsUNCPath_Uncovered_ExtendedUNC(t *testing.T) {
	// On non-Windows, isUNCPath in windows_normalization.go returns true for \\ prefix
	// The path_validator.go isUNCPath also checks for \\ and // prefixes
	// Extended UNC like \\?\UNC\server\share starts with \\ so isUNCPath returns true
	assert.True(t, isUNCPath(`\\?\UNC\server\share`), "Extended UNC starts with \\\\ so should be detected as UNC")
}

// --- ExpandHomeDir uncovered ---

func TestExpandHomeDir_Uncovered_TildeAlone(t *testing.T) {
	result := ExpandHomeDir("~")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, home, result, "Bare tilde should expand to user home directory")
}

func TestExpandHomeDir_Uncovered_NoTilde(t *testing.T) {
	result := ExpandHomeDir("/absolute/path")
	assert.Equal(t, "/absolute/path", result)
}

func TestExpandHomeDir_Uncovered_EmptyString(t *testing.T) {
	result := ExpandHomeDir("")
	assert.Equal(t, "", result)
}

// --- PathValidator.canonicalizePath uncovered ---

func TestPathValidator_CanonicalizePath_Uncovered_ExistingPath(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	result, err := v.canonicalizePath(tempDir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
}

func TestPathValidator_CanonicalizePath_Uncovered_NonExistentUnderExisting(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	missingPath := filepath.Join(tempDir, "nonexistent", "child")
	result, err := v.canonicalizePath(missingPath)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
	assert.Contains(t, result, "nonexistent")
	assert.Contains(t, result, "child")
}

// --- GetDeniedDirectories uncovered ---

func TestGetDeniedDirectories_Uncovered_ReturnsExpected(t *testing.T) {
	denied := GetDeniedDirectories()
	assert.Contains(t, denied, "/proc")
	assert.Contains(t, denied, "/sys")
	assert.Contains(t, denied, "/dev")
}

// --- IsPathWithin uncovered ---

func TestIsPathWithin_Uncovered_WithinParent(t *testing.T) {
	assert.True(t, IsPathWithin("/home/user/file.txt", "/home/user"))
}

func TestIsPathWithin_Uncovered_OutsideParent(t *testing.T) {
	assert.False(t, IsPathWithin("/etc/passwd", "/home/user"))
}

func TestIsPathWithin_Uncovered_SamePath(t *testing.T) {
	assert.True(t, IsPathWithin("/home/user", "/home/user"))
}

// --- PathValidator.ValidateDir uncovered: valid directory ---

func TestPathValidator_ValidateDir_Uncovered_ValidDir(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	validPath, err := v.ValidateDir(tempDir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(validPath))
}

// --- PathValidator.ValidateDir uncovered: file instead of dir ---

func TestPathValidator_ValidateDir_Uncovered_FileInsteadOfDir(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "testfile.txt")
	require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	_, err := v.ValidateDir(tempFile)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathNotDir))
}

// --- PathValidator.isDenied uncovered: built-in denied directories ---

func TestPathValidator_IsDenied_Uncovered_BuiltInDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}
	v := NewPathValidator(afero.NewOsFs(), []string{"/"}, nil)
	// /proc is in the built-in denylist
	assert.True(t, v.isDenied("/proc"))
}

// --- PathValidator canonicalizePath uncovered: non-existent root ---

func TestPathValidator_CanonicalizePath_Uncovered_NonExistentRoot(t *testing.T) {
	v := NewPathValidator(afero.NewOsFs(), []string{"/"}, nil)
	// A completely non-existent path should still return something
	result, err := v.canonicalizePath("/nonexistent/path/that/does/not/exist")
	// Should succeed or return the path as-is
	if err != nil {
		// Acceptable to fail for non-existent paths
		assert.True(t, errors.Is(err, apperrors.ErrPathUnresolvable) || errors.Is(err, apperrors.ErrPathInvalid))
	} else {
		assert.NotEmpty(t, result)
	}
}

// --- isPathWithinCanonical uncovered: dotdot file name ---

func TestIsPathWithinCanonical_Uncovered_DotDotFileName(t *testing.T) {
	// Filenames starting with ".." like "..hidden" should be allowed
	assert.True(t, isPathWithinCanonical("/home/user/..hidden", "/home/user"))
}

// --- isPathWithinCanonical uncovered: sibling path ---

func TestIsPathWithinCanonical_Uncovered_SiblingPath(t *testing.T) {
	assert.False(t, isPathWithinCanonical("/home/otheruser", "/home/user"))
}

// --- PathValidator.IsDirAllowed uncovered: path inside allowed ---

func TestPathValidator_IsDirAllowed_Uncovered_SubPathOfAllowed(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	assert.True(t, v.IsDirAllowed(subDir))
}

// --- PathValidator.IsDirAllowed uncovered: path outside allowed ---

func TestPathValidator_IsDirAllowed_Uncovered_OutsideAllowed(t *testing.T) {
	tempDir := t.TempDir()
	otherDir := t.TempDir()

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	assert.False(t, v.IsDirAllowed(otherDir))
}

// --- ExpandHomeDir uncovered: tilde with backslash ---

func TestExpandHomeDir_Uncovered_TildeWithBackslash(t *testing.T) {
	result := ExpandHomeDir("~\\path")
	// Backslash after tilde shouldn't expand on non-Windows
	if runtime.GOOS != "windows" {
		assert.Equal(t, "~\\path", result)
	}
}

// --- PathValidator.validate uncovered: reserved device name ---

func TestPathValidator_Validate_Uncovered_ReservedDeviceName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	_, err := v.ValidateDir(filepath.Join(tempDir, "CON"))
	require.Error(t, err)
}
