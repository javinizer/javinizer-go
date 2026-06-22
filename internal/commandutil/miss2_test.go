package commandutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- RunBatchCommand tests ---

func TestRunBatchCommand_ConfigLoadError(t *testing.T) {
	// Use an invalid config file path that will cause LoadOrCreate to fail
	invalidConfigPath := testutil.InvalidConfigPath(t)

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:  invalidConfigPath,
		SourcePath:  "/nonexistent",
		Destination: "/dest",
		Presenter:   &SilentBatchCommandPresenter{},
	}

	err := RunBatchCommand(context.Background(), &buf, opts)
	// Should fail because the config file has invalid YAML
	assert.Error(t, err)
}

func TestRunBatchCommand_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Write an invalid config file with an unsupported config version
	cfgContent := "config_version: 999999\ndatabase:\n  dsn: ':memory:'\n"
	err := os.WriteFile(configPath, []byte(cfgContent), 0644)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:  configPath,
		SourcePath:  tmpDir,
		Destination: "/dest",
		Presenter:   &SilentBatchCommandPresenter{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	assert.Error(t, err)
}

func TestRunBatchCommand_ValidConfig_NoFiles(t *testing.T) {
	// Create a config file in a temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:        configPath,
		SourcePath:        tmpDir,
		Destination:       tmpDir,
		Recursive:         false,
		DryRun:            true,
		CommandLabel:      "Javinizer Sort",
		ActionVerb:        "Processing files",
		CompletionMessage: "Sort complete!",
		Presenter:         &SilentBatchCommandPresenter{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	// Should succeed (no files to process in empty dir)
	assert.NoError(t, err)
}

func TestRunBatchCommand_DefaultPresenter(t *testing.T) {
	// Test with nil presenter — should use defaultBatchCommandPresenter
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:        configPath,
		SourcePath:        tmpDir,
		Destination:       tmpDir,
		Recursive:         false,
		DryRun:            true,
		CommandLabel:      "Javinizer Sort",
		ActionVerb:        "Processing files",
		CompletionMessage: "Sort complete!",
		Presenter:         nil, // should use default presenter
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	assert.NoError(t, err)

	// Default presenter should have written output
	output := buf.String()
	assert.Contains(t, output, "=== Javinizer Sort ===")
	assert.Contains(t, output, "Scanning for video files")
	assert.Contains(t, output, "No files to process")
}

func TestRunBatchCommand_DownloadExtrafanartFlag(t *testing.T) {
	// Test that DownloadExtrafanart flag is applied to config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	cfg.Output.Download.DownloadExtrafanart = false
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile:          configPath,
		SourcePath:          tmpDir,
		Destination:         tmpDir,
		DownloadExtrafanart: true,
		Presenter:           &SilentBatchCommandPresenter{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	assert.NoError(t, err)
}

func TestRunBatchCommand_SummaryPrinterBackwardCompat_NoFiles(t *testing.T) {
	// Test the SummaryPrinter backward-compatible path
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	summaryCalled := false
	opts := BatchCommandOptions{
		ConfigFile:  configPath,
		SourcePath:  tmpDir,
		Destination: tmpDir,
		Presenter:   &SilentBatchCommandPresenter{},
		SummaryPrinter: func(w io.Writer, o BatchCommandOptions, r BatchCommandResult) {
			summaryCalled = true
		},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	assert.NoError(t, err)
	// When no files found, OnNoFiles returns before summary is printed
	assert.False(t, summaryCalled, "SummaryPrinter should not be called when no files found")
}

// --- NewDependenciesWithOptions error paths ---

func TestNewDependenciesWithOptions_DirCreationFailure(t *testing.T) {
	// Create a path where the parent is a file, not a directory
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "blocking-file")
	err := os.WriteFile(filePath, []byte("block"), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(filePath, "subdir", "test.db"),
		},
	}

	deps, err := NewDependenciesWithOptions(cfg, nil)
	assert.Error(t, err)
	assert.Nil(t, deps)
}

func TestNewDependenciesWithOptions_InjectedDBAndCtx(t *testing.T) {
	// Test providing both DB and context via opts
	mockDB, err := database.New(&database.Config{DSN: ":memory:"})
	require.NoError(t, err)
	defer func() { _ = mockDB.Close() }()

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	ctx := context.Background()
	opts := &DependenciesOptions{
		DB:  mockDB,
		Ctx: ctx,
	}

	deps, err := NewDependenciesWithOptions(cfg, opts)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()
	assert.NotNil(t, deps)
	assert.Equal(t, mockDB, deps.DB)
}

// --- Bootstrap error paths ---

func TestBootstrap_InvalidDSN(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Root user can create directories anywhere")
	}

	// Create a path where the parent is a file, so DSN directory creation fails
	tmpDir := t.TempDir()
	blockFile := filepath.Join(tmpDir, "blockfile")
	err := os.WriteFile(blockFile, []byte("x"), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(blockFile, "sub", "test.db"),
		},
	}

	result, err := Bootstrap(cfg)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestBootstrapScrapeOnly_InvalidDSN(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Root user can create directories anywhere")
	}

	// Create a path where the parent is a file, so DSN directory creation fails
	tmpDir := t.TempDir()
	blockFile := filepath.Join(tmpDir, "blockfile")
	err := os.WriteFile(blockFile, []byte("x"), 0644)
	require.NoError(t, err)

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(blockFile, "sub", "test.db"),
		},
	}

	result, err := BootstrapScrapeOnly(cfg)
	assert.Error(t, err)
	assert.Nil(t, result)
}

// --- BatchCommandResult tests ---

func TestBatchCommandResult_Fields(t *testing.T) {
	result := BatchCommandResult{
		ScanResult: &workflow.ScanAndMatchResult{
			Files: []models.FileMatchInfo{
				{Path: "/test/file.mp4", MovieID: "ABC-123"},
			},
		},
		FilePaths:    []string{"/test/file.mp4"},
		MatchedCount: 1,
		UniqueIDs:    map[string]bool{"ABC-123": true},
		Movies:       map[string]*models.Movie{"ABC-123": {}},
		SuccessCount: 1,
		FailedCount:  0,
	}

	assert.Equal(t, 1, result.MatchedCount)
	assert.Equal(t, 1, result.SuccessCount)
	assert.Equal(t, 0, result.FailedCount)
	assert.True(t, result.UniqueIDs["ABC-123"])
	assert.NotNil(t, result.Movies["ABC-123"])
}

// --- BatchCommandOptions fields ---

func TestBatchCommandOptions_EventHandler(t *testing.T) {
	handlerCalled := false
	customHandler := func(w io.Writer, event worker.JobEvent) {
		handlerCalled = true
	}

	opts := BatchCommandOptions{
		EventHandler: customHandler,
	}

	// Verify the handler can be called
	var buf bytes.Buffer
	opts.EventHandler(&buf, worker.JobEvent{
		Step:    worker.StepFailed,
		Message: "test",
	})
	assert.True(t, handlerCalled)
}

// --- defaultEventHandler additional tests ---

func TestDefaultEventHandler_FailedWithMessage(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepFailed,
		Message: "error occurred",
	}
	defaultEventHandler(&buf, event)

	output := buf.String()
	assert.Contains(t, output, "❌")
	assert.Contains(t, output, "error occurred")
}

func TestDefaultEventHandler_CompleteWithMessage(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepComplete,
		Message: "done",
	}
	defaultEventHandler(&buf, event)

	output := buf.String()
	assert.Contains(t, output, "✅")
	assert.Contains(t, output, "done")
}

func TestDefaultEventHandler_QueuedStep(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepQueued,
		Message: "queued",
		MovieID: "TEST-001",
	}
	defaultEventHandler(&buf, event)
	// Queued step should not write to buffer (goes to debug log)
	assert.Empty(t, buf.String())
}

// --- UpdateEventHandler edge cases ---

func TestUpdateEventHandler_FailedNoMessage(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepFailed,
		Message: "",
	}
	UpdateEventHandler(&buf, event)
	// Failed step with empty message still writes
	assert.Contains(t, buf.String(), "❌")
}

func TestUpdateEventHandler_ScrapeCompleteNoMovieID(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepComplete,
		Phase:   worker.JobEventPhaseScrape,
		MovieID: "",
	}
	UpdateEventHandler(&buf, event)
	output := buf.String()
	assert.Contains(t, output, "✅")
	assert.Contains(t, output, "(scraped)")
}

// --- defaultSummaryPrinter edge cases ---

func TestDefaultSummaryPrinter_ZeroFiles(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       false,
		SkipOrganize: false,
	}
	result := BatchCommandResult{
		ScanResult:   &workflow.ScanAndMatchResult{},
		MatchedCount: 0,
		SuccessCount: 0,
		FailedCount:  0,
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "Organized 0 file(s)")
}

func TestDefaultSummaryPrinter_UpdateStyleWithFailures(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       false,
		SkipOrganize: true,
	}
	result := BatchCommandResult{
		ScanResult: &workflow.ScanAndMatchResult{
			Files: []models.FileMatchInfo{
				{Path: "/test/file1.mp4", MovieID: "ABC-123"},
				{Path: "/test/file2.mp4", MovieID: "DEF-456"},
			},
		},
		MatchedCount: 2,
		SuccessCount: 1,
		FailedCount:  1,
		Movies:       map[string]*models.Movie{"ABC-123": {}},
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "Updated: 1, Failed: 1")
}

func TestDefaultSummaryPrinter_CustomCompletion(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:            false,
		SkipOrganize:      false,
		CompletionMessage: "Custom done!",
	}
	result := BatchCommandResult{
		ScanResult:   &workflow.ScanAndMatchResult{},
		MatchedCount: 0,
		SuccessCount: 0,
		FailedCount:  0,
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "✅ Custom done!")
}

func TestDefaultSummaryPrinter_NoModeLine(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       false,
		SkipOrganize: true,
		ModeLine:     "",
	}
	result := BatchCommandResult{
		ScanResult:   &workflow.ScanAndMatchResult{},
		MatchedCount: 0,
		SuccessCount: 0,
		FailedCount:  0,
		Movies:       map[string]*models.Movie{},
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.NotContains(t, output, "Mode:")
}

func TestDefaultSummaryPrinter_GenerateNFO_SkipOrganize(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       false,
		SkipOrganize: true,
		GenerateNFO:  true,
	}
	result := BatchCommandResult{
		ScanResult: &workflow.ScanAndMatchResult{
			Files: []models.FileMatchInfo{
				{Path: "/test/file.mp4", MovieID: "ABC-123"},
			},
		},
		MatchedCount: 1,
		SuccessCount: 1,
		FailedCount:  0,
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	// NFOs should be printed even with SkipOrganize
	assert.Contains(t, output, "NFOs generated: 1")
	// "Files organized" should not be printed with SkipOrganize
	assert.NotContains(t, output, "Files organized")
}

// --- CoreDeps_GetLogger with full deps ---

func TestNewDependencies_GetLogger_Default(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	logger := deps.GetLogger()
	assert.NotNil(t, logger)
}

// --- HasConfig with full deps ---

func TestNewDependencies_HasConfig_True(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: filepath.Join(t.TempDir(), "test.db"),
		},
	}

	deps, err := NewDependencies(cfg)
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	assert.True(t, deps.HasConfig())
}

// --- BatchCommandOptions Resolved field ---

func TestBatchCommandOptions_ResolvedSeamStrings(t *testing.T) {
	opts := BatchCommandOptions{
		Resolved: &workflow.ResolvedSeamStrings{
			LinkMode:       "hard",
			ScalarStrategy: "prefer-new",
			ArrayStrategy:  true,
		},
	}

	assert.Equal(t, "hard", string(opts.Resolved.LinkMode))
	assert.Equal(t, "prefer-new", string(opts.Resolved.ScalarStrategy))
	assert.True(t, opts.Resolved.ArrayStrategy)
}

// --- RunBatchCommand with cancelled context ---

func TestRunBatchCommand_CancelledContext(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	err := config.Save(cfg, configPath)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	var buf bytes.Buffer
	opts := BatchCommandOptions{
		ConfigFile: configPath,
		SourcePath: tmpDir,
		Presenter:  &SilentBatchCommandPresenter{},
	}

	err = RunBatchCommand(ctx, &buf, opts)
	// May succeed if scan completes before context is checked, or may fail
	// Either way it should not panic
	_ = err
}

// --- Full summary output verification ---

func TestDefaultSummaryPrinter_FullSortOutput(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:            false,
		SkipOrganize:      false,
		GenerateNFO:       true,
		DownloadMedia:     true,
		ModeLine:          "Sort (MOVE)",
		CompletionMessage: "Sort complete!",
	}
	result := BatchCommandResult{
		ScanResult: &workflow.ScanAndMatchResult{
			Files: []models.FileMatchInfo{
				{Path: "/test/file1.mp4", MovieID: "ABC-123"},
				{Path: "/test/file2.mp4", MovieID: "DEF-456"},
			},
		},
		MatchedCount: 2,
		SuccessCount: 2,
		FailedCount:  0,
		Movies: map[string]*models.Movie{
			"ABC-123": {},
			"DEF-456": {},
		},
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "Organized 2 file(s)")
	assert.Contains(t, output, "Files scanned: 2")
	assert.Contains(t, output, "IDs matched: 2")
	assert.Contains(t, output, "Metadata found: 2")
	assert.Contains(t, output, "NFOs generated: 2")
	assert.Contains(t, output, "Files organized: 2")
	assert.Contains(t, output, "Mode: Sort (MOVE)")
	assert.Contains(t, output, "✅ Sort complete!")
}

func TestDefaultSummaryPrinter_FullUpdateOutput(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:            false,
		SkipOrganize:      true,
		GenerateNFO:       true,
		DownloadMedia:     true,
		ModeLine:          "Update (metadata & artwork)",
		CompletionMessage: "Update complete!",
	}
	result := BatchCommandResult{
		ScanResult: &workflow.ScanAndMatchResult{
			Files: []models.FileMatchInfo{
				{Path: "/test/file1.mp4", MovieID: "ABC-123"},
			},
		},
		MatchedCount: 1,
		SuccessCount: 1,
		FailedCount:  0,
		Movies:       map[string]*models.Movie{"ABC-123": {}},
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "Updated: 1, Failed: 0")
	assert.Contains(t, output, "NFOs generated: 1")
	assert.NotContains(t, output, "Files organized")
	assert.Contains(t, output, "Mode: Update (metadata & artwork)")
	assert.Contains(t, output, "✅ Update complete!")
}

// Ensure imports are used
var _ = fmt.Sprintf
var _ = strings.Contains
