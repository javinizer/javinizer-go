//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// A system directory can become unresolvable independently of the requested
// media directory (for example, an inaccessible mount). It must not prevent
// unrelated allowed paths from being used or disable the configured denylist.
func TestPathValidatorUnavailableSystemDirectoriesPreserveAccessPolicy(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "media")
	denied := filepath.Join(allowed, "private")
	outside := filepath.Join(root, "outside")
	for _, dir := range []string{allowed, denied, outside} {
		require.NoError(t, os.MkdirAll(dir, 0700))
	}
	original := evalSymlinksFunc
	t.Cleanup(func() { evalSymlinksFunc = original })
	failures := make(map[string]int)
	evalSymlinksFunc = func(path string) (string, error) {
		for _, system := range getDeniedDirectories() {
			if path == system {
				failures[path]++
				return "", &os.PathError{Op: "lstat", Path: path, Err: os.ErrPermission}
			}
		}
		return original(path)
	}
	validator := NewPathValidator(afero.NewOsFs(), []string{allowed}, []string{denied})
	canonical, err := validator.ValidateDir(allowed)
	require.NoError(t, err)
	expected, err := original(allowed)
	require.NoError(t, err)
	require.Equal(t, expected, canonical)
	require.True(t, validator.IsDirAllowed(allowed))
	_, err = validator.ValidateDir(denied)
	require.ErrorIs(t, err, apperrors.ErrPathInDenylist)
	require.False(t, validator.IsDirAllowed(denied))
	_, err = validator.ValidateDir(outside)
	require.ErrorIs(t, err, apperrors.ErrPathOutsideAllowed)
	require.False(t, validator.IsDirAllowed(outside))
	for _, system := range getDeniedDirectories() {
		require.GreaterOrEqual(t, failures[system], 2, "both access-check entry points must tolerate unavailable system paths")
	}
}
