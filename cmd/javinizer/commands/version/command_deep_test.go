package version_test

import (
	"testing"

	versioncmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := versioncmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "version", cmd.Use)
	assert.Contains(t, cmd.Short, "version")

	// Verify flags
	shortFlag := cmd.Flags().Lookup("short")
	require.NotNil(t, shortFlag, "short flag should be registered")
	assert.Equal(t, "s", shortFlag.Shorthand)
	shortDefault, _ := cmd.Flags().GetBool("short")
	assert.False(t, shortDefault, "short flag should default to false")

	checkFlag := cmd.Flags().Lookup("check")
	require.NotNil(t, checkFlag, "check flag should be registered")
	assert.Equal(t, "c", checkFlag.Shorthand)
	checkDefault, _ := cmd.Flags().GetBool("check")
	assert.False(t, checkDefault, "check flag should default to false")

	// Verify Args validation
	assert.NotNil(t, cmd.Args, "Args validation should be set (NoArgs)")

	// Verify RunE is set
	assert.NotNil(t, cmd.RunE, "RunE should be set")
}
