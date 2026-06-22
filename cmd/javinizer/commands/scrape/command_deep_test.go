package scrape_test

import (
	"testing"

	scrapecmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructure_Deep(t *testing.T) {
	cmd := scrapecmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "scrape [id]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE, "RunE should be set")
	assert.NotNil(t, cmd.Args, "Args validation should be set")

	// Verify flags
	assert.NotNil(t, cmd.Flags().Lookup("scrapers"), "scrapers flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("force"), "force flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("scrape-actress"), "scrape-actress flag should be registered")
	assert.NotNil(t, cmd.Flags().Lookup("no-scrape-actress"), "no-scrape-actress flag should be registered")

	// Verify flag defaults
	scrapersDefault, _ := cmd.Flags().GetStringSlice("scrapers")
	assert.Empty(t, scrapersDefault, "scrapers should default to empty")

	forceDefault, _ := cmd.Flags().GetBool("force")
	assert.False(t, forceDefault, "force should default to false")
}
