package word_test

import (
	"testing"

	wordcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/word"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := wordcmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "word", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Should have subcommands
	subCommands := cmd.Commands()
	subNames := make(map[string]bool)
	for _, sub := range subCommands {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["list"], "should have list subcommand")
	assert.True(t, subNames["add"], "should have add subcommand")
	assert.True(t, subNames["remove"], "should have remove subcommand")
	assert.True(t, subNames["export"], "should have export subcommand")
	assert.True(t, subNames["import"], "should have import subcommand")

	// Verify list subcommand has RunE
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
