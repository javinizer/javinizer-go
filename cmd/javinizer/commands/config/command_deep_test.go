package config_test

import (
	"testing"

	configcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructureDeep(t *testing.T) {
	cmd := configcmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "config", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Should have subcommands
	subCommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subCommands), 1, "should have at least one subcommand")

	// Find migrate subcommand
	var migrateCmd *cobra.Command
	for _, sub := range subCommands {
		if sub.Use == "migrate" {
			migrateCmd = sub
			break
		}
	}
	require.NotNil(t, migrateCmd, "migrate subcommand should be registered")

	// Verify migrate subcommand flags
	dryRunFlag := migrateCmd.Flags().Lookup("dry-run")
	require.NotNil(t, dryRunFlag, "dry-run flag should be registered on migrate")
	dryRunDefault, _ := migrateCmd.Flags().GetBool("dry-run")
	assert.False(t, dryRunDefault, "dry-run should default to false")
}
