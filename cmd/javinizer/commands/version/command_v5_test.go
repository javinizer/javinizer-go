package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCommand_V5_Structure(t *testing.T) {
	cmd := NewCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "version", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)

	// Check flags
	flags := cmd.Flags()
	_, err := flags.GetBool("check")
	assert.NoError(t, err, "check flag should exist")
}

func TestNewCommand_V5_HasCheckFlag(t *testing.T) {
	cmd := NewCommand()
	checkVal, err := cmd.Flags().GetBool("check")
	assert.NoError(t, err)
	assert.False(t, checkVal, "check flag should default to false")
}
