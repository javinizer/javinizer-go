package history

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// TestTruncatePathV3 tests truncatePath with various inputs
func TestTruncatePathV3(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		maxLen int
		want   string
	}{
		{"short path", "/short/path", 47, "/short/path"},
		{"long path", "/very/long/path/that/exceeds/the/maximum/length/allowed/for/display", 30, ".../length/allowed/for/display"},
		{"exact length", "/exact", 6, "/exact"},
		{"empty path", "", 10, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncatePath(tt.path, tt.maxLen)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestPercentageV3_Additional tests percentage with additional edge cases
func TestPercentageV3_Additional(t *testing.T) {
	assert.InDelta(t, 100.0, percentage(100, 100), 0.001)
	assert.InDelta(t, 33.333333, percentage(1, 3), 0.001)
	assert.InDelta(t, 0.0, percentage(0, 0), 0.001)
	assert.InDelta(t, 0.0, percentage(0, 100), 0.001)
}

// TestNewRevertCommandV3 tests NewRevertCommand properties
func TestNewRevertCommandV3(t *testing.T) {
	cmd := NewRevertCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "revert [batch-id]", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("scrape-ids"))
}

// TestNewCommandV3_Subcommands tests that the history command has expected subcommands
func TestNewCommandV3_Subcommands(t *testing.T) {
	cmd := NewCommand()
	assert.NotNil(t, cmd)

	subNames := map[string]bool{}
	for _, sub := range cmd.Commands() {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["list"], "should have list subcommand")
	assert.True(t, subNames["stats"], "should have stats subcommand")
	assert.True(t, subNames["movie"], "should have movie subcommand")
	assert.True(t, subNames["clean"], "should have clean subcommand")
	assert.True(t, subNames["revert"], "should have revert subcommand")
}

// TestNewCommandV3_ListFlags tests list subcommand flags
func TestNewCommandV3_ListFlags(t *testing.T) {
	cmd := NewCommand()
	var listCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "list" {
			listCmd = sub
			break
		}
	}
	assert.NotNil(t, listCmd)
	assert.NotNil(t, listCmd.Flags().Lookup("limit"))
	assert.NotNil(t, listCmd.Flags().Lookup("operation"))
	assert.NotNil(t, listCmd.Flags().Lookup("status"))
	assert.NotNil(t, listCmd.Flags().Lookup("batch"))
}

// TestNewCommandV3_CleanFlags tests clean subcommand flags
func TestNewCommandV3_CleanFlags(t *testing.T) {
	cmd := NewCommand()
	var cleanCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "clean" {
			cleanCmd = sub
			break
		}
	}
	assert.NotNil(t, cleanCmd)
	assert.NotNil(t, cleanCmd.Flags().Lookup("days"))
}
