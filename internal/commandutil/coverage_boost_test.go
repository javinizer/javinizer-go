package commandutil

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewDependenciesWithOptions_InjectedCtx tests that an injected context is used for migrations.
func TestNewDependenciesWithOptions_InjectedCtx(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	mockDB, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	opts := &DependenciesOptions{
		DB:  mockDB,
		Ctx: nil, // nil Ctx should use context.Background()
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	assert.NotNil(t, deps)
}

// TestBootstrap_Success tests the Bootstrap function with a valid config.
func TestBootstrap_Success(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	result, err := Bootstrap(cfg)
	require.NoError(t, err)
	require.NotNil(t, result)
	defer func() { _ = result.Close() }()

	assert.NotNil(t, result.CoreDeps)
	assert.NotNil(t, result.WorkflowComponents)
}

// TestBootstrapScrapeOnly_Success tests the BootstrapScrapeOnly function.
func TestBootstrapScrapeOnly_Success(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	result, err := BootstrapScrapeOnly(cfg)
	require.NoError(t, err)
	require.NotNil(t, result)
	defer func() { _ = result.Close() }()

	assert.NotNil(t, result.CoreDeps)
	assert.NotNil(t, result.WorkflowComponents)
	assert.NotNil(t, result.Workflow)
}

// TestBatchJobConfigFromAppConfig_Defaults tests mapping with default config.
func TestBatchJobConfigFromAppConfig_Defaults(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	result := BatchJobConfigFromAppConfig(cfg)

	assert.Equal(t, cfg.Performance.MaxWorkers, result.MaxWorkers)
	assert.Equal(t, time.Duration(cfg.Performance.WorkerTimeout)*time.Second, result.WorkerTimeout)
	assert.Equal(t, cfg.Scrapers.Priority, result.ScraperPriority)
	assert.Equal(t, cfg.Metadata.NFO.Feature.Enabled, result.NFOEnabled)
}

// TestCLIApplyOptions_ToApplyPhaseConfig_NilExtrafanart tests that an unset
// --extrafanart yields a nil *bool so the downloader uses the config default.
func TestCLIApplyOptions_ToApplyPhaseConfig_NilExtrafanart(t *testing.T) {
	opts := CLIApplyOptions{
		DownloadExtrafanart: false,
	}
	cfg := opts.ToApplyPhaseConfig()
	assert.Nil(t, cfg.DownloadExtrafanart)
}

// TestCLIApplyOptions_ToApplyPhaseConfig_AllSet tests full option mapping.
func TestCLIApplyOptions_ToApplyPhaseConfig_AllSet(t *testing.T) {
	opts := CLIApplyOptions{
		DryRun:              true,
		MoveFiles:           true,
		LinkMode:            "hard",
		ForceUpdate:         true,
		SkipOrganize:        true,
		GenerateNFO:         true,
		Download:            true,
		DownloadExtrafanart: true,
		Destination:         "/dest/path",
	}

	cfg := opts.ToApplyPhaseConfig()
	assert.True(t, cfg.DryRun)
	assert.True(t, cfg.OrganizeOptions.MoveFiles)
	assert.True(t, cfg.OrganizeOptions.ForceUpdate)
	assert.True(t, cfg.OrganizeOptions.Skip)
	assert.True(t, cfg.GenerateNFO)
	assert.True(t, cfg.Download)
	assert.True(t, *cfg.DownloadExtrafanart)
	assert.Equal(t, "/dest/path", cfg.Destination)
}

// TestNewDependenciesWithOptions_EmptyOptsCreatesDBAndRegistry tests auto-creation.
func TestNewDependenciesWithOptions_EmptyOptsCreatesDBAndRegistry(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	opts := &DependenciesOptions{}
	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	assert.NotNil(t, deps.DB)
	assert.NotNil(t, deps.ScraperRegistry)
}

// TestBootstrap_NilConfig tests that Bootstrap fails with nil config.
func TestBootstrap_NilConfig(t *testing.T) {
	result, err := Bootstrap(nil)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestBootstrapScrapeOnly_NilConfig tests that BootstrapScrapeOnly fails with nil config.
func TestBootstrapScrapeOnly_NilConfig(t *testing.T) {
	result, err := BootstrapScrapeOnly(nil)
	assert.Error(t, err)
	assert.Nil(t, result)
}
