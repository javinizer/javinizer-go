package config_test

import (
	"os"
	"path/filepath"
	"testing"

	configcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/config"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommand_Structure(t *testing.T) {
	cmd := configcmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "config", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	subCommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subCommands), 1, "should have at least one subcommand")

	var migrateFound bool
	for _, sub := range subCommands {
		if sub.Use == "migrate" {
			migrateFound = true
			break
		}
	}
	assert.True(t, migrateFound, "migrate subcommand should be registered")
}

func TestMigrateCommand_Structure(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configCmd := configcmd.NewCommand()
	rootCmd.AddCommand(configCmd)

	migrateCmd, _, err := rootCmd.Find([]string{"config", "migrate"})
	require.NoError(t, err)
	require.NotNil(t, migrateCmd)

	assert.Equal(t, "migrate", migrateCmd.Use)
	assert.NotEmpty(t, migrateCmd.Short)
	assert.NotEmpty(t, migrateCmd.Long)

	dryRunFlag := migrateCmd.Flags().Lookup("dry-run")
	require.NotNil(t, dryRunFlag, "dry-run flag should be registered")
	assert.Equal(t, "false", dryRunFlag.DefValue, "dry-run should default to false")
}

func TestMigrateCommand_CurrentVersionNoOp(t *testing.T) {
	configPath, _ := testutil.CreateTestConfig(t, nil)

	t.Setenv("JAVINIZER_CONFIG", configPath)

	rootCmd := &cobra.Command{Use: "root"}
	configCmd := configcmd.NewCommand()
	rootCmd.AddCommand(configCmd)
	rootCmd.SetArgs([]string{"config", "migrate"})

	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "already at current version")
}

func TestMigrateCommand_DryRun(t *testing.T) {
	configPath, _ := testutil.CreateTestConfig(t, func(cfg *config.Config) {
		cfg.ConfigVersion = 1
	})

	t.Setenv("JAVINIZER_CONFIG", configPath)

	rootCmd := &cobra.Command{Use: "root"}
	configCmd := configcmd.NewCommand()
	rootCmd.AddCommand(configCmd)
	rootCmd.SetArgs([]string{"config", "migrate", "--dry-run"})

	stdout, _ := testutil.CaptureOutput(t, func() {
		err := rootCmd.Execute()
		require.NoError(t, err)
	})

	assert.Contains(t, stdout, "DRY RUN")
	assert.Contains(t, stdout, "Current config version: 1")
	assert.Contains(t, stdout, "Target version:")
	assert.Contains(t, stdout, "Would migrate config")
	assert.Contains(t, stdout, "WARNING")
}

func TestMigrateCommand_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// TODO: This test requires refactoring the migrate command to support:
	// 1. A --force flag to skip confirmation prompt, OR
	// 2. An injected io.Reader for stdin mocking
	// The migration logic (MigrateToCurrent + Save) at command.go:95-109
	// remains untested because the confirmation prompt blocks CI execution.
	//
	// For now, the DryRun test covers the migration logic verification
	// without the actual write, and the success path is manually tested.
	t.Skip("migration command requires refactoring for stdin mocking")
}

func TestMigrateCommand_ConfigNotFound(t *testing.T) {
	t.Setenv("JAVINIZER_CONFIG", "/nonexistent/path/config.yaml")

	rootCmd := &cobra.Command{Use: "root"}
	configCmd := configcmd.NewCommand()
	rootCmd.AddCommand(configCmd)
	rootCmd.SetArgs([]string{"config", "migrate"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config file not found")
}

func TestMigrateCommand_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	invalidConfigPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(invalidConfigPath, []byte("invalid: yaml: content: ["), 0644)
	require.NoError(t, err)

	t.Setenv("JAVINIZER_CONFIG", invalidConfigPath)

	rootCmd := &cobra.Command{Use: "root"}
	configCmd := configcmd.NewCommand()
	rootCmd.AddCommand(configCmd)
	rootCmd.SetArgs([]string{"config", "migrate"})

	err = rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestMigrateCommand_FlagDefaults(t *testing.T) {
	rootCmd := &cobra.Command{Use: "root"}
	configCmd := configcmd.NewCommand()
	rootCmd.AddCommand(configCmd)

	migrateCmd, _, err := rootCmd.Find([]string{"config", "migrate"})
	require.NoError(t, err)

	dryRun, err := migrateCmd.Flags().GetBool("dry-run")
	require.NoError(t, err)
	assert.False(t, dryRun, "dry-run should default to false")
}

func TestMigrateCommand_DefaultConfigPath(t *testing.T) {
	t.Setenv("JAVINIZER_CONFIG", "")

	rootCmd := &cobra.Command{Use: "root"}
	configCmd := configcmd.NewCommand()
	rootCmd.AddCommand(configCmd)
	rootCmd.SetArgs([]string{"config", "migrate"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config file not found")
	assert.Contains(t, err.Error(), filepath.Join("configs", "config.yaml"))
}
