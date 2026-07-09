package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
)

func TestIsFilesystemRoot(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/", true},
		{"/home", false},
		{"", false},
		{".", false},
		{"/proc", false},
	}

	tests = append(tests,
		struct {
			path string
			want bool
		}{"C:\\", true},
		struct {
			path string
			want bool
		}{"C:/", true},
		struct {
			path string
			want bool
		}{"D:\\", true},
		struct {
			path string
			want bool
		}{"D:/", true},
	)

	for _, tt := range tests {
		name := tt.path
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, isFilesystemRoot(tt.path))
		})
	}
}

func TestEffectiveAllowedBase_RootResolvingDotUnderRootCWD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific CWD test")
	}

	t.Chdir("/")

	v := NewPathValidator(afero.NewOsFs(), []string{"."}, nil)

	canonicalBase, usable := v.effectiveAllowedBase(".")
	assert.False(t, usable, "\".\" under CWD \"/\" should resolve to root and be unusable")
	assert.Empty(t, canonicalBase)
}

func TestEffectiveAllowedBase_DotUnderRealCWD(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	v := NewPathValidator(afero.NewOsFs(), []string{"."}, nil)

	canonicalBase, usable := v.effectiveAllowedBase(".")
	assert.True(t, usable, "\".\" under a real CWD should resolve to a non-root absolute path")
	assert.NotEmpty(t, canonicalBase)
	assert.NotEqual(t, "/", canonicalBase)
}

func TestEffectiveAllowedBase_ExplicitRoot(t *testing.T) {
	v := NewPathValidator(afero.NewOsFs(), []string{"/"}, nil)

	_, usable := v.effectiveAllowedBase("/")
	assert.False(t, usable, "explicit \"/\" should be unusable")
}

func TestValidateDir_DotUnderRootCWD_DeniesByDefault(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific CWD test")
	}

	t.Chdir("/")

	outsideDir := t.TempDir()

	v := NewPathValidatorWithUNC(afero.NewOsFs(), []string{"."}, nil, false, nil)
	_, err := v.ValidateDir(outsideDir)

	require.Error(t, err)
	assert.True(t, apperrors.IsPathError(err, apperrors.ErrAllowedDirsEmpty),
		"expected ErrAllowedDirsEmpty when \".\" resolves to root, got %v", err)
}

func TestValidateDir_DotUnderRealCWD_AllowsCWD(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	v := NewPathValidatorWithUNC(afero.NewOsFs(), []string{"."}, nil, false, nil)
	_, err := v.ValidateDir(tempDir)
	assert.NoError(t, err, "\".\" under a real CWD should allow scanning within that dir")
}

func TestValidateDir_MixedRootAndValid_AllowsValidOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific CWD test")
	}

	t.Chdir("/")

	validDir := t.TempDir()
	outsideDir := t.TempDir()

	v := NewPathValidatorWithUNC(afero.NewOsFs(), []string{".", validDir}, nil, false, nil)

	_, err := v.ValidateDir(validDir)
	assert.NoError(t, err, "valid allowlist entry should allow its own dir")

	_, err = v.ValidateDir(outsideDir)
	require.Error(t, err)
	assert.True(t, apperrors.IsPathError(err, apperrors.ErrPathOutsideAllowed) || apperrors.IsPathError(err, apperrors.ErrAllowedDirsEmpty),
		"expected path-outside or empty-allowlist error for dir outside valid entry, got %v", err)
}

func TestValidateDir_ExplicitRoot_DeniesByDefault(t *testing.T) {
	tempDir := t.TempDir()

	v := NewPathValidatorWithUNC(afero.NewOsFs(), []string{"/"}, nil, false, nil)
	_, err := v.ValidateDir(tempDir)

	require.Error(t, err)
	assert.True(t, apperrors.IsPathError(err, apperrors.ErrAllowedDirsEmpty),
		"explicit \"/\" should behave as empty allowlist (deny-by-default), got %v", err)
}

func TestIsDirAllowed_DotUnderRootCWD_ReturnsFalse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific CWD test")
	}

	t.Chdir("/")

	outsideDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{"."}, nil)

	assert.False(t, v.IsDirAllowed(outsideDir),
		"IsDirAllowed should return false for outside dir when \".\" resolves to root")
}

func TestIsDirAllowed_DotUnderRealCWD_ReturnsTrue(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	v := NewPathValidator(afero.NewOsFs(), []string{"."}, nil)
	assert.True(t, v.IsDirAllowed(tempDir),
		"IsDirAllowed should return true for CWD dir when \".\" resolves to a real CWD")
}

func TestIsDirAllowed_ExplicitRoot_ReturnsFalse(t *testing.T) {
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{"/"}, nil)

	assert.False(t, v.IsDirAllowed(tempDir),
		"IsDirAllowed should return false when allowlist is [\"/\"] (root = unusable)")
}

func TestIsDirAllowed_MixedRootAndValid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific CWD test")
	}

	t.Chdir("/")

	validDir := t.TempDir()
	outsideDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{".", validDir}, nil)

	assert.True(t, v.IsDirAllowed(validDir), "valid entry should allow its dir")
	assert.False(t, v.IsDirAllowed(outsideDir), "outside dir should be denied")
}

func TestValidateDir_DotWithSubpathOutsideCWD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific CWD test")
	}

	tempDir := t.TempDir()
	t.Chdir(tempDir)

	v := NewPathValidatorWithUNC(afero.NewOsFs(), []string{"."}, nil, false, nil)

	// A subdirectory of the CWD (tempDir) should be allowed
	subDir := filepath.Join(tempDir, "subfolder")
	require.NoError(t, os.Mkdir(subDir, 0755))
	_, err := v.ValidateDir(subDir)
	assert.NoError(t, err)
}

func TestEffectiveAllowedBase_CanonicalizeError(t *testing.T) {
	v := NewPathValidator(afero.NewOsFs(), []string{"/tmp"}, nil)

	origEval := evalSymlinksFunc
	t.Cleanup(func() { evalSymlinksFunc = origEval })

	evalSymlinksFunc = func(string) (string, error) {
		return "", assert.AnError
	}

	_, usable := v.effectiveAllowedBase("/tmp")
	assert.False(t, usable, "canonicalizePath error should make entry unusable")
}

func TestIsFilesystemRoot_NotRoot(t *testing.T) {
	assert.False(t, isFilesystemRoot("/home"))
	assert.False(t, isFilesystemRoot("/tmp/test"))
	assert.False(t, isFilesystemRoot(""))
	assert.False(t, isFilesystemRoot("."))
	assert.False(t, isFilesystemRoot("relative/path"))
}

func TestIsFilesystemRoot_Exported(t *testing.T) {
	assert.True(t, IsFilesystemRoot("/"))
	assert.True(t, IsFilesystemRoot("C:\\"))
	assert.False(t, IsFilesystemRoot("/home"))
	assert.False(t, IsFilesystemRoot(""))
}

func TestEffectiveAllowedBase_FilepathAbsError(t *testing.T) {
	v := NewPathValidator(afero.NewOsFs(), []string{"/tmp"}, nil)

	origAbs := filepathAbs
	t.Cleanup(func() { filepathAbs = origAbs })

	filepathAbs = func(string) (string, error) { return "", assert.AnError }

	_, usable := v.effectiveAllowedBase("/tmp")
	assert.False(t, usable, "filepath.Abs error should make entry unusable")
}
