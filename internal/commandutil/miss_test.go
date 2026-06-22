package commandutil

import (
	"bytes"
	"io"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- defaultBatchCommandPresenter tests ---

func TestDefaultBatchCommandPresenter_OnHeader(t *testing.T) {
	p := &defaultBatchCommandPresenter{}
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		CommandLabel:   "Javinizer Sort",
		SourcePath:     "/source/path",
		Destination:    "/dest/path",
		DryRun:         false,
		OperationLabel: "COPY",
		GenerateNFO:    true,
		DownloadMedia:  true,
	}
	p.OnHeader(&buf, opts)

	output := buf.String()
	assert.Contains(t, output, "=== Javinizer Sort ===")
	assert.Contains(t, output, "Source: /source/path")
	assert.Contains(t, output, "Destination: /dest/path")
	assert.Contains(t, output, "Mode: LIVE")
	assert.Contains(t, output, "Operation: COPY")
	assert.Contains(t, output, "Generate NFO: true")
	assert.Contains(t, output, "Download Media: true")
}

func TestDefaultBatchCommandPresenter_OnHeader_DryRun(t *testing.T) {
	p := &defaultBatchCommandPresenter{}
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		CommandLabel:  "Javinizer Update",
		SourcePath:    "/src",
		Destination:   "/dst",
		DryRun:        true,
		DownloadMedia: false,
	}
	p.OnHeader(&buf, opts)

	output := buf.String()
	assert.Contains(t, output, "Mode: DRY RUN")
	assert.NotContains(t, output, "Operation:")
	assert.NotContains(t, output, "Generate NFO:")
}

func TestDefaultBatchCommandPresenter_OnHeader_NoOperationLabel(t *testing.T) {
	p := &defaultBatchCommandPresenter{}
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		CommandLabel:  "Javinizer Sort",
		SourcePath:    "/src",
		Destination:   "/dst",
		DownloadMedia: false,
	}
	p.OnHeader(&buf, opts)

	output := buf.String()
	assert.NotContains(t, output, "Operation:")
}

func TestDefaultBatchCommandPresenter_OnHeader_NoGenerateNFO(t *testing.T) {
	p := &defaultBatchCommandPresenter{}
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		CommandLabel:  "Javinizer Sort",
		SourcePath:    "/src",
		Destination:   "/dst",
		GenerateNFO:   false,
		DownloadMedia: true,
	}
	p.OnHeader(&buf, opts)

	output := buf.String()
	assert.NotContains(t, output, "Generate NFO:")
}

func TestDefaultBatchCommandPresenter_OnScanStart(t *testing.T) {
	p := &defaultBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnScanStart(&buf)

	output := buf.String()
	assert.Contains(t, output, "Scanning for video files")
}

func TestDefaultBatchCommandPresenter_OnNoFiles(t *testing.T) {
	p := &defaultBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnNoFiles(&buf)

	output := buf.String()
	assert.Contains(t, output, "No files to process")
}

func TestDefaultBatchCommandPresenter_OnProcessingStart(t *testing.T) {
	p := &defaultBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnProcessingStart(&buf, "Processing files")

	output := buf.String()
	assert.Contains(t, output, "Processing files")
}

func TestDefaultBatchCommandPresenter_OnSummary(t *testing.T) {
	p := &defaultBatchCommandPresenter{}
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		CompletionMessage: "Sort complete!",
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
		Movies:       map[string]*models.Movie{"ABC-123": {}},
	}
	p.OnSummary(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "=== Summary ===")
	assert.Contains(t, output, "Sort complete!")
}

// --- SilentBatchCommandPresenter tests ---

func TestSilentBatchCommandPresenter_OnHeader(t *testing.T) {
	p := &SilentBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnHeader(&buf, BatchCommandOptions{})
	assert.Empty(t, buf.String())
}

func TestSilentBatchCommandPresenter_OnScanStart(t *testing.T) {
	p := &SilentBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnScanStart(&buf)
	assert.Empty(t, buf.String())
}

func TestSilentBatchCommandPresenter_OnNoFiles(t *testing.T) {
	p := &SilentBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnNoFiles(&buf)
	assert.Empty(t, buf.String())
}

func TestSilentBatchCommandPresenter_OnProcessingStart(t *testing.T) {
	p := &SilentBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnProcessingStart(&buf, "test")
	assert.Empty(t, buf.String())
}

func TestSilentBatchCommandPresenter_OnSummary(t *testing.T) {
	p := &SilentBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnSummary(&buf, BatchCommandOptions{}, BatchCommandResult{})
	assert.Empty(t, buf.String())
}

// --- defaultEventHandler tests ---

func TestDefaultEventHandler_StepFailed(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepFailed,
		Message: "file failed to process",
	}
	defaultEventHandler(&buf, event)

	output := buf.String()
	assert.Contains(t, output, "❌")
	assert.Contains(t, output, "file failed to process")
}

func TestDefaultEventHandler_StepComplete(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepComplete,
		Message: "file processed successfully",
	}
	defaultEventHandler(&buf, event)

	output := buf.String()
	assert.Contains(t, output, "✅")
	assert.Contains(t, output, "file processed successfully")
}

func TestDefaultEventHandler_OtherStep(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepScrape,
		Message: "scraping metadata",
		MovieID: "ABC-123",
	}
	// Should not write to writer for non-failed/non-complete steps
	// (logs via logging.Debugf instead)
	defaultEventHandler(&buf, event)
	assert.Empty(t, buf.String())
}

func TestDefaultEventHandler_EmptyMessage(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepFailed,
		Message: "",
	}
	// Empty message — defaultEventHandler still writes since it checks step type
	defaultEventHandler(&buf, event)
	// The handler writes regardless of empty message;
	// RunBatchCommand filters empty messages before calling the handler
}

// --- UpdateEventHandler tests ---

func TestUpdateEventHandler_StepFailed(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepFailed,
		Message: "update failed",
	}
	UpdateEventHandler(&buf, event)

	output := buf.String()
	assert.Contains(t, output, "❌")
	assert.Contains(t, output, "update failed")
}

func TestUpdateEventHandler_ScrapePhaseComplete(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepComplete,
		Phase:   worker.JobEventPhaseScrape,
		MovieID: "ABC-123",
	}
	UpdateEventHandler(&buf, event)

	output := buf.String()
	assert.Contains(t, output, "ABC-123")
	assert.Contains(t, output, "✅")
	assert.Contains(t, output, "(scraped)")
}

func TestUpdateEventHandler_NonScrapePhaseComplete(t *testing.T) {
	var buf bytes.Buffer
	event := worker.JobEvent{
		Step:    worker.StepComplete,
		Phase:   "apply",
		MovieID: "ABC-123",
	}
	UpdateEventHandler(&buf, event)

	// Non-scrape phase completion should produce no output
	assert.Empty(t, buf.String())
}

// --- defaultSummaryPrinter tests ---

func TestDefaultSummaryPrinter_SortStyle_DryRun(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:            true,
		SkipOrganize:      false,
		GenerateNFO:       true,
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
	assert.Contains(t, output, "Would organize 2 file(s)")
	assert.Contains(t, output, "NFOs generated: 2 (dry-run)")
	assert.Contains(t, output, "Files organized: 2 (dry-run)")
	assert.Contains(t, output, "Run without --dry-run to apply changes")
}

func TestDefaultSummaryPrinter_SortStyle_Live(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:            false,
		SkipOrganize:      false,
		GenerateNFO:       false,
		CompletionMessage: "Sort complete!",
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
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "Organized 1 file(s)")
	assert.Contains(t, output, "✅ Sort complete!")
	assert.NotContains(t, output, "NFOs generated")
}

func TestDefaultSummaryPrinter_UpdateStyle(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       false,
		SkipOrganize: true,
		ModeLine:     "Update (metadata & artwork)",
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
	assert.Contains(t, output, "Mode: Update (metadata & artwork)")
	assert.Contains(t, output, "✅ Complete!")
	assert.NotContains(t, output, "Files organized")
}

func TestDefaultSummaryPrinter_DefaultCompletion(t *testing.T) {
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
	assert.Contains(t, output, "✅ Complete!")
}

func TestDefaultSummaryPrinter_WithFailures(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       false,
		SkipOrganize: false,
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
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "Organized 1 file(s)")
	assert.Contains(t, output, "IDs matched: 2")
	assert.Contains(t, output, "Metadata found: 1")
}

func TestDefaultSummaryPrinter_NFODryRun(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       true,
		SkipOrganize: false,
		GenerateNFO:  true,
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
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "NFOs generated: 1 (dry-run)")
	assert.Contains(t, output, "Files organized: 1 (dry-run)")
}

func TestDefaultSummaryPrinter_NFOLive(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       false,
		SkipOrganize: false,
		GenerateNFO:  true,
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
	}
	defaultSummaryPrinter(&buf, opts, result)

	output := buf.String()
	assert.Contains(t, output, "NFOs generated: 1")
	assert.NotContains(t, output, "NFOs generated: 1 (dry-run)")
}

// --- GetLogger tests ---

func TestCoreDeps_GetLogger_Injected(t *testing.T) {
	deps := &CoreDeps{
		Logger: logging.GlobalLogger(),
	}
	logger := deps.GetLogger()
	assert.NotNil(t, logger)
}

func TestCoreDeps_GetLogger_NilFallsBackToGlobal(t *testing.T) {
	deps := &CoreDeps{}
	logger := deps.GetLogger()
	assert.NotNil(t, logger)
	// Should be the global logger since no Logger was injected
	assert.Equal(t, logging.GlobalLogger(), logger)
}

// --- HasConfig tests ---

func TestCoreDeps_HasConfig_True(t *testing.T) {
	deps := &CoreDeps{}
	deps.config.Store(&config.Config{})
	assert.True(t, deps.HasConfig())
}

func TestCoreDeps_HasConfig_False(t *testing.T) {
	deps := &CoreDeps{}
	assert.False(t, deps.HasConfig())
}

// --- NewDependenciesWithOptions with injected logger ---

func TestNewDependenciesWithOptions_InjectedLogger(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			DSN: ":memory:",
		},
	}

	mockDB, err := createInMemoryDBForMiss()
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

	assert.Equal(t, injectedLogger, deps.Logger, "Injected logger should be stored")
	gotLogger := deps.GetLogger()
	assert.Equal(t, injectedLogger, gotLogger, "GetLogger should return the injected logger")
}

// --- SummaryPrinter backward compatibility test ---

func TestSummaryPrinter_BackwardCompat(t *testing.T) {
	var buf bytes.Buffer
	customPrinterCalled := false
	customSummaryPrinter := func(w io.Writer, opts BatchCommandOptions, result BatchCommandResult) {
		customPrinterCalled = true
		w.Write([]byte("custom summary\n"))
	}

	opts := BatchCommandOptions{
		SummaryPrinter: customSummaryPrinter,
		DryRun:         false,
		SkipOrganize:   false,
	}
	result := BatchCommandResult{
		ScanResult:   &workflow.ScanAndMatchResult{},
		MatchedCount: 0,
		SuccessCount: 0,
		FailedCount:  0,
	}

	// Call the custom summary printer directly
	opts.SummaryPrinter(&buf, opts, result)
	assert.True(t, customPrinterCalled)
	assert.Contains(t, buf.String(), "custom summary")
}

// --- ModeLine in summary ---

func TestDefaultSummaryPrinter_ModeLine(t *testing.T) {
	var buf bytes.Buffer
	opts := BatchCommandOptions{
		DryRun:       false,
		SkipOrganize: true,
		ModeLine:     "Update (metadata & artwork, files remain in place)",
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
	assert.Contains(t, output, "Mode: Update (metadata & artwork, files remain in place)")
}

// helper to create an in-memory DB for tests
func createInMemoryDBForMiss() (*database.DB, error) {
	return database.New(&database.Config{DSN: ":memory:"})
}
