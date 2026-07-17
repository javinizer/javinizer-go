package main

import (
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipConfigInit_ScrapeJSONOutput(t *testing.T) {
	cmd := scrape.NewCommand()
	require.NoError(t, cmd.Flags().Set("output", "json"))
	assert.True(t, shouldSkipConfigInit(cmd))
}

func TestShouldSkipConfigInit_ScrapeDefaultOutput(t *testing.T) {
	cmd := scrape.NewCommand()
	assert.False(t, shouldSkipConfigInit(cmd))
}

func TestShouldSkipConfigInit_ScrapeTextOutput(t *testing.T) {
	cmd := scrape.NewCommand()
	require.NoError(t, cmd.Flags().Set("output", "text"))
	assert.False(t, shouldSkipConfigInit(cmd))
}
