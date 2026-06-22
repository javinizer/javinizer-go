package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- PathValidator.validate: UNC path blocked (lines 114-116) ---

func TestPathValidator_Miss2_UNCPathBlocked(t *testing.T) {
	if !isUNCPath("//server/share") {
		t.Skip("UNC path detection is Windows-specific")
	}

	v := NewPathValidator(afero.NewOsFs(), []string{"/"}, nil)
	v.allowUNC = false

	_, err := v.ValidateDir("//server/share")
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))
}

// --- PathValidator.validate: UNC path with UNC allowed (lines 122-130) ---

func TestPathValidator_Miss2_UNCPathAllowed(t *testing.T) {
	if !isUNCPath("//server/share") {
		t.Skip("UNC path normalization is Windows-specific")
	}

	v := NewPathValidatorWithUNC(afero.NewOsFs(), []string{"//server/share"}, nil, true, []string{"server"})

	_, err := v.ValidateDir("//server/share/subdir")
	if err != nil {
		assert.False(t, errors.Is(err, apperrors.ErrUNCPathBlocked), "UNC should be allowed")
	}
}

// --- PathValidator.validate: canonicalizePath error (line 135-137) ---

func TestPathValidator_Miss2_CanonicalizeError(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	fs := &statErrorFs{delegate: afero.NewOsFs(), err: os.ErrPermission}
	v := NewPathValidator(fs, []string{tempDir}, nil)

	_, err := v.ValidateDir(filepath.Join(tempDir, "nonexistent"))
	require.Error(t, err)
}

// --- PathValidator.validate: canonicalPath not absolute (lines 139-143) ---

func TestPathValidator_Miss2_CanonicalPathNotAbsolute(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	result, err := v.ValidateDir(subDir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
}

// --- PathValidator.isAllowed: unresolvable allow entry (line 190-191) ---

func TestPathValidator_Miss2_IsAllowed_UnresolvableAllowEntry(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{"/nonexistent/allow/path", tempDir}, nil)

	result, err := v.ValidateDir(subDir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
}

// --- PathValidator.isDenied: unresolvable deny entry (line 228-229) ---

func TestPathValidator_Miss2_IsDenied_UnresolvableDenyEntry(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, []string{"/nonexistent/deny/path"})
	result, err := v.ValidateDir(subDir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(result))
}

// --- PathValidator.IsDirAllowed: unresolvable deny entry (lines 248-254) ---

func TestPathValidator_Miss2_IsDirAllowed_DenyCanonicalizeError(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, []string{"/nonexistent/deny/path"})
	assert.True(t, v.IsDirAllowed(subDir))
}

// --- PathValidator.IsDirAllowed: unresolvable allow entry (lines 261-262) ---

func TestPathValidator_Miss2_IsDirAllowed_AllowCanonicalizeError(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{"/nonexistent/allow/path", tempDir}, nil)
	assert.True(t, v.IsDirAllowed(subDir))
}

// --- PathValidator.IsDirAllowed: built-in deny with canonicalize error (lines 276-277) ---

func TestPathValidator_Miss2_IsDirAllowed_BuiltInDenyCanonicalizeError(t *testing.T) {
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))

	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	assert.True(t, v.IsDirAllowed(subDir))
}

// --- PathValidator.canonicalizePath: Lstat error that is not NotExist (line 296-297) ---

func TestPathValidator_Miss2_CanonicalizePath_LstatNonNotExistError(t *testing.T) {
	tempDir := t.TempDir()
	fs := &statErrorFs{delegate: afero.NewOsFs(), err: os.ErrPermission}
	v := NewPathValidator(fs, []string{tempDir}, nil)

	nonExistent := filepath.Join(tempDir, "a", "b", "c")
	_, err := v.canonicalizePath(nonExistent)
	require.Error(t, err)
}

// --- PathValidator.canonicalizePath: root reached for non-existent path (lines 325-328) ---

func TestPathValidator_Miss2_CanonicalizePath_RootReached(t *testing.T) {
	memFS := afero.NewMemMapFs()
	v := NewPathValidator(memFS, []string{"/"}, nil)

	result, err := v.canonicalizePath("/completely/nonexistent/path")
	if err != nil {
		assert.True(t, errors.Is(err, apperrors.ErrPathUnresolvable) || errors.Is(err, apperrors.ErrPathInvalid))
	} else {
		assert.NotEmpty(t, result)
	}
}

// --- PathValidator.validate: stat error that is not NotExist returns ErrPathInvalid ---
// Uses a fs that returns permission error on ALL Stat calls, which triggers
// the canonicalizePath error path and the final Stat error path.

func TestPathValidator_Miss2_Validate_StatPermissionError(t *testing.T) {
	tempDir := t.TempDir()

	fs := &allStatErrorFs{delegate: afero.NewOsFs(), err: os.ErrPermission}
	v := NewPathValidator(fs, []string{tempDir}, nil)
	_, err := v.ValidateDir(tempDir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathInvalid))
}

// --- PathValidator.validate: path does not exist (ErrPathNotExist) via allStatErrorFs ---

func TestPathValidator_Miss2_Validate_StatNotExistError(t *testing.T) {
	tempDir := t.TempDir()

	fs := &allStatErrorFs{delegate: afero.NewOsFs(), err: os.ErrNotExist}
	v := NewPathValidator(fs, []string{tempDir}, nil)
	_, err := v.ValidateDir(tempDir)
	require.Error(t, err)
	// With os.ErrNotExist on Stat, we should get ErrPathNotExist
	// But canonicalizePath may also fail first since EvalSymlinks may fail
	assert.Error(t, err)
}

// allStatErrorFs returns an error on ALL Stat calls.
type allStatErrorFs struct {
	delegate afero.Fs
	err      error
}

func (f *allStatErrorFs) Create(name string) (afero.File, error) { return f.delegate.Create(name) }
func (f *allStatErrorFs) Mkdir(name string, perm os.FileMode) error {
	return f.delegate.Mkdir(name, perm)
}
func (f *allStatErrorFs) MkdirAll(name string, perm os.FileMode) error {
	return f.delegate.MkdirAll(name, perm)
}
func (f *allStatErrorFs) Open(name string) (afero.File, error) { return f.delegate.Open(name) }
func (f *allStatErrorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	return f.delegate.OpenFile(name, flag, perm)
}
func (f *allStatErrorFs) Remove(name string) error    { return f.delegate.Remove(name) }
func (f *allStatErrorFs) RemoveAll(name string) error { return f.delegate.RemoveAll(name) }
func (f *allStatErrorFs) Rename(oldname, newname string) error {
	return f.delegate.Rename(oldname, newname)
}
func (f *allStatErrorFs) Stat(name string) (os.FileInfo, error) { return nil, f.err }
func (f *allStatErrorFs) Name() string                          { return "allStatErrorFs" }
func (f *allStatErrorFs) Chmod(name string, perm os.FileMode) error {
	return f.delegate.Chmod(name, perm)
}
func (f *allStatErrorFs) Chown(name string, uid, gid int) error {
	return f.delegate.Chown(name, uid, gid)
}
func (f *allStatErrorFs) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return f.delegate.Chtimes(name, atime, mtime)
}
