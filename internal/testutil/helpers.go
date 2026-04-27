// Package testutil provides shared test utilities and helpers for javinizer-go tests.
package testutil

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// CaptureOutput captures stdout and stderr from a function execution.
// This is useful for testing CLI commands that write to console.
//
// Example:
//
//	stdout, stderr := testutil.CaptureOutput(t, func() {
//	    cmd.Execute()
//	})
func CaptureOutput(t *testing.T, fn func()) (stdout, stderr string) {
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

// CreateRootCommandWithConfig creates a root cobra command with the config flag set.
// This is the standard pattern for testing commands that need access to --config flag.
//
// Example:
//
//	rootCmd := testutil.CreateRootCommandWithConfig(t, configPath)
//	cmd := mycommand.NewCommand()
//	rootCmd.AddCommand(cmd)
//	rootCmd.SetArgs([]string{"mycommand", "arg1"})
func CreateRootCommandWithConfig(t *testing.T, configPath string) *cobra.Command {
	t.Helper()
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	return rootCmd
}

// SetupTestDB creates a temporary database with migrations for testing.
// Returns the config path and database path.
// The database directory is automatically created.
//
// Example:
//
//	configPath, dbPath := testutil.SetupTestDB(t)
//	// Use configPath to load config
//	// Database is ready with all migrations applied
func SetupTestDB(t *testing.T) (configPath string, dbPath string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath = filepath.Join(tmpDir, "data", "test.db")

	// Ensure database directory exists
	err := os.MkdirAll(filepath.Dir(dbPath), config.DirPerm)
	require.NoError(t, err, "Failed to create database directory")

	// Create test config
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	configPath = filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Initialize database with migrations
	db, err := database.New(testCfg)
	require.NoError(t, err, "Failed to create database")
	err = db.AutoMigrate()
	require.NoError(t, err, "Failed to run migrations")
	_ = db.Close()

	return configPath, dbPath
}

// SetupTestDBWithConfig creates a temporary database with custom config options.
// This allows tests to customize the config before database creation.
//
// Example:
//
//	configPath, dbPath := testutil.SetupTestDBWithConfig(t, func(cfg *config.Config) {
//	    cfg.Scrapers.Priority = []string{"r18dev"}
//	})
func SetupTestDBWithConfig(t *testing.T, customizeFn func(*config.Config)) (configPath string, dbPath string) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath = filepath.Join(tmpDir, "data", "test.db")

	// Ensure database directory exists
	err := os.MkdirAll(filepath.Dir(dbPath), config.DirPerm)
	require.NoError(t, err, "Failed to create database directory")

	// Create test config with customizations
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	if customizeFn != nil {
		customizeFn(testCfg)
	}
	configPath = filepath.Join(tmpDir, "config.yaml")
	err = config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Initialize database with migrations
	db, err := database.New(testCfg)
	require.NoError(t, err, "Failed to create database")
	err = db.AutoMigrate()
	require.NoError(t, err, "Failed to run migrations")
	_ = db.Close()

	return configPath, dbPath
}

// CreateTestConfig creates a test configuration file with optional customizations.
// Returns the config path and the config object.
//
// Example:
//
//	configPath, cfg := testutil.CreateTestConfig(t, func(cfg *config.Config) {
//	    cfg.Scrapers.Priority = []string{"dmm", "r18dev"}
//	})
func CreateTestConfig(t *testing.T, customizeFn func(*config.Config)) (configPath string, cfg *config.Config) {
	t.Helper()
	tmpDir := t.TempDir()
	configPath = filepath.Join(tmpDir, "config.yaml")

	cfg = config.DefaultConfig()
	cfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	if customizeFn != nil {
		customizeFn(cfg)
	}

	err := config.Save(cfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	return configPath, cfg
}

// AssertFileExists checks that a file exists at the given path.
// Fails the test if the file does not exist.
//
// Example:
//
//	testutil.AssertFileExists(t, "/path/to/file.txt")
func AssertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.NoError(t, err, "File should exist: %s", path)
}

// AssertFileNotExists checks that a file does NOT exist at the given path.
// Fails the test if the file exists.
//
// Example:
//
//	testutil.AssertFileNotExists(t, "/path/to/deleted.txt")
func AssertFileNotExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "File should not exist: %s", path)
}

// CreateTestVideoFile creates a test video file with dummy content.
// Returns the full path to the created file.
//
// Example:
//
//	videoPath := testutil.CreateTestVideoFile(t, tmpDir, "IPX-535.mp4")
func CreateTestVideoFile(t *testing.T, dir string, filename string) string {
	t.Helper()

	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte("dummy video content"), 0644)
	require.NoError(t, err, "Failed to create test video file")

	return path
}
