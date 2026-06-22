package info_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/info"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/update"
	appversion "github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	reg := scraperutil.NewScraperRegistry()
	scraper.RegisterAll(reg)
}

type failAfterNWriter struct {
	failAt int
	writes int
}

func (w *failAfterNWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes >= w.failAt {
		return 0, errors.New("forced write failure")
	}
	return len(p), nil
}

type countOnlyWriter struct {
	writes int
}

func (w *countOnlyWriter) Write(p []byte) (int, error) {
	w.writes++
	return len(p), nil
}

// Test helpers

type ConfigOption func(*config.Config)

func WithScraperPriority(priority []string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Scrapers.Priority = priority
	}
}

func WithOutputFolder(format string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Output.Template.FolderFormat = format
	}
}

func WithOutputFile(format string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Output.Template.FileFormat = format
	}
}

func WithDownloadCover(enabled bool) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Output.Download.DownloadCover = enabled
	}
}

func WithDatabaseDSN(dsn string) ConfigOption {
	return func(cfg *config.Config) {
		cfg.Database.DSN = dsn
	}
}

func WithVersionCheckEnabled(enabled bool) ConfigOption {
	return func(cfg *config.Config) {
		cfg.System.VersionCheckEnabled = enabled
	}
}

func writeTestState(t *testing.T, path, ver, checkedAt string, available, prerelease bool, source update.UpdateSource) {
	t.Helper()
	state := map[string]any{
		"version":    ver,
		"checked_at": checkedAt,
		"available":  available,
		"prerelease": prerelease,
		"source":     string(source),
	}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func createTestConfig(t *testing.T, opts ...ConfigOption) (string, *config.Config) {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(tmpDir, "test.db")

	for _, opt := range opts {
		opt(cfg)
	}

	err := config.Save(cfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	return configPath, cfg
}

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

	require.NoError(t, wOut.Close())
	require.NoError(t, wErr.Close())

	return <-outC, <-errC
}

// Tests

// TestRunInfo_DisplaysConfiguration verifies that run displays config information
func TestRunInfo_DisplaysConfiguration(t *testing.T) {
	configPath, testCfg := createTestConfig(t,
		WithScraperPriority([]string{"r18dev", "dmm"}),
		WithOutputFolder("<ID> - <TITLE>"),
		WithOutputFile("<ID>"),
		WithDownloadCover(true),
	)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the info subcommand
	rootCmd.SetArgs([]string{"info"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err, "command execution failed")
	})

	// Verify header
	assert.Contains(t, stdout, "=== Javinizer Configuration ===")

	// Verify config file path is shown
	assert.Contains(t, stdout, "Config file:")

	// Verify database info
	assert.Contains(t, stdout, "Database:")
	assert.Contains(t, stdout, testCfg.Database.DSN)
	assert.Contains(t, stdout, testCfg.Database.Type)

	// Verify server info
	assert.Contains(t, stdout, "Server:")
	assert.Contains(t, stdout, testCfg.Server.Host)

	// Verify scrapers section
	assert.Contains(t, stdout, "Scrapers:")
	assert.Contains(t, stdout, "Priority:")
	assert.Contains(t, stdout, "r18dev")
	assert.Contains(t, stdout, "dmm")

	// Verify scraper status
	assert.Contains(t, stdout, "R18.dev:")
	assert.Contains(t, stdout, "DMM/Fanza:")

	// Verify output settings
	assert.Contains(t, stdout, "Output:")
	assert.Contains(t, stdout, "Folder format:")
	assert.Contains(t, stdout, "<ID> - <TITLE>")
	assert.Contains(t, stdout, "File format:")
	assert.Contains(t, stdout, "<ID>")
	assert.Contains(t, stdout, "Download cover:")
	assert.Contains(t, stdout, "true")
}

// TestRunInfo_ShowsScraperPriority verifies scraper priority is displayed correctly
func TestRunInfo_ShowsScraperPriority(t *testing.T) {
	tests := []struct {
		name     string
		priority []string
		contains []string
	}{
		{
			name:     "r18dev first",
			priority: []string{"r18dev", "dmm"},
			contains: []string{"r18dev", "dmm"},
		},
		{
			name:     "dmm only",
			priority: []string{"dmm"},
			contains: []string{"dmm"},
		},
		{
			name:     "custom priority order",
			priority: []string{"dmm", "r18dev"},
			contains: []string{"dmm", "r18dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath, _ := createTestConfig(t,
				WithScraperPriority(tt.priority),
			)

			// Set up root command with persistent flag
			rootCmd := &cobra.Command{Use: "root"}
			rootCmd.PersistentFlags().String("config", configPath, "config file")

			cmd := info.NewCommand()
			rootCmd.AddCommand(cmd)

			// Execute the info subcommand
			rootCmd.SetArgs([]string{"info"})

			stdout, _ := captureOutput(t, func() {
				err := rootCmd.Execute()
				require.NoError(t, err, "command execution failed")
			})

			// Verify all expected scrapers are shown
			for _, scraper := range tt.contains {
				assert.Contains(t, stdout, scraper,
					"Expected scraper %s to be shown in priority", scraper)
			}
		})
	}
}

// TestRunInfo_ShowsOutputConfiguration verifies output settings are displayed
func TestRunInfo_ShowsOutputConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		folderFormat   string
		fileFormat     string
		downloadCover  bool
		downloadExtras bool
	}{
		{
			name:           "basic template",
			folderFormat:   "<ID>",
			fileFormat:     "<ID>",
			downloadCover:  true,
			downloadExtras: false,
		},
		{
			name:           "complex template",
			folderFormat:   "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			fileFormat:     "<ID> - <TITLE>",
			downloadCover:  true,
			downloadExtras: true,
		},
		{
			name:           "minimal downloads",
			folderFormat:   "<ID>",
			fileFormat:     "<ID>",
			downloadCover:  false,
			downloadExtras: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath, _ := createTestConfig(t,
				WithOutputFolder(tt.folderFormat),
				WithOutputFile(tt.fileFormat),
				WithDownloadCover(tt.downloadCover),
			)

			// Set up root command with persistent flag
			rootCmd := &cobra.Command{Use: "root"}
			rootCmd.PersistentFlags().String("config", configPath, "config file")

			cmd := info.NewCommand()
			rootCmd.AddCommand(cmd)

			// Execute the info subcommand
			rootCmd.SetArgs([]string{"info"})

			stdout, _ := captureOutput(t, func() {
				err := rootCmd.Execute()
				require.NoError(t, err, "command execution failed")
			})

			// Verify folder format
			assert.Contains(t, stdout, "Folder format:")
			assert.Contains(t, stdout, tt.folderFormat)

			// Verify file format
			assert.Contains(t, stdout, "File format:")
			assert.Contains(t, stdout, tt.fileFormat)

			// Verify download settings
			assert.Contains(t, stdout, "Download cover:")
			if tt.downloadCover {
				assert.Contains(t, stdout, "true")
			} else {
				assert.Contains(t, stdout, "false")
			}
		})
	}
}

// TestRunInfo_ShowsDatabasePath verifies database configuration is displayed
func TestRunInfo_ShowsDatabasePath(t *testing.T) {
	tmpDir := t.TempDir()
	customDBPath := filepath.Join(tmpDir, "custom_database.db")

	configPath, _ := createTestConfig(t,
		WithDatabaseDSN(customDBPath),
	)

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the info subcommand
	rootCmd.SetArgs([]string{"info"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err, "command execution failed")
	})

	// Verify custom database path is shown
	assert.Contains(t, stdout, "Database:")
	assert.Contains(t, stdout, customDBPath)
	assert.Contains(t, stdout, "sqlite")
}

// TestRunInfo_WithDefaultConfig verifies info works with default config
func TestRunInfo_WithDefaultConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Create config with all defaults
	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save default config")

	// Set up root command with persistent flag
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")

	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)

	// Execute the info subcommand
	rootCmd.SetArgs([]string{"info"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err, "command execution failed")
	})

	// Verify basic sections are present even with defaults
	assert.Contains(t, stdout, "Javinizer Configuration")
	assert.Contains(t, stdout, "Config file:")
	assert.Contains(t, stdout, "Database:")
	assert.Contains(t, stdout, "Server:")
	assert.Contains(t, stdout, "Scrapers:")
	assert.Contains(t, stdout, "Output:")

	// Verify no errors or panics occurred
	assert.NotContains(t, stdout, "error")
	assert.NotContains(t, stdout, "panic")
}

func TestRunInfo_UpdateSection_Disabled(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	configPath, _ := createTestConfig(t, WithVersionCheckEnabled(false))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"info"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err, "command execution failed")
	})

	assert.Contains(t, stdout, "Update:")
	assert.Contains(t, stdout, "Update enabled: false")
	assert.Contains(t, stdout, "Updates are disabled in config")
}

func TestRunInfo_UpdateSection_NeverChecked(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	configPath, _ := createTestConfig(t, WithVersionCheckEnabled(true))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"info"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err, "command execution failed")
	})

	assert.Contains(t, stdout, "Update enabled: true")
	assert.Contains(t, stdout, "Last checked: never")
	assert.Contains(t, stdout, "Latest version: (unknown)")
}

func TestRunInfo_UpdateSection_CachedState(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	configPath, _ := createTestConfig(t, WithVersionCheckEnabled(true))

	checkedAt := time.Now().UTC().Format(time.RFC3339)
	statePath := filepath.Join(tempDataDir, "update_cache.json")
	writeTestState(t, statePath, "v9.9.9", checkedAt, true, false, update.UpdateSourceCached)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"info"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err, "command execution failed")
	})

	assert.Contains(t, stdout, "Latest version: v9.9.9")
	assert.Contains(t, stdout, "Update available: true")
	assert.Contains(t, stdout, "Last checked: "+checkedAt)
}

func TestRunInfo_UpdateSection_PrereleaseWarning(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	if update.IsPrerelease(appversion.Short()) {
		t.Skip("current build version is prerelease; warning path requires stable current version")
	}

	configPath, _ := createTestConfig(t, WithVersionCheckEnabled(true))

	statePath := filepath.Join(tempDataDir, "update_cache.json")
	writeTestState(t, statePath, "v2.0.0-rc1", time.Now().UTC().Format(time.RFC3339), true, true, update.UpdateSourceCached)

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"info"})

	stdout, _ := captureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err, "command execution failed")
	})

	assert.Contains(t, stdout, "Latest version: v2.0.0-rc1")
	assert.Contains(t, stdout, "Warning: Latest version is a prerelease")
}

func TestRunInfo_LoadConfigError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.Mkdir(configPath, 0o755))

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"info"})

	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestRunInfo_WriteFailures(t *testing.T) {
	configPath, _ := createTestConfig(t)

	failPoints := []int{1, 2, 3, 5, 8, 12, 16}
	for _, failAt := range failPoints {
		t.Run(fmt.Sprintf("fail_at_write_%d", failAt), func(t *testing.T) {
			rootCmd := &cobra.Command{Use: "root"}
			rootCmd.PersistentFlags().String("config", configPath, "config file")
			cmd := info.NewCommand()
			rootCmd.AddCommand(cmd)
			rootCmd.SetArgs([]string{"info"})

			w := &failAfterNWriter{failAt: failAt}
			cmd.SetOut(w)
			cmd.SetErr(w)

			err := rootCmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "forced write failure")
		})
	}
}

func TestRunInfo_WriteFailures_Exhaustive(t *testing.T) {
	tempDataDir := t.TempDir()
	t.Setenv("JAVINIZER_DATA_DIR", tempDataDir)

	configPath, _ := createTestConfig(t, WithVersionCheckEnabled(true))

	// First run: count total writes for this command path.
	countWriter := &countOnlyWriter{}
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := info.NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"info"})
	cmd.SetOut(countWriter)
	cmd.SetErr(countWriter)
	require.NoError(t, rootCmd.Execute())
	require.Greater(t, countWriter.writes, 0)

	// Then fail each write index to cover all write-error return branches.
	for failAt := 1; failAt <= countWriter.writes; failAt++ {
		t.Run(fmt.Sprintf("exhaustive_fail_at_write_%d", failAt), func(t *testing.T) {
			rootCmd := &cobra.Command{Use: "root"}
			rootCmd.PersistentFlags().String("config", configPath, "config file")
			cmd := info.NewCommand()
			rootCmd.AddCommand(cmd)
			rootCmd.SetArgs([]string{"info"})

			w := &failAfterNWriter{failAt: failAt}
			cmd.SetOut(w)
			cmd.SetErr(w)

			err := rootCmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "forced write failure")
		})
	}
}
