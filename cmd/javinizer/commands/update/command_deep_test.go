package update_test

import (
	"testing"

	updatecmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructureDeep(t *testing.T) {
	cmd := updatecmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "update", "command Use should contain 'update'")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE, "RunE should be set")
	assert.NotNil(t, cmd.Args, "Args validation should be set")

	// Verify additional flags beyond what's tested in TestNewCommand
	assert.NotNil(t, cmd.Flags().Lookup("force-overwrite"), "force-overwrite flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("preserve-nfo"), "preserve-nfo flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("show-merge-stats"), "show-merge-stats flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("preset"), "preset flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("scalar-strategy"), "scalar-strategy flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("array-strategy"), "array-strategy flag should be registered")
}
