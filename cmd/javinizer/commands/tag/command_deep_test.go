package tag_test

import (
	"testing"

	tagcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/tag"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := tagcmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "tag", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Should have subcommands
	subCommands := cmd.Commands()
	subNames := make(map[string]bool)
	for _, sub := range subCommands {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["add"], "should have add subcommand")
	assert.True(t, subNames["list"], "should have list subcommand")
	assert.True(t, subNames["remove"], "should have remove subcommand")
	assert.True(t, subNames["search"], "should have search subcommand")
	assert.True(t, subNames["tags"], "should have tags subcommand")

	// Verify add subcommand
	var addCmd *cobra.Command
	for _, sub := range subCommands {
		if sub.Name() == "add" {
			addCmd = sub
			break
		}
	}
	require.NotNil(t, addCmd)
	assert.NotNil(t, addCmd.Args, "add subcommand should have Args validation (MinimumNArgs)")
	assert.NotNil(t, addCmd.RunE, "add subcommand should have RunE")

	// Verify list subcommand
	var listCmd *cobra.Command
	for _, sub := range subCommands {
		if sub.Name() == "list" {
			listCmd = sub
			break
		}
	}
	require.NotNil(t, listCmd)
	assert.NotNil(t, listCmd.RunE, "list subcommand should have RunE")
}
