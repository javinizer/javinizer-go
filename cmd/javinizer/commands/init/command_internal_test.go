package initcmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errDiskLoadStub   = errors.New("disk load stub failure")
	errSaveSparseStub = errors.New("save sparse stub failure")
)

func newInitTempSetup(t *testing.T) (configPath, dbPath string) {
	t.Helper()
	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "config.yaml")
	dbPath = filepath.Join(tmpDir, "data", "javinizer.db")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	require.NoError(t, config.Save(cfg, configPath))
	return configPath, dbPath
}

func noOpDeps() func(cfg *config.Config) (*commandutil.CoreDeps, error) {
	return func(*config.Config) (*commandutil.CoreDeps, error) {
		return &commandutil.CoreDeps{}, nil
	}
}

func TestRun_DiskConfigLoadError(t *testing.T) {
	configPath, _ := newInitTempSetup(t)
	deps := initDeps{
		loadDiskConfig: func(_ string) (*config.Config, error) {
			return nil, errDiskLoadStub
		},
		saveSparseConfig: func(_ *config.Config, _ string, _ config.SparseSaveContext) error {
			t.Fatal("saveSparseConfig must not run when disk load fails")
			return nil
		},
		newDeps: noOpDeps(),
	}
	err := run(&cobra.Command{}, configPath, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load disk config")
	assert.ErrorIs(t, err, errDiskLoadStub)
}

func TestRun_SaveSparseError(t *testing.T) {
	configPath, _ := newInitTempSetup(t)
	emptyDeps, _ := noOpDeps()(nil)
	assert.NoError(t, emptyDeps.Close())
	deps := initDeps{
		loadDiskConfig: config.Load,
		saveSparseConfig: func(_ *config.Config, _ string, _ config.SparseSaveContext) error {
			return errSaveSparseStub
		},
		newDeps: noOpDeps(),
	}
	err := run(&cobra.Command{}, configPath, deps)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save config")
	assert.ErrorIs(t, err, errSaveSparseStub)
}

func TestDefaultInitDeps_RoundTripsSavedConfig(t *testing.T) {
	configPath, dbPath := newInitTempSetup(t)
	diskCfg, err := config.Load(configPath)
	require.NoError(t, err)
	diskCfg.Logging.Level = "debug"
	require.NoError(t, config.Save(diskCfg, configPath))

	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	require.NoError(t, run(&cobra.Command{}, configPath, defaultInitDeps()))

	reloaded, err := config.Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "debug", reloaded.Logging.Level)

	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "timeout_seconds")
}
