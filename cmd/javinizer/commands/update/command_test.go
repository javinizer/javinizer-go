package update_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCommand verifies the command is created with correct structure
func TestNewCommand(t *testing.T) {
	cmd := update.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "update", cmd.Use[:6])
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify flags are registered
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("download"))
	assert.NotNil(t, cmd.Flags().Lookup("extrafanart"))
	assert.NotNil(t, cmd.Flags().Lookup("scrapers"))
	assert.NotNil(t, cmd.Flags().Lookup("force-refresh"))
	assert.NotNil(t, cmd.Flags().Lookup("force-overwrite"))
	assert.NotNil(t, cmd.Flags().Lookup("preserve-nfo"))
	assert.NotNil(t, cmd.Flags().Lookup("show-merge-stats"))
	assert.NotNil(t, cmd.Flags().Lookup("preset"))
	assert.NotNil(t, cmd.Flags().Lookup("scalar-strategy"))
	assert.NotNil(t, cmd.Flags().Lookup("array-strategy"))
}

// TestFlags_DefaultValues verifies default flag values
func TestFlags_DefaultValues(t *testing.T) {
	cmd := update.NewCommand()

	// Check default values
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	assert.False(t, dryRun, "dry-run should default to false")

	download, _ := cmd.Flags().GetBool("download")
	assert.True(t, download, "download should default to true")

	extrafanart, _ := cmd.Flags().GetBool("extrafanart")
	assert.False(t, extrafanart, "extrafanart should default to false")

	forceRefresh, _ := cmd.Flags().GetBool("force-refresh")
	assert.False(t, forceRefresh, "force-refresh should default to false")

	forceOverwrite, _ := cmd.Flags().GetBool("force-overwrite")
	assert.False(t, forceOverwrite, "force-overwrite should default to false")

	preserveNFO, _ := cmd.Flags().GetBool("preserve-nfo")
	assert.False(t, preserveNFO, "preserve-nfo should default to false")

	showMergeStats, _ := cmd.Flags().GetBool("show-merge-stats")
	assert.False(t, showMergeStats, "show-merge-stats should default to false")

	preset, _ := cmd.Flags().GetString("preset")
	assert.Empty(t, preset, "preset should default to empty")

	scalarStrategy, _ := cmd.Flags().GetString("scalar-strategy")
	assert.Equal(t, "prefer-nfo", scalarStrategy, "scalar-strategy should default to prefer-nfo")

	arrayStrategy, _ := cmd.Flags().GetString("array-strategy")
	assert.Equal(t, "merge", arrayStrategy, "array-strategy should default to merge")
}

// TestFlags_ShortForms verifies short flag forms work
func TestFlags_ShortForms(t *testing.T) {
	cmd := update.NewCommand()

	// Verify short forms are registered
	assert.NotNil(t, cmd.Flags().ShorthandLookup("n"), "should have -n for dry-run")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("p"), "should have -p for scrapers")
}

// TestFlags_MergeStrategies verifies merge strategy flags
func TestFlags_MergeStrategies(t *testing.T) {
	cmd := update.NewCommand()

	// Verify scalar strategy flag
	scalarStrategy, err := cmd.Flags().GetString("scalar-strategy")
	assert.NoError(t, err)
	assert.Equal(t, "prefer-nfo", scalarStrategy)

	// Verify array strategy flag
	arrayStrategy, err := cmd.Flags().GetString("array-strategy")
	assert.NoError(t, err)
	assert.Equal(t, "merge", arrayStrategy)

	// Verify preset flag exists
	preset, err := cmd.Flags().GetString("preset")
	assert.NoError(t, err)
	assert.Empty(t, preset)
}

// TestFlags_MutuallyExclusiveOptions verifies conflicting flags are available
func TestFlags_MutuallyExclusiveOptions(t *testing.T) {
	cmd := update.NewCommand()

	// force-overwrite and preserve-nfo are mutually exclusive in behavior
	// but both flags should exist
	assert.NotNil(t, cmd.Flags().Lookup("force-overwrite"))
	assert.NotNil(t, cmd.Flags().Lookup("preserve-nfo"))

	// Both should default to false
	forceOverwrite, _ := cmd.Flags().GetBool("force-overwrite")
	preserveNFO, _ := cmd.Flags().GetBool("preserve-nfo")
	assert.False(t, forceOverwrite)
	assert.False(t, preserveNFO)
}

// Note: Full integration tests for update command including:
// - update_merge_test.go (preset application, strategy parsing, merge behavior)
// - construct_nfo_path_test.go (NFO path construction and sanitization)
// remain in cmd/cli/ until the complete migration to the new command structure is finished.
// These smoke tests verify the command structure and flags are correct.
