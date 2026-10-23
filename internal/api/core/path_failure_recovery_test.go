package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPathValidatorRejectsSymlinkLoopAndSkipsUnresolvableDenyEntry(t *testing.T) {
	root := t.TempDir()
	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	valid := filepath.Join(root, "valid")
	require.NoError(t, os.Mkdir(valid, 0700))
	validator := NewPathValidator(afero.NewOsFs(), []string{root}, []string{loop})
	require.False(t, validator.IsDirAllowed(loop), "an unresolvable requested path must fail closed")
	canonical, err := validator.ValidateDir(loop)
	require.Empty(t, canonical)
	require.ErrorIs(t, err, apperrors.ErrPathUnresolvable)
	require.True(t, validator.IsDirAllowed(valid), "a broken deny entry must not make unrelated allowed directories unusable")
	canonical, err = validator.ValidateDir(valid)
	require.NoError(t, err)
	realValid, err := filepath.EvalSymlinks(valid)
	require.NoError(t, err)
	require.Equal(t, realValid, canonical)
}

func TestRuntimeBatchFactoryUnavailableAfterInvalidMatcherConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "["
	rt := NewAPIRuntime(&APIDeps{})
	rt.ReplaceReloadable(cfg, scraperutil.NewScraperRegistry())
	require.Nil(t, rt.GetBatchJobFactory(), "invalid matcher must not produce a partially wired job factory")
	require.Nil(t, rt.GetBatchJobFactory(), "repeated access remains safely unavailable")
}

func TestPathValidatorRejectsRelativePathAfterWorkingDirectoryRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not permit removal of the active working directory")
	}
	cwd := t.TempDir()
	t.Chdir(cwd)
	require.NoError(t, os.Remove(cwd))
	validator := NewPathValidatorBrowse(afero.NewOsFs(), nil, false, nil)
	resolved, err := validator.ValidateDir("relative")
	require.Empty(t, resolved)
	require.ErrorIs(t, err, apperrors.ErrPathInvalid)
	require.False(t, validator.IsDirAllowed("relative"), "a missing working directory must not authorize a relative path")
}
