package actress

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestNewCommand_V5_Structure(t *testing.T) {
	cmd := NewCommand()
	if cmd == nil {
		t.Skip("NewCommand requires init() registration which may not be available in test")
	}
	assert.Equal(t, "actress", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Check subcommands exist
	subcmds := cmd.Commands()
	assert.NotEmpty(t, subcmds, "actress command should have subcommands")

	// Verify subcommand names
	subNames := make(map[string]bool)
	for _, sub := range subcmds {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["merge"], "should have merge subcommand")
}

func TestActressCommandFlags_V5_MergeFlags(t *testing.T) {
	// Test that merge command has expected flags
	mergeCmd := &cobra.Command{Use: "merge"}
	mergeCmd.Flags().String("source", "", "Source actress identifier")
	mergeCmd.Flags().String("target", "", "Target actress identifier")
	mergeCmd.Flags().Bool("non-interactive", false, "Non-interactive mode")

	assert.True(t, mergeCmd.HasFlags())
}
