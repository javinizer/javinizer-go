// Package testutil provides shared test utilities and helpers for javinizer-go tests.
package testutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
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
	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
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

// LoadGoldenFile loads a golden file from the testdata directory relative to the caller's test.
// Golden files are reference files used for snapshot testing complex outputs like XML or JSON.
// The file must be located in testdata/*.golden relative to the test file.
//
// Returns the file contents as []byte. Fails the test if the file cannot be read.
//
// Example:
//
//	func TestNFOGeneration(t *testing.T) {
//	    expected := testutil.LoadGoldenFile(t, "movie_complete.xml.golden")
//	    actual := nfo.Generate(movie)
//	    assert.Equal(t, expected, actual)
//	}
func LoadGoldenFile(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("testdata", name)
	content, err := os.ReadFile(path)
	if err != nil {
		require.NoError(t, err, "Failed to load golden file: %s\nCreate the golden file at: %s", name, path)
	}

	return content
}

// CompareGoldenFile compares actual test output against a golden file.
// If the content doesn't match, the test fails with a readable diff showing the differences.
// Golden files must be in testdata/*.golden relative to the test file.
//
// This is useful for snapshot testing complex outputs like NFO XML, JSON responses, or templates.
//
// Example:
//
//	func TestAPIResponse(t *testing.T) {
//	    response := api.GetMovie("IPX-123")
//	    actual, _ := json.MarshalIndent(response, "", "  ")
//	    testutil.CompareGoldenFile(t, "movie_response.json.golden", actual)
//	}
func CompareGoldenFile(t *testing.T, name string, actual []byte) {
	t.Helper()

	expected := LoadGoldenFile(t, name)

	if !bytes.Equal(expected, actual) {
		// Generate a readable diff
		diff := generateDiff(string(expected), string(actual))
		require.Fail(t, "Golden file mismatch",
			"Golden file: testdata/%s\n\nDiff (expected vs actual):\n%s", name, diff)
	}
}

// UpdateGoldenFile writes content to a golden file in the testdata directory.
// This function is intended for MANUAL USE ONLY during test development to create or update
// golden files. It should NOT be called in automated tests.
//
// Creates the testdata/ directory if it doesn't exist.
// Returns an error if the file cannot be written.
//
// Example usage during test development:
//
//	// Temporarily add this to generate/update the golden file
//	func TestGenerateGolden(t *testing.T) {
//	    output := generateComplexOutput()
//	    err := testutil.UpdateGoldenFile("output.golden", output)
//	    require.NoError(t, err)
//	}
//	// Remove after golden file is created
func UpdateGoldenFile(name string, content []byte) error {
	testdataDir := "testdata"

	// Create testdata directory if it doesn't exist
	if err := os.MkdirAll(testdataDir, 0755); err != nil {
		return fmt.Errorf("failed to create testdata directory: %w", err)
	}

	path := filepath.Join(testdataDir, name)
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("failed to write golden file %s: %w", path, err)
	}

	return nil
}

// GoldenFilePath returns the absolute path to a golden file in a package's testdata directory.
// This function is useful when you need to access golden files from nested package directories.
//
// Parameters:
//   - t: The testing.T instance (used for determining the package path)
//   - packageName: The package subdirectory (e.g., "dmm", "r18dev")
//   - filename: The golden file name (e.g., "response.html.golden")
//
// Returns the path: internal/scraper/{packageName}/testdata/{filename}
//
// Example:
//
//	path := testutil.GoldenFilePath(t, "dmm", "search_success.html.golden")
//	// Returns: "internal/scraper/dmm/testdata/search_success.html.golden"
func GoldenFilePath(t *testing.T, packageName, filename string) string {
	t.Helper()
	return filepath.Join("internal", "scraper", packageName, "testdata", filename)
}

// generateDiff creates a line-by-line diff between expected and actual strings.
// Similar to the output of `diff -u` but simplified for test output.
func generateDiff(expected, actual string) string {
	expectedLines := splitLines(expected)
	actualLines := splitLines(actual)

	var diff strings.Builder
	maxLines := len(expectedLines)
	if len(actualLines) > maxLines {
		maxLines = len(actualLines)
	}

	for i := 0; i < maxLines; i++ {
		var expLine, actLine string
		if i < len(expectedLines) {
			expLine = expectedLines[i]
		}
		if i < len(actualLines) {
			actLine = actualLines[i]
		}

		if expLine != actLine {
			if expLine != "" {
				diff.WriteString(fmt.Sprintf("- [line %d] %s\n", i+1, expLine))
			}
			if actLine != "" {
				diff.WriteString(fmt.Sprintf("+ [line %d] %s\n", i+1, actLine))
			}
		}
	}

	if diff.Len() == 0 {
		return "(no differences found)"
	}

	return diff.String()
}

// splitLines splits a string into lines, preserving empty lines.
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}
