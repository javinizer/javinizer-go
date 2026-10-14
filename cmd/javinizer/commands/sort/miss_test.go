package sort_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	sortcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/sort"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_SourceIsFile_DestIsFileDir tests that when source is a single file
// (not a directory), the destination defaults to the file's parent directory.
func TestRun_SourceIsFile_DestIsFileDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a subdirectory and a video file in it
	subDir := filepath.Join(tmpDir, "videos")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	videoFile := filepath.Join(subDir, "unmatched.mp4")
	testutil.CreateTestVideoFile(t, subDir, "unmatched.mp4")

	configPath, _ := createOfflineSortConfig(t, nil)

	cmd := sortcmd.NewCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{videoFile}, configPath)
	assert.NoError(t, runErr)
	// Output should show destination as the video's directory
	assert.Contains(t, buf.String(), subDir)
}

// TestRun_InvalidLinkMode tests that an invalid link-mode value is rejected.
func TestRun_InvalidLinkMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("link-mode", "invalid"))

	runErr := sortcmd.Run(cmd, []string{tmpDir}, "")
	assert.Error(t, runErr)
}

// TestRun_MoveFlagWithNoLinkMode tests that --move without --link-mode works (copy → move).
func TestRun_MoveFlagWithNoLinkMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-001.mp4")

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("move", "true"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	assert.NoError(t, runErr)
	// Output should show MOVE operation
	assert.Contains(t, buf.String(), "MOVE")
	assertSortCompleted(t, buf.String(), tmpDir, "GOOD-001")
	assert.NoFileExists(t, filepath.Join(tmpDir, "GOOD-001.mp4"))
	assertSyntheticVideo(t, filepath.Join(tmpDir, "GOOD-001", "GOOD-001.mp4"))
}

// TestRun_LinkModeSoft tests soft link mode display.
func TestRun_LinkModeSoft(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("link-mode", "soft"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	require.NoError(t, sortcmd.Run(cmd, []string{tmpDir}, configPath))
	assert.Contains(t, buf.String(), "SOFTLINK")
}

// TestRun_LinkModeHard tests hard link mode display.
func TestRun_LinkModeHard(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("link-mode", "hard"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	require.NoError(t, sortcmd.Run(cmd, []string{tmpDir}, configPath))
	assert.Contains(t, buf.String(), "HARDLINK")
}

// TestRun_DryRunOutputFormat tests that dry-run mode shows the correct output labels.
func TestRun_DryRunOutputFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-456.mp4")

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("dry-run", "true"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	assert.NoError(t, runErr)
	// Dry run mode should show "DRY RUN"
	assert.Contains(t, buf.String(), "DRY RUN")
	assert.Contains(t, buf.String(), "Would organize 1 file(s)")
	assert.NoDirExists(t, filepath.Join(tmpDir, "GOOD-456"))
}

// TestRun_NoMatchingIDs tests the branch where scanned files have no matching IDs.
func TestRun_NoMatchingIDs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	// Create a video file with a name that won't match any JAV ID pattern
	videoFile := filepath.Join(tmpDir, "random_video.mp4")
	require.NoError(t, os.WriteFile(videoFile, []byte("fake video data"), 0644))

	cmd := sortcmd.NewCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	// When no IDs match, the function returns nil (no error)
	assert.NoError(t, runErr)
	assert.FileExists(t, videoFile)
	assert.NotContains(t, buf.String(), "Processing files")
}

// TestRun_ExtrafanartOverride tests that --extrafanart flag overrides config.
func TestRun_ExtrafanartOverride(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, imageData := createLocalMediaSortConfig(t)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-789.mp4")

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("extrafanart", "true"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	require.NoError(t, runErr)
	assertSortCompleted(t, buf.String(), tmpDir, "GOOD-789")
	downloaded, err := os.ReadFile(filepath.Join(tmpDir, "GOOD-789", "extrafanart", "fanart1.jpg"))
	require.NoError(t, err)
	assert.Equal(t, imageData, downloaded)
}

// TestRun_ExplicitDestPath tests providing an explicit --dest flag.
func TestRun_ExplicitDestPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	destDir := filepath.Join(tmpDir, "output")
	require.NoError(t, os.MkdirAll(destDir, 0755))

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-100.mp4")

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("dest", destDir))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	assert.NoError(t, runErr)
	assert.Contains(t, buf.String(), destDir)
	assertSortCompleted(t, buf.String(), destDir, "GOOD-100")
	assert.NoDirExists(t, filepath.Join(tmpDir, "GOOD-100"))
}

// TestRun_CopyOperationLabel tests that COPY is shown when no move/link flags set.
func TestRun_CopyOperationLabel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-200.mp4")

	cmd := sortcmd.NewCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	assert.NoError(t, runErr)
	assert.Contains(t, buf.String(), "COPY")
	assertSortCompleted(t, buf.String(), tmpDir, "GOOD-200")
	assertSyntheticVideo(t, filepath.Join(tmpDir, "GOOD-200.mp4"))
	assertSyntheticVideo(t, filepath.Join(tmpDir, "GOOD-200", "GOOD-200.mp4"))
}

// TestRun_NoVideoFiles tests when scan returns no files at all.
func TestRun_NoVideoFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)

	cmd := sortcmd.NewCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	assert.NoError(t, runErr)
	assert.Contains(t, buf.String(), "No files to process")
}

// TestRun_NilContextFallback tests that Run works when cmd.Context() returns nil.
func TestRun_NilContextFallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-999.mp4")

	// Create a bare command with nil context (not executed via Execute())
	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	// Set required flags
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().Bool("recursive", true, "")
	cmd.Flags().String("dest", "", "")
	cmd.Flags().Bool("move", false, "")
	cmd.Flags().String("link-mode", "none", "")
	cmd.Flags().Bool("nfo", true, "")
	cmd.Flags().Bool("download", true, "")
	cmd.Flags().Bool("extrafanart", false, "")
	cmd.Flags().StringSlice("scrapers", nil, "")
	cmd.Flags().Bool("force-update", false, "")
	cmd.Flags().Bool("force-refresh", false, "")

	// This should not panic even with nil context
	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	require.NoError(t, runErr)
	assertSortCompleted(t, buf.String(), tmpDir, "GOOD-999")
}

// TestRun_ForceUpdateAndForceRefresh tests both force flags together.
func TestRun_ForceUpdateAndForceRefresh(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-300.mp4")

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("force-update", "true"))
	require.NoError(t, cmd.Flags().Set("force-refresh", "true"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	require.NoError(t, runErr)
	assertSortCompleted(t, buf.String(), tmpDir, "GOOD-300")
}

// TestRun_PrintConfigOutput tests that the configuration summary is printed.
func TestRun_PrintConfigOutput(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-111.mp4")

	cmd := sortcmd.NewCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	require.NoError(t, runErr)
	assertSortCompleted(t, buf.String(), tmpDir, "GOOD-111")

	output := buf.String()
	assert.Contains(t, output, "=== Javinizer Sort ===")
	assert.Contains(t, output, "Source:")
	assert.Contains(t, output, "Destination:")
	assert.Contains(t, output, "Generate NFO:")
	assert.Contains(t, output, "Download Media:")
}

// TestRun_ScraperPriorityFlag tests --scrapers flag.
func TestRun_ScraperPriorityFlag(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-333.mp4")

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("scrapers", "libredmm"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	require.NoError(t, runErr)
	assertSortCompleted(t, buf.String(), tmpDir, "GOOD-333")
}

// TestRun_NoNfoNoDownload tests with --nfo=false --download=false.
func TestRun_NoNfoNoDownload(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createLocalMediaSortConfig(t)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-444.mp4")

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("nfo", "false"))
	require.NoError(t, cmd.Flags().Set("download", "false"))
	require.NoError(t, cmd.Flags().Set("extrafanart", "true"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	assert.NoError(t, runErr)
	// Summary should not show NFO count
	output := buf.String()
	assertSortCompleted(t, output, tmpDir, "GOOD-444")
	assert.NotContains(t, output, "NFOs generated:")
	assert.NoFileExists(t, filepath.Join(tmpDir, "GOOD-444", "GOOD-444.nfo"))
	assert.NoDirExists(t, filepath.Join(tmpDir, "GOOD-444", "extrafanart"))
}

// TestRun_InvalidConfig tests the error path for invalid configuration.
func TestRun_InvalidConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cmd := sortcmd.NewCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	configPath := testutil.UnreachableConfigPath(t)
	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	assert.Error(t, runErr)
	assert.Contains(t, runErr.Error(), "failed to load config")
}

// TestRun_LinkModeNoneWithMove tests that --link-mode=none permits --move.
func TestRun_LinkModeNoneWithMove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("move", "true"))
	require.NoError(t, cmd.Flags().Set("link-mode", "none"))

	// The default link mode does not conflict with --move.
	require.NoError(t, sortcmd.Run(cmd, []string{tmpDir}, configPath))
}

// TestRun_NonNoneLinkModeWithMoveRejected tests that non-none link-mode with --move is rejected.
func TestRun_NonNoneLinkModeWithMoveRejected(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("move", "true"))
	require.NoError(t, cmd.Flags().Set("link-mode", "soft"))

	runErr := sortcmd.Run(cmd, []string{tmpDir}, "")
	assert.Error(t, runErr)
	assert.Contains(t, runErr.Error(), "--link-mode can only be used when --move is disabled")
}

// TestRun_InvalidPrepare tests the Prepare() validation error path.
func TestRun_InvalidPrepare(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a config with an invalid combination
	configPath, _ := createOfflineSortConfig(t, func(c *config.Config) {
		// Use an out-of-range value that normalization does not default.
		c.Performance.MaxWorkers = 101
	})

	cmd := sortcmd.NewCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err = sortcmd.Run(cmd, []string{tmpDir}, configPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
	assert.Contains(t, err.Error(), "performance.max_workers")
}

// TestRun_DryRunWouldOrganize tests dry-run message about "Would organize".
func TestRun_DryRunWouldOrganize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-555.mp4")

	cmd := sortcmd.NewCommand()
	require.NoError(t, cmd.Flags().Set("dry-run", "true"))
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	assert.NoError(t, runErr)

	output := buf.String()
	assert.Contains(t, output, "Would organize 1 file(s)")
	assert.Contains(t, output, "Metadata found: 1")
	assert.NoDirExists(t, filepath.Join(tmpDir, "GOOD-555"))
}

// TestRun_SortCompleteMessage tests that the sort complete message is shown in live mode.
func TestRun_SortCompleteMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-777.mp4")

	cmd := sortcmd.NewCommand()
	// Not dry-run (live mode)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	runErr := sortcmd.Run(cmd, []string{tmpDir}, configPath)
	require.NoError(t, runErr)
	assertSortCompleted(t, buf.String(), tmpDir, "GOOD-777")
	nfo, err := os.ReadFile(filepath.Join(tmpDir, "GOOD-777", "GOOD-777.nfo"))
	require.NoError(t, err)
	assert.Contains(t, string(nfo), "Synthetic sort fixture GOOD-777")
}
