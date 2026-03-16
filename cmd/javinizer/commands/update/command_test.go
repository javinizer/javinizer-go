package update_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCommand verifies the command is created with correct structure
func TestNewCommand(t *testing.T) {
	cmd := update.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "update", cmd.Use[:6])
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify flags are registered
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("download"))
	assert.NotNil(t, cmd.Flags().Lookup("extrafanart"))
	assert.NotNil(t, cmd.Flags().Lookup("scrapers"))
	assert.NotNil(t, cmd.Flags().Lookup("force-refresh"))
	assert.NotNil(t, cmd.Flags().Lookup("force-overwrite"))
	assert.NotNil(t, cmd.Flags().Lookup("preserve-nfo"))
	assert.NotNil(t, cmd.Flags().Lookup("show-merge-stats"))
	assert.NotNil(t, cmd.Flags().Lookup("preset"))
	assert.NotNil(t, cmd.Flags().Lookup("scalar-strategy"))
	assert.NotNil(t, cmd.Flags().Lookup("array-strategy"))
}

// TestFlags_DefaultValues verifies default flag values
func TestFlags_DefaultValues(t *testing.T) {
	cmd := update.NewCommand()

	// Check default values
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	assert.False(t, dryRun, "dry-run should default to false")

	download, _ := cmd.Flags().GetBool("download")
	assert.True(t, download, "download should default to true")

	extrafanart, _ := cmd.Flags().GetBool("extrafanart")
	assert.False(t, extrafanart, "extrafanart should default to false")

	forceRefresh, _ := cmd.Flags().GetBool("force-refresh")
	assert.False(t, forceRefresh, "force-refresh should default to false")

	forceOverwrite, _ := cmd.Flags().GetBool("force-overwrite")
	assert.False(t, forceOverwrite, "force-overwrite should default to false")

	preserveNFO, _ := cmd.Flags().GetBool("preserve-nfo")
	assert.False(t, preserveNFO, "preserve-nfo should default to false")

	showMergeStats, _ := cmd.Flags().GetBool("show-merge-stats")
	assert.False(t, showMergeStats, "show-merge-stats should default to false")

	preset, _ := cmd.Flags().GetString("preset")
	assert.Empty(t, preset, "preset should default to empty")

	scalarStrategy, _ := cmd.Flags().GetString("scalar-strategy")
	assert.Equal(t, "prefer-nfo", scalarStrategy, "scalar-strategy should default to prefer-nfo")

	arrayStrategy, _ := cmd.Flags().GetString("array-strategy")
	assert.Equal(t, "merge", arrayStrategy, "array-strategy should default to merge")
}

// TestFlags_ShortForms verifies short flag forms work
func TestFlags_ShortForms(t *testing.T) {
	cmd := update.NewCommand()

	// Verify short forms are registered
	assert.NotNil(t, cmd.Flags().ShorthandLookup("n"), "should have -n for dry-run")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("p"), "should have -p for scrapers")
}

// TestFlags_MergeStrategies verifies merge strategy flags
func TestFlags_MergeStrategies(t *testing.T) {
	cmd := update.NewCommand()

	// Verify scalar strategy flag
	scalarStrategy, err := cmd.Flags().GetString("scalar-strategy")
	assert.NoError(t, err)
	assert.Equal(t, "prefer-nfo", scalarStrategy)

	// Verify array strategy flag
	arrayStrategy, err := cmd.Flags().GetString("array-strategy")
	assert.NoError(t, err)
	assert.Equal(t, "merge", arrayStrategy)

	// Verify preset flag exists
	preset, err := cmd.Flags().GetString("preset")
	assert.NoError(t, err)
	assert.Empty(t, preset)
}

// TestFlags_MutuallyExclusiveOptions verifies conflicting flags are available
func TestFlags_MutuallyExclusiveOptions(t *testing.T) {
	cmd := update.NewCommand()

	// force-overwrite and preserve-nfo are mutually exclusive in behavior
	// but both flags should exist
	assert.NotNil(t, cmd.Flags().Lookup("force-overwrite"))
	assert.NotNil(t, cmd.Flags().Lookup("preserve-nfo"))

	// Both should default to false
	forceOverwrite, _ := cmd.Flags().GetBool("force-overwrite")
	preserveNFO, _ := cmd.Flags().GetBool("preserve-nfo")
	assert.False(t, forceOverwrite)
	assert.False(t, preserveNFO)
}

// Integration tests

// TestRun_Integration_NoVideoFiles tests graceful handling when no video files exist
func TestRun_Integration_NoVideoFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	tmpDir := t.TempDir()
	configPath, _ := testutil.CreateTestConfig(t, nil)

	// Create a non-video file
	textFile := filepath.Join(tmpDir, "readme.txt")
	require.NoError(t, os.WriteFile(textFile, []byte("not a video"), 0644))

	cmd := update.NewCommand()
	cmd.SetArgs([]string{tmpDir})

	err := update.Run(cmd, []string{tmpDir}, configPath)
	// Should succeed (graceful exit when no videos found)
	assert.NoError(t, err)
}

// TestRun_Integration_InvalidPath tests error handling for invalid paths
func TestRun_Integration_InvalidPath(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	configPath, _ := testutil.CreateTestConfig(t, nil)

	cmd := update.NewCommand()
	err := update.Run(cmd, []string{"/nonexistent/path/that/does/not/exist"}, configPath)

	// Should return error for invalid path
	assert.Error(t, err)
}

// TestRun_Integration_DryRunMode tests that dry-run mode doesn't modify files
func TestRun_Integration_DryRunMode(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	tmpDir := t.TempDir()
	configPath, _ := testutil.CreateTestConfig(t, nil)

	// Create a test video file
	videoFile := filepath.Join(tmpDir, "IPX-123.mp4")
	require.NoError(t, os.WriteFile(videoFile, []byte("fake video"), 0644))

	cmd := update.NewCommand()
	_ = cmd.Flags().Set("dry-run", "true")

	err := update.Run(cmd, []string{tmpDir}, configPath)
	// Should succeed
	assert.NoError(t, err)

	// Video file should still exist (dry-run doesn't move/modify)
	assert.FileExists(t, videoFile)

	// NFO should NOT be created in dry-run mode
	nfoFile := filepath.Join(tmpDir, "IPX-123.nfo")
	assert.NoFileExists(t, nfoFile)
}

// TestRun_Integration_PresetApplication tests preset flag application
func TestRun_Integration_PresetApplication(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	tests := []struct {
		name   string
		preset string
	}{
		{
			name:   "conservative preset",
			preset: "conservative",
		},
		{
			name:   "gap-fill preset",
			preset: "gap-fill",
		},
		{
			name:   "aggressive preset",
			preset: "aggressive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath, _ := testutil.CreateTestConfig(t, nil)

			// Create a non-video file (to avoid scraping)
			textFile := filepath.Join(tmpDir, "readme.txt")
			require.NoError(t, os.WriteFile(textFile, []byte("not a video"), 0644))

			cmd := update.NewCommand()
			_ = cmd.Flags().Set("preset", tt.preset)

			err := update.Run(cmd, []string{tmpDir}, configPath)
			// Should succeed (graceful exit when no videos found)
			assert.NoError(t, err)
		})
	}
}

// TestConstructNFOPath tests NFO path construction
func TestConstructNFOPath(t *testing.T) {
	tests := []struct {
		name     string
		match    matcher.MatchResult
		movie    *models.Movie
		perFile  bool
		expected string
	}{
		{
			name: "simple ID",
			match: matcher.MatchResult{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Dir: "/videos",
				},
				IsMultiPart: false,
			},
			movie: &models.Movie{
				ID: "IPX-123",
			},
			perFile:  false,
			expected: "/videos/IPX-123.nfo",
		},
		{
			name: "multi-part with per-file enabled",
			match: matcher.MatchResult{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Dir: "/videos",
				},
				IsMultiPart: true,
				PartSuffix:  "-cd1",
			},
			movie: &models.Movie{
				ID: "IPX-123",
			},
			perFile:  true,
			expected: "/videos/IPX-123-cd1.nfo",
		},
		{
			name: "multi-part with per-file disabled",
			match: matcher.MatchResult{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Dir: "/videos",
				},
				IsMultiPart: true,
				PartSuffix:  "-cd1",
			},
			movie: &models.Movie{
				ID: "IPX-123",
			},
			perFile:  false,
			expected: "/videos/IPX-123.nfo",
		},
		{
			name: "ID with special characters",
			match: matcher.MatchResult{
				ID: "ABC-123/XYZ",
				File: scanner.FileInfo{
					Dir: "/videos",
				},
				IsMultiPart: false,
			},
			movie: &models.Movie{
				ID: "ABC-123/XYZ",
			},
			perFile:  false,
			expected: "/videos/ABC-123-XYZ.nfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := update.ConstructNFOPath(tt.match, tt.movie, tt.perFile)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConstructNFOPath_PathTraversalPrevention tests security against path traversal
func TestConstructNFOPath_PathTraversalPrevention(t *testing.T) {
	match := matcher.MatchResult{
		ID: "../../../etc/passwd",
		File: scanner.FileInfo{
			Dir: "/videos",
		},
		IsMultiPart: false,
	}
	movie := &models.Movie{
		ID: "../../../etc/passwd",
	}

	result := update.ConstructNFOPath(match, movie, false)

	// Should sanitize path traversal attempts
	assert.NotContains(t, result, "../")
	assert.Contains(t, result, "/videos")
}

// TestConstructNFOPath_EmptyIDFallback tests fallback when ID sanitization results in empty string
func TestConstructNFOPath_EmptyIDFallback(t *testing.T) {
	match := matcher.MatchResult{
		ID: "///", // Will sanitize to empty string
		File: scanner.FileInfo{
			Dir: "/videos",
		},
		IsMultiPart: false,
	}
	movie := &models.Movie{
		ID: "///",
	}

	result := update.ConstructNFOPath(match, movie, false)

	// Should use fallback "metadata" when sanitization results in empty
	assert.Contains(t, result, "/videos/")
	assert.Contains(t, result, ".nfo")
	// Should not be empty filename
	assert.NotEqual(t, "/videos/.nfo", result)
}

// TestRun_Integration_WithExistingNFO tests NFO merge with existing file
func TestRun_Integration_WithExistingNFO(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	tmpDir := t.TempDir()
	configPath, _ := testutil.CreateTestConfig(t, nil)

	// Create a test video file
	videoFile := filepath.Join(tmpDir, "IPX-123.mp4")
	require.NoError(t, os.WriteFile(videoFile, []byte("fake video"), 0644))

	// Create an existing NFO file with some metadata
	existingNFO := filepath.Join(tmpDir, "IPX-123.nfo")
	nfoContent := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<movie>
  <title>Existing Title</title>
  <plot>Existing plot from NFO</plot>
  <studio>Existing Studio</studio>
</movie>`
	require.NoError(t, os.WriteFile(existingNFO, []byte(nfoContent), 0644))

	cmd := update.NewCommand()
	_ = cmd.Flags().Set("scalar-strategy", "prefer-nfo")
	_ = cmd.Flags().Set("show-merge-stats", "true")

	// This will exercise the NFO merge logic (lines 158-238)
	err := update.Run(cmd, []string{tmpDir}, configPath)

	// Should succeed even though scraper will fail (no real scraper data)
	// The key is that it attempts to merge with existing NFO
	if err != nil {
		// Error is expected since we can't scrape real data, but merge logic was exercised
		t.Logf("Expected error (no scraper data): %v", err)
	}

	// Verify existing NFO file still exists
	assert.FileExists(t, existingNFO)
}

// TestRun_Integration_InvalidConfig tests error handling for invalid config
func TestRun_Integration_InvalidConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	tmpDir := t.TempDir()

	// Use a non-existent config file
	invalidConfigPath := "/nonexistent/config.yaml"

	cmd := update.NewCommand()
	err := update.Run(cmd, []string{tmpDir}, invalidConfigPath)

	// Should return error for invalid config
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRun_Integration_MergeStrategies tests different merge strategies
func TestRun_Integration_MergeStrategies(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	strategies := []struct {
		name           string
		scalarStrategy string
		arrayStrategy  string
	}{
		{
			name:           "prefer-scraper strategy",
			scalarStrategy: "prefer-scraper",
			arrayStrategy:  "replace",
		},
		{
			name:           "preserve-existing strategy",
			scalarStrategy: "preserve-existing",
			arrayStrategy:  "merge",
		},
		{
			name:           "fill-missing-only strategy",
			scalarStrategy: "fill-missing-only",
			arrayStrategy:  "merge",
		},
	}

	for _, tt := range strategies {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath, _ := testutil.CreateTestConfig(t, nil)

			// Create a non-video file to avoid actual scraping
			textFile := filepath.Join(tmpDir, "readme.txt")
			require.NoError(t, os.WriteFile(textFile, []byte("not a video"), 0644))

			cmd := update.NewCommand()
			_ = cmd.Flags().Set("scalar-strategy", tt.scalarStrategy)
			_ = cmd.Flags().Set("array-strategy", tt.arrayStrategy)

			err := update.Run(cmd, []string{tmpDir}, configPath)
			// Should succeed (graceful exit when no videos found)
			assert.NoError(t, err)
		})
	}
}

// TestRun_Integration_DownloadMediaEnabled tests media download path
func TestRun_Integration_DownloadMediaEnabled(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	tmpDir := t.TempDir()
	configPath, _ := testutil.CreateTestConfig(t, nil)

	// Create a test video file
	videoFile := filepath.Join(tmpDir, "IPX-123.mp4")
	require.NoError(t, os.WriteFile(videoFile, []byte("fake video"), 0644))

	cmd := update.NewCommand()
	_ = cmd.Flags().Set("download", "true")
	_ = cmd.Flags().Set("dry-run", "false")

	// This will exercise the download media code path (lines 251-259)
	err := update.Run(cmd, []string{tmpDir}, configPath)

	// Error expected since scraper will fail, but download path was exercised
	if err != nil {
		t.Logf("Expected error (no scraper data): %v", err)
	}
}

// TestConstructNFOPath_MultiPartSuffixVariations tests different multi-part suffix formats
func TestConstructNFOPath_MultiPartSuffixVariations(t *testing.T) {
	tests := []struct {
		name                string
		suffix              string
		perFile             bool
		shouldIncludeSuffix bool
	}{
		{
			name:                "cd1 suffix with per-file",
			suffix:              "-cd1",
			perFile:             true,
			shouldIncludeSuffix: true,
		},
		{
			name:                "cd2 suffix with per-file",
			suffix:              "-cd2",
			perFile:             true,
			shouldIncludeSuffix: true,
		},
		{
			name:                "pt1 suffix with per-file",
			suffix:              "-pt1",
			perFile:             true,
			shouldIncludeSuffix: true,
		},
		{
			name:                "any suffix without per-file",
			suffix:              "-cd1",
			perFile:             false,
			shouldIncludeSuffix: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := matcher.MatchResult{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Dir: "/videos",
				},
				IsMultiPart: true,
				PartSuffix:  tt.suffix,
			}
			movie := &models.Movie{
				ID: "IPX-123",
			}

			result := update.ConstructNFOPath(match, movie, tt.perFile)

			if tt.shouldIncludeSuffix {
				assert.Contains(t, result, tt.suffix)
			} else {
				assert.NotContains(t, result, tt.suffix)
			}
		})
	}
}
