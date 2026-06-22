package sort_test

import (
	"testing"

	sortcmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/sort"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructureDeep(t *testing.T) {
	cmd := sortcmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "sort", "command Use should contain 'sort'")
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE, "RunE should be set")
	assert.NotNil(t, cmd.Args, "Args validation should be set")

	// Verify additional flags beyond what's tested in TestNewCommand
	assert.NotNil(t, cmd.Flags().Lookup("link-mode"), "link-mode flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("nfo"), "nfo flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("force-update"), "force-update flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("force-refresh"), "force-refresh flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("extrafanart"), "extrafanart flag should be registered")
}
