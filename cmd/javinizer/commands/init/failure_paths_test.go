package initcmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestInitRejectsMalformedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.yaml")
	require.NoError(t, os.WriteFile(path, []byte("["), 0o600))
	err := run(&cobra.Command{}, path, initDeps{})
	require.ErrorContains(t, err, "failed to load config:")
}

func TestInitRejectsBlockedDataDirectory(t *testing.T) {
	path, _ := newInitTempSetup(t)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	blocker := filepath.Join(filepath.Dir(path), "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("fixture"), 0o600))
	cfg.Database.DSN = filepath.Join(blocker, "database.sqlite")
	require.NoError(t, config.Save(cfg, path))
	t.Setenv("JAVINIZER_DB", cfg.Database.DSN)
	err = run(&cobra.Command{}, path, initDeps{})
	require.ErrorContains(t, err, "failed to create data directory:")
	var pathErr *os.PathError
	require.True(t, errors.As(err, &pathErr))
	require.Equal(t, blocker, pathErr.Path)
}

func TestInitPropagatesDependencyFailure(t *testing.T) {
	path, dbPath := newInitTempSetup(t)
	want := errors.New("dependency fixture failure")
	calls := 0
	deps := initDeps{newDeps: func(cfg *config.Config) (*commandutil.CoreDeps, error) {
		calls++
		require.Equal(t, dbPath, cfg.Database.DSN)
		return nil, want
	}}
	err := run(&cobra.Command{}, path, deps)
	require.EqualError(t, err, "failed to initialize dependencies: dependency fixture failure")
	require.ErrorIs(t, err, want)
	require.Equal(t, 1, calls)
	require.DirExists(t, filepath.Dir(dbPath))
	require.NoFileExists(t, dbPath)
}
