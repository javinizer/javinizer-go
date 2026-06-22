package history_test

import (
	"testing"

	historycmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/history"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := historycmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "history", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Should have subcommands
	subCommands := cmd.Commands()
	subNames := make(map[string]bool)
	for _, sub := range subCommands {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["list"], "should have list subcommand")
	assert.True(t, subNames["stats"], "should have stats subcommand")
	assert.True(t, subNames["movie"], "should have movie subcommand")
	assert.True(t, subNames["clean"], "should have clean subcommand")

	// Verify list subcommand flags
	var listCmd *cobra.Command
	for _, sub := range subCommands {
		if sub.Name() == "list" {
			listCmd = sub
			break
		}
	}
	require.NotNil(t, listCmd)
	assert.NotNil(t, listCmd.Flags().Lookup("limit"), "list should have limit flag")
	assert.NotNil(t, listCmd.Flags().Lookup("operation"), "list should have operation flag")
	assert.NotNil(t, listCmd.Flags().Lookup("status"), "list should have status flag")
	assert.NotNil(t, listCmd.Flags().Lookup("batch"), "list should have batch flag")
}
