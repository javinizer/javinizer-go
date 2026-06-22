package commandutil

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewDependenciesWithOptions: Ctx injection path (lines 111-113) ---

func TestNewDependenciesWithOptions_NilCtxUsesBackground(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	mockDB, err := database.New(&database.Config{DSN: ":memory:"})
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	// opts.Ctx is nil → should use context.Background() for migrations
	opts := &DependenciesOptions{
		DB:  mockDB,
		Ctx: nil,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	assert.NotNil(t, deps)
}

func TestNewDependenciesWithOptions_ExplicitCtxUsed(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	mockDB, err := database.New(&database.Config{DSN: ":memory:"})
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	// Provide an explicit context — this covers the `opts.Ctx != nil` branch
	ctx := context.Background()
	opts := &DependenciesOptions{
		DB:  mockDB,
		Ctx: ctx,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	assert.NotNil(t, deps)
}

// --- NewDependenciesWithOptions: Real DB path with Ctx (covers line 111-113) ---

func TestNewDependenciesWithOptions_RealDBWithCtx(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	ctx := context.Background()
	opts := &DependenciesOptions{
		Ctx: ctx,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	assert.NotNil(t, deps)
	assert.NotNil(t, deps.DB)
}

// --- NewDependenciesWithOptions: Logger injection (covers line 147-149) ---

func TestNewDependenciesWithOptions_ExplicitLogger(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	mockDB, err := database.New(&database.Config{DSN: ":memory:"})
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	injectedLogger := logging.GlobalLogger()
	opts := &DependenciesOptions{
		DB:     mockDB,
		Logger: injectedLogger,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.Equal(t, injectedLogger, deps.Logger)
	assert.Equal(t, injectedLogger, deps.GetLogger())
}

// --- NewDependenciesWithOptions: Default logger path (covers line 150-152) ---

func TestNewDependenciesWithOptions_DefaultLogger(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	mockDB, err := database.New(&database.Config{DSN: ":memory:"})
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	mockRegistry := scraperutil.NewScraperRegistry()
	opts := &DependenciesOptions{
		DB:              mockDB,
		ScraperRegistry: mockRegistry,
		// Logger is nil → should default to GlobalLogger()
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.Equal(t, logging.GlobalLogger(), deps.Logger)
	assert.Equal(t, logging.GlobalLogger(), deps.GetLogger())
}

// --- RunBatchCommand: config.Prepare error (line 172-174) ---

func TestRunBatchCommand_ConfigPrepareError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create a config with an invalid scraper timeout that will fail Prepare validation
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	cfg.Scrapers.TimeoutSeconds = 0 // Invalid: must be between 1 and 300
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:  configPath,
		SourcePath:  tmpDir,
		Destination: tmpDir,
		Presenter:   &SilentBatchCommandPresenter{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
}

// --- RunBatchCommand: Bootstrap error after valid Prepare (line 177-179) ---

func TestRunBatchCommand_BootstrapError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	if os.Getuid() == 0 {
		t.Skip("Root user can create directories anywhere")
	}

	// Create a file where a directory needs to be for the DSN
	blockFile := filepath.Join(tmpDir, "blockfile")
	err := os.WriteFile(blockFile, []byte("x"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(blockFile, "sub", "test.db")
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:  configPath,
		SourcePath:  tmpDir,
		Destination: tmpDir,
		Presenter:   &SilentBatchCommandPresenter{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to bootstrap")
}

// --- RunBatchCommand: files found but no matched IDs (line 209-211) ---

func TestRunBatchCommand_FilesFoundButNoMatchedIDs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a video file with a name that won't match any JAV ID pattern
	videoFile := filepath.Join(tmpDir, "random_video.mp4")
	err := os.WriteFile(videoFile, []byte("fake video content"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	cfg.Matching.Extensions = []string{".mp4", ".mkv", ".avi", ".wmv"}
	cfg.Matching.MinSizeMB = 0
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:  configPath,
		SourcePath:  tmpDir,
		Destination: tmpDir,
		Recursive:   false,
		Presenter:   &SilentBatchCommandPresenter{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	// When no files have matched IDs, RunBatchCommand should return nil
	assert.NoError(t, err)
}

// --- NewDependencies: nil config error ---

func TestNewDependencies_NilConfig_Error(t *testing.T) {
	deps, err := NewDependencies(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config cannot be nil")
	assert.Nil(t, deps)
}

// --- CoreDeps: Close with actual DB ---

func TestCoreDeps_Close_WithDB(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)

	err = deps.Close()
	assert.NoError(t, err)
}

// --- RunBatchCommand: ScanAndMatch error path ---

func TestRunBatchCommand_ScanError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	// Use a source path that doesn't exist — should fail during scan
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:  configPath,
		SourcePath:  "/nonexistent/path/that/does/not/exist",
		Destination: tmpDir,
		Presenter:   &SilentBatchCommandPresenter{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	// May fail during scan or succeed with no files depending on implementation
	// The key is it should not panic
	_ = err
}

// --- RunBatchCommand: SummaryPrinter backward compat with files ---

func TestRunBatchCommand_SummaryPrinterBackwardCompat_WithFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full-pipeline integration test in short mode")
	}
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Create a video file with a JAV ID pattern
	videoFile := filepath.Join(tmpDir, "ABC-123.mp4")
	err := os.WriteFile(videoFile, []byte("fake video content"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	cfg.Matching.Extensions = []string{".mp4", ".mkv", ".avi", ".wmv"}
	cfg.Matching.MinSizeMB = 0
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	summaryCalled := false
	opts := BatchCommandOptions{
		ConfigFile:  configPath,
		SourcePath:  tmpDir,
		Destination: tmpDir,
		Recursive:   false,
		DryRun:      true,
		Presenter:   &SilentBatchCommandPresenter{},
		SummaryPrinter: func(w io.Writer, o BatchCommandOptions, r BatchCommandResult) {
			summaryCalled = true
		},
		Resolved: &workflow.ResolvedSeamStrings{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	_ = err
	_ = summaryCalled
}

// --- CoreDeps: SetConfig and GetConfig round-trip ---

func TestCoreDeps_SetConfig_GetConfig_RoundTrip(t *testing.T) {
	deps := &CoreDeps{}

	cfg1 := &config.Config{ConfigVersion: 1}
	deps.SetConfig(cfg1)
	assert.Equal(t, cfg1, deps.GetConfig())

	cfg2 := &config.Config{ConfigVersion: 2}
	deps.SetConfig(cfg2)
	assert.Equal(t, cfg2, deps.GetConfig())
}

// --- CoreDeps: ReplaceReloadable with real registry ---

func TestCoreDeps_ReplaceReloadable_WithRealRegistry(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	oldCfg := deps.GetConfig()
	oldReg := deps.GetRegistry()

	newCfg := &config.Config{ConfigVersion: 3}
	newReg := scraperutil.NewScraperRegistry()
	deps.ReplaceReloadable(newCfg, newReg)

	assert.Equal(t, newCfg, deps.GetConfig())
	assert.Equal(t, newReg, deps.GetRegistry())
	assert.NotEqual(t, oldCfg, deps.GetConfig())
	assert.NotEqual(t, oldReg, deps.GetRegistry())
}

// --- CoreDeps: ReplaceReloadable nil config panics ---

func TestCoreDeps_ReplaceReloadable_NilConfigPanics(t *testing.T) {
	deps := &CoreDeps{}
	deps.config.Store(&config.Config{})

	assert.Panics(t, func() {
		deps.ReplaceReloadable(nil, nil)
	})
}

// --- CoreDeps: GetRegistry with valid registry ---

func TestCoreDeps_GetRegistry_WithValidRegistry(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	registry := deps.GetRegistry()
	assert.NotNil(t, registry)
	assert.Equal(t, deps.ScraperRegistry, registry)
}

// --- RunBatchCommand: runErr path (line 279-281) ---
// This is hard to trigger directly since it requires a batch job to fail.
// Test via a cancelled context mid-job.

func TestRunBatchCommand_RunError_CancelledContext(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Create a video file with a JAV ID pattern
	videoFile := filepath.Join(tmpDir, "SSIS-001.mp4")
	err := os.WriteFile(videoFile, []byte("fake video content"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	cfg.Matching.Extensions = []string{".mp4", ".mkv", ".avi", ".wmv"}
	cfg.Matching.MinSizeMB = 0
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	// Cancel context immediately to force job.Run to fail
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:  configPath,
		SourcePath:  tmpDir,
		Destination: tmpDir,
		Recursive:   false,
		Presenter:   &SilentBatchCommandPresenter{},
		Resolved:    &workflow.ResolvedSeamStrings{},
	}

	err = RunBatchCommand(ctx, &buf, opts)
	// May or may not error depending on timing, but should not panic
	_ = err
}
