package scrape

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommand_ArgsValidator(t *testing.T) {
	cmd := NewCommand()
	err := cmd.Args(cmd, []string{"TEST-001"})
	require.NoError(t, err)
	err = cmd.Args(cmd, []string{})
	require.Error(t, err)
	err = cmd.Args(cmd, []string{"TEST-001", "extra"})
	require.Error(t, err)
}

func TestNewCommand_HasOutputFlag(t *testing.T) {
	cmd := NewCommand()
	flag := cmd.Flags().Lookup("output")
	require.NotNil(t, flag)
	assert.Equal(t, "text", flag.DefValue)
}
