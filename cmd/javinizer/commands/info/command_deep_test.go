package info_test

import (
	"testing"

	infocmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/info"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := infocmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "info", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE, "RunE should be set")

	// Info command should not have subcommands
	assert.Empty(t, cmd.Commands(), "info command should not have subcommands")
}
