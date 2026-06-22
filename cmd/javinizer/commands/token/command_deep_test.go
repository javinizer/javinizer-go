package token

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "token", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify persistent flags
	jsonFlag := cmd.PersistentFlags().Lookup("json")
	require.NotNil(t, jsonFlag, "json persistent flag should be registered")
	jsonDefault, _ := cmd.PersistentFlags().GetBool("json")
	assert.False(t, jsonDefault, "json flag should default to false")

	// Verify subcommands
	subCommands := cmd.Commands()
	subNames := make(map[string]bool)
	for _, sub := range subCommands {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["create"], "should have create subcommand")
	assert.True(t, subNames["revoke"], "should have revoke subcommand")
	assert.True(t, subNames["list"], "should have list subcommand")
}
