package actress_test

import (
	"testing"

	actresscmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/actress"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := actresscmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "actress", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Should have subcommands
	subCommands := cmd.Commands()
	subNames := make(map[string]bool)
	for _, sub := range subCommands {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["merge"], "should have merge subcommand")
	assert.True(t, subNames["export"], "should have export subcommand")
	assert.True(t, subNames["import"], "should have import subcommand")

	// Verify merge subcommand flags
	var mergeCmd *cobra.Command
	for _, sub := range subCommands {
		if sub.Name() == "merge" {
			mergeCmd = sub
			break
		}
	}
	require.NotNil(t, mergeCmd)
	assert.NotNil(t, mergeCmd.Flags().Lookup("target"), "merge should have target flag")
	assert.NotNil(t, mergeCmd.Flags().Lookup("source"), "merge should have source flag")
	assert.NotNil(t, mergeCmd.Flags().Lookup("non-interactive"), "merge should have non-interactive flag")
	assert.NotNil(t, mergeCmd.Flags().Lookup("prefer"), "merge should have prefer flag")
	assert.NotNil(t, mergeCmd.Flags().Lookup("yes"), "merge should have yes flag")
}
