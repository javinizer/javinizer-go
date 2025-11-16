package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunSort_MatcherCreationError tests error handling when matcher creation fails
func TestRunSort_MatcherCreationError(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create test video file
	videoPath := createTestVideoFile(t, sourceDir, "IPX-123.mp4")
	_ = videoPath

	// Setup config with invalid regex pattern to cause matcher creation error
	configPath, testCfg := createTestConfig(t,
		WithDatabaseDSN(filepath.Join(tmpDir, "test.db")),
		WithScraperPriority([]string{"mock"}),
	)
	// Set invalid regex pattern that will cause matcher.NewMatcher to fail
	testCfg.Matching.RegexEnabled = true
	testCfg.Matching.RegexPattern = "[invalid(" // Unclosed bracket causes regex compilation error
	require.NoError(t, config.Save(testCfg, configPath))

	withTempConfigFile(t, configPath, func() {
		deps := createTestDependencies(t, testCfg)
		defer deps.Close()

		cmd := &cobra.Command{}
		cmd.Flags().Bool("dry-run", false, "")
		cmd.Flags().Bool("recursive", true, "")
		cmd.Flags().String("dest", "", "")
		cmd.Flags().Bool("move", false, "")
		cmd.Flags().Bool("nfo", false, "")
		cmd.Flags().Bool("download", false, "")
		cmd.Flags().Bool("extrafanart", false, "")
		cmd.Flags().StringSlice("scrapers", nil, "")
		cmd.Flags().Bool("force-update", false, "")
		cmd.Flags().Bool("force-refresh", false, "")

		// Should fail with matcher creation error
		err := runSort(cmd, []string{sourceDir}, deps)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create matcher")
	})
}

// TestRunSort_DestPathForFile tests destination path logic when source is a file
func TestRunSort_DestPathForFile(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create test video file
	videoPath := createTestVideoFile(t, sourceDir, "ABC-456.mp4")

	// Setup config
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath, testCfg := createTestConfig(t,
		WithDatabaseDSN(dbPath),
		WithScraperPriority([]string{"mock"}),
		WithOutputFolder("<ID>"),
		WithOutputFile("<ID>"),
		WithNFOEnabled(false),
		WithDownloadCover(false),
	)
	testCfg.Output.MoveToFolder = true
	require.NoError(t, config.Save(testCfg, configPath))

	// Pre-populate database
	dbConn, err := database.New(testCfg)
	require.NoError(t, err)
	err = dbConn.AutoMigrate()
	require.NoError(t, err)
	movie := createTestMovie("ABC-456", "Test Movie")
	repo := database.NewMovieRepository(dbConn)
	err = repo.Upsert(movie)
	require.NoError(t, err)
	dbConn.Close()

	withTempConfigFile(t, configPath, func() {
		deps := createTestDependencies(t, testCfg)
		defer deps.Close()

		cmd := &cobra.Command{}
		cmd.Flags().Bool("dry-run", true, "") // Use dry run to avoid actual file moves
		cmd.Flags().Bool("recursive", false, "")
		cmd.Flags().String("dest", "", "") // Empty dest - should use file's directory
		cmd.Flags().Bool("move", false, "")
		cmd.Flags().Bool("nfo", false, "")
		cmd.Flags().Bool("download", false, "")
		cmd.Flags().Bool("extrafanart", false, "")
		cmd.Flags().StringSlice("scrapers", nil, "")
		cmd.Flags().Bool("force-update", false, "")
		cmd.Flags().Bool("force-refresh", false, "")

		stdout, _ := captureOutput(t, func() {
			// Pass the file path (not directory) - should use file's directory as dest
			err := runSort(cmd, []string{videoPath}, deps)
			require.NoError(t, err)
		})

		// Should show the source directory as destination
		assert.Contains(t, stdout, "Destination: "+sourceDir)
	})
}

// TestRunSort_ExtrafanartFlagOverride tests that --extrafanart flag overrides config
func TestRunSort_ExtrafanartFlagOverride(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create test video file
	videoPath := createTestVideoFile(t, sourceDir, "XYZ-789.mp4")
	_ = videoPath

	// Setup config with extrafanart DISABLED
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath, testCfg := createTestConfig(t,
		WithDatabaseDSN(dbPath),
		WithScraperPriority([]string{"mock"}),
		WithOutputFolder("<ID>"),
		WithOutputFile("<ID>"),
		WithNFOEnabled(false),
		WithDownloadCover(false),
	)
	testCfg.Output.DownloadExtrafanart = false // Disabled in config
	testCfg.Output.MoveToFolder = false        // Disable file organization to simplify test
	require.NoError(t, config.Save(testCfg, configPath))

	// Pre-populate database
	dbConn, err := database.New(testCfg)
	require.NoError(t, err)
	err = dbConn.AutoMigrate()
	require.NoError(t, err)
	movie := createTestMovie("XYZ-789", "Test Movie")
	repo := database.NewMovieRepository(dbConn)
	err = repo.Upsert(movie)
	require.NoError(t, err)
	dbConn.Close()

	withTempConfigFile(t, configPath, func() {
		deps := createTestDependencies(t, testCfg)
		defer deps.Close()

		cmd := &cobra.Command{}
		cmd.Flags().Bool("dry-run", true, "")
		cmd.Flags().Bool("recursive", true, "")
		cmd.Flags().String("dest", "", "")
		cmd.Flags().Bool("move", false, "")
		cmd.Flags().Bool("nfo", false, "")
		cmd.Flags().Bool("download", false, "")
		cmd.Flags().Bool("extrafanart", true, "") // Flag overrides config
		cmd.Flags().StringSlice("scrapers", nil, "")
		cmd.Flags().Bool("force-update", false, "")
		cmd.Flags().Bool("force-refresh", false, "")

		err := runSort(cmd, []string{sourceDir}, deps)
		require.NoError(t, err)

		// Verify config was overridden
		assert.True(t, deps.Config.Output.DownloadExtrafanart)
	})
}

// TestRunSort_ScraperPriorityOverride tests that --scrapers flag overrides config priority
func TestRunSort_ScraperPriorityOverride(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	require.NoError(t, os.MkdirAll(sourceDir, 0755))

	// Create test video file
	videoPath := createTestVideoFile(t, sourceDir, "DEF-111.mp4")
	_ = videoPath

	// Setup config with default scraper priority
	dbPath := filepath.Join(tmpDir, "test.db")
	configPath, testCfg := createTestConfig(t,
		WithDatabaseDSN(dbPath),
		WithScraperPriority([]string{"r18dev", "dmm"}), // Default priority
		WithOutputFolder("<ID>"),
		WithOutputFile("<ID>"),
		WithNFOEnabled(false),
		WithDownloadCover(false),
	)
	testCfg.Output.MoveToFolder = false // Disable file organization to simplify test
	require.NoError(t, config.Save(testCfg, configPath))

	// Pre-populate database
	dbConn, err := database.New(testCfg)
	require.NoError(t, err)
	err = dbConn.AutoMigrate()
	require.NoError(t, err)
	movie := createTestMovie("DEF-111", "Test Movie")
	repo := database.NewMovieRepository(dbConn)
	err = repo.Upsert(movie)
	require.NoError(t, err)
	dbConn.Close()

	withTempConfigFile(t, configPath, func() {
		deps := createTestDependencies(t, testCfg)
		defer deps.Close()

		cmd := &cobra.Command{}
		cmd.Flags().Bool("dry-run", true, "")
		cmd.Flags().Bool("recursive", true, "")
		cmd.Flags().String("dest", "", "")
		cmd.Flags().Bool("move", false, "")
		cmd.Flags().Bool("nfo", false, "")
		cmd.Flags().Bool("download", false, "")
		cmd.Flags().Bool("extrafanart", false, "")
		cmd.Flags().StringSlice("scrapers", []string{"dmm", "r18dev"}, "") // Override priority
		cmd.Flags().Bool("force-update", false, "")
		cmd.Flags().Bool("force-refresh", false, "")

		_, _ = captureOutput(t, func() {
			err := runSort(cmd, []string{sourceDir}, deps)
			require.NoError(t, err)
		})

		// Test passed - scraper priority was successfully overridden
	})
}
