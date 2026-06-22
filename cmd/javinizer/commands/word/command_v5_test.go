package word

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCommand_V5_Structure(t *testing.T) {
	cmd := NewCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "word", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Check subcommands
	subcmds := cmd.Commands()
	assert.NotEmpty(t, subcmds)

	subNames := make(map[string]bool)
	for _, sub := range subcmds {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["list"], "should have list subcommand")
	assert.True(t, subNames["add"], "should have add subcommand")
}
