package sort_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/sort"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCommand verifies the command is created with correct structure
func TestNewCommand(t *testing.T) {
	cmd := sort.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "sort", cmd.Use[:4])
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify flags are registered
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("recursive"))
	assert.NotNil(t, cmd.Flags().Lookup("dest"))
	assert.NotNil(t, cmd.Flags().Lookup("move"))
	assert.NotNil(t, cmd.Flags().Lookup("link-mode"))
	assert.NotNil(t, cmd.Flags().Lookup("nfo"))
	assert.NotNil(t, cmd.Flags().Lookup("download"))
	assert.NotNil(t, cmd.Flags().Lookup("extrafanart"))
	assert.NotNil(t, cmd.Flags().Lookup("scrapers"))
	assert.NotNil(t, cmd.Flags().Lookup("force-update"))
	assert.NotNil(t, cmd.Flags().Lookup("force-refresh"))
}

// TestFlags_DefaultValues verifies default flag values
func TestFlags_DefaultValues(t *testing.T) {
	cmd := sort.NewCommand()

	// Check default values
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	assert.False(t, dryRun, "dry-run should default to false")

	recursive, _ := cmd.Flags().GetBool("recursive")
	assert.True(t, recursive, "recursive should default to true")

	move, _ := cmd.Flags().GetBool("move")
	assert.False(t, move, "move should default to false (copy mode)")

	linkMode, _ := cmd.Flags().GetString("link-mode")
	assert.Equal(t, "none", linkMode, "link-mode should default to none")

	nfo, _ := cmd.Flags().GetBool("nfo")
	assert.True(t, nfo, "nfo should default to true")

	download, _ := cmd.Flags().GetBool("download")
	assert.True(t, download, "download should default to true")

	extrafanart, _ := cmd.Flags().GetBool("extrafanart")
	assert.False(t, extrafanart, "extrafanart should default to false")

	forceUpdate, _ := cmd.Flags().GetBool("force-update")
	assert.False(t, forceUpdate, "force-update should default to false")

	forceRefresh, _ := cmd.Flags().GetBool("force-refresh")
	assert.False(t, forceRefresh, "force-refresh should default to false")
}

// TestFlags_ShortForms verifies short flag forms work
func TestFlags_ShortForms(t *testing.T) {
	cmd := sort.NewCommand()

	// Verify short forms are registered
	assert.NotNil(t, cmd.Flags().ShorthandLookup("n"), "should have -n for dry-run")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("r"), "should have -r for recursive")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("d"), "should have -d for dest")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("m"), "should have -m for move")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("p"), "should have -p for scrapers")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("f"), "should have -f for force-update")
}

// Integration tests

func TestRun_Integration_NoVideoFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)

	// Create a non-video file
	textFile := filepath.Join(tmpDir, "readme.txt")
	require.NoError(t, os.WriteFile(textFile, []byte("not a video"), 0644))

	cmd := sort.NewCommand()
	runErr := sort.Run(cmd, []string{tmpDir}, configPath)

	assert.NoError(t, runErr)
}

// TestRun_Integration_InvalidPath tests error handling for invalid paths
func TestRun_Integration_InvalidPath(t *testing.T) {
	configPath, _ := createOfflineSortConfig(t, nil)

	cmd := sort.NewCommand()
	err := sort.Run(cmd, []string{"/nonexistent/path/that/does/not/exist"}, configPath)

	// Should return error for invalid path
	assert.Error(t, err)
}

func TestRun_Integration_InvalidConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cmd := sort.NewCommand()
	configPath := testutil.UnreachableConfigPath(t)
	runErr := sort.Run(cmd, []string{tmpDir}, configPath)

	assert.Error(t, runErr)
	assert.Contains(t, runErr.Error(), "failed to load config")
}

func TestRun_LinkModeWithMoveRejected(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cmd := sort.NewCommand()
	require.NoError(t, cmd.Flags().Set("move", "true"))
	require.NoError(t, cmd.Flags().Set("link-mode", "hard"))

	runErr := sort.Run(cmd, []string{tmpDir}, "")
	assert.Error(t, runErr)
	assert.Contains(t, runErr.Error(), "--link-mode can only be used when --move is disabled")
}

func TestRun_Integration_DryRunMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, _ := createOfflineSortConfig(t, nil)
	videoFile := testutil.CreateTestVideoFile(t, tmpDir, "GOOD-123.mp4")

	cmd := sort.NewCommand()
	require.NoError(t, cmd.Flags().Set("dry-run", "true"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	runErr := sort.Run(cmd, []string{tmpDir}, configPath)

	assert.NoError(t, runErr)
	// File should still exist in original location (dry-run doesn't move)
	assertSyntheticVideo(t, videoFile)
	assert.NoDirExists(t, filepath.Join(tmpDir, "GOOD-123"))
	assert.Contains(t, output.String(), "Would organize 1 file(s)")
}

func TestRun_Integration_FlagOverrides(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath, imageData := createLocalMediaSortConfig(t)
	testutil.CreateTestVideoFile(t, tmpDir, "GOOD-123.mp4")

	cmd := sort.NewCommand()
	require.NoError(t, cmd.Flags().Set("extrafanart", "true"))
	require.NoError(t, cmd.Flags().Set("scrapers", "libredmm"))
	var output bytes.Buffer
	cmd.SetOut(&output)
	err = sort.Run(cmd, []string{tmpDir}, configPath)

	require.NoError(t, err)
	assertSortCompleted(t, output.String(), tmpDir, "GOOD-123")
	downloaded, err := os.ReadFile(filepath.Join(tmpDir, "GOOD-123", "extrafanart", "fanart1.jpg"))
	require.NoError(t, err)
	assert.Equal(t, imageData, downloaded)
}
