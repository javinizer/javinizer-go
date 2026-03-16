package initcmd_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	initcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/init"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func captureOutput(t *testing.T, fn func()) (string, string) {
	t.Helper()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	fn()

	_ = wOut.Close()
	_ = wErr.Close()

	return <-outC, <-errC
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "File should exist: %s", path)
}

// Tests

// TestRunInit_Success verifies that init creates config and database successfully
func TestRunInit_Success(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "data", "javinizer.db")

	// Create initial config
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	err := config.Save(testCfg, configPath)
	require.NoError(t, err)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := initcmd.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the init subcommand
	rootCmd.SetArgs([]string{"init"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Verify config file exists
	assertFileExists(t, configPath)

	// Verify database was created
	assertFileExists(t, dbPath)

	// Verify output messages
	assert.Contains(t, stdout, "Initializing Javinizer")
	assert.Contains(t, stdout, "Created data directory")
	assert.Contains(t, stdout, "Initialized database")
	assert.Contains(t, stdout, "Saved configuration")
	assert.Contains(t, stdout, "Initialization complete")
}

// TestRunInit_DatabaseMigrations verifies that all tables are created during initialization
func TestRunInit_DatabaseMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "data", "javinizer.db")

	// Create initial config
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	err := config.Save(testCfg, configPath)
	require.NoError(t, err)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := initcmd.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the init subcommand
	rootCmd.SetArgs([]string{"init"})

	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Verify database file was created
	assertFileExists(t, dbPath)

	// Load the created config
	cfgLoaded, err := config.Load(configPath)
	require.NoError(t, err)

	// Connect to the created database
	db, err := database.New(cfgLoaded)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Verify critical tables exist by attempting to query them
	// If migrations failed, these queries would fail
	var count int64

	// Test movies table
	err = db.Model(&struct{ ID string }{}).Table("movies").Count(&count).Error
	assert.NoError(t, err, "movies table should exist")

	// Test actresses table
	err = db.Model(&struct{ ID uint }{}).Table("actresses").Count(&count).Error
	assert.NoError(t, err, "actresses table should exist")

	// Test genres table
	err = db.Model(&struct{ ID uint }{}).Table("genres").Count(&count).Error
	assert.NoError(t, err, "genres table should exist")

	// Test genre_replacements table
	err = db.Model(&struct{ ID uint }{}).Table("genre_replacements").Count(&count).Error
	assert.NoError(t, err, "genre_replacements table should exist")

	// Test movie_tags table
	err = db.Model(&struct{ ID uint }{}).Table("movie_tags").Count(&count).Error
	assert.NoError(t, err, "movie_tags table should exist")
}

// TestRunInit_DirectoryCreation verifies that necessary directories are created
func TestRunInit_DirectoryCreation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dataDir := filepath.Join(tmpDir, "data")
	dbPath := filepath.Join(dataDir, "javinizer.db")

	// Create initial config
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	err := config.Save(testCfg, configPath)
	require.NoError(t, err)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := initcmd.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the init subcommand
	rootCmd.SetArgs([]string{"init"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Verify data directory was created
	info, err := os.Stat(dataDir)
	require.NoError(t, err, "data directory should exist")
	assert.True(t, info.IsDir(), "data should be a directory")

	// Verify output mentions directory creation
	assert.Contains(t, stdout, "Created data directory")
	assert.Contains(t, stdout, dataDir)
}

// TestRunInit_ConfigFileContent verifies the created config has valid content
func TestRunInit_ConfigFileContent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "data", "javinizer.db")

	// Create initial config
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	err := config.Save(testCfg, configPath)
	require.NoError(t, err)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := initcmd.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the init subcommand
	rootCmd.SetArgs([]string{"init"})

	captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	// Load and verify config content
	cfgLoaded, err := config.Load(configPath)
	require.NoError(t, err)

	// Verify critical config fields have sensible defaults
	assert.NotEmpty(t, cfgLoaded.Database.DSN, "database DSN should be set")
	assert.Equal(t, "sqlite", cfgLoaded.Database.Type, "default database type should be sqlite")
	assert.NotEmpty(t, cfgLoaded.Scrapers.Priority, "scraper priority should be set")
	assert.NotEmpty(t, cfgLoaded.Output.FolderFormat, "folder format should be set")
	assert.NotEmpty(t, cfgLoaded.Output.FileFormat, "file format should be set")

	// Verify scrapers are configured
	assert.True(t, len(cfgLoaded.Scrapers.Priority) > 0, "should have at least one scraper in priority")
}

// TestRunInit_RepeatedInitialization verifies running init multiple times
func TestRunInit_RepeatedInitialization(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	dbPath := filepath.Join(tmpDir, "data", "javinizer.db")

	// Create initial config
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	err := config.Save(testCfg, configPath)
	require.NoError(t, err)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := initcmd.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the init subcommand first time
	rootCmd.SetArgs([]string{"init"})

	stdout1, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout1, "Initialization complete")

	// Verify config exists
	assertFileExists(t, configPath)

	// Get initial config content
	initialContent, err := os.ReadFile(configPath)
	require.NoError(t, err)

	// Create new command instance for second run
	rootCmd2 := &cobra.Command{Use: "root"}
	rootCmd2.PersistentFlags().String("config", configPath, "config file")

	cmd2 := initcmd.NewCommand()
	rootCmd2.AddCommand(cmd2)

	// Execute the init subcommand second time
	rootCmd2.SetArgs([]string{"init"})

	stdout2, _ := captureOutput(t, func() {
		err := rootCmd2.Execute()
		require.NoError(t, err)
	})
	assert.Contains(t, stdout2, "Initialization complete")

	// Verify config still exists and wasn't corrupted
	assertFileExists(t, configPath)

	// Verify both configs are valid (content should be idempotent)
	assert.NotEmpty(t, initialContent)

	// Both initializations should have created valid configs
	_, err = config.Load(configPath)
	assert.NoError(t, err, "config should be valid after repeated initialization")
}
