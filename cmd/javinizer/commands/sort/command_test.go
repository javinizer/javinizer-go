package sort_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/sort"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCommand verifies the command is created with correct structure
func TestNewCommand(t *testing.T) {
	cmd := sort.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "sort", cmd.Use[:4])
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify flags are registered
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("recursive"))
	assert.NotNil(t, cmd.Flags().Lookup("dest"))
	assert.NotNil(t, cmd.Flags().Lookup("move"))
	assert.NotNil(t, cmd.Flags().Lookup("nfo"))
	assert.NotNil(t, cmd.Flags().Lookup("download"))
	assert.NotNil(t, cmd.Flags().Lookup("extrafanart"))
	assert.NotNil(t, cmd.Flags().Lookup("scrapers"))
	assert.NotNil(t, cmd.Flags().Lookup("force-update"))
	assert.NotNil(t, cmd.Flags().Lookup("force-refresh"))
}

// TestFlags_DefaultValues verifies default flag values
func TestFlags_DefaultValues(t *testing.T) {
	cmd := sort.NewCommand()

	// Check default values
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	assert.False(t, dryRun, "dry-run should default to false")

	recursive, _ := cmd.Flags().GetBool("recursive")
	assert.True(t, recursive, "recursive should default to true")

	move, _ := cmd.Flags().GetBool("move")
	assert.False(t, move, "move should default to false (copy mode)")

	nfo, _ := cmd.Flags().GetBool("nfo")
	assert.True(t, nfo, "nfo should default to true")

	download, _ := cmd.Flags().GetBool("download")
	assert.True(t, download, "download should default to true")

	extrafanart, _ := cmd.Flags().GetBool("extrafanart")
	assert.False(t, extrafanart, "extrafanart should default to false")

	forceUpdate, _ := cmd.Flags().GetBool("force-update")
	assert.False(t, forceUpdate, "force-update should default to false")

	forceRefresh, _ := cmd.Flags().GetBool("force-refresh")
	assert.False(t, forceRefresh, "force-refresh should default to false")
}

// TestFlags_ShortForms verifies short flag forms work
func TestFlags_ShortForms(t *testing.T) {
	cmd := sort.NewCommand()

	// Verify short forms are registered
	assert.NotNil(t, cmd.Flags().ShorthandLookup("n"), "should have -n for dry-run")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("r"), "should have -r for recursive")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("d"), "should have -d for dest")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("m"), "should have -m for move")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("p"), "should have -p for scrapers")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("f"), "should have -f for force-update")
}

// Note: Full integration tests for sort command remain in cmd/cli/sort_test.go
// until the complete migration to the new command structure is finished.
// These smoke tests verify the command structure and flags are correct.
