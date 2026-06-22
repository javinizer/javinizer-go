package scrape

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_InvalidConfigPath tests Run with a nonexistent config.
func TestRun_InvalidConfigPath(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	cmd := NewCommand()
	configPath := testutil.UnreachableConfigPath(t)
	movie, results, err := Run(context.Background(), cmd, []string{"TEST-001"}, configPath, nil)
	assert.Error(t, err)
	assert.Nil(t, movie)
	assert.Nil(t, results)
	assert.Contains(t, err.Error(), "failed to load config")
}

// TestRun_WithInjectedDeps_NilWorkflowType tests the unexpected workflow type error.
func TestRun_WithInjectedDeps_NilDeps(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	cmd := NewCommand()

	// Create a minimal config with :memory: DB
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	registry := scraperutil.NewScraperRegistry()
	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run with injected deps — empty registry will yield "No results from any scraper"
	movie, results, err := Run(context.Background(), cmd, []string{"TEST-001"}, "", deps)
	assert.Error(t, err)
	assert.Nil(t, movie)
	assert.Nil(t, results)
}

// TestRun_BootstrapError tests that bootstrap errors propagate from Run.
func TestRun_BootstrapError(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	cmd := NewCommand()

	// Create a config file with bad DSN (will fail during bootstrap)
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.yaml"
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = testutil.UnreachableConfigPath(t)
	require.NoError(t, config.Save(cfg, configPath))

	// Run without injected deps — will try to bootstrap
	movie, results, err := Run(context.Background(), cmd, []string{"TEST-001"}, configPath, nil)
	assert.Error(t, err)
	assert.Nil(t, movie)
	assert.Nil(t, results)
}

// TestRunScrape_CallsRunAndFormatsOutput tests runScrape's output formatting path.
func TestRunScrape_CallsRunAndFormatsOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	// Write a minimal config file
	tmpFile := t.TempDir() + "/config.yaml"
	configContent := `
config_version: 3
database:
  dsn: ":memory:"
scrapers:
  priority: []
metadata:
  priority:
    id: []
    content_id: []
    title: []
    description: []
matching:
  extensions: [".mp4"]
  regex_enabled: false
`
	require.NoError(t, os.WriteFile(tmpFile, []byte(configContent), 0644))

	cmd := NewCommand()
	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", tmpFile, "config file")
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"scrape", "TEST-001"})

	// Will fail because no scrapers are configured, but exercises runScrape path
	err := rootCmd.Execute()
	assert.Error(t, err)
}

// TestApplyFlagOverrides_UnchangedFlags tests that ApplyFlagOverrides is a no-op when flags aren't changed.
func TestApplyFlagOverrides_UnchangedFlags(t *testing.T) {
	cmd := NewCommand()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.ActressDatabase.Enabled = true
	cfg.Metadata.GenreReplacement.Enabled = true

	// Don't change any flags — should not modify config
	ApplyFlagOverrides(cmd, cfg)
	assert.True(t, cfg.Metadata.ActressDatabase.Enabled)
	assert.True(t, cfg.Metadata.GenreReplacement.Enabled)
}
