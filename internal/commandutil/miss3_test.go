package commandutil

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- RunBatchCommand integration tests ---

func TestRunBatchCommand_WithVideoFile_FullPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full-pipeline integration test in short mode")
	}
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a video file with a JAV ID pattern in the name
	videoFile := filepath.Join(tmpDir, "ABC-123.mp4")
	err := os.WriteFile(videoFile, []byte("fake video content"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	fixture := testutil.ConfigureLocalScraper(t, cfg, false)
	cfg.Database.DSN = dbPath
	cfg.Matching.Extensions = []string{".mp4", ".mkv", ".avi", ".wmv"}
	cfg.Matching.MinSizeMB = 0
	err = config.Save(cfg, configPath)
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
		Presenter:         nil, // use default presenter
		Resolved:          &workflow.ResolvedSeamStrings{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	require.NoError(t, err)
	assert.Equal(t, []string{"ABC-123"}, fixture.SearchIDs())
	assert.Contains(t, buf.String(), "Metadata found: 1")
	assert.Contains(t, buf.String(), "Would organize 1 file(s)")
	assert.FileExists(t, videoFile)
}

func TestRunBatchCommand_WithVideoFile_SilentPresenter(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a video file with a JAV ID pattern in the name
	videoFile := filepath.Join(tmpDir, "DEF-456.mp4")
	err := os.WriteFile(videoFile, []byte("fake video content"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	fixture := testutil.ConfigureLocalScraper(t, cfg, false)
	cfg.Database.DSN = dbPath
	cfg.Matching.Extensions = []string{".mp4", ".mkv", ".avi", ".wmv"}
	cfg.Matching.MinSizeMB = 0
	err = config.Save(cfg, configPath)
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
		Resolved:          &workflow.ResolvedSeamStrings{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	require.NoError(t, err)
	assert.Equal(t, []string{"DEF-456"}, fixture.SearchIDs())
	assert.FileExists(t, videoFile)
}

func TestRunBatchCommand_WithVideoFile_CustomEventHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full-pipeline integration test in short mode")
	}
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a video file with a JAV ID pattern in the name
	videoFile := filepath.Join(tmpDir, "GHI-789.mp4")
	err := os.WriteFile(videoFile, []byte("fake video content"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	fixture := testutil.ConfigureLocalScraper(t, cfg, false)
	cfg.Database.DSN = dbPath
	cfg.Matching.Extensions = []string{".mp4", ".mkv", ".avi", ".wmv"}
	cfg.Matching.MinSizeMB = 0
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	eventMessages := []string{}
	customHandler := func(w io.Writer, event worker.JobEvent) {
		if event.Message != "" {
			eventMessages = append(eventMessages, event.Message)
		}
	}

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
		EventHandler:      customHandler,
		Resolved:          &workflow.ResolvedSeamStrings{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	require.NoError(t, err)
	assert.Equal(t, []string{"GHI-789"}, fixture.SearchIDs())
	assert.NotEmpty(t, eventMessages, "the custom handler must receive pipeline events")
}

func TestRunBatchCommand_WithVideoFile_CustomSummaryPrinter(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create a video file with a JAV ID pattern
	videoFile := filepath.Join(tmpDir, "JKL-012.mp4")
	err := os.WriteFile(videoFile, []byte("fake video content"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	fixture := testutil.ConfigureLocalScraper(t, cfg, false)
	cfg.Database.DSN = dbPath
	cfg.Matching.Extensions = []string{".mp4", ".mkv", ".avi", ".wmv"}
	cfg.Matching.MinSizeMB = 0
	err = config.Save(cfg, configPath)
	require.NoError(t, err)

	var buf bytes.Buffer
	summaryCalled := false
	summaryPrinter := func(w io.Writer, o BatchCommandOptions, r BatchCommandResult) {
		summaryCalled = true
		assert.Equal(t, 1, r.SuccessCount)
		assert.Zero(t, r.FailedCount)
		assert.Len(t, r.Movies, 1)
	}

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
		SummaryPrinter:    summaryPrinter,
		Resolved:          &workflow.ResolvedSeamStrings{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	require.NoError(t, err)
	assert.True(t, summaryCalled)
	assert.Equal(t, []string{"JKL-012"}, fixture.SearchIDs())
}

func TestRunBatchCommand_WithVideoFile_RecursiveSearch(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Create a subdirectory with a video file, but scan non-recursive
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)
	videoFile := filepath.Join(subDir, "MNO-345.mp4")
	err = os.WriteFile(videoFile, []byte("fake"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	fixture := testutil.ConfigureLocalScraper(t, cfg, false)
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
	assert.NoError(t, err)
	assert.Empty(t, fixture.SearchIDs(), "non-recursive scan must not scrape the nested video")
}

func TestRunBatchCommand_Recursive_FindsFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full-pipeline integration test in short mode")
	}
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Create a subdirectory with a video file
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)
	videoFile := filepath.Join(subDir, "PQR-678.mp4")
	err = os.WriteFile(videoFile, []byte("fake video content"), 0644)
	require.NoError(t, err)

	cfg := config.DefaultConfig(nil, nil)
	fixture := testutil.ConfigureLocalScraper(t, cfg, false)
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
		Recursive:   true,
		DryRun:      true,
		Presenter:   &SilentBatchCommandPresenter{},
		Resolved:    &workflow.ResolvedSeamStrings{},
	}

	err = RunBatchCommand(context.Background(), &buf, opts)
	require.NoError(t, err)
	assert.Equal(t, []string{"PQR-678"}, fixture.SearchIDs())
	assert.FileExists(t, videoFile)
}
