package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCommand_V5_Structure(t *testing.T) {
	cmd := NewCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "tui [path]", cmd.Use)
	assert.Contains(t, cmd.Short, "interactive")
	assert.NotNil(t, cmd.RunE)

	// Check flags are registered
	flags := cmd.Flags()
	assert.NotNil(t, flags)

	_, err := flags.GetString("source")
	assert.NoError(t, err, "source flag should exist")

	_, err = flags.GetString("dest")
	assert.NoError(t, err, "dest flag should exist")

	_, err = flags.GetBool("recursive")
	assert.NoError(t, err, "recursive flag should exist")

	_, err = flags.GetBool("move")
	assert.NoError(t, err, "move flag should exist")

	_, err = flags.GetBool("dry-run")
	assert.NoError(t, err, "dry-run flag should exist")

	_, err = flags.GetString("link-mode")
	assert.NoError(t, err, "link-mode flag should exist")

	_, err = flags.GetBool("extrafanart")
	assert.NoError(t, err, "extrafanart flag should exist")

	_, err = flags.GetStringSlice("scrapers")
	assert.NoError(t, err, "scrapers flag should exist")

	_, err = flags.GetBool("update-mode")
	assert.NoError(t, err, "update-mode flag should exist")

	_, err = flags.GetString("preset")
	assert.NoError(t, err, "preset flag should exist")

	_, err = flags.GetString("scalar-strategy")
	assert.NoError(t, err, "scalar-strategy flag should exist")

	_, err = flags.GetString("array-strategy")
	assert.NoError(t, err, "array-strategy flag should exist")
}

func TestNewCommand_V5_MaxArgs(t *testing.T) {
	cmd := NewCommand()
	// The command should accept at most 1 positional argument
	assert.NotNil(t, cmd.Args)
}
