package genre_test

import (
	"testing"

	genrecmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/genre"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := genrecmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "genre", cmd.Use)
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
	assert.True(t, subNames["export"], "should have export subcommand")
	assert.True(t, subNames["import"], "should have import subcommand")

	// Verify add subcommand has Args validation
	var addCmd *cobra.Command
	for _, sub := range subCommands {
		if sub.Name() == "add" {
			addCmd = sub
			break
		}
	}
	require.NotNil(t, addCmd)
	assert.NotNil(t, addCmd.Args, "add subcommand should have Args validation")
	assert.NotNil(t, addCmd.RunE, "add subcommand should have RunE")
}
